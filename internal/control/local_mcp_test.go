package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/engine"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/mcp"
	"github.com/GuardaChuva22/avora/internal/store"
	"github.com/GuardaChuva22/avora/internal/vault"
)

func TestChatCreatesAndUsesLocalMCP(t *testing.T) {
	const (
		name       = "Saudação"
		source     = "# Código fornecido pelo modelo"
		pluginTool = "local_sauda_o_hello"
	)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		last := req.Messages[len(req.Messages)-1]
		delta := map[string]any{}
		if last.Role == "tool" && strings.Contains(last.Content, "Olá, Avora") {
			delta["content"] = "O MCP foi criado e respondeu Olá, Avora."
		} else {
			toolName, args := createMCPTool, map[string]any{"nome": name, "linguagem": "python3", "codigo": source}
			if last.Role == "tool" {
				toolName, args = pluginTool, map[string]any{"who": "Avora"}
			}
			if !slices.ContainsFunc(req.Tools, func(tool llm.Tool) bool { return tool.Function.Name == toolName }) {
				t.Errorf("%s não foi oferecida ao modelo", toolName)
			}
			raw, _ := json.Marshal(args)
			delta["tool_calls"] = []map[string]any{{
				"index": 0, "id": "call_" + toolName, "type": "function",
				"function": map[string]string{"name": toolName, "arguments": string(raw)},
			}}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		msg, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": delta}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", msg)
	}))
	defer model.Close()
	provider, err := llm.NewProvider("MCP test", model.URL, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	s := chatServer(t, engine.NewNative(provider, "fake-model"))
	workspace := t.TempDir()
	if err := s.SetWorkspace(workspace); err != nil {
		t.Fatal(err)
	}
	s.SetConnectors(connect.NewRegistry(), vault.Random())
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)
	conv := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, conv.ID, "Crie um MCP de saudação e use para dizer olá à Avora.")
	ap := waitApproval(t, s)
	if ap.Action != createMCPTool {
		t.Fatalf("approval: %+v", ap)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})

	job := waitMCPClaim(t, s, vps.Token)
	var spec mcp.PluginJob
	if err := json.Unmarshal([]byte(job.Agent.Prompt), &spec); err != nil {
		t.Fatal(err)
	}
	if job.Action != store.ActionMCPRun || job.Agent.Engine != "mcp" || job.Agent.Mode != "offline" ||
		spec.Mode != "list" || spec.Runtime != "python3" {
		t.Fatalf("unexpected MCP job: %+v %+v", job, spec)
	}
	file, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(job.Agent.Workdir), spec.Entry))
	if err != nil || string(file) != source {
		t.Fatalf("generated source = %q, error = %v", file, err)
	}
	completeMCPJob(t, s, vps.Token, job.ID, mcp.PluginResult{
		Server: mcp.ServerInfo{Name: "hello"},
		Tools:  []mcp.Tool{{Name: "hello", Description: "Greets someone"}},
	})
	callJob := waitMCPClaim(t, s, vps.Token)
	spec = mcp.PluginJob{}
	if err := json.Unmarshal([]byte(callJob.Agent.Prompt), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Mode != "call" || spec.Name != "hello" || string(spec.Arguments) != `{"who":"Avora"}` ||
		callJob.Agent.Workdir != job.Agent.Workdir {
		t.Fatalf("unexpected MCP call: %+v %+v", callJob, spec)
	}
	completeMCPJob(t, s, vps.Token, callJob.ID, mcp.PluginResult{Result: mcp.CallResult{
		Content: []mcp.Content{{Type: "text", Text: "Olá, Avora"}},
	}})
	if response := <-answer; !strings.Contains(response.Assistant.Content, "Olá, Avora") {
		t.Fatalf("response: %+v", response)
	}
	s.SetConnectors(connect.NewRegistry(), s.vault)
	if err := s.RestoreConnectors(t.Context()); err != nil {
		t.Fatal(err)
	}
	list := decode[[]ConnectorView](t, request(t, s, "GET", "/v1/connectors", adminTestToken, nil))
	if len(list) != 1 || list[0].Name != name || len(list[0].Connections) != 1 ||
		list[0].Connections[0].Status != "ok" || len(list[0].Tools) != 1 || list[0].Tools[0].Name != pluginTool {
		t.Fatalf("restored plugins: %+v", list)
	}
	if w := request(t, s, "DELETE", "/v1/connectors/"+list[0].Kind, adminTestToken, nil); w.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(job.Agent.Workdir))); !os.IsNotExist(err) {
		t.Fatalf("plugin files were not removed: %v", err)
	}
}

