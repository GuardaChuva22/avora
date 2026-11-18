package control

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// waitApproval returns the first pending approval once there is one.
func waitApproval(t *testing.T, s *Server) store.Approval {
	t.Helper()
	for i := 0; i < 400; i++ {
		list := decode[[]store.Approval](t, request(t, s, "GET", "/v1/approvals?status=pending", adminTestToken, nil))
		if len(list) > 0 {
			return list[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no pending approval")
	return store.Approval{}
}

func routineFor(t *testing.T, s *Server, agent string) store.Routine {
	t.Helper()
	return decode[store.Routine](t, request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{
		AgentID: agent, Title: "Checar gastos", Prompt: "Cheque.", Schedule: store.Schedule{At: "08:00"}}))
}

func TestDeniedActionIsNotDone(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	s := chatServer(t, e)
	rt := routineFor(t, s, "cora")
	e.calls["Cora"] = []llm.ToolCall{call("apagar_rotina", map[string]any{"id": rt.ID})}
	c := agentConversation(t, s, "cora")
	answer := sendAsync(t, s, c.ID, "apaga essa rotina")
	ap := waitApproval(t, s)
	if ap.Action != "apagar_rotina" || !strings.Contains(ap.Summary, "Apagar a rotina «Checar gastos» de Cora") {
		t.Fatalf("approval: %+v", ap)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "deny"})
	r := <-answer
	if _, err := s.store.GetRoutine(t.Context(), rt.ID); err != nil {
		t.Fatal("denied deletion happened")
	}
	if !strings.Contains(r.Assistant.Content, "O dono negou esta ação") {
		t.Fatalf("answer: %q", r.Assistant.Content)
	}
	var steps []AgentStep
	_ = json.Unmarshal(r.Assistant.Steps, &steps)
	if len(steps) != 1 || !strings.HasPrefix(steps[0].Summary, "negada: ") {
		t.Fatalf("steps: %+v", steps)
	}
	// A second answer to a decided approval is refused.
	if w := request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"}); w.Code != 409 {
		t.Fatalf("second decision: %d", w.Code)
	}
	if w := request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "maybe"}); w.Code != 400 {
		t.Fatalf("bad decision: %d", w.Code)
	}
	evs := decode[[]store.Event](t, request(t, s, "GET", "/v1/events?limit=200", adminTestToken, nil))
	requested, decided := false, false
	for _, ev := range evs {
		requested = requested || ev.Type == store.EventApprovalRequested
		decided = decided || (ev.Type == store.EventApprovalDecided && strings.Contains(string(ev.Payload), `"denied"`))
	}
	if !requested || !decided {
		t.Fatal("approval not audited")
	}
}

func TestAlwaysAllowBecomesAGrant(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	s := chatServer(t, e)
	first, second := routineFor(t, s, "cora"), routineFor(t, s, "cora")
	c := agentConversation(t, s, "cora")

	e.calls["Cora"] = []llm.ToolCall{call("apagar_rotina", map[string]any{"id": first.ID})}
	answer := sendAsync(t, s, c.ID, "apaga")
	ap := waitApproval(t, s)
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "always"})
	<-answer
	grants := decode[[]GrantView](t, request(t, s, "GET", "/v1/grants?agent=cora", adminTestToken, nil))
	if len(grants) != 1 || grants[0].Action != "apagar_rotina" || grants[0].Label != "apagar rotinas" {
		t.Fatalf("grants: %+v", grants)
	}

	// With the grant, the next deletion runs without asking.
	e.calls["Cora"] = []llm.ToolCall{call("apagar_rotina", map[string]any{"id": second.ID})}
	request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "apaga a outra"})
	if list, _ := s.store.ListRoutines(t.Context(), "cora"); len(list) != 0 {
		t.Fatalf("routines left: %+v", list)
	}
	if pending, _ := s.store.ListApprovals(t.Context(), "pending", 10); len(pending) != 0 {
		t.Fatalf("asked again: %+v", pending)
	}
	// Revoked, it asks again.
	if w := request(t, s, "DELETE", "/v1/grants/"+grants[0].ID, adminTestToken, nil); w.Code != 204 {
		t.Fatalf("revoke: %d", w.Code)
	}
	third := routineFor(t, s, "cora")
	e.calls["Cora"] = []llm.ToolCall{call("apagar_rotina", map[string]any{"id": third.ID})}
	answer = sendAsync(t, s, c.ID, "e essa")
	ap = waitApproval(t, s)
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	<-answer
}

