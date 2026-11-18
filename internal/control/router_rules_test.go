package control

import (
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

func TestRuleRoute(t *testing.T) {
	now := time.Now()
	team := []store.Agent{{ID: "vega", Name: "Vega"}, {ID: "iris", Name: "Iris"}, {ID: "atlas", Name: "Atlas"},
		{ID: "cora", Name: "Cora", Style: store.AgentStyle{Talk: "muito"}}, {ID: "nico", Name: "Nico", Style: store.AgentStyle{Talk: "pouco"}}}
	recent := []store.ChatMessage{{Role: "user", Content: "oi"}, {Role: "assistant", AgentID: "atlas", Content: "fala", CreatedAt: now.Add(-time.Minute)}}

	for _, msg := range []string{"olá time!", "fala time, como estão?", "bom dia", "pessoal, o que acham?", "o pessoal não vai responder?", "E aí galera"} {
		for range 20 {
			d := ruleRoute(team, recent, []string{msg}, now)
			if len(d.agents) < 2 || len(d.agents) > routerMaxAgents || d.agents[0] != "atlas" {
				t.Fatalf("%q: %+v", msg, d)
			}
			seen := map[string]bool{}
			for _, id := range d.agents {
				if seen[id] {
					t.Fatalf("%q: repeated %v", msg, d.agents)
				}
				seen[id] = true
			}
		}
	}
	// Nobody talked lately: the chief opens.
	if d := ruleRoute(team, nil, []string{"olá time!"}, now); d.agents[0] != DefaultAgentID {
		t.Fatalf("no last speaker: %+v", d)
	}
	for msg, emoji := range map[string]string{"valeu": "🙌", "ok": "👍", "kkkkkk": "😂", "obrigado!": "❤️", "👍": "👍", "valeu, boa": "🙌"} {
		if d := ruleRoute(team, recent, []string{msg}, now); len(d.agents) != 0 || d.reaction != emoji {
			t.Errorf("%q: %+v", msg, d)
		}
	}
	if d := ruleRoute(team, recent, []string{"cora e iris, quanto sobra no mês?"}, now); len(d.agents) != 2 || d.agents[0] != "iris" && d.agents[0] != "cora" {
		t.Errorf("named: %+v", d)
	}
	if d := ruleRoute(team, recent, []string{"quanto sobra no mês?"}, now); len(d.agents) != 1 || d.agents[0] != "atlas" {
		t.Errorf("continue: %+v", d)
	}
	if d := ruleRoute(team, nil, []string{"quanto sobra no mês?"}, now); len(d.agents) != 1 || d.agents[0] != DefaultAgentID {
		t.Errorf("chief: %+v", d)
	}
	if d := ruleRoute(team, recent, []string{"ok, pode mandar?"}, now); len(d.agents) == 0 {
		t.Errorf("a question is not an ack: %+v", d)
	}
}
