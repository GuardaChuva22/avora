package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestAgentsAreVersioned(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		a := Agent{ID: "atlas", Name: "Atlas", Role: "Dev", Persona: "p1", Engine: "claude", Mode: "edit", Color: "#7fd1ff", Position: 1}
		v1, err := s.SaveAgent(ctx, a, ActorSystem, t0)
		if err != nil || v1.Version != 1 || !v1.CreatedAt.Equal(t0) {
			t.Fatalf("v1: %+v %v", v1, err)
		}
		a.Persona, a.Engine = "p2", "codex"
		v2, err := s.SaveAgent(ctx, a, ActorAdmin, t0.Add(time.Hour))
		if err != nil || v2.Version != 2 || !v2.CreatedAt.Equal(t0) || !v2.UpdatedAt.Equal(t0.Add(time.Hour)) {
			t.Fatalf("v2: %+v %v", v2, err)
		}
		if _, err := s.SaveAgent(ctx, Agent{ID: "vega", Name: "Vega", Engine: "native", Mode: "read"}, ActorSystem, t0); err != nil {
			t.Fatal(err)
		}
		list, err := s.ListAgents(ctx)
		if err != nil || len(list) != 2 || list[0].ID != "vega" || list[1].Persona != "p2" {
			t.Fatalf("list: %+v %v", list, err)
		}
		versions, err := s.AgentVersions(ctx, "atlas")
		if err != nil || len(versions) != 2 || versions[0].Version != 2 || versions[1].Agent.Persona != "p1" || versions[1].Agent.Engine != "claude" {
			t.Fatalf("versions: %+v %v", versions, err)
		}
		if _, err := s.GetAgent(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing agent: %v", err)
		}
	})
}

func TestConversationJobIsAnsweredOnce(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for _, c := range []Conversation{
			{ID: "c1", AgentID: "atlas", Engine: "claude", CreatedAt: t0},
			{ID: "c2", AgentID: "vega", Engine: "native", Model: "m", CreatedAt: t0},
		} {
			if _, err := s.CreateConversation(ctx, c, ActorAdmin); err != nil {
				t.Fatal(err)
			}
		}
		if list, err := s.ListConversations(ctx, "atlas", 10); err != nil || len(list) != 1 || list[0].ID != "c1" || list[0].AgentID != "atlas" {
			t.Fatalf("by agent: %+v %v", list, err)
		}
		if err := s.StartConversationJob(ctx, "c1", "j1"); err != nil {
			t.Fatal(err)
		}
		if err := s.StartConversationJob(ctx, "c1", "j2"); !errors.Is(err, ErrConflict) {
			t.Fatalf("second job while busy: %v", err)
		}
		if err := s.StartConversationJob(ctx, "missing", "j3"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing conversation: %v", err)
		}
		steps := json.RawMessage(`[{"tool":"Read","summary":"file_path: a.go"}]`)
		answer := ChatMessage{Role: "assistant", Content: "pronto,\n  feito", Steps: steps, CreatedAt: t0.Add(time.Minute)}
		msg, err := s.FinishConversationJob(ctx, "c1", "j1", answer, "sess-1", "node-pc", "node:node-pc")
		if err != nil || msg.JobID != "j1" || msg.ConversationID != "c1" || string(msg.Steps) == "" {
			t.Fatalf("finish: %+v %v", msg, err)
		}
		if _, err := s.FinishConversationJob(ctx, "c1", "j1", answer, "", "", ActorSystem); !errors.Is(err, ErrConflict) {
			t.Fatalf("second finish: %v", err)
		}
		c, err := s.GetConversation(ctx, "c1")
		if err != nil || c.JobID != "" || c.SessionID != "sess-1" || c.SessionNode != "node-pc" || c.Preview != "pronto, feito" || c.LastRole != "assistant" {
			t.Fatalf("conversation: %+v %v", c, err)
		}
		// A failed answer keeps the session it had and previews the error.
		if err := s.StartConversationJob(ctx, "c1", "j2"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.FinishConversationJob(ctx, "c1", "j2", ChatMessage{Role: "assistant", Error: "parado", CreatedAt: t0.Add(2 * time.Minute)}, "", "", ActorSystem); err != nil {
			t.Fatal(err)
		}
		c, _ = s.GetConversation(ctx, "c1")
		if c.SessionID != "sess-1" || c.Preview != "parado" {
			t.Fatalf("after failure: %+v", c)
		}
		msgs, err := s.Messages(ctx, "c1")
		if err != nil || len(msgs) != 2 || msgs[1].Error != "parado" || len(msgs[1].Steps) != 0 {
			t.Fatalf("messages: %+v %v", msgs, err)
		}
	})
}

