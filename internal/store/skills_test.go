package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSkillsAndRoutineSkill(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
		sk, err := s.SaveSkill(ctx, Skill{ID: "relatorio-mensal", Name: "relatorio-mensal", Description: "Fecha o mês",
			Body: "1. Some os gastos", Author: ActorAdmin}, ActorAdmin, t0)
		if err != nil || !sk.CreatedAt.Equal(t0) {
			t.Fatalf("save: %+v %v", sk, err)
		}
		sk.Body, sk.ProjectID = "1. Some tudo", "trabalho"
		if sk, err = s.SaveSkill(ctx, sk, ActorAdmin, t0.Add(time.Hour)); err != nil || !sk.CreatedAt.Equal(t0) || sk.ProjectID != "trabalho" {
			t.Fatalf("update: %+v %v", sk, err)
		}
		if list, _ := s.ListSkills(ctx); len(list) != 1 || list[0].Body != "1. Some tudo" {
			t.Fatalf("list: %+v", list)
		}

		if _, err := s.SaveAgent(ctx, Agent{ID: "atlas", Name: "Atlas", Engine: "native", Mode: "read"}, ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		rt, err := s.SaveRoutine(ctx, Routine{ID: "r1", AgentID: "atlas", Title: "Mês", Prompt: "Feche o mês", Skill: "relatorio-mensal",
			Schedule: Schedule{At: "08:00"}, Deliver: "time", Enabled: true}, ActorAdmin, t0)
		if err != nil || rt.Skill != "relatorio-mensal" {
			t.Fatalf("routine: %+v %v", rt, err)
		}

		if err := s.DeleteSkill(ctx, "relatorio-mensal", ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetSkill(ctx, "relatorio-mensal"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted: %v", err)
		}
	})
}
