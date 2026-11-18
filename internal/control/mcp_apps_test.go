package control

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
	"github.com/GuardaChuva22/avora/internal/vault"
)

// fakeMCP is an MCP server with the whole one-click login: protected
// resource metadata, authorization server metadata, dynamic registration,
// PKCE, refresh, sessions and answers in SSE.
type fakeMCP struct {
	mu        sync.Mutex
	srv       *httptest.Server
	clients   map[string]string // client_id -> redirect
	challenge map[string]string // code -> challenge
	valid     map[string]bool   // access tokens accepted
	refreshes int
	calls     []string
	sessions  int
	open      bool
}

func newFakeMCP(t *testing.T) *fakeMCP {
	f := &fakeMCP{clients: map[string]string{}, challenge: map[string]string{}, valid: map[string]bool{}}
	f.srv = httptest.NewTLSServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeMCP) url() string { return f.srv.URL + "/mcp" }

func (f *fakeMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	base := f.srv.URL
	switch r.URL.Path {
	case "/.well-known/oauth-protected-resource/mcp":
		fmt.Fprintf(w, `{"resource":%q,"authorization_servers":[%q],"scopes_supported":["read","write"]}`, base+"/mcp", base)
	case "/.well-known/oauth-authorization-server":
		fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"registration_endpoint":%q}`,
			base, base+"/authorize", base+"/token", base+"/register")
	case "/register":
		var req struct {
			RedirectURIs []string `json:"redirect_uris"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id := fmt.Sprintf("client-%d", len(f.clients)+1)
		f.clients[id] = req.RedirectURIs[0]
		w.WriteHeader(201)
		fmt.Fprintf(w, `{"client_id":%q}`, id)
	case "/token":
		_ = r.ParseForm()
		if r.Form.Get("resource") != base+"/mcp" || f.clients[r.Form.Get("client_id")] == "" {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"invalid_request"}`)
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if f.challenge[r.Form.Get("code")] != base64.RawURLEncoding.EncodeToString(sum[:]) ||
				f.clients[r.Form.Get("client_id")] != r.Form.Get("redirect_uri") {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":"invalid_grant"}`)
				return
			}
			f.valid["at1"] = true
			fmt.Fprint(w, `{"access_token":"at1","refresh_token":"rt1","expires_in":3600,"token_type":"Bearer"}`)
		case "refresh_token":
			if r.Form.Get("refresh_token") != "rt1" {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":"invalid_grant"}`)
				return
			}
			f.refreshes++
			f.valid["at2"] = true
			fmt.Fprint(w, `{"access_token":"at2","expires_in":3600}`)
		}
	case "/mcp":
		if !f.open && !f.valid[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+base+`/.well-known/oauth-protected-resource/mcp"`)
			w.WriteHeader(401)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		_ = json.Unmarshal(body, &msg)
		if msg.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != fmt.Sprint(f.sessions) {
			w.WriteHeader(404)
			return
		}
		var result string
		switch msg.Method {
		case "initialize":
			f.sessions++
			w.Header().Set("Mcp-Session-Id", fmt.Sprint(f.sessions))
			result = `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fake","title":"Fake App"}}`
		case "notifications/initialized":
			w.WriteHeader(202)
			return
		case "tools/list":
			result = `{"tools":[
				{"name":"create_task","description":"Creates a task.","inputSchema":{"type":"object","properties":{"name":{"type":"string"}}}},
				{"name":"send_message","description":"Sends a chat message.","inputSchema":{"type":"object","properties":{"text":{"type":"string"}}}},
				{"name":"get_task","description":"Reads a task.","annotations":{"readOnlyHint":true}},
				{"name":"delete_task","description":"Deletes a task."}]}`
		case "tools/call":
			f.calls = append(f.calls, msg.Params.Name+" "+string(msg.Params.Arguments))
			result = fmt.Sprintf(`{"content":[{"type":"text","text":"ok: %s"}]}`, msg.Params.Name)
		}
		// Answer as a stream, after a notification, the way many servers do.
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n")
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, strings.Join(strings.Fields(result), " "))
	default:
		w.WriteHeader(404)
	}
}

