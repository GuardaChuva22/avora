package control

import (
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/store"
)

func TestOnlyEmoji(t *testing.T) {
	for in, want := range map[string]bool{"👍": true, " 😂🔥 ": true, "❤️": true, "ok 👍": false, "[quieto]": false, "": false, "1": false, "kkk": false} {
		if _, got := onlyEmoji(in); got != want {
			t.Errorf("onlyEmoji(%q) = %v", in, got)
		}
	}
}

func TestChannelReactsAndTalksLikeAGroup(t *testing.T) {
	engine := &teamEngine{lines: map[string]string{
		"router": `{"agentes": ["cora", "atlas"], "motivo": "para o time"}`,
		"Cora":   "Tô bem! Fechando o mês aqui.",
		"Atlas":  "👍",
	}}
	s := chatServer(t, engine)
	say(t, s, "fala time, como estão?")
	settle(t, s)
	msgs := channelMessages(t, s)
	// Cora answers in a bubble; Atlas, who only agrees, reacts on the
	// owner's message and writes nothing.
	if len(msgs) != 2 || msgs[1].AgentID != "cora" {
		t.Fatalf("messages: %+v", msgs)
	}
	if r := msgs[0].Reactions; len(r) != 1 || r[0].AgentID != "atlas" || r[0].Emoji != "👍" {
		t.Fatalf("reactions: %+v", r)
	}
	// Atlas came after Cora and read her answer.
	if !strings.Contains(engine.prompts["Atlas"], "Cora: Tô bem!") {
		t.Fatalf("atlas did not read cora: %q", engine.prompts["Atlas"])
	}
	// In the group nobody is told to write headings and tables.
	if sys := engine.systems["Cora"]; strings.Contains(sys, formatRule) || !strings.Contains(sys, "grupo de WhatsApp") {
		t.Fatalf("channel system: %q", sys)
	}

	// "valeu" needs nobody: the router says so and Cora, who was talking,
	// reacts without a model call.
	engine.mu.Lock()
	engine.lines["router"] = `{"agentes": [], "reacao": "🙌", "motivo": "agradecimento"}`
	calls := engine.calls
	engine.mu.Unlock()
	say(t, s, "valeu")
	settle(t, s)
	msgs = channelMessages(t, s)
	if len(msgs) != 3 || len(msgs[2].Reactions) != 1 || msgs[2].Reactions[0].AgentID != "cora" || msgs[2].Reactions[0].Emoji != "🙌" {
		t.Fatalf("after valeu: %+v", msgs)
	}
	if engine.calls != calls {
		t.Fatalf("an agent ran for valeu")
	}
}

func TestAgentStyle(t *testing.T) {
	if _, err := validStyle(store.AgentStyle{Talk: "sempre"}); err == nil {
		t.Fatal("unknown talk accepted")
	}
	st, err := validStyle(store.AgentStyle{Voice: "  descontraída ", Talk: "normal", Emoji: "bastante", Length: "curta"})
	if err != nil || st.Voice != "descontraída" || st.Talk != "" || st.Emoji != "bastante" {
		t.Fatalf("style: %+v %v", st, err)
	}
	p := agentPreamble(store.Agent{Name: "Cora", Style: st}, nil)
	if !strings.Contains(p, "Seu jeito de falar: descontraída") || !strings.Contains(p, "Escreva curto") {
		t.Fatalf("preamble: %q", p)
	}
	s := chatServer(t, &teamEngine{})
	saved := decode[store.Agent](t, request(t, s, "PUT", "/v1/agents/cora", adminTestToken, map[string]any{"name": "Cora", "style": map[string]any{"voice": "seca", "talk": "pouco"}}))
	if saved.Style.Voice != "seca" || saved.Style.Talk != "pouco" {
		t.Fatalf("saved: %+v", saved.Style)
	}
}
