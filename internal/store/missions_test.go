package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/scheduler"
)

func TestMissionsStore(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
		m, err := s.SaveMission(ctx, Mission{ID: "m1", AgentID: "cora", ConversationID: "c1", Title: "Fotos", Objective: "Organizar",
			Plan: []MissionStep{{Text: "Listar", Status: StepDoing}}, Status: MissionRunning, Spec: AgentSpec{Engine: "claude", Mode: "read"},
			Target: scheduler.VPS, NodeID: "n1", JobID: "j1", Runs: 1, WakePending: true}, ActorAdmin, t0)
		if err != nil || !m.CreatedAt.Equal(t0) || m.Engine != "claude" || m.Spec.Mode != "read" || len(m.Plan) != 1 {
			t.Fatalf("save: %+v %v", m, err)
		}
		if got, err := s.MissionByJob(ctx, "j1"); err != nil || got.ID != "m1" || !got.WakePending || got.Plan[0].Status != StepDoing {
			t.Fatalf("by job: %+v %v", got, err)
		}
		done := t0.Add(time.Hour)
		m.Status, m.JobID, m.Session, m.FinishedAt = MissionDone, "", "sess-1", &done
		if m, err = s.SaveMission(ctx, m, ActorAdmin, done); err != nil || !m.CreatedAt.Equal(t0) || m.Session != "sess-1" || m.FinishedAt == nil {
			t.Fatalf("update: %+v %v", m, err)
		}
		if _, err := s.MissionByJob(ctx, "j1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("old job: %v", err)
		}
		if list, _ := s.ListMissions(ctx, MissionFilter{ConversationID: "c1"}); len(list) != 1 {
			t.Fatalf("list: %+v", list)
		}
		if list, _ := s.ListMissions(ctx, MissionFilter{ActiveOnly: true}); len(list) != 0 {
			t.Fatalf("active: %+v", list)
		}
	})
}

func TestConversationSummaries(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
		if _, err := s.GetSummary(ctx, "c1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("none: %v", err)
		}
		if _, err := s.SaveSummary(ctx, ConversationSummary{ConversationID: "c1", Summary: "a", Upto: 5, Count: 1}, ActorSystem, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveSummary(ctx, ConversationSummary{ConversationID: "c1", Summary: "b", Upto: 9, Count: 2}, ActorSystem, t0); err != nil {
			t.Fatal(err)
		}
		if got, err := s.GetSummary(ctx, "c1"); err != nil || got.Summary != "b" || got.Upto != 9 || got.Count != 2 || !got.UpdatedAt.Equal(t0) {
			t.Fatalf("got: %+v %v", got, err)
		}
	})
}

func TestRoutineTriggerRoundTrip(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
		if _, err := s.SaveAgent(ctx, Agent{ID: "cora", Name: "Cora", Engine: "native", Mode: "read"}, ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		r, err := s.SaveRoutine(ctx, Routine{ID: "r1", AgentID: "cora", Title: "E-mail", Prompt: "x", Deliver: "time", Enabled: true,
			Trigger: &Trigger{Kind: "conector", Tool: "gmail_buscar", Args: map[string]any{"busca": "in:inbox"}, Every: 5, Seen: []string{"m1"}}}, ActorAdmin, t0)
		if err != nil || r.Trigger == nil || r.Trigger.Args["busca"] != "in:inbox" || len(r.Trigger.Seen) != 1 {
			t.Fatalf("save: %+v %v", r, err)
		}
		r.Trigger = nil
		if r, err = s.SaveRoutine(ctx, r, ActorAdmin, t0); err != nil || r.Trigger != nil {
			t.Fatalf("cleared: %+v %v", r.Trigger, err)
		}
	})
}
