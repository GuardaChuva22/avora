package control

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// overseerEngine is a tool engine whose plain replies are the overseer's verdict.
type overseerEngine struct {
	*toolEngine
	verdict string
}

func (e *overseerEngine) Reply(context.Context, string, []store.ChatMessage, func(llm.Delta)) (llm.Response, error) {
	return llm.Response{Content: e.verdict}, nil
}

func waitGoal(t *testing.T, s *Server, ok func(store.Goal) bool) store.Goal {
	t.Helper()
	var g store.Goal
	for i := 0; i < 400; i++ {
		if list, _ := s.store.ListGoals(t.Context(), ""); len(list) == 1 {
			g = list[0]
			if ok(g) {
				return g
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("goal never got there: %+v", g)
	return g
}

func TestGoalIsPursuedOnItsOwn(t *testing.T) {
	e := &overseerEngine{toolEngine: &toolEngine{calls: map[string][]llm.ToolCall{}}, verdict: "PERMITIDO"}
	s := chatServer(t, e)
	setCalls(e.toolEngine, "Vega", call("objetivo_criar", map[string]any{"titulo": "Prospecção de clínicas", "resultado": "5 reuniões com clínicas",
		"prazo": "2026-11-30", "mandato": "pode mandar a primeira mensagem do roteiro no Instagram", "funcionario": "nico"}))
	setCalls(e.toolEngine, "Nico",
		call("objetivo_item", map[string]any{"nome": "Clínica Sorriso", "etapa": "contatado", "contato": "@sorriso"}),
		call("objetivo_atualizar", map[string]any{"acordar_em_minutos": 120, "motivo": "respondem à tarde", "licao": "só clínicas do centro"}))

	c := agentConversation(t, s, "vega")
	resp := <-sendAsync(t, s, c.ID, "consegue 5 reuniões com clínicas até o fim do mês")
	if !strings.Contains(resp.Assistant.Content, "Objetivo «Prospecção de clínicas» criado") {
		t.Fatalf("answer: %q", resp.Assistant.Content)
	}
	// Nico was woken in the goal's own conversation and worked on it.
	g := waitGoal(t, s, func(g store.Goal) bool { return len(g.Items) == 1 && len(g.Lessons) == 1 })
	if g.AgentID != "nico" || g.Items[0].Stage != "contatado" || g.Items[0].Contact != "@sorriso" || g.WakeReason != "respondem à tarde" ||
		g.NextWake == nil || g.NextWake.Sub(s.now()) < 110*time.Minute || len(g.Stages) != len(goalDefaultStages) {
		t.Fatalf("goal: %+v", g)
	}
	conv, err := s.store.GetConversation(t.Context(), g.ConversationID)
	if err != nil || conv.AgentID != "nico" || !strings.HasPrefix(conv.Title, goalPrefix) {
		t.Fatalf("conversation: %+v %v", conv, err)
	}

	// The overseer lets the mandate through, and only the mandate.
	if got, ok := s.goalOfConversation(t.Context(), "nico", g.ConversationID); !ok || got.ID != g.ID {
		t.Fatal("goal of conversation")
	}
	if !s.mandateAllows(t.Context(), g, "Enviar mensagem para @sorriso") {
		t.Fatal("mandate refused")
	}
	e.verdict = "PERGUNTAR"
	if s.mandateAllows(t.Context(), g, "Fechar contrato") {
		t.Fatal("overseer ignored")
	}
	g.Mandate = ""
	e.verdict = "PERMITIDO"
	if s.mandateAllows(t.Context(), g, "x") {
		t.Fatal("no mandate must ask")
	}

	// A wake that is due runs; the item updates instead of duplicating.
	waitIdle := func() {
		for i := 0; i < 400; i++ {
			s.goals.mu.Lock()
			busy := len(s.goals.running) > 0
			s.goals.mu.Unlock()
			if !busy {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitIdle()
	cur, _ := s.store.GetGoal(t.Context(), g.ID)
	past := s.now().Add(-time.Minute)
	cur.NextWake = &past
	_, _ = s.store.SaveGoal(t.Context(), cur, store.ActorSystem, s.now())
	if s.goalHours() {
		s.TickGoals(t.Context())
		g = waitGoal(t, s, func(g store.Goal) bool { return g.LastReview != nil && len(g.Lessons) == 2 })
		if len(g.Items) != 1 {
			t.Fatalf("items: %+v", g.Items)
		}
	}
	waitIdle()

	// The owner pauses it from the app.
	paused := "pausado"
	if w := request(t, s, "POST", "/v1/goals/"+g.ID, adminTestToken, GoalEdit{Status: &paused}); w.Code != 200 {
		t.Fatalf("pause: %d", w.Code)
	}
	if list := decode[[]store.Goal](t, request(t, s, "GET", "/v1/goals", adminTestToken, nil)); len(list) != 1 || list[0].Status != "pausado" {
		t.Fatalf("list: %+v", list)
	}
}

func TestPageHashIgnoresClocks(t *testing.T) {
	if pageHash("Ana: oi! há 5 min") != pageHash("Ana: oi!  há 12 min") {
		t.Fatal("clock changed the hash")
	}
	if pageHash("Ana: oi!") == pageHash("Ana: oi! Bia: tenho interesse") {
		t.Fatal("new message did not change the hash")
	}
}
