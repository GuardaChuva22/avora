package control

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

func TestNextRun(t *testing.T) {
	sp, _ := time.LoadLocation("America/Sao_Paulo")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, sp)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	weekdays := store.Schedule{At: "08:00", Days: []int{1, 2, 3, 4, 5}}
	cases := []struct {
		sc    store.Schedule
		after string
		want  string
	}{
		{weekdays, "2026-11-13 09:00", "2026-11-16 08:00"}, // Friday -> Monday
		{weekdays, "2026-11-10 07:59", "2026-11-10 08:00"},
		{weekdays, "2026-11-10 08:00", "2026-11-11 08:00"}, // never the same instant
		{store.Schedule{At: "23:30"}, "2026-11-10 23:45", "2026-11-11 23:30"},
		{store.Schedule{At: "09:00", Days: []int{1}}, "2026-11-10 10:00", "2026-11-16 09:00"},
		{store.Schedule{Every: 30}, "2026-11-10 10:07", "2026-11-10 10:37"},
	}
	for _, c := range cases {
		if got := nextRun(c.sc, at(c.after), sp); !got.Equal(at(c.want)) {
			t.Errorf("%+v after %s: got %s, want %s", c.sc, c.after, got.In(sp).Format("2006-01-02 15:04 Mon"), c.want)
		}
	}
	if got := scheduleText(weekdays); got != "seg, ter, qua, qui, sex às 08:00" {
		t.Errorf("text: %q", got)
	}
	if got := scheduleText(store.Schedule{Every: 120}); got != "a cada 2 h" {
		t.Errorf("text: %q", got)
	}
}

func TestResponsibilities(t *testing.T) {
	engine := &fakeEngine{}
	s := chatServer(t, engine)
	list := []store.Responsibility{{Goal: "Gastos do mês abaixo do orçamento", Metric: "gasto / orçamento", Cadence: "diária"}}
	edit := map[string]any{"name": "Cora", "role": "Financeiro", "engine": "native", "responsibilities": list}
	cora := decode[store.Agent](t, request(t, s, "PUT", "/v1/agents/cora", adminTestToken, edit))
	if len(cora.Responsibilities) != 1 || cora.Responsibilities[0].ID == "" {
		t.Fatalf("cora: %+v", cora.Responsibilities)
	}
	// An edit without the field keeps them (the app's editor of phase 1).
	delete(edit, "responsibilities")
	edit["persona"] = "nova"
	cora = decode[store.Agent](t, request(t, s, "PUT", "/v1/agents/cora", adminTestToken, edit))
	if len(cora.Responsibilities) != 1 || cora.Persona != "nova" {
		t.Fatalf("kept: %+v", cora)
	}
	for _, bad := range [][]store.Responsibility{{{Goal: ""}}, {{Goal: "a", ID: "x"}, {Goal: "b", ID: "x"}}} {
		edit["responsibilities"] = bad
		if w := request(t, s, "PUT", "/v1/agents/cora", adminTestToken, edit); w.Code != 400 {
			t.Fatalf("accepted %+v: %d", bad, w.Code)
		}
	}
	c := decode[store.Conversation](t, request(t, s, "POST", "/v1/conversations", adminTestToken, ConversationRequest{AgentID: "cora"}))
	request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "e aí?"})
	if !strings.Contains(engine.system, "Suas responsabilidades permanentes:\n- Gastos do mês abaixo do orçamento (métrica: gasto / orçamento; cadência: diária)") {
		t.Fatalf("system: %q", engine.system)
	}
}

