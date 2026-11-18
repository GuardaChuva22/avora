package control

import (
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestGoalWaitWakesOnNewItemAndDeadline(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}, answers: map[string]string{}}
	s, f := pluginServer(t, e)
	connectClickUp(t, s)
	f.mu.Lock()
	f.listed = []string{"a1", "a2"}
	f.mu.Unlock()
	ctx := t.Context()

	if _, err := s.goalWait(ctx, "cora", goalWaitArgs{Evento: "telegrama"}); err == nil {
		t.Fatal("unknown event")
	}
	if _, err := s.goalWait(ctx, "cora", goalWaitArgs{Evento: "email"}); err == nil {
		t.Fatal("Gmail is not connected: the wait must say so")
	}
	w, err := s.goalWait(ctx, "cora", goalWaitArgs{Evento: "tarefa", ListaID: "l1", Motivo: "a tarefa do fornecedor"})
	if err != nil || w.Args["lista_id"] != "l1" || w.Every != goalWaitEvery || !strings.Contains(s.waitText(w), "lista l1") {
		t.Fatalf("wait: %+v %v", w, err)
	}
	g, err := s.store.SaveGoal(ctx, store.Goal{AgentID: "cora", Title: "Orçamentos", Outcome: "3 orçamentos",
		Status: store.GoalActive, Watches: []store.GoalWatch{w}}, store.ActorSystem, s.now())
	if err != nil {
		t.Fatal(err)
	}

	// The first look only learns what is there.
	s.checkWait(ctx, g.ID, "cora", w)
	g, _ = s.store.GetGoal(ctx, g.ID)
	if g.NextWake != nil || len(g.Watches) != 1 || !g.Watches[0].Learned || len(g.Watches[0].Seen) != 2 {
		t.Fatalf("baseline: %+v", g)
	}
	if !strings.Contains(s.goalBrief(g, false), "Espera "+w.ID) {
		t.Fatalf("brief: %s", s.goalBrief(g, false))
	}
	// Something new wakes the goal with it, and a one-time wait ends.
	f.mu.Lock()
	f.listed = []string{"a3", "a1", "a2"}
	f.mu.Unlock()
	s.checkWait(ctx, g.ID, "cora", g.Watches[0])
	g, _ = s.store.GetGoal(ctx, g.ID)
	if g.NextWake == nil || !strings.Contains(g.WakeReason, "Tarefa a3") || strings.Contains(g.WakeReason, "Tarefa a1") || len(g.Watches) != 0 {
		t.Fatalf("fired: %+v", g)
	}

	// A wait past its deadline wakes the goal saying nothing came.
	past := s.now().Add(-time.Minute)
	g.NextWake = nil
	g.Watches = []store.GoalWatch{{ID: "w2", Event: "email", Why: "resposta da loja", Until: &past, Args: map[string]any{"busca": "from:loja"}}}
	g, _ = s.store.SaveGoal(ctx, g, store.ActorSystem, s.now())
	s.checkWaits(ctx, g)
	g, _ = s.store.GetGoal(ctx, g.ID)
	if g.NextWake == nil || !strings.Contains(g.WakeReason, "prazo da espera venceu") || !strings.Contains(g.WakeReason, "from:loja") || len(g.Watches) != 0 {
		t.Fatalf("deadline: %+v", g)
	}
}

func TestWaitEntries(t *testing.T) {
	ig := "DMs do Instagram (...):\n" +
		"- ⏳ @ana (id 1) · há 5 min · conversa_id c1 · última: @ana: «tem horário?»\n" +
		"- @bia (id 2) · ontem · conversa_id c2 · última: você: «obrigado»\n"
	list := waitEntries(ig, true)
	if len(list) != 1 || !strings.HasPrefix(list[0].key, "c1#") {
		t.Fatalf("instagram: %+v", list)
	}
	// The same last message is the same key even when the time moves on.
	again := waitEntries(strings.Replace(ig, "há 5 min", "há 9 min", 1), true)
	if again[0].key != list[0].key {
		t.Fatal("the key must not change with the clock")
	}
	newer := waitEntries(strings.Replace(ig, "tem horário?", "e amanhã?", 1), true)
	if newer[0].key == list[0].key {
		t.Fatal("a new message must be a new key")
	}
	tinder := "Matches do Tinder:\n- ⏳ Ana, 27 · match_id m1\n  Última: Ana: «oi»\n- Bia · match_id m2\n"
	if got := waitEntries(tinder, false); len(got) != 2 || got[0].key != "m1" || !strings.Contains(got[0].text, "Última") {
		t.Fatalf("tinder: %+v", got)
	}
	gmail := "[m1] hoje · De: Loja <l@x> · Orçamento\n   snippet\n[m2] ontem · De: B · Oi"
	if got := waitEntries(gmail, false); len(got) != 2 || got[0].key != "m1" || !strings.Contains(got[0].text, "snippet") {
		t.Fatalf("gmail: %+v", got)
	}
}
