package control

import (
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/store"
)

func TestAgentTemplatesWelcomeAndOnboardingGreet(t *testing.T) {
	s, _ := makeServer(t)

	templates := decode[[]AgentTemplate](t, request(t, s, "GET", "/v1/agent-templates", adminTestToken, nil))
	wantIDs := []string{"pm", "dev", "pesquisa", "ops", "assistente", "em-branco"}
	if len(templates) != len(wantIDs) {
		t.Fatalf("templates len = %d, want %d: %+v", len(templates), len(wantIDs), templates)
	}
	for i, want := range wantIDs {
		if templates[i].ID != want {
			t.Fatalf("template[%d].ID = %q, want %q", i, templates[i].ID, want)
		}
		if want != "em-branco" && (templates[i].Persona == "" || templates[i].Role == "" || len(templates[i].Responsibilities) == 0 || len(templates[i].SuggestedSkills) == 0) {
			t.Fatalf("template %q missing fields: %+v", want, templates[i])
		}
	}

	// Create an agent from the Assistente pessoal template and generate its "Pra eu começar:" conversation.
	tpl := templates[4]
	created := decode[store.Agent](t, request(t, s, "POST", "/v1/agents", adminTestToken, AgentRequest{
		Name:             tpl.DefaultName,
		Role:             tpl.Role,
		Persona:          tpl.Persona,
		Color:            tpl.Color,
		Responsibilities: &tpl.Responsibilities,
	}))
	w := request(t, s, "POST", "/v1/agents/"+created.ID+"/welcome", adminTestToken, WelcomeRequest{
		TemplateID:      tpl.ID,
		SuggestedSkills: tpl.SuggestedSkills,
	})
	if w.Code != 201 {
		t.Fatalf("welcome status=%d: %s", w.Code, w.Body.String())
	}
	welcome := decode[WelcomeResponse](t, w)
	if welcome.Conversation.AgentID != created.ID {
		t.Fatalf("welcome conversation = %+v", welcome.Conversation)
	}
	for _, link := range []string{
		"Pra eu começar:",
		"avora:setup/brain/" + created.ID,
		"avora:setup/tools/" + created.ID,
		"avora:setup/channels/" + created.ID,
		"avora:setup/routine/" + created.ID,
	} {
		if !strings.Contains(welcome.Message.Content, link) {
			t.Fatalf("welcome message missing %q:\n%s", link, welcome.Message.Content)
		}
	}

	// Onboarding greeting posts in #time even before an AI engine is configured.
	gw := request(t, s, "POST", "/v1/onboarding/greet", adminTestToken, nil)
	if gw.Code != 200 {
		t.Fatalf("onboarding greet status=%d: %s", gw.Code, gw.Body.String())
	}
	greet := decode[OnboardingGreetResponse](t, gw)
	if greet.ChannelID != TeamChannelID || greet.Agents == 0 {
		t.Fatalf("onboarding greet = %+v", greet)
	}
	msgs := decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/"+greet.ChannelID+"/messages", adminTestToken, nil))
	if len(msgs) < 2 {
		t.Fatalf("expected greeting messages in #time, got %+v", msgs)
	}
}
