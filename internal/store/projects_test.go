package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProjectsAndMovingAgents(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
		if _, err := s.SaveAgent(ctx, Agent{ID: "atlas", Name: "Atlas", Engine: "native", Mode: "read"}, ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		p, err := s.SaveProject(ctx, Project{ID: "trabalho", Name: "Trabalho", Instructions: "# Regras",
			Folders: []ProjectFolder{{Node: "pc", Path: `C:\Trabalho`}, {Node: "vps", Path: "projects/trabalho/docs", Label: "docs"}}}, ActorAdmin, t0)
		if err != nil || len(p.Folders) != 2 || !p.CreatedAt.Equal(t0) {
			t.Fatalf("save: %+v %v", p, err)
		}
		if _, err := s.SaveProject(ctx, Project{ID: "lado", Name: "Lado", Position: -1}, ActorAdmin, t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if list, _ := s.ListProjects(ctx); len(list) != 2 || list[0].ID != "lado" || list[1].Folders[1].Label != "docs" {
			t.Fatalf("list: %+v", list)
		}

		a, err := s.MoveAgent(ctx, "atlas", "trabalho", ActorAdmin, t0)
		if err != nil || a.ProjectID != "trabalho" || a.Version != 1 {
			t.Fatalf("move: %+v %v", a, err)
		}
		// Saving the agent (a new version) keeps its project.
		a.Persona = "nova"
		if a, err = s.SaveAgent(ctx, a, ActorAdmin, t0.Add(time.Hour)); err != nil || a.ProjectID != "trabalho" {
			t.Fatalf("save keeps project: %+v %v", a, err)
		}
		if _, err := s.MoveAgent(ctx, "atlas", "nada", ActorAdmin, t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown project: %v", err)
		}
		if err := s.DeleteProject(ctx, "trabalho", ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		if a, _ := s.GetAgent(ctx, "atlas"); a.ProjectID != "" {
			t.Fatalf("agent of a deleted project: %+v", a)
		}
		if _, err := s.GetProject(ctx, "trabalho"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("after delete: %v", err)
		}
	})
}