func TestUnansweredApprovalExpires(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Atlas": {
		call("claude_code", map[string]any{"tarefa": "Edite x", "pode_editar": true}),
		call("claude_code", map[string]any{"tarefa": "Só leia y"}),
	}}}
	s := chatServer(t, e)
	s.approvalTTL = 30 * time.Millisecond
	c := agentConversation(t, s, "atlas")
	r := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "vai"}))
	// The edit expired; reading needs no approval (and fails for lack of a node).
	if !strings.Contains(r.Assistant.Content, "a ação não foi feita: Claude Code vai criar") || !strings.Contains(r.Assistant.Content, "nenhum nó") {
		t.Fatalf("answer: %q", r.Assistant.Content)
	}
	all := decode[[]store.Approval](t, request(t, s, "GET", "/v1/approvals", adminTestToken, nil))
	if len(all) != 1 || all[0].Status != "expired" || all[0].DecidedBy != store.ActorSystem {
		t.Fatalf("approvals: %+v", all)
	}
}

func TestStopWhileWaitingForApproval(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Atlas": {call("codex", map[string]any{"tarefa": "Edite x", "pode_editar": true})}}}
	s := chatServer(t, e)
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "vai")
	ap := waitApproval(t, s)
	if ap.Action != "codex.edit" {
		t.Fatalf("approval: %+v", ap)
	}
	request(t, s, "POST", "/v1/conversations/"+c.ID+"/stop", adminTestToken, nil)
	<-answer
	got, _ := s.store.GetApproval(t.Context(), ap.ID)
	msgs := decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/"+c.ID+"/messages", adminTestToken, nil))
	if got.Status != "expired" || msgs[len(msgs)-1].Error != "parado por você" {
		t.Fatalf("after stop: %+v %+v", got, msgs[len(msgs)-1])
	}
}

func TestRestartExpiresPendingApprovals(t *testing.T) {
	s := chatServer(t, &fakeEngine{})
	if _, err := s.store.CreateApproval(t.Context(), store.Approval{ID: "old", AgentID: "atlas", Action: "codex.edit", Summary: "x",
		CreatedAt: s.now(), ExpiresAt: s.now().Add(time.Hour)}, "agent:atlas"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverTurns(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.store.GetApproval(t.Context(), "old"); got.Status != "expired" {
		t.Fatalf("after restart: %+v", got)
	}
}

// Each agent picks when it asks (D56): "never" deletes a routine on its
// own; "always" asks even to create one.
func TestApprovalsLevelPerAgent(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	s := chatServer(t, e)
	level := func(v string) {
		cora, _ := s.store.GetAgent(t.Context(), "cora")
		w := request(t, s, "PUT", "/v1/agents/cora", adminTestToken, AgentRequest{Name: cora.Name, Role: cora.Role, Persona: cora.Persona, Approvals: &v})
		if w.Code != 200 {
			t.Fatalf("approvals %q: %d %s", v, w.Code, w.Body)
		}
	}
	bad := "sometimes"
	if w := request(t, s, "PUT", "/v1/agents/cora", adminTestToken, AgentRequest{Name: "Cora", Approvals: &bad}); w.Code != 400 {
		t.Fatalf("bad level: %d", w.Code)
	}
	c := agentConversation(t, s, "cora")

	level("never")
	rt := routineFor(t, s, "cora")
	e.calls["Cora"] = []llm.ToolCall{call("apagar_rotina", map[string]any{"id": rt.ID})}
	r := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "apaga"}))
	if _, err := s.store.GetRoutine(t.Context(), rt.ID); err == nil {
		t.Fatal("never: the routine is still there")
	}
	if !strings.Contains(string(r.Assistant.Steps), "sem perguntar") {
		t.Fatalf("never: steps %s", r.Assistant.Steps)
	}

	level("always")
	e.calls["Cora"] = []llm.ToolCall{call("criar_rotina", map[string]any{"titulo": "Resumo", "instrucoes": "Resuma.", "horario": "09:00"})}
	answer := sendAsync(t, s, c.ID, "cria uma rotina")
	ap := waitApproval(t, s)
	if ap.Action != "criar_rotina" || !strings.HasPrefix(ap.Summary, "criar rotinas: ") {
		t.Fatalf("always: %+v", ap)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "deny"})
	<-answer
	if list, _ := s.store.ListRoutines(t.Context(), "cora"); len(list) != 0 {
		t.Fatalf("always: created anyway %+v", list)
	}

	// Back to the default: creating runs freely.
	level("risky")
	if a, _ := s.store.GetAgent(t.Context(), "cora"); a.Approvals != "" {
		t.Fatalf("risky saved as %q", a.Approvals)
	}
}
