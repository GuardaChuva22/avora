package control

import (
	"context"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

func TestAgentUsage(t *testing.T) {
	s, c := makeServer(t)
	ctx := context.Background()
	conv, err := s.store.CreateConversation(ctx, store.Conversation{ID: "c1", AgentID: "atlas", Engine: "native", Model: "m"}, store.ActorAdmin)
	if err != nil {
		t.Fatal(err)
	}
	add := func(at time.Time, role, agent, model string, in, out int) {
		t.Helper()
		_, err := s.store.AppendMessage(ctx, store.ChatMessage{ConversationID: conv.ID, Role: role, AgentID: agent, Model: model,
			PromptTokens: in, CompletionTokens: out, CreatedAt: at}, store.ActorAdmin)
		if err != nil {
			t.Fatal(err)
		}
	}
	now := c.now()
	add(now, "assistant", "", "big", 100, 10)
	add(now.Add(-time.Hour), "assistant", "", "small", 50, 5)
	add(now.AddDate(0, 0, -2), "assistant", "", "big", 1000, 100)
	add(now, "user", "", "", 7, 7)                            // the owner's message
	add(now, "assistant", "vega", "big", 9, 9)                // another agent in a shared conversation
	add(now.AddDate(0, 0, -40), "assistant", "", "big", 5, 5) // too old

	w := request(t, s, "GET", "/v1/agents/atlas/usage?days=30", adminTestToken, nil)
	if w.Code != 200 {
		t.Fatalf("usage: %d %s", w.Code, w.Body.String())
	}
	v := decode[UsageView](t, w)
	if v.Totals.Replies != 3 || v.Totals.PromptTokens != 1150 || v.Totals.CompletionTokens != 115 {
		t.Fatalf("totals: %+v", v.Totals)
	}
	if len(v.ByDay) != 2 || v.ByDay[0].PromptTokens != 1000 || v.ByDay[1].Replies != 2 {
		t.Fatalf("by day: %+v", v.ByDay)
	}
	if len(v.ByModel) != 2 || v.ByModel[0].Model != "big" || v.ByModel[0].Replies != 2 {
		t.Fatalf("by model: %+v", v.ByModel)
	}
	if w := request(t, s, "GET", "/v1/agents/ninguem/usage", adminTestToken, nil); w.Code != 404 {
		t.Fatalf("unknown: %d", w.Code)
	}
}

func TestArchiveAgent(t *testing.T) {
	s, _ := makeServer(t)
	if w := request(t, s, "POST", "/v1/agents/atlas/archive", adminTestToken, ArchiveRequest{Archived: true}); w.Code != 200 {
		t.Fatalf("archive: %d %s", w.Code, w.Body.String())
	}
	for _, a := range decode[[]AgentStatus](t, request(t, s, "GET", "/v1/agents", adminTestToken, nil)) {
		if a.ID == "atlas" {
			t.Fatal("archived agent still listed")
		}
	}
	archived := decode[[]store.Agent](t, request(t, s, "GET", "/v1/agents/archived", adminTestToken, nil))
	if len(archived) != 1 || archived[0].ID != "atlas" || archived[0].ArchivedAt == nil {
		t.Fatalf("archived: %+v", archived)
	}
	// It is still readable, so its old conversations show who it was.
	if a := decode[store.Agent](t, request(t, s, "GET", "/v1/agents/atlas", adminTestToken, nil)); a.ArchivedAt == nil {
		t.Fatal("get lost archived_at")
	}
	// Saving a new version keeps it archived.
	a, _ := s.store.GetAgent(context.Background(), "atlas")
	a.Persona = "outra"
	if saved, err := s.store.SaveAgent(context.Background(), a, store.ActorAdmin, time.Now()); err != nil || saved.ArchivedAt == nil {
		t.Fatalf("save: %v %+v", err, saved.ArchivedAt)
	}
	if w := request(t, s, "POST", "/v1/agents/atlas/archive", adminTestToken, ArchiveRequest{Archived: false}); w.Code != 200 {
		t.Fatalf("unarchive: %d", w.Code)
	}
	if list := decode[[]store.Agent](t, request(t, s, "GET", "/v1/agents/archived", adminTestToken, nil)); len(list) != 0 {
		t.Fatalf("still archived: %+v", list)
	}
}

func TestArchivedTeamIsNotReseeded(t *testing.T) {
	s, _ := makeServer(t)
	ctx := context.Background()
	team, _ := s.store.ListAgents(ctx)
	for _, a := range team {
		if _, err := s.store.ArchiveAgent(ctx, a.ID, true, store.ActorAdmin, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.EnsureTeam(ctx); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.store.ListAgents(ctx); len(list) != 0 {
		t.Fatalf("reseeded %d agents", len(list))
	}
}

func TestDocsByActor(t *testing.T) {
	s, _ := makeServer(t)
	ctx := context.Background()
	for _, d := range []store.Doc{{ID: "a", Title: "Do Atlas", Kind: "doc"}, {ID: "b", Title: "Meu", Kind: "doc"}} {
		actor := store.ActorAdmin
		if d.ID == "a" {
			actor = "agent:atlas"
		}
		if _, err := s.store.CreateDoc(ctx, d, "", actor, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	list := decode[[]store.Doc](t, request(t, s, "GET", "/v1/docs?actor=agent:atlas", adminTestToken, nil))
	if len(list) != 1 || list[0].ID != "a" {
		t.Fatalf("docs: %+v", list)
	}
}
