package control

import (
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func waitHeartbeat(t *testing.T, s *Server, decision string) HeartbeatState {
	t.Helper()
	var st HeartbeatState
	for i := 0; i < 400; i++ {
		st = s.heartbeatState(t.Context())
		if st.LastDecision == decision {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("heartbeat never got to %q: %+v", decision, st)
	return st
}

func TestHeartbeatTellsOnlyWhatIsNew(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}, answers: map[string]string{"Vega": "A rotina «Relatório» falhou: quer que eu rode de novo?"}}
	s := chatServer(t, e)
	ctx := t.Context()

	// Nothing to look at: no wake of the model.
	if d := s.Beat(ctx); d != "nada" {
		t.Fatalf("empty beat: %q", d)
	}
	// A failed routine wakes the chief of staff, who tells #time.
	at := s.now().Add(-time.Hour)
	if _, err := s.store.SaveRoutine(ctx, store.Routine{ID: "rel", AgentID: "vega", Title: "Relatório", Prompt: "x", Deliver: "time",
		LastRun: &at, LastStatus: "failed", LastPreview: "o nó estava offline"}, store.ActorSystem, s.now()); err != nil {
		t.Fatal(err)
	}
	if d := s.Beat(ctx); d != "acordou" {
		t.Fatalf("beat: %q", d)
	}
	st := waitHeartbeat(t, s, "avisou")
	if st.Used != 1 || len(st.Seen) != 1 || st.ConversationID == "" {
		t.Fatalf("state: %+v", st)
	}
	msgs, _ := s.store.Messages(ctx, TeamChannelID)
	last := msgs[len(msgs)-1]
	if last.AgentID != "vega" || !strings.HasPrefix(last.Content, heartbeatPrefix) || !strings.Contains(last.Content, "Relatório") {
		t.Fatalf("#time: %+v", last)
	}
	conv, _ := s.store.Messages(ctx, st.ConversationID)
	if len(conv) == 0 || !strings.Contains(conv[0].Content, "o nó estava offline") || !strings.Contains(conv[0].Content, "checklist") {
		t.Fatalf("prompt: %+v", conv)
	}
	// The same failure is not told twice.
	if d := s.Beat(ctx); d != "nada" {
		t.Fatalf("second beat: %q", d)
	}

	// "Nada novo." from the agent: nothing goes to #time.
	e.mu.Lock()
	e.answers["Vega"] = "Nada novo."
	e.mu.Unlock()
	at2 := s.now().Add(-time.Minute)
	_, _ = s.store.SaveRoutine(ctx, store.Routine{ID: "rel", AgentID: "vega", Title: "Relatório", Prompt: "x", Deliver: "time",
		LastRun: &at2, LastStatus: "failed", LastPreview: "de novo"}, store.ActorSystem, s.now())
	before, _ := s.store.Messages(ctx, TeamChannelID)
	if d := s.Beat(ctx); d != "acordou" {
		t.Fatalf("third beat: %q", d)
	}
	waitHeartbeat(t, s, "nada")
	if after, _ := s.store.Messages(ctx, TeamChannelID); len(after) != len(before) {
		t.Fatal("a silent beat must not write in #time")
	}

	// Out of budget: told once, and the items wait.
	raw := []byte(`{"enabled":true,"budget":2}`)
	_ = s.store.SetSetting(ctx, heartbeatKey, raw, store.ActorAdmin, s.now())
	at3 := s.now()
	_, _ = s.store.SaveRoutine(ctx, store.Routine{ID: "rel", AgentID: "vega", Title: "Relatório", Prompt: "x", Deliver: "time",
		LastRun: &at3, LastStatus: "failed", LastPreview: "terceira"}, store.ActorSystem, s.now())
	if d := s.Beat(ctx); d != "sem orçamento" {
		t.Fatalf("budget: %q", d)
	}
	if d := s.Beat(ctx); d != "sem orçamento" {
		t.Fatalf("budget again: %q", d)
	}
	msgs, _ = s.store.Messages(ctx, TeamChannelID)
	notices := 0
	for _, m := range msgs {
		if strings.Contains(m.Content, "conferências de hoje") {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("budget notices: %d", notices)
	}
}

func TestHeartbeatSettings(t *testing.T) {
	s := chatServer(t, &toolEngine{calls: map[string][]llm.ToolCall{}})
	v := decode[HeartbeatView](t, request(t, s, "GET", "/v1/heartbeat", adminTestToken, nil))
	if !v.Enabled || v.AgentID != DefaultAgentID || v.Every != 30 || v.Budget != 12 || v.Checklist != defaultHeartbeatChecklist {
		t.Fatalf("defaults: %+v", v)
	}
	v = decode[HeartbeatView](t, request(t, s, "PUT", "/v1/heartbeat", adminTestToken, Heartbeat{Enabled: false, Every: 5, Checklist: "só e-mails de clientes"}))
	if v.Enabled || v.Every != 15 || v.Checklist != "só e-mails de clientes" {
		t.Fatalf("saved: %+v", v)
	}
	if w := request(t, s, "PUT", "/v1/heartbeat", adminTestToken, Heartbeat{AgentID: "ninguem"}); w.Code != 400 {
		t.Fatalf("unknown agent: %d", w.Code)
	}
}