func TestChatInstallsGitHubMCPOnlyAfterApprovalAndCheck(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Atlas": {call(installMCPTool, map[string]any{
		"nome": "Polymarket", "repositorio": "https://github.com/guangxiangdebizi/PolyMarket-MCP.git", "entrada": "build/index.js",
	})}}}
	s := chatServer(t, e)
	if err := s.SetWorkspace(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	s.SetConnectors(connect.NewRegistry(), vault.Random())
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)
	conv := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, conv.ID, "Instale o MCP do Polymarket do GitHub.")
	ap := waitApproval(t, s)
	if ap.Action != "instalar_mcp.codigo" || !strings.Contains(ap.Summary, "https://github.com/guangxiangdebizi/polymarket-mcp") {
		t.Fatalf("approval: %+v", ap)
	}
	if w := request(t, s, "POST", "/v1/runtime/claim", vps.Token, ClaimRequest{MCP: true}); w.Code != http.StatusNoContent {
		t.Fatalf("job queued before approval: %d %s", w.Code, w.Body)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	job := waitMCPClaim(t, s, vps.Token)
	var spec mcp.PluginJob
	if err := json.Unmarshal([]byte(job.Agent.Prompt), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Mode != "install" || spec.Runtime != "node" || spec.Entry != "build/index.js" || job.Agent.Mode != "net" {
		t.Fatalf("unexpected install job: %+v %+v", job, spec)
	}
	if len(s.connectors.All()) != 0 {
		t.Fatal("plugin was visible before the check")
	}
	completeMCPJob(t, s, vps.Token, job.ID, mcp.PluginResult{
		Server: mcp.ServerInfo{Name: "polymarket"},
		Tools:  []mcp.Tool{{Name: "get_markets"}},
	})
	<-answer
	list := decode[[]ConnectorView](t, request(t, s, "GET", "/v1/connectors", adminTestToken, nil))
	if len(list) != 1 || len(list[0].Connections) != 1 || list[0].Name != "Polymarket" ||
		list[0].Connections[0].Status != "ok" || len(list[0].Tools) != 1 {
		t.Fatalf("installed plugins: %+v", list)
	}
}

func TestAdminInstallsGitHubMCP(t *testing.T) {
	s := chatServer(t, nil)
	if err := s.SetWorkspace(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	s.SetConnectors(connect.NewRegistry(), vault.Random())
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)
	const endpoint = "/v1/connectors/mcp"
	req := AddMCPRequest{Name: "Polymarket", Repository: "https://github.com/guangxiangdebizi/polymarket-mcp", Entry: "build/index.js"}
	if w := request(t, s, "POST", endpoint, "", req); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated install: %d", w.Code)
	}
	if w := request(t, s, "POST", endpoint, adminTestToken, AddMCPRequest{URL: "https://www.npmjs.com/package/example"}); w.Code != http.StatusBadRequest {
		t.Fatalf("npm listing accepted as a remote MCP: %d", w.Code)
	}
	answer := make(chan *httptest.ResponseRecorder, 1)
	go func() { answer <- request(t, s, "POST", endpoint, adminTestToken, req) }()
	job := waitMCPClaim(t, s, vps.Token)
	completeMCPJob(t, s, vps.Token, job.ID, mcp.PluginResult{
		Server: mcp.ServerInfo{Name: "polymarket"},
		Tools:  []mcp.Tool{{Name: "get_markets"}},
	})
	w := <-answer
	if w.Code != http.StatusCreated {
		t.Fatalf("install: %d %s", w.Code, w.Body)
	}
	app := decode[ConnectorView](t, w)
	if app.Name != req.Name || len(app.Tools) != 1 || len(app.Connections) != 1 || app.Connections[0].Status != "ok" {
		t.Fatalf("installed app: %+v", app)
	}
	if w := request(t, s, "POST", endpoint, adminTestToken, req); w.Code != http.StatusOK {
		t.Fatalf("reinstall: %d %s", w.Code, w.Body)
	}
}

func waitMCPClaim(t *testing.T, s *Server, token string) Job {
	t.Helper()
	for range 400 {
		w := request(t, s, "POST", "/v1/runtime/claim", token, ClaimRequest{MCP: true})
		if w.Code == http.StatusOK {
			return decode[Job](t, w)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no MCP job to claim")
	return Job{}
}

func completeMCPJob(t *testing.T, s *Server, token, id string, result mcp.PluginResult) {
	t.Helper()
	raw, _ := json.Marshal(result)
	w := request(t, s, "POST", "/v1/runtime/jobs/"+id+"/complete", token, JobResult{Success: true, Output: string(raw)})
	if w.Code != http.StatusOK {
		t.Fatalf("complete MCP job: %d %s", w.Code, w.Body)
	}
}
