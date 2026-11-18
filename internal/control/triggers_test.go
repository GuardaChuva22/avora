package control

import (
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestTriggersFireOnNewItemsAndNodes(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}, answers: map[string]string{"Cora": "Vi a tarefa nova."}}
	s, f := pluginServer(t, e)
	connectClickUp(t, s)
	f.mu.Lock()
	f.listed = []string{"a1", "a2"}
	f.mu.Unlock()

	rt := decode[store.Routine](t, request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{AgentID: "cora", Title: "Tarefas novas",
		Prompt: "Avise das tarefas novas.", Deliver: "conversation",
		Trigger: &store.Trigger{Kind: "conector", Tool: "clickup_tarefas", Args: map[string]any{"lista_id": "l1", "quantidade": 999.0}}}))
	if rt.Trigger == nil || rt.Trigger.Every != defaultTriggerEvery || rt.NextRun != nil || rt.Trigger.Args["lista_id"] != "l1" ||
		rt.Trigger.Args["quantidade"] != 40 && rt.Trigger.Args["quantidade"] != 40.0 || !strings.Contains(whenOf(rt), "Tarefa nova no ClickUp «l1»") {
		t.Fatalf("routine: %+v %+v", rt, rt.Trigger)
	}
	if w := request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{AgentID: "cora", Title: "X", Prompt: "x",
		Trigger: &store.Trigger{Kind: "conector", Tool: "gmail_enviar"}}); w.Code != 400 {
		t.Fatalf("a write tool cannot be a trigger: %d", w.Code)
	}

	// The first check learns what is there; nothing runs.
	s.TickTriggers(t.Context())
	got, _ := s.store.GetRoutine(t.Context(), rt.ID)
	if len(got.Trigger.Seen) != 2 || got.LastRun != nil {
		t.Fatalf("baseline: %+v %+v", got, got.Trigger)
	}
	// A new task fires the routine with it.
	f.mu.Lock()
	f.listed = []string{"a3", "a1", "a2"}
	f.mu.Unlock()
	s.triggers.mu.Lock()
	s.triggers.checked = nil
	s.triggers.mu.Unlock()
	s.TickTriggers(t.Context())
	var conv store.Routine
	for i := 0; i < 400; i++ {
		conv, _ = s.store.GetRoutine(t.Context(), rt.ID)
		if conv.LastStatus == "ok" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if conv.LastStatus != "ok" || conv.ConversationID == "" || len(conv.Trigger.Seen) != 3 || conv.Trigger.Seen[0] != "a3" {
		t.Fatalf("fired: %+v %+v", conv, conv.Trigger)
	}
	msgs, _ := s.store.Messages(t.Context(), conv.ConversationID)
	if len(msgs) != 2 || msgs[1].Content != "Vi a tarefa nova." {
		t.Fatalf("messages: %+v", msgs)
	}
	// Seen again: nothing new, no run.
	s.triggers.mu.Lock()
	s.triggers.checked = nil
	s.triggers.mu.Unlock()
	s.TickTriggers(t.Context())
	time.Sleep(20 * time.Millisecond)
	if msgs, _ := s.store.Messages(t.Context(), conv.ConversationID); len(msgs) != 2 {
		t.Fatalf("ran again: %d", len(msgs))
	}

	// A node going offline fires its routine (the first look only learns).
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)
	node := decode[store.Routine](t, request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{AgentID: "cora", Title: "VPS caiu",
		Prompt: "Avise.", Deliver: "conversation", Trigger: &store.Trigger{Kind: "no", Node: vps.NodeID, Event: "offline"}}))
	if !strings.Contains(whenOf(node), "quando vps ficar offline") {
		t.Fatalf("node trigger: %s", whenOf(node))
	}
	s.TickTriggers(t.Context())
	s.now = func() time.Time { return time.Now().Add(time.Hour) }
	s.TickTriggers(t.Context())
	for i := 0; i < 400; i++ {
		if node, _ = s.store.GetRoutine(t.Context(), node.ID); node.LastStatus == "ok" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if node.LastStatus != "ok" {
		t.Fatalf("node fired: %+v", node)
	}
}