func TestRoutineAPI(t *testing.T) {
	s, _ := makeServer(t)
	seeded := decode[[]store.Routine](t, request(t, s, "GET", "/v1/routines?agent=vega", adminTestToken, nil))
	if len(seeded) != 2 || seeded[0].ID != "briefing-manha" || !seeded[0].Enabled {
		t.Fatalf("seeded: %+v", seeded)
	}
	good := RoutineRequest{AgentID: "iris", Title: "Notícias de IA", Prompt: "Resuma as novidades.", Schedule: store.Schedule{At: "07:30", Days: []int{1, 4}}}
	for _, bad := range []func(r *RoutineRequest){
		func(r *RoutineRequest) { r.Schedule = store.Schedule{Every: 10} },
		func(r *RoutineRequest) { r.Schedule = store.Schedule{At: "7h"} },
		func(r *RoutineRequest) { r.Schedule = store.Schedule{At: "07:00", Every: 60} },
		func(r *RoutineRequest) { r.Schedule.Days = []int{1, 1} },
		func(r *RoutineRequest) { r.AgentID = "ninguem" },
		func(r *RoutineRequest) { r.ResponsibilityID = "nao-existe" },
		func(r *RoutineRequest) { r.Deliver = "email" },
		func(r *RoutineRequest) { r.Title = "" },
	} {
		req := good
		bad(&req)
		if w := request(t, s, "POST", "/v1/routines", adminTestToken, req); w.Code != 400 {
			t.Fatalf("accepted %+v: %d", req, w.Code)
		}
	}
	w := request(t, s, "POST", "/v1/routines", adminTestToken, good)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	rt := decode[store.Routine](t, w)
	// Tuesday 09:00 in São Paulo: next is Thursday 07:30 (10:30 UTC).
	if !rt.Enabled || rt.Deliver != "time" || rt.NextRun == nil || !rt.NextRun.Equal(time.Date(2026, 11, 12, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("created: %+v next %v", rt, rt.NextRun)
	}
	off := false
	good.Enabled = &off
	rt = decode[store.Routine](t, request(t, s, "PUT", "/v1/routines/"+rt.ID, adminTestToken, good))
	if rt.Enabled || rt.NextRun != nil {
		t.Fatalf("disabled: %+v", rt)
	}
	if w := request(t, s, "DELETE", "/v1/routines/"+rt.ID, adminTestToken, nil); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := request(t, s, "POST", "/v1/routines/"+rt.ID+"/run", adminTestToken, nil); w.Code != 404 {
		t.Fatalf("run deleted: %d", w.Code)
	}
}

func waitRoutine(t *testing.T, s *Server, id string) store.Routine {
	t.Helper()
	for i := 0; i < 400; i++ {
		rt, err := s.store.GetRoutine(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if rt.LastStatus != "running" && rt.LastStatus != "" {
			return rt
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the routine never finished")
	return store.Routine{}
}

func TestRoutineRunsInChannel(t *testing.T) {
	s, clk := makeServer(t)
	engine := &teamEngine{lines: map[string]string{"Cora": "Gastos ok: 62% do orçamento."}}
	s.SetChatEngine(engine)
	w := request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{
		AgentID: "cora", Title: "Checar gastos", Prompt: "Como estão os gastos?", Schedule: store.Schedule{Every: 60},
	})
	rt := decode[store.Routine](t, w)

	s.TickRoutines(t.Context()) // not due yet
	if got, _ := s.store.GetRoutine(t.Context(), rt.ID); got.LastRun != nil {
		t.Fatalf("ran early: %+v", got)
	}
	clk.advance(61 * time.Minute)
	s.TickRoutines(t.Context())
	done := waitRoutine(t, s, rt.ID)
	settle(t, s)
	if done.LastStatus != "ok" || done.LastPreview != "Gastos ok: 62% do orçamento." || done.Failures != 0 ||
		!done.NextRun.Equal(clk.now().Add(time.Hour)) {
		t.Fatalf("after run: %+v", done)
	}
	msgs := channelMessages(t, s)
	if len(msgs) != 2 || msgs[0].Role != "system" || !strings.HasPrefix(msgs[0].Content, "⏰ Rotina «Checar gastos» · Cora") ||
		msgs[1].AgentID != "cora" {
		t.Fatalf("channel: %+v", msgs)
	}
	p := engine.prompts["Cora"]
	if !strings.Contains(p, "a rotina «Checar gastos» chamou você") || !strings.Contains(p, "--- Rotina «Checar gastos» (a cada 1 h)") ||
		!strings.Contains(p, "«Briefing da manhã» de Vega") {
		t.Fatalf("prompt: %q", p)
	}
	// A routine notice is not the loop lock.
	if st := settle(t, s); st.Locked {
		t.Fatalf("locked by a routine: %+v", st)
	}
}

func TestRoutineFailuresDisableIt(t *testing.T) {
	s, _ := makeServer(t)
	s.SetChatEngine(&fakeEngine{err: errors.New("boom")})
	rt := decode[store.Routine](t, request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{
		AgentID: "cora", Title: "Gastos", Prompt: "Como estão?", Schedule: store.Schedule{At: "08:00"}, Deliver: "conversation",
	}))
	for i := 1; i <= RoutineMaxFailures; i++ {
		if w := request(t, s, "POST", "/v1/routines/"+rt.ID+"/run", adminTestToken, nil); w.Code != 202 {
			t.Fatalf("run %d: %d %s", i, w.Code, w.Body.String())
		}
		got := waitRoutine(t, s, rt.ID)
		if got.Failures != i || got.LastStatus != "failed" {
			t.Fatalf("run %d: %+v", i, got)
		}
		rt = got
		rt.LastStatus = ""
		_, _ = s.store.SaveRoutine(t.Context(), rt, "", s.now())
	}
	rt, _ = s.store.GetRoutine(t.Context(), rt.ID)
	if rt.Enabled || rt.NextRun != nil || rt.ConversationID == "" {
		t.Fatalf("still on: %+v", rt)
	}
	convs := decode[[]store.Conversation](t, request(t, s, "GET", "/v1/conversations?agent=cora", adminTestToken, nil))
	if len(convs) != 1 || convs[0].Title != "⏰ Gastos" { // every run reuses the routine's conversation
		t.Fatalf("conversations: %+v", convs)
	}
	msgs := channelMessages(t, s)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content, "desativada depois de 10 falhas") {
		t.Fatalf("channel: %+v", msgs)
	}
	// Turning it back on resets the count.
	on := true
	rt = decode[store.Routine](t, request(t, s, "PUT", "/v1/routines/"+rt.ID, adminTestToken, RoutineRequest{
		AgentID: "cora", Title: "Gastos", Prompt: "Como estão?", Schedule: store.Schedule{At: "08:00"}, Deliver: "conversation", Enabled: &on,
	}))
	if !rt.Enabled || rt.Failures != 0 || rt.NextRun == nil {
		t.Fatalf("re-enabled: %+v", rt)
	}
}

func TestRoutineConversation(t *testing.T) {
	s, _ := makeServer(t)
	engine := &fakeEngine{}
	s.SetChatEngine(engine)
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	rt := decode[store.Routine](t, request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{
		AgentID: "iris", Title: "Notícias", Prompt: "Resuma as novidades de IA.", Schedule: store.Schedule{At: "07:00"}, Deliver: "conversation",
	}))
	request(t, s, "POST", "/v1/routines/"+rt.ID+"/run", adminTestToken, nil)
	done := waitRoutine(t, s, rt.ID)
	if done.LastStatus != "ok" || !strings.HasPrefix(done.LastPreview, "eco: Resuma as novidades de IA.") {
		t.Fatalf("done: %+v", done)
	}
	// The snapshot reaches the model, not the stored message.
	seen := engine.lastSeen[len(engine.lastSeen)-1].Content
	if !strings.Contains(seen, "--- Rotina «Notícias»") || !strings.Contains(seen, "pc (windows) online") {
		t.Fatalf("model saw: %q", seen)
	}
	msgs := decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/"+done.ConversationID+"/messages", adminTestToken, nil))
	if len(msgs) != 2 || msgs[0].Content != "Resuma as novidades de IA." || msgs[1].Role != "assistant" {
		t.Fatalf("messages: %+v", msgs)
	}
}

