package control

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// teamEngine answers as whichever agent the system prompt names, with that
// agent's scripted line.
type teamEngine struct {
	mu      sync.Mutex
	lines   map[string]string
	prompts map[string]string
	systems map[string]string
	calls   int
}

var whoPattern = regexp.MustCompile(`Você é (\pL+),`)

func init() {
	routerQuiet = 5 * time.Millisecond
	routerStagger = func() time.Duration { return time.Millisecond }
}

func (e *teamEngine) Name() string     { return "native" }
func (e *teamEngine) Model() string    { return "fake-model" }
func (e *teamEngine) Provider() string { return "fake" }
func (e *teamEngine) Reply(_ context.Context, system string, history []store.ChatMessage, _ func(llm.Delta)) (llm.Response, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.prompts == nil {
		e.prompts = map[string]string{}
	}
	if strings.HasPrefix(system, "Você decide quem") {
		e.prompts["router"] = history[len(history)-1].Content
		return llm.Response{Content: e.lines["router"]}, nil
	}
	who := whoPattern.FindStringSubmatch(system)[1]
	e.calls++
	e.prompts[who] = history[len(history)-1].Content
	if e.systems == nil {
		e.systems = map[string]string{}
	}
	e.systems[who] = system
	return llm.Response{Content: e.lines[who]}, nil
}

func channelMessages(t *testing.T, s *Server) []store.ChatMessage {
	t.Helper()
	return decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/time/messages", adminTestToken, nil))
}

