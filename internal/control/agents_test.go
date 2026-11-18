package control

import (
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/store"
)

func TestTeamAndAgentVersions(t *testing.T) {
	s, _ := makeServer(t)
	team := decode[[]AgentStatus](t, request(t, s, "GET", "/v1/agents", adminTestToken, nil))
	if len(team) != len(teamTemplates) || team[0].ID != "vega" || team[1].Engine != "native" || team[0].Version != 1 {
		t.Fatalf("team: %+v", team)
	}

	// A second EnsureTeam never duplicates or resets agents.
	if err := s.EnsureTeam(t.Context()); err != nil {
		t.Fatal(err)
	}

	w := request(t, s, "POST", "/v1/agents", adminTestToken, AgentRequest{Name: "Ágata Estudos", Role: "Tutora", Engine: "native", Color: "#AABBCC"})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := decode[store.Agent](t, w)
	if created.ID != "agata-estudos" || created.Mode != "read" || created.Color != "#aabbcc" || created.Position != 5 {
		t.Fatalf("created: %+v", created)
	}
	if w := request(t, s, "POST", "/v1/agents", adminTestToken, AgentRequest{Name: "Ágata Estudos", Engine: "native"}); w.Code != 409 {
		t.Fatalf("duplicate id: %d", w.Code)
	}
	for _, bad := range []AgentRequest{
		{Name: "", Engine: "native"},
		{Name: "X", Engine: "native", Color: "red"},
		{Name: "X", Engine: "native", Persona: strings.Repeat("a", maxPersona+1)},
		{ID: "Bad ID", Name: "X", Engine: "native"},
	} {
		if w := request(t, s, "POST", "/v1/agents", adminTestToken, bad); w.Code != 400 {
			t.Fatalf("accepted %+v: %d", bad, w.Code)
		}
	}

	edit := AgentRequest{Name: "Atlas", Role: "Dev sênior", Persona: "nova persona", Engine: "codex", Mode: "edit"}
	updated := decode[store.Agent](t, request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, edit))
	// Engine and mode are ignored: every agent is native and delegates.
	if updated.Version != 2 || updated.Engine != "native" || updated.Mode != "read" || updated.Position != 1 {
		t.Fatalf("updated: %+v", updated)
	}
	versions := decode[[]store.AgentVersion](t, request(t, s, "GET", "/v1/agents/atlas/versions", adminTestToken, nil))
	if len(versions) != 2 || versions[0].Version != 2 || versions[1].Agent.Role != "Dev" {
		t.Fatalf("versions: %+v", versions)
	}
	restored := decode[store.Agent](t, request(t, s, "POST", "/v1/agents/atlas/versions/1/restore", adminTestToken, nil))
	if restored.Version != 3 || restored.Role != "Dev" {
		t.Fatalf("restored: %+v", restored)
	}
	if w := request(t, s, "POST", "/v1/agents/atlas/versions/9/restore", adminTestToken, nil); w.Code != 404 {
		t.Fatalf("missing version: %d", w.Code)
	}
}

func TestNativeAgentUsesPersona(t *testing.T) {
	engine := &fakeEngine{}
	s := chatServer(t, engine)
	w := request(t, s, "POST", "/v1/conversations", adminTestToken, ConversationRequest{AgentID: "cora"})
	c := decode[store.Conversation](t, w)
	if c.AgentID != "cora" || c.Engine != "native" {
		t.Fatalf("conversation: %+v", c)
	}
	request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "quanto gastei?"})
	if !strings.Contains(engine.system, "Você é Cora, Financeiro") || !strings.Contains(engine.system, "Vega (Chefe de Gabinete)") {
		t.Fatalf("system prompt: %q", engine.system)
	}
	team := decode[[]AgentStatus](t, request(t, s, "GET", "/v1/agents", adminTestToken, nil))
	if team[3].ID != "cora" || team[3].Conversations != 1 || team[3].Preview != "eco: quanto gastei?" {
		t.Fatalf("cora status: %+v", team[3])
	}
	if list := decode[[]store.Conversation](t, request(t, s, "GET", "/v1/conversations?agent=vega", adminTestToken, nil)); len(list) != 0 {
		t.Fatalf("vega's list has cora's conversation: %+v", list)
	}
}

type jobAccepted struct {
	User  store.ChatMessage `json:"user"`
	JobID string            `json:"job_id"`
}
