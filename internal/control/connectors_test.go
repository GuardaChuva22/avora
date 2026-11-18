package control

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
	"github.com/GuardaChuva22/avora/internal/vault"
)

const (
	fakeClickUpToken  = "pk_123_SECRET"
	fakeClickUpToken2 = "pk_456_SECRET" // a second account (Ana)
)

// fakeClickUp answers like ClickUp and records what it was asked to do.
type fakeClickUp struct {
	mu       sync.Mutex
	revoked  bool
	created  []string
	by       []string // which token created each task
	comments []string
	listed   []string // task names GET /list/l1/task returns (ids are the names)
}

func (f *fakeClickUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	users := map[string]string{
		fakeClickUpToken:  `{"user":{"username":"Gui","email":"gui@example.com"}}`,
		fakeClickUpToken2: `{"user":{"username":"Ana","email":"ana@example.com"}}`,
	}
	token := r.Header.Get("Authorization")
	user, known := users[token]
	if f.revoked || !known {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"err":"Token invalid"}`)
		return
	}
	switch {
	case r.URL.Path == "/user":
		fmt.Fprint(w, user)
	case r.Method == "POST" && r.URL.Path == "/list/l1/task":
		f.created = append(f.created, string(body))
		f.by = append(f.by, token)
		fmt.Fprint(w, `{"id":"t9","name":"Responder Ana","status":{"status":"to do"},"url":"https://app.clickup.com/t/t9"}`)
	case r.Method == "GET" && r.URL.Path == "/list/l1/task":
		tasks := []map[string]any{}
		for _, n := range f.listed {
			tasks = append(tasks, map[string]any{"id": n, "name": "Tarefa " + n, "status": map[string]string{"status": "to do"}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks, "last_page": true})
	case r.Method == "POST" && r.URL.Path == "/task/t1/comment":
		f.comments = append(f.comments, string(body))
		fmt.Fprint(w, `{"id":"c1"}`)
	default:
		w.WriteHeader(404)
	}
}

func send(t *testing.T, s *Server, convID, content string) MessageResponse {
	t.Helper()
	return <-sendAsync(t, s, convID, content)
}

func pluginServer(t *testing.T, e ChatEngine) (*Server, *fakeClickUp) {
	t.Helper()
	f := &fakeClickUp{}
	api := httptest.NewServer(f)
	t.Cleanup(api.Close)
	s := chatServer(t, e)
	s.SetConnectors(connect.NewRegistry(&connect.Google{}, &connect.ClickUp{BaseURL: api.URL}, &connect.Discord{}), vault.Random())
	return s, f
}

func connectClickUp(t *testing.T, s *Server) ConnectorView {
	t.Helper()
	w := request(t, s, "PUT", "/v1/connectors/clickup-token", adminTestToken, ConnectRequest{Token: fakeClickUpToken})
	if w.Code != 200 {
		t.Fatalf("connect: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "SECRET") {
		t.Fatal("token in the answer")
	}
	return decode[ConnectorView](t, w)
}

func TestPluginsCatalogAndConnect(t *testing.T) {
	s, f := pluginServer(t, nil)
	if w := request(t, s, "GET", "/v1/connectors", "", nil); w.Code != 401 {
		t.Fatalf("anonymous catalog: %d", w.Code)
	}
	list := decode[[]ConnectorView](t, request(t, s, "GET", "/v1/connectors", adminTestToken, nil))
	if len(list) != 3 || list[0].Kind != "google" || list[0].Ready || list[0].Connection != nil {
		t.Fatalf("catalog: %+v", list)
	}
	if w := request(t, s, "PUT", "/v1/connectors/clickup-token", adminTestToken, ConnectRequest{Token: "pk_wrong"}); w.Code != 400 || !strings.Contains(w.Body.String(), "recusou") {
		t.Fatalf("wrong token: %d %s", w.Code, w.Body)
	}
	if w := request(t, s, "PUT", "/v1/connectors/google", adminTestToken, ConnectRequest{Token: "x"}); w.Code != 400 {
		t.Fatalf("token for oauth connector: %d", w.Code)
	}
	v := connectClickUp(t, s)
	if v.Connection == nil || v.Connection.Account != "Gui · gui@example.com" || v.Connection.Status != "ok" {
		t.Fatalf("connected: %+v", v.Connection)
	}
	locked := map[string]bool{}
	for _, tl := range v.Tools {
		locked[tl.Name] = tl.Locked
	}
	if !locked["clickup_comentar"] || locked["clickup_criar_tarefa"] {
		t.Fatalf("risks: %v", locked)
	}
	// The credential is sealed in the store and nowhere in the log.
	conn, _ := s.store.GetConnection(t.Context(), "clickup-token")
	if len(conn.Secret) == 0 || strings.Contains(string(conn.Secret), "SECRET") {
		t.Fatal("secret not sealed")
	}
	evs := request(t, s, "GET", "/v1/events?limit=200", adminTestToken, nil).Body.String()
	if strings.Contains(evs, "SECRET") || !strings.Contains(evs, store.EventConnectionSaved) {
		t.Fatalf("events: %s", evs)
	}
	if w := request(t, s, "POST", "/v1/connectors/clickup-token/test", adminTestToken, nil); w.Code != 200 || decode[ConnectorView](t, w).Connection.Status != "ok" {
		t.Fatalf("test: %d", w.Code)
	}
	f.mu.Lock()
	f.revoked = true
	f.mu.Unlock()
	if v := decode[ConnectorView](t, request(t, s, "POST", "/v1/connectors/clickup-token/test", adminTestToken, nil)); v.Connection.Status != "reconnect" {
		t.Fatalf("revoked: %+v", v.Connection)
	}
	if w := request(t, s, "DELETE", "/v1/connectors/clickup-token", adminTestToken, nil); w.Code != 204 {
		t.Fatalf("disconnect: %d", w.Code)
	}
	if _, err := s.store.GetConnection(t.Context(), "clickup-token"); err == nil {
		t.Fatal("still connected")
	}
}

func TestAgentUsesPlugin(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	s, f := pluginServer(t, e)
	c := agentConversation(t, s, "cora")

	// Not connected: no tools, and the prompt says where to connect.
	e.answers = map[string]string{"Cora": "ok"}
	send(t, s, c.ID, "oi")
	if slices.Contains(e.tools, "clickup_criar_tarefa") {
		t.Fatal("tools before connecting")
	}
	delete(e.answers, "Cora")
	connectClickUp(t, s)

	// Writing in the owner's space runs at once.
	e.calls["Cora"] = []llm.ToolCall{call("clickup_criar_tarefa", map[string]any{"lista_id": "l1", "nome": "Responder Ana"})}
	r := send(t, s, c.ID, "cria a tarefa")
	if !slices.Contains(e.tools, "clickup_comentar") || len(f.created) != 1 || !strings.Contains(r.Assistant.Content, "Tarefa criada") {
		t.Fatalf("create: %v %q", f.created, r.Assistant.Content)
	}
	if strings.Contains(r.Assistant.Content, "SECRET") {
		t.Fatal("token reached the answer")
	}

	// Sending something waits for the owner.
	e.calls["Cora"] = []llm.ToolCall{call("clickup_comentar", map[string]any{"id": "t1", "comentario": "Orçamento enviado."})}
	answer := sendAsync(t, s, c.ID, "comenta")
	ap := waitApproval(t, s)
	if ap.Action != "clickup_comentar" || !strings.Contains(ap.Summary, "Orçamento enviado.") {
		t.Fatalf("approval: %+v", ap)
	}
	if len(f.comments) != 0 {
		t.Fatal("commented before the approval")
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	<-answer
	if len(f.comments) != 1 {
		t.Fatalf("comments: %v", f.comments)
	}
	evs := decode[[]store.Event](t, request(t, s, "GET", "/v1/events?limit=200", adminTestToken, nil))
	calls := 0
	for _, ev := range evs {
		if ev.Type == store.EventConnectorCalled {
			calls++
			if strings.Contains(string(ev.Payload), "Orçamento") {
				t.Fatal("content in the audit event")
			}
		}
	}
	if calls != 2 {
		t.Fatalf("connector events: %d", calls)
	}

	// Restricted to another agent: no tools, and a direct call is refused.
	if w := request(t, s, "PUT", "/v1/connectors/clickup-token/agents", adminTestToken, ConnectorAgentsRequest{Agents: []string{"atlas"}}); w.Code != 200 {
		t.Fatalf("agents: %d %s", w.Code, w.Body)
	}
	e.calls["Cora"] = []llm.ToolCall{call("clickup_criar_tarefa", map[string]any{"lista_id": "l1", "nome": "x"})}
	r = send(t, s, c.ID, "de novo")
	if slices.Contains(e.tools, "clickup_criar_tarefa") || !strings.Contains(r.Assistant.Content, "não liberou") || len(f.created) != 1 {
		t.Fatalf("restricted: %q", r.Assistant.Content)
	}
	request(t, s, "PUT", "/v1/connectors/clickup-token/agents", adminTestToken, ConnectorAgentsRequest{Agents: []string{}})

	// A revoked token flips the connection to reconnect and tells the owner.
	f.mu.Lock()
	f.revoked = true
	f.mu.Unlock()
	r = send(t, s, c.ID, "mais uma")
	if !strings.Contains(r.Assistant.Content, "reconect") {
		t.Fatalf("revoked: %q", r.Assistant.Content)
	}
	conn, _ := s.store.GetConnection(t.Context(), "clickup-token")
	if conn.Status != "reconnect" {
		t.Fatalf("status: %s", conn.Status)
	}
	msgs, _ := s.store.Messages(t.Context(), TeamChannelID)
	found := false
	for _, m := range msgs {
		found = found || strings.Contains(m.Content, "ClickUp (token) (") && strings.Contains(m.Content, "perdeu o acesso")
	}
	if !found {
		t.Fatal("owner not told in #time")
	}
	send(t, s, c.ID, "e agora")
	if slices.Contains(e.tools, "clickup_criar_tarefa") {
		t.Fatal("tools offered while reconnect")
	}
}

func TestOAuthStartChecksRedirect(t *testing.T) {
	s, _ := pluginServer(t, nil)
	s.SetConnectors(connect.NewRegistry(&connect.Google{ClientID: "cid", ClientSecret: "cs"}), vault.Random())
	for _, bad := range []string{"https://evil.example/cb", "http://example.com:5000/cb", "http://127.0.0.1/cb"} {
		if w := request(t, s, "POST", "/v1/connectors/google/oauth/start", adminTestToken, OAuthStartRequest{RedirectURI: bad}); w.Code != 400 {
			t.Fatalf("%s: %d", bad, w.Code)
		}
	}
	w := request(t, s, "POST", "/v1/connectors/google/oauth/start", adminTestToken, OAuthStartRequest{RedirectURI: "http://127.0.0.1:53111/cb"})
	if w.Code != 200 {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	var st OAuthStartResponse
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	if !strings.Contains(st.URL, "code_challenge=") || !strings.Contains(st.URL, "state="+st.State) {
		t.Fatalf("url: %s", st.URL)
	}
	if w := request(t, s, "POST", "/v1/connectors/google/oauth/finish", adminTestToken, OAuthFinishRequest{State: "nope", Code: "c"}); w.Code != 400 {
		t.Fatalf("unknown state: %d", w.Code)
	}
}

func TestPluginSeveralAccounts(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	s, f := pluginServer(t, e)
	c := agentConversation(t, s, "cora")
	first := connectClickUp(t, s)

	// The same account again refreshes it instead of adding a copy.
	if v := connectClickUp(t, s); len(v.Connections) != 1 || v.Connections[0].ID != "clickup-token" {
		t.Fatalf("same account twice: %+v", v.Connections)
	}
	w := request(t, s, "PUT", "/v1/connectors/clickup-token", adminTestToken, ConnectRequest{Token: fakeClickUpToken2})
	v := decode[ConnectorView](t, w)
	if w.Code != 200 || len(v.Connections) != 2 || v.Connections[0].ID != first.Connection.ID ||
		!strings.HasPrefix(v.Connections[1].ID, "clickup-token.") || !strings.HasPrefix(v.Connections[1].Account, "Ana") {
		t.Fatalf("second account: %d %+v", w.Code, v.Connections)
	}
	ana := v.Connections[1]

	// Reconnecting one account with the other's login is refused.
	w = request(t, s, "PUT", "/v1/connectors/clickup-token", adminTestToken, ConnectRequest{Token: fakeClickUpToken, Connection: ana.ID})
	if w.Code != 400 || !strings.Contains(w.Body.String(), "Adicionar conta") {
		t.Fatalf("wrong account on reconnect: %d %s", w.Code, w.Body)
	}

	// Each tool takes "conta" listing both; without it the first account works.
	params := ""
	for _, tl := range s.connectorTools(t.Context(), "cora", nil) {
		if tl.Function.Name == "clickup_criar_tarefa" {
			b, _ := json.Marshal(tl.Function.Parameters)
			params = string(b)
		}
	}
	if !strings.Contains(params, `"conta"`) || !strings.Contains(params, "Ana") || !strings.Contains(params, "Gui") {
		t.Fatalf("params: %s", params)
	}
	if block := s.connectionsBlock(t.Context(), "cora"); !strings.Contains(block, "contas:") || !strings.Contains(block, "argumento conta") {
		t.Fatalf("block: %s", block)
	}
	e.calls["Cora"] = []llm.ToolCall{call("clickup_criar_tarefa", map[string]any{"lista_id": "l1", "nome": "Da Ana", "conta": "ana@example.com"})}
	send(t, s, c.ID, "cria na conta da Ana")
	e.calls["Cora"] = []llm.ToolCall{call("clickup_criar_tarefa", map[string]any{"lista_id": "l1", "nome": "Padrão"})}
	send(t, s, c.ID, "cria")
	if len(f.by) != 2 || f.by[0] != fakeClickUpToken2 || f.by[1] != fakeClickUpToken || strings.Contains(f.created[0], "conta") {
		t.Fatalf("accounts used: %v %v", f.by, f.created)
	}
	e.calls["Cora"] = []llm.ToolCall{call("clickup_criar_tarefa", map[string]any{"lista_id": "l1", "nome": "x", "conta": "zé"})}
	if r := send(t, s, c.ID, "cria na do zé"); !strings.Contains(r.Assistant.Content, "não tem a conta") || len(f.by) != 2 {
		t.Fatalf("unknown account: %q", r.Assistant.Content)
	}

	// Only Ana for Cora: one account, no choice to make, and it is Ana's.
	if w := request(t, s, "PUT", "/v1/connectors/clickup-token/agents?id="+first.Connection.ID, adminTestToken, ConnectorAgentsRequest{Agents: []string{"atlas"}}); w.Code != 200 {
		t.Fatalf("agents: %d", w.Code)
	}
	for _, tl := range s.connectorTools(t.Context(), "cora", nil) {
		if b, _ := json.Marshal(tl.Function.Parameters); strings.Contains(string(b), `"conta"`) {
			t.Fatalf("conta offered with one usable account: %s", b)
		}
	}
	e.calls["Cora"] = []llm.ToolCall{call("clickup_criar_tarefa", map[string]any{"lista_id": "l1", "nome": "Só Ana"})}
	send(t, s, c.ID, "cria")
	if len(f.by) != 3 || f.by[2] != fakeClickUpToken2 {
		t.Fatalf("restricted: %v", f.by)
	}

	// Each account is tested and disconnected on its own.
	if v := decode[ConnectorView](t, request(t, s, "POST", "/v1/connectors/clickup-token/test?id="+ana.ID, adminTestToken, nil)); len(v.Connections) != 2 {
		t.Fatalf("test: %+v", v.Connections)
	}
	if w := request(t, s, "DELETE", "/v1/connectors/clickup-token?id="+ana.ID, adminTestToken, nil); w.Code != 204 {
		t.Fatalf("disconnect Ana: %d", w.Code)
	}
	if w := request(t, s, "DELETE", "/v1/connectors/google?id=clickup-token", adminTestToken, nil); w.Code != 404 {
		t.Fatalf("another app's account: %d", w.Code)
	}
	list := s.accounts(t.Context(), "clickup-token")
	if len(list) != 1 || list[0].ID != first.Connection.ID {
		t.Fatalf("left: %+v", list)
	}
}