func TestRoutinesAndResponsibilities(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		resp := []Responsibility{{ID: "r1", Goal: "Gastos abaixo do orçamento", Metric: "gasto/orçamento", Cadence: "diária"}}
		a, err := s.SaveAgent(ctx, Agent{ID: "cora", Name: "Cora", Engine: "native", Mode: "read", Responsibilities: resp}, ActorSystem, t0)
		if err != nil || len(a.Responsibilities) != 1 || a.Responsibilities[0].Goal != resp[0].Goal {
			t.Fatalf("agent: %+v %v", a, err)
		}
		if got, _ := s.GetAgent(ctx, "cora"); len(got.Responsibilities) != 1 || got.Responsibilities[0].Cadence != "diária" {
			t.Fatalf("stored responsibilities: %+v", got.Responsibilities)
		}
		if v, _ := s.SaveAgent(ctx, Agent{ID: "vega", Name: "Vega", Engine: "native", Mode: "read"}, ActorSystem, t0); v.Responsibilities == nil {
			t.Fatal("responsibilities must be an empty list, not null")
		}

		next := t0.Add(8 * time.Hour)
		r := Routine{ID: "rt1", AgentID: "cora", ResponsibilityID: "r1", Title: "Checar gastos", Prompt: "Como estão os gastos?",
			Schedule: Schedule{At: "08:00", Days: []int{1, 3}}, Deliver: "time", Enabled: true, NextRun: &next}
		saved, err := s.SaveRoutine(ctx, r, ActorAdmin, t0)
		if err != nil || saved.Schedule.At != "08:00" || len(saved.Schedule.Days) != 2 || !saved.NextRun.Equal(next) || saved.LastRun != nil {
			t.Fatalf("saved: %+v %v", saved, err)
		}
		ran := t0.Add(9 * time.Hour)
		saved.LastRun, saved.LastStatus, saved.LastPreview, saved.Failures = &ran, "failed", "erro", 2
		if _, err := s.SaveRoutine(ctx, saved, "", ran); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetRoutine(ctx, "rt1")
		if err != nil || got.LastStatus != "failed" || got.Failures != 2 || !got.LastRun.Equal(ran) || !got.CreatedAt.Equal(t0) {
			t.Fatalf("got: %+v %v", got, err)
		}
		if _, err := s.SaveRoutine(ctx, Routine{ID: "rt2", AgentID: "nobody", Title: "x", Prompt: "x", Schedule: Schedule{Every: 60}, Deliver: "time"}, ActorAdmin, t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("routine of a missing agent: %v", err)
		}
		if list, _ := s.ListRoutines(ctx, "vega"); len(list) != 0 {
			t.Fatalf("vega's routines: %+v", list)
		}
		if list, _ := s.ListRoutines(ctx, ""); len(list) != 1 {
			t.Fatalf("all routines: %+v", list)
		}
		if err := s.DeleteRoutine(ctx, "rt1", ActorAdmin, ran); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetRoutine(ctx, "rt1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted: %v", err)
		}
		if err := s.DeleteRoutine(ctx, "rt1", ActorAdmin, ran); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete twice: %v", err)
		}
	})
}

func TestApprovalsAndGrants(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if _, err := s.SaveAgent(ctx, Agent{ID: "atlas", Name: "Atlas", Engine: "native", Mode: "read"}, ActorSystem, t0); err != nil {
			t.Fatal(err)
		}
		mk := func(id string) Approval {
			a, err := s.CreateApproval(ctx, Approval{ID: id, AgentID: "atlas", ConversationID: "c1", TurnID: "t1",
				Action: "claude_code.edit", Summary: "editar backup.py", CreatedAt: t0, ExpiresAt: t0.Add(15 * time.Minute)}, "agent:atlas")
			if err != nil || a.Status != "pending" || a.DecidedAt != nil {
				t.Fatalf("create %s: %+v %v", id, a, err)
			}
			return a
		}
		mk("a1")
		mk("a2")
		if _, err := s.CreateApproval(ctx, Approval{ID: "a1", AgentID: "atlas", Action: "x", Summary: "x", CreatedAt: t0, ExpiresAt: t0}, "agent:atlas"); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate: %v", err)
		}
		if list, _ := s.ListApprovals(ctx, "pending", 10); len(list) != 2 {
			t.Fatalf("pending: %+v", list)
		}

		// Denied once: no grant. Approved "always": a grant, once.
		if d, err := s.DecideApproval(ctx, "a1", "denied", true, ActorAdmin, t0.Add(time.Minute)); err != nil || d.Status != "denied" || d.Always {
			t.Fatalf("deny: %+v %v", d, err)
		}
		if _, err := s.DecideApproval(ctx, "a1", "approved", false, ActorAdmin, t0); !errors.Is(err, ErrConflict) {
			t.Fatalf("second decision: %v", err)
		}
		if _, err := s.DecideApproval(ctx, "nope", "approved", false, ActorAdmin, t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
		ok, err := s.DecideApproval(ctx, "a2", "approved", true, ActorAdmin, t0.Add(2*time.Minute))
		if err != nil || ok.Status != "approved" || !ok.Always || ok.DecidedBy != ActorAdmin || ok.DecidedAt == nil {
			t.Fatalf("approve: %+v %v", ok, err)
		}
		grants, _ := s.ListGrants(ctx, "atlas")
		if len(grants) != 1 || grants[0].Action != "claude_code.edit" {
			t.Fatalf("grants: %+v", grants)
		}
		mk("a3")
		if _, err := s.DecideApproval(ctx, "a3", "approved", true, ActorAdmin, t0.Add(3*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if grants, _ = s.ListGrants(ctx, ""); len(grants) != 1 {
			t.Fatalf("grant duplicated: %+v", grants)
		}
		if err := s.DeleteGrant(ctx, grants[0].ID, ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteGrant(ctx, grants[0].ID, ActorAdmin, t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("second delete: %v", err)
		}
		got, _ := s.GetApproval(ctx, "a2")
		if got.Status != "approved" || !got.Always {
			t.Fatalf("get: %+v", got)
		}
		if all, _ := s.ListApprovals(ctx, "", 2); len(all) != 2 || all[0].ID != "a3" {
			t.Fatalf("list all: %+v", all)
		}
	})
}