// login runs the browser part: the owner consents and the app hands the
// code back.
func (f *fakeMCP) login(t *testing.T, s *Server, kind string) ConnectorView {
	t.Helper()
	redirect := "http://127.0.0.1:53111/callback"
	w := request(t, s, "POST", "/v1/connectors/"+kind+"/oauth/start", adminTestToken, OAuthStartRequest{RedirectURI: redirect})
	if w.Code != 200 {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	start := decode[OAuthStartResponse](t, w)
	u, _ := url.Parse(start.URL)
	q := u.Query()
	if q.Get("redirect_uri") != redirect || q.Get("code_challenge_method") != "S256" || q.Get("resource") != f.url() || q.Get("scope") != "read write" {
		t.Fatalf("auth url: %s", start.URL)
	}
	f.mu.Lock()
	f.challenge["code-1"] = q.Get("code_challenge")
	f.mu.Unlock()
	w = request(t, s, "POST", "/v1/connectors/"+kind+"/oauth/finish", adminTestToken, OAuthFinishRequest{State: start.State, Code: "code-1"})
	if w.Code != 200 {
		t.Fatalf("finish: %d %s", w.Code, w.Body)
	}
	return decode[ConnectorView](t, w)
}

func mcpServer(t *testing.T, e ChatEngine, f *fakeMCP) (*Server, *connect.MCPApp) {
	t.Helper()
	old := mcpHTTP
	mcpHTTP = f.srv.Client()
	t.Cleanup(func() { mcpHTTP = old })
	app := &connect.MCPApp{Kind: "fake", Name: "Fake", URL: f.url(), HTTP: f.srv.Client(), Category: "Teste"}
	s := chatServer(t, e)
	s.SetConnectors(connect.NewRegistry(app), vault.Random())
	return s, app
}

func TestMCPAppOneClick(t *testing.T) {
	f := newFakeMCP(t)
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	s, app := mcpServer(t, e, f)

	v := f.login(t, s, "fake")
	if v.Connection == nil || v.Connection.Status != "ok" || v.Connection.Account != "Fake App · 4 ferramentas" || len(v.Tools) != 4 {
		t.Fatalf("connected: %+v %+v", v.Connection, v.Tools)
	}
	locked := map[string]bool{}
	for _, tl := range v.Tools {
		locked[tl.Name] = tl.Locked
	}
	if locked["fake_create_task"] || locked["fake_get_task"] || !locked["fake_send_message"] || !locked["fake_delete_task"] {
		t.Fatalf("risks: %v", locked)
	}
	body := request(t, s, "GET", "/v1/connectors", adminTestToken, nil).Body.String() +
		request(t, s, "GET", "/v1/events?limit=200", adminTestToken, nil).Body.String()
	if strings.Contains(body, "at1") || strings.Contains(body, "rt1") {
		t.Fatal("token leaked")
	}

	c := agentConversation(t, s, "cora")
	e.calls["Cora"] = []llm.ToolCall{call("fake_create_task", map[string]any{"name": "Orçamento"})}
	r := send(t, s, c.ID, "cria")
	if !slices.Contains(e.tools, "fake_send_message") || !strings.Contains(r.Assistant.Content, "ok: create_task") {
		t.Fatalf("create: %q %v", r.Assistant.Content, e.tools)
	}

	// Sending waits for the owner, with the arguments on the card.
	e.calls["Cora"] = []llm.ToolCall{call("fake_send_message", map[string]any{"text": "Olá, cliente"})}
	answer := sendAsync(t, s, c.ID, "manda")
	ap := waitApproval(t, s)
	if !strings.Contains(ap.Summary, "Fake · send_message") || !strings.Contains(ap.Summary, "Olá, cliente") {
		t.Fatalf("approval: %s", ap.Summary)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	<-answer

	// The token expires on the app's side: Avora refreshes it and goes on.
	f.mu.Lock()
	delete(f.valid, "at1")
	f.mu.Unlock()
	e.calls["Cora"] = []llm.ToolCall{call("fake_get_task", map[string]any{})}
	r = send(t, s, c.ID, "lê")
	if !strings.Contains(r.Assistant.Content, "ok: get_task") || f.refreshes != 1 {
		t.Fatalf("refresh: %q %d", r.Assistant.Content, f.refreshes)
	}
	f.mu.Lock()
	calls := strings.Join(f.calls, "|")
	f.mu.Unlock()
	if calls != `create_task {"name":"Orçamento"}|send_message {"text":"Olá, cliente"}|get_task {}` {
		t.Fatalf("calls: %s", calls)
	}

	// After a restart the tools come back from the database.
	fresh := &connect.MCPApp{Kind: "fake", Name: "Fake", URL: f.url(), HTTP: f.srv.Client()}
	s.SetConnectors(connect.NewRegistry(fresh), s.vault)
	if err := s.RestoreConnectors(t.Context()); err != nil || len(fresh.Tools()) != 4 {
		t.Fatalf("restore: %v %d", err, len(fresh.Tools()))
	}
	_ = app
}

func TestAddMCPByAddress(t *testing.T) {
	f := newFakeMCP(t)
	s, _ := mcpServer(t, nil, f)
	// The same address as a catalog app opens that app.
	if v := decode[ConnectorView](t, request(t, s, "POST", "/v1/connectors/mcp", adminTestToken, AddMCPRequest{URL: f.url()})); v.Kind != "fake" {
		t.Fatalf("catalog address: %s", v.Kind)
	}
	s.SetConnectors(connect.NewRegistry(), s.vault)
	if w := request(t, s, "POST", "/v1/connectors/mcp", adminTestToken, AddMCPRequest{URL: "http://insecure.example/mcp"}); w.Code != 400 {
		t.Fatalf("http: %d", w.Code)
	}
	w := request(t, s, "POST", "/v1/connectors/mcp", adminTestToken, AddMCPRequest{URL: f.url(), Name: "Meu app"})
	if w.Code != 201 {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	v := decode[ConnectorView](t, w)
	if v.Kind != "mcp-127" || v.Auth != "oauth" || !v.Custom || v.Name != "Meu app" {
		t.Fatalf("added: %+v", v.Info)
	}
	if len(v.Connections) != 0 || v.Connection != nil {
		t.Fatalf("pending registration shown as a connected account: %+v", v.Connections)
	}
	pending, err := s.store.GetConnection(t.Context(), v.Kind)
	if err != nil || pending.Status != "pending" || usableBy(pending, "cora") {
		t.Fatalf("pending registration: %+v, %v", pending, err)
	}

	// A restart before login preserves the plugin and the ability to connect.
	s.SetConnectors(connect.NewRegistry(), s.vault)
	if err := s.RestoreConnectors(t.Context()); err != nil {
		t.Fatal(err)
	}
	w = request(t, s, "POST", "/v1/connectors/mcp", adminTestToken, AddMCPRequest{URL: f.url() + "#fragment"})
	if w.Code != 200 {
		t.Fatalf("repeat add: %d %s", w.Code, w.Body)
	}
	if again := decode[ConnectorView](t, w); again.Kind != v.Kind || again.Name != v.Name || again.Connection != nil {
		t.Fatalf("duplicate registration: %+v", again)
	}
	v = f.login(t, s, v.Kind)
	if len(v.Connections) != 1 || v.Connection == nil || v.Connection.ID != pending.ID || v.Connection.Status != "ok" {
		t.Fatalf("login did not activate pending registration: %+v", v.Connections)
	}

	// A restart brings the added app back with its tools.
	s.SetConnectors(connect.NewRegistry(), s.vault)
	if err := s.RestoreConnectors(t.Context()); err != nil {
		t.Fatal(err)
	}
	con, ok := s.connectors.Get("mcp-127")
	if !ok || len(con.Tools()) != 4 || con.Info().URL != f.url() {
		t.Fatalf("restored: %v", ok)
	}
	if w := request(t, s, "DELETE", "/v1/connectors/mcp-127", adminTestToken, nil); w.Code != 204 {
		t.Fatalf("remove: %d", w.Code)
	}
	if _, ok := s.connectors.Get("mcp-127"); ok {
		t.Fatal("custom app still listed")
	}
	if _, err := s.store.GetConnection(t.Context(), "mcp-127"); err != store.ErrNotFound {
		t.Fatal("connection kept")
	}

	if w := request(t, s, "POST", "/v1/connectors/mcp", adminTestToken, AddMCPRequest{URL: f.url()}); w.Code != 201 {
		t.Fatalf("re-add: %d %s", w.Code, w.Body)
	}
	if w := request(t, s, "DELETE", "/v1/connectors/mcp-127", adminTestToken, nil); w.Code != 204 {
		t.Fatalf("remove pending plugin: %d %s", w.Code, w.Body)
	}
	if _, err := s.store.GetConnection(t.Context(), "mcp-127"); err != store.ErrNotFound {
		t.Fatal("pending registration kept")
	}
	if _, ok := s.connectors.Get("mcp-127"); ok {
		t.Fatal("pending plugin still listed")
	}
}

func TestInstallOpenMCP(t *testing.T) {
	f := newFakeMCP(t)
	f.mu.Lock()
	f.open = true
	f.mu.Unlock()
	s, _ := mcpServer(t, nil, f)
	s.SetConnectors(connect.NewRegistry(), s.vault)
	ctx := t.Context()

	first, created, err := s.installRemoteMCP(ctx, AddMCPRequest{URL: f.url()}, "agent:cora")
	if err != nil || !created || len(first.Tools()) != 4 {
		t.Fatalf("open MCP install: created=%v, err=%v", created, err)
	}
	id := first.Info().Kind
	conn, err := s.store.GetConnection(ctx, id)
	if err != nil || conn.Status != "ok" || !usableBy(conn, "cora") {
		t.Fatalf("connection: %+v, %v", conn, err)
	}
	events := request(t, s, "GET", "/v1/events?limit=200", adminTestToken, nil).Body.String()
	if !strings.Contains(events, store.EventConnectionSaved) || !strings.Contains(events, "agent:cora") {
		t.Fatalf("connection audit event missing installer: %s", events)
	}

	// Two servers at different addresses on one host need distinct tool names.
	second, created, err := s.installRemoteMCP(ctx, AddMCPRequest{URL: f.url() + "?workspace=other"}, "agent:cora")
	if err != nil || !created || second.Info().Kind != id+"-2" {
		t.Fatalf("second install: created=%v, err=%v", created, err)
	}
	if _, _, ok := s.connectors.Tool("127_2_get_task"); !ok {
		t.Fatal("tools were not renamed to the installed plugin's prefix")
	}

	if w := request(t, s, "DELETE", "/v1/connectors/"+id, adminTestToken, nil); w.Code != 204 {
		t.Fatalf("remove: %d %s", w.Code, w.Body)
	}
	// A call that started before removal cannot recreate the deleted connection.
	if _, err := s.useConnection(ctx, first, conn, func(context.Context, *connect.Env) (string, error) {
		return "done", nil
	}, "cora", "127_get_task"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.GetConnection(ctx, id); err != store.ErrNotFound {
		t.Fatalf("removed connection returned: %v", err)
	}
}
