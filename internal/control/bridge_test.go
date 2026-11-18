package control

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/llm"
)

func rpc(t *testing.T, s *Server, key string, id int, method string, params any) (int, map[string]any) {
	t.Helper()
	w := request(t, s, "POST", "/v1/bridge/mcp", key, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestBridgeGivesDelegatedRunThePlugins(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Atlas": {
		call("claude_code", map[string]any{"tarefa": "Organize minhas tarefas do ClickUp"}),
	}}}
	s, f := pluginServer(t, e)
	connectClickUp(t, s)
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "organiza o clickup")

	job := waitClaim(t, s, pc.Token)
	key := job.Agent.Bridge
	if !strings.HasPrefix(key, "avb_") || !strings.Contains(job.Agent.Prompt, `servidor MCP "avora"`) || !strings.Contains(job.Agent.Prompt, "ClickUp (Gui") {
		t.Fatalf("job: bridge=%q prompt=%s", key, job.Agent.Prompt)
	}

	if code, _ := rpc(t, s, "avb_wrong", 1, "initialize", nil); code != 401 {
		t.Fatalf("wrong key: %d", code)
	}
	if code, _ := rpc(t, s, adminTestToken, 1, "initialize", nil); code != 401 {
		t.Fatalf("admin token on the bridge: %d", code)
	}
	if code, out := rpc(t, s, key, 1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}); code != 200 || out["result"] == nil {
		t.Fatalf("initialize: %d %v", code, out)
	}
	if w := request(t, s, "POST", "/v1/bridge/mcp", key, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); w.Code != 202 {
		t.Fatalf("notification: %d", w.Code)
	}
	_, out := rpc(t, s, key, 2, "tools/list", nil)
	list, _ := json.Marshal(out["result"])
	if !strings.Contains(string(list), `"name":"clickup_criar_tarefa"`) || !strings.Contains(string(list), `"readOnlyHint":true`) ||
		strings.Contains(string(list), "claude_code") || strings.Contains(string(list), "SECRET") {
		t.Fatalf("tools: %s", list)
	}

	// Writing in the owner's space runs at once, with the agent's account.
	_, out = rpc(t, s, key, 3, "tools/call", map[string]any{"name": "clickup_criar_tarefa", "arguments": map[string]any{"lista_id": "l1", "nome": "Pela ponte"}})
	res, _ := json.Marshal(out["result"])
	if len(f.created) != 1 || !strings.Contains(string(res), "Tarefa criada") || strings.Contains(string(res), `"isError":true`) {
		t.Fatalf("call: %s %v", res, f.created)
	}
	// Something only Avora's own tools do is not on the bridge.
	if _, out = rpc(t, s, key, 4, "tools/call", map[string]any{"name": "criar_rotina", "arguments": map[string]any{}}); out["error"] == nil {
		t.Fatalf("native tool through the bridge: %v", out)
	}

	// A locked action waits for the owner, then runs.
	done := make(chan map[string]any, 1)
	go func() {
		_, out := rpc(t, s, key, 5, "tools/call", map[string]any{"name": "clickup_comentar", "arguments": map[string]any{"id": "t1", "comentario": "Feito."}})
		done <- out
	}()
	ap := waitApproval(t, s)
	if ap.Action != "clickup_comentar" || ap.AgentID != "atlas" || len(f.comments) != 0 {
		t.Fatalf("approval: %+v %v", ap, f.comments)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	if out := <-done; len(f.comments) != 1 {
		t.Fatalf("after approval: %v %v", out, f.comments)
	}

	// The key dies with the run.
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token, JobResult{Success: true, Output: "Organizei."})
	<-answer
	if code, _ := rpc(t, s, key, 6, "tools/list", nil); code != 401 {
		t.Fatalf("key after the run: %d", code)
	}
}

func TestDelegationWithoutPluginsHasNoBridge(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Atlas": {
		call("claude_code", map[string]any{"tarefa": "Leia o README"}),
	}}}
	s, _ := pluginServer(t, e)
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "lê o readme")
	job := waitClaim(t, s, pc.Token)
	if job.Agent.Bridge != "" || strings.Contains(job.Agent.Prompt, "avora") {
		t.Fatalf("bridge without plugins: %+v", job.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token, JobResult{Success: true, Output: "ok"})
	<-answer
}
