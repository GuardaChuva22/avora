package control

import (
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestNightShift(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}, answers: map[string]string{}}
	s := chatServer(t, e)
	ctx := t.Context()

	// From the chat: the agent leaves it for the night.
	setCalls(e, "Vega", call(nightTool, map[string]any{"tarefa": "pesquisar 5 fornecedores de embalagem em SP"}))
	c := agentConversation(t, s, "vega")
	r := send(t, s, c.ID, "deixa pra noite: pesquisa fornecedores de embalagem")
	if !strings.Contains(r.Assistant.Content, "Fica para o turno da noite") {
		t.Fatalf("answer: %q", r.Assistant.Content)
	}
	// From the app.
	v := decode[NightView](t, request(t, s, "POST", "/v1/night/tasks", adminTestToken, map[string]string{"text": "publicar o post da promoção"}))
	if len(v.Tasks) != 2 || v.Tasks[0].Source != "chat" || v.Tasks[1].Source != "app" || v.Start != "01:00" || !v.Enabled {
		t.Fatalf("queue: %+v", v)
	}

	// The shift: the first is done; the second needs a locked action, which
	// is refused at once (nobody approves at night).
	e.mu.Lock()
	delete(e.answers, "Vega")
	e.mu.Unlock()
	setCalls(e, "Vega", call("claude_code", map[string]any{"tarefa": "publicar", "pode_editar": true}))
	if err := s.NightShiftRun(ctx, true); err != nil {
		t.Fatal(err)
	}
	q := s.nightQueue(ctx)
	if len(q.Tasks) != 2 || q.Running || q.Tasks[0].ConversationID == "" {
		t.Fatalf("after: %+v", q)
	}
	for _, task := range q.Tasks {
		if task.DoneAt == nil || task.Status == nightPending || task.Status == nightDoing {
			t.Fatalf("task not finished: %+v", task)
		}
	}
	msgs, _ := s.store.Messages(ctx, q.Tasks[1].ConversationID)
	steps := ""
	for _, m := range msgs {
		steps += string(m.Steps)
	}
	if !strings.Contains(steps, "turno da noite, não rodou") {
		t.Fatalf("the locked action must be refused at night: %s", steps)
	}
	if aps, _ := s.store.ListApprovals(ctx, "pending", 10); len(aps) != 0 {
		t.Fatalf("no approval waits at night: %+v", aps)
	}
	team, _ := s.store.Messages(ctx, TeamChannelID)
	last := team[len(team)-1]
	if last.AgentID != "vega" || !strings.HasPrefix(last.Content, nightPrefix+" Turno da noite") || !strings.Contains(last.Content, "fornecedores") {
		t.Fatalf("report: %+v", last)
	}

	// The status comes from the agent's last line.
	e.mu.Lock()
	e.answers["Vega"] = "Montei o texto.\nESPERANDO APROVAÇÃO: o post está pronto, falta você aprovar a publicação"
	e.mu.Unlock()
	request(t, s, "POST", "/v1/night/tasks", adminTestToken, map[string]string{"text": "preparar o post"})
	if err := s.NightShiftRun(ctx, true); err != nil {
		t.Fatal(err)
	}
	q = s.nightQueue(ctx)
	got := q.Tasks[len(q.Tasks)-1]
	if got.Status != nightWaiting || !strings.Contains(got.Result, "falta você aprovar") {
		t.Fatalf("waiting: %+v", got)
	}
	// Remove from the queue.
	if w := request(t, s, "DELETE", "/v1/night/tasks/"+got.ID, adminTestToken, nil); w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}
}

func TestShiftWindow(t *testing.T) {
	s := chatServer(t, nil)
	at := func(h, m int) { s.now = func() time.Time { return time.Date(2026, 11, 17, h, m, 0, 0, s.zone()) } }
	n := cleanNight(NightShift{Start: "23:00", End: "05:00"})
	at(23, 30)
	if day, in := s.shiftDay(n); !in || day != "2026-11-17" {
		t.Fatalf("23:30: %s %v", day, in)
	}
	at(2, 0)
	if day, in := s.shiftDay(n); !in || day != "2026-11-16" {
		t.Fatalf("02:00 belongs to the shift that started the day before: %s %v", day, in)
	}
	at(12, 0)
	if _, in := s.shiftDay(n); in {
		t.Fatal("noon is not night")
	}
	at(3, 0)
	if _, in := s.shiftDay(cleanNight(NightShift{})); !in {
		t.Fatal("default window 01:00-06:00")
	}
	_ = store.ActorSystem
}