func TestReminderRunsOnce(t *testing.T) {
	s, c := makeServer(t)
	loc := s.zone()
	// 12:00 UTC on 10/11 is 09:00 in Brasília; remind at 15:00 on the 11th.
	w := request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{AgentID: "vega", Title: "Dentista", Prompt: "Lembre o dono de ligar pro dentista.",
		Schedule: store.Schedule{At: "15:00", Date: "2026-11-11"}, Deliver: "time"})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	rt := decode[store.Routine](t, w)
	if rt.NextRun == nil || rt.NextRun.In(loc).Format("02/01 15:04") != "11/11 15:00" || scheduleText(rt.Schedule) != "uma vez, 11/11/2026 às 15:00" {
		t.Fatalf("next: %v %q", rt.NextRun, scheduleText(rt.Schedule))
	}
	if w := request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{AgentID: "vega", Title: "x", Prompt: "x",
		Schedule: store.Schedule{At: "08:00", Date: "2026-11-09"}, Deliver: "time"}); w.Code != 400 {
		t.Fatalf("past reminder: %d", w.Code)
	}
	if w := request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{AgentID: "vega", Title: "x", Prompt: "x",
		Schedule: store.Schedule{At: "08:00", Date: "2026-11-12", Days: []int{1}}, Deliver: "time"}); w.Code != 400 {
		t.Fatalf("date with days: %d", w.Code)
	}
	c.t = time.Date(2026, 11, 11, 18, 1, 0, 0, time.UTC)
	s.TickRoutines(context.Background())
	got, _ := s.store.GetRoutine(context.Background(), rt.ID)
	if got.Enabled || got.NextRun != nil || got.LastRun == nil {
		t.Fatalf("after the run: enabled=%v next=%v last=%v", got.Enabled, got.NextRun, got.LastRun)
	}
}
