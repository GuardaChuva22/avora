package control

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func loginView(t *testing.T, s *Server, id string) LoginView {
	t.Helper()
	var v LoginView
	for i := 0; i < 200; i++ {
		w := request(t, s, "GET", "/v1/models/providers/"+id+"/login", adminTestToken, nil)
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		if v.State != "starting" {
			return v
		}
		time.Sleep(5 * time.Millisecond)
	}
	return v
}

func TestExtraClaudeAccountLogsInOnTheNode(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Atlas": {
		call("claude_code", map[string]any{"tarefa": "Revise o projeto", "conta": "trabalho"}),
	}}}
	s, _ := pluginServer(t, e)
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)

	w := request(t, s, "POST", "/v1/models/providers", adminTestToken, ProviderRequest{Kind: "claude", Name: "Trabalho"})
	var acc ProviderView
	_ = json.Unmarshal(w.Body.Bytes(), &acc)
	if w.Code != 200 || acc.Status != "login" || !strings.HasPrefix(acc.ID, "claude.") || len(acc.ID) != len("claude.")+6 {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	job := waitClaim(t, s, vps.Token)
	if job.Action != store.ActionAgentLogin || job.Agent.Account != acc.ID || job.Agent.Engine != "claude" {
		t.Fatalf("login job: %+v %+v", job, job.Agent)
	}
	// The node relays the link, then waits for the code.
	link := "https://claude.com/cai/oauth/authorize?code=true&" + strings.Repeat("x", 600)
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/steps", vps.Token, AgentStep{Tool: "url", Summary: link})
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/steps", vps.Token, AgentStep{Tool: "paste"})
	var v LoginView
	for i := 0; i < 200 && !v.NeedsCode; i++ {
		v = loginView(t, s, acc.ID)
		time.Sleep(5 * time.Millisecond)
	}
	if v.URL != link || !v.NeedsCode {
		t.Fatalf("login view: %+v", v)
	}
	if w := request(t, s, "GET", "/v1/runtime/jobs/"+job.ID+"/input", vps.Token, nil); w.Code != 204 {
		t.Fatalf("input before the owner pasted: %d", w.Code)
	}
	if w := request(t, s, "POST", "/v1/models/providers/"+acc.ID+"/login/code", adminTestToken, AccountRequest{Code: "abc#123"}); w.Code != 204 {
		t.Fatalf("code: %d %s", w.Code, w.Body)
	}
	w = request(t, s, "GET", "/v1/runtime/jobs/"+job.ID+"/input", vps.Token, nil)
	if !strings.Contains(w.Body.String(), "abc#123") {
		t.Fatalf("input: %d %s", w.Code, w.Body)
	}
	if w := request(t, s, "GET", "/v1/runtime/jobs/"+job.ID+"/input", vps.Token, nil); w.Code != 204 {
		t.Fatalf("the code is handed over once: %d", w.Code)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", vps.Token, JobResult{Success: true, Output: `{"account":"eu@trabalho.com · Max"}`})
	for i := 0; i < 200 && v.State != "done"; i++ {
		v = loginView(t, s, acc.ID)
		time.Sleep(5 * time.Millisecond)
	}
	p, _ := s.store.GetProvider(t.Context(), acc.ID)
	if v.State != "done" || p.Status != "ok" || p.Account != "eu@trabalho.com · Max" || p.NodeID != vps.NodeID {
		t.Fatalf("after login: %+v %+v", v, p)
	}
	// Links and codes stay out of the audit log.
	if ev := request(t, s, "GET", "/v1/events", adminTestToken, nil).Body.String(); strings.Contains(ev, "oauth/authorize") || strings.Contains(ev, "abc#123") {
		t.Fatalf("login secrets in the log: %s", ev)
	}
	// It cannot be the main model.
	if w := request(t, s, "PUT", "/v1/models/main", adminTestToken, MainModel{Provider: acc.ID, Model: "x"}); w.Code != 400 {
		t.Fatalf("subscription as main model: %d", w.Code)
	}

	// An agent asked for that account: the run goes to its node, on it.
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "revisa o projeto na conta do trabalho")
	run := waitClaim(t, s, vps.Token)
	if run.Action != store.ActionAgentRun || run.Agent.Account != acc.ID {
		t.Fatalf("run: %+v", run.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+run.ID+"/complete", vps.Token, JobResult{Success: true, Output: "Revisado."})
	<-answer
}

func TestAccountFallsBackWhenThePrincipalIsLimited(t *testing.T) {
	s, _ := pluginServer(t, nil)
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)
	ctx := t.Context()
	s.store.SaveProvider(ctx, store.Provider{ID: "codex.a1b2c3", Kind: "codex", Name: "Codex 2", NodeID: vps.NodeID, Status: "ok"}, "", s.now())
	if p, err := s.accountFor(ctx, "codex", "", vps.NodeID); err != nil || p.ID != "" {
		t.Fatalf("principal free: %+v %v", p, err)
	}
	s.quotaBook.set(vps.NodeID, []Quota{{Engine: "codex", Limited: true}})
	if p, _ := s.accountFor(ctx, "codex", "", vps.NodeID); p.ID != "codex.a1b2c3" {
		t.Fatalf("fallback: %+v", p)
	}
	if p, _ := s.accountFor(ctx, "codex", "principal", vps.NodeID); p.ID != "" {
		t.Fatalf("asked for the principal: %+v", p)
	}
	if _, err := s.accountFor(ctx, "codex", "pessoal", vps.NodeID); err == nil || !strings.Contains(err.Error(), "Codex 2") {
		t.Fatalf("unknown account: %v", err)
	}
}
