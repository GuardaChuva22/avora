package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAgentMemories(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for _, id := range []string{"atlas", "iris"} {
			if _, err := s.SaveAgent(ctx, Agent{ID: id, Name: id, Engine: "native", Mode: "read"}, ActorSystem, t0); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.AddMemory(ctx, AgentMemory{AgentID: "ghost", Content: "x"}, ActorAdmin, t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown agent: %v", err)
		}
		a, err := s.AddMemory(ctx, AgentMemory{AgentID: "atlas", Content: "O dono prefere notebooks Lenovo", Source: "conversa"}, "agent:atlas", t0)
		if err != nil || a.ID == 0 || a.Source != "conversa" {
			t.Fatalf("add: %+v %v", a, err)
		}
		b, _ := s.AddMemory(ctx, AgentMemory{AgentID: "atlas", Content: "Orçamento máximo: R$ 4.000 (50% de 8_000)"}, "agent:atlas", t0.Add(time.Minute))
		_, _ = s.AddMemory(ctx, AgentMemory{AgentID: "iris", Content: "Lenovo também"}, "agent:iris", t0)

		all, err := s.ListMemories(ctx, "atlas", "", 0)
		if err != nil || len(all) != 2 || all[0].ID != b.ID {
			t.Fatalf("list: %+v %v", all, err)
		}
		if got, _ := s.ListMemories(ctx, "atlas", "lenovo NOTEBOOKS", 10); len(got) != 1 || got[0].ID != a.ID {
			t.Fatalf("search: %+v", got)
		}
		if got, _ := s.ListMemories(ctx, "atlas", "50%", 10); len(got) != 1 || got[0].ID != b.ID {
			t.Fatalf("search with %%: %+v", got)
		}
		if got, _ := s.ListMemories(ctx, "atlas", "lenovo dell", 10); len(got) != 0 {
			t.Fatalf("all words must match: %+v", got)
		}
		up, err := s.UpdateMemory(ctx, a.ID, "O dono prefere Lenovo ou Dell", ActorAdmin, t0.Add(2*time.Minute))
		if err != nil || up.Content != "O dono prefere Lenovo ou Dell" {
			t.Fatalf("update: %+v %v", up, err)
		}
		if all, _ = s.ListMemories(ctx, "atlas", "", 1); len(all) != 1 || all[0].ID != a.ID {
			t.Fatalf("newest first after update: %+v", all)
		}
		if _, err := s.DeleteMemory(ctx, b.ID, ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DeleteMemory(ctx, b.ID, ActorAdmin, t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete twice: %v", err)
		}
		if all, _ = s.ListMemories(ctx, "atlas", "", 0); len(all) != 1 {
			t.Fatalf("after delete: %+v", all)
		}
	})
}
