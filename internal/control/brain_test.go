package control

import (
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Each agent can think with its own brain (D55): Claude Code with a model
// and an effort here, the main model when its brain cannot answer.
func TestAgentBrain(t *testing.T) {
	main := &fakeEngine{}
	s := chatServer(t, main)
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, HeartbeatRequest{})

	brains := decode[[]BrainOption](t, request(t, s, "GET", "/v1/brains", adminTestToken, nil))
	if len(brains) == 0 || brains[0].Provider != claudeBrain || !brains[0].Ready || len(brains[0].Efforts) != 4 {
		t.Fatalf("brains: %+v", brains)
	}

	atlas, err := s.store.GetAgent(t.Context(), "atlas")
	if err != nil {
		t.Fatal(err)
	}
	put := func(b store.AgentBrain) int {
		return request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, AgentRequest{Name: atlas.Name, Role: atlas.Role, Persona: atlas.Persona, Brain: &b}).Code
	}
	if code := put(store.AgentBrain{Provider: "nope"}); code != 400 {
		t.Fatalf("unknown provider: %d", code)
	}
	if code := put(store.AgentBrain{Provider: claudeBrain, Effort: "ultra"}); code != 400 {
		t.Fatalf("unknown effort: %d", code)
	}
	if code := put(store.AgentBrain{Provider: claudeBrain, Model: "opus", Effort: "max"}); code != 200 {
		t.Fatalf("claude brain: %d", code)
	}
	if a, _ := s.store.GetAgent(t.Context(), "atlas"); a.Brain.Model != "opus" || a.Name != atlas.Name {
		t.Fatalf("saved: %+v", a)
	}

	// Atlas's turn runs on Claude Code, with Avora's tools on the bridge.
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "organiza minha semana")
	job := waitClaim(t, s, pc.Token)
	spec := job.Agent
	if spec.Engine != "claude" || spec.Model != "opus" || spec.Effort != "max" || spec.Mode != "read" || spec.Bridge == "" ||
		spec.Workdir != "agents/atlas" || !strings.Contains(spec.Prompt, "organiza minha semana") {
		t.Fatalf("claude job: %+v", spec)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token,
		JobResult{Success: true, Output: "Feito, montei a semana.", Stats: []byte(`{"input_tokens":12,"output_tokens":4}`)})
	r := <-answer
	if r.Assistant.Content != "Feito, montei a semana." || r.Assistant.Model != "opus" || r.Assistant.Error != "" {
		t.Fatalf("answer: %+v", r.Assistant)
	}

	// Its brain gone: the main model answers and the turn says why.
	if code := put(store.AgentBrain{Provider: "codex@sumiu", Effort: "high"}); code != 200 {
		t.Fatalf("codex brain: %d", code)
	}
	r = <-sendAsync(t, s, c.ID, "e amanhã?")
	if r.Assistant.Content != "eco: e amanhã?" || !strings.Contains(string(r.Assistant.Steps), "Cérebro") {
		t.Fatalf("fallback: %+v %s", r.Assistant, r.Assistant.Steps)
	}

	// Back to the main model.
	if code := put(store.AgentBrain{}); code != 200 {
		t.Fatalf("clear: %d", code)
	}
	if a, _ := s.store.GetAgent(t.Context(), "atlas"); a.Brain != (store.AgentBrain{}) {
		t.Fatalf("cleared: %+v", a.Brain)
	}
}

func TestCodexEffort(t *testing.T) {
	if codexEffort("max") != "xhigh" || codexEffort("low") != "low" {
		t.Fatal("codex effort")
	}
	spec := &store.AgentSpec{Engine: "codex", Effort: "max", Prompt: "x"}
	if err := validateAgent(spec); err != nil || spec.Effort != "xhigh" {
		t.Fatalf("validate: %v %q", err, spec.Effort)
	}
}