// settle waits until nobody is working in #time.
func settle(t *testing.T, s *Server) ChannelStatus {
	t.Helper()
	for i := 0; i < 400; i++ {
		st := decode[ChannelStatus](t, request(t, s, "GET", "/v1/channel", adminTestToken, nil))
		if len(st.Working) == 0 && !st.Routing && len(st.Next) == 0 {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the channel never settled")
	return ChannelStatus{}
}

func say(t *testing.T, s *Server, content string) []string {
	t.Helper()
	w := request(t, s, "POST", "/v1/conversations/time/messages", adminTestToken, MessageRequest{Content: content})
	if w.Code != 202 {
		t.Fatalf("send: %d %s", w.Code, w.Body.String())
	}
	return decode[struct {
		Called []string `json:"called"`
	}](t, w).Called
}

func TestMentions(t *testing.T) {
	team := teamTemplates
	cases := map[string]string{
		"oi":                         "",
		"@Atlas e @iris, vejam isso": "atlas iris",
		"@ATLAS @atlas":              "atlas",
		"email@cora.com @Ninguém":    "cora", // an address still names Cora: harmless
		"@todos bom dia":             "vega atlas iris cora nico",
		"fala @Nico.":                "nico",
	}
	for text, want := range cases {
		if got := strings.Join(mentions(text, team, team), " "); got != want {
			t.Errorf("mentions(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestChannelHandsWorkAround(t *testing.T) {
	engine := &teamEngine{lines: map[string]string{
		"Vega": "Vou pedir as contas. @Cora, quanto sobra por mês?",
		"Cora": "Sobram R$ 800.",
	}}
	s := chatServer(t, engine)

	// No mention and nothing the router can read: the chief of staff
	// answers and calls Cora, and Cora's answer comes back to her (D59).
	if called := say(t, s, "quanto consigo guardar?"); len(called) != 0 {
		t.Fatalf("called: %v", called)
	}
	st := settle(t, s)
	msgs := channelMessages(t, s)
	if len(msgs) != 4 || msgs[1].AgentID != "vega" || msgs[2].AgentID != "cora" || msgs[2].Content != "Sobram R$ 800." || msgs[3].AgentID != "vega" {
		t.Fatalf("messages: %+v", msgs)
	}
	if !strings.Contains(engine.prompts["Vega"], "Cora já responderam o que você pediu") {
		t.Fatalf("vega's hand-back: %q", engine.prompts["Vega"])
	}
	if st.Streak != 3 || st.Locked {
		t.Fatalf("status: %+v", st)
	}
	// Cora read the channel, with names, and knows who called her.
	p := engine.prompts["Cora"]
	if !strings.Contains(p, "Dono: quanto consigo guardar?") || !strings.Contains(p, "Vega: Vou pedir as contas.") ||
		!strings.Contains(p, "Vega chamou você. Responda agora no canal como Cora. ---") || !strings.Contains(p, quietMark) {
		t.Fatalf("cora's prompt: %q", p)
	}
	// The channel is not anyone's direct conversation.
	if list := decode[[]store.Conversation](t, request(t, s, "GET", "/v1/conversations?agent=vega", adminTestToken, nil)); len(list) != 0 {
		t.Fatalf("vega's conversations: %+v", list)
	}
}

func TestChannelLoopLock(t *testing.T) {
	engine := &teamEngine{lines: map[string]string{
		"Vega": "@Cora sua vez",
		"Cora": "@Vega não, sua vez",
	}}
	s := chatServer(t, engine)
	say(t, s, "@vega começa")
	st := settle(t, s)
	msgs := channelMessages(t, s)
	answers := 0
	for _, m := range msgs {
		if m.Role == "assistant" {
			answers++
		}
	}
	last := msgs[len(msgs)-1]
	if answers != ChannelLoopLimit || last.Role != "system" || !strings.Contains(last.Content, "Trava do canal") || !st.Locked {
		t.Fatalf("answers %d, last %+v, status %+v", answers, last, st)
	}
	// The owner writing unlocks it.
	say(t, s, "@cora só você agora")
	time.Sleep(20 * time.Millisecond)
	st = settle(t, s)
	if st.Locked && st.Streak == 0 {
		t.Fatalf("still locked: %+v", st)
	}
}

func TestChannelRouter(t *testing.T) {
	engine := &teamEngine{lines: map[string]string{
		"router": `Claro: {"agentes": ["cora", "@Atlas", "ninguem"], "motivo": "finanças e pesquisa"}`,
		"Cora":   "Sobram R$ 800.",
		"Atlas":  "[quieto]",
	}}
	s := chatServer(t, engine)

	// Two messages in a row are one decision; the router reads both and the
	// team, and picks Cora first (she answers) and Atlas (he may pass).
	routerQuiet = 80 * time.Millisecond
	t.Cleanup(func() { routerQuiet = 5 * time.Millisecond })
	say(t, s, "bom dia")
	say(t, s, "quanto sobra no mês?")
	settle(t, s)
	p := engine.prompts["router"]
	if !strings.Contains(p, "bom dia\nquanto sobra no mês?") || !strings.Contains(p, "Cora (id cora)") {
		t.Fatalf("router prompt: %q", p)
	}
	msgs := channelMessages(t, s)
	if len(msgs) != 3 || msgs[2].AgentID != "cora" {
		t.Fatalf("messages: %+v", msgs)
	}
	if strings.Contains(engine.prompts["Cora"], quietMark) || !strings.Contains(engine.prompts["Atlas"], quietMark) {
		t.Fatalf("only Atlas may pass:\n%q\n%q", engine.prompts["Cora"], engine.prompts["Atlas"])
	}
	evs, _ := s.store.Events(t.Context(), store.EventFilter{Latest: true, Limit: 50})
	seen := map[string]bool{}
	for _, e := range evs {
		seen[e.Type] = true
	}
	for _, typ := range []string{EventChannelRouted, EventChannelWorking, EventChannelPassed} {
		if !seen[typ] {
			t.Errorf("no %s event", typ)
		}
	}

	// A message with @ does not wait for the router.
	if called := say(t, s, "@atlas e você?"); strings.Join(called, ",") != "atlas" {
		t.Fatalf("called: %v", called)
	}
	settle(t, s)
}
