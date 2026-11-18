package store

import (
	"context"
	"testing"
	"time"
)

func TestStoreMemoriesPhase4(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if _, err := s.SaveAgent(ctx, Agent{ID: "atlas", WorkspaceID: "pessoal", Name: "Atlas"}, ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveAgent(ctx, Agent{ID: "outro", WorkspaceID: "empresa", Name: "Outro"}, ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}

		m1, err := s.AddMemory(ctx, AgentMemory{
			AgentID:  "atlas",
			Scope:    "agent",
			Kind:     "lesson",
			Content:  "Sempre rodar testes antes de entregar.",
			Source:   "conversa",
			Verified: false,
			Pinned:   false,
		}, ActorAdmin, t0)
		if err != nil {
			t.Fatal(err)
		}
		if m1.WorkspaceID != "pessoal" || m1.Kind != "lesson" || m1.Scope != "agent" || m1.Verified || m1.Occurrences != 1 {
			t.Fatalf("unexpected agent memory: %+v", m1)
		}

		teamMem, err := s.AddMemory(WithWorkspace(ctx, "pessoal"), AgentMemory{
			Scope:    "team",
			Kind:     "decision",
			Content:  "Deploy em produção somente às terças.",
			Source:   "dono",
			Verified: true,
			Pinned:   true,
		}, ActorAdmin, t0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if teamMem.Scope != "team" || teamMem.AgentID != "" || teamMem.WorkspaceID != "pessoal" || !teamMem.Pinned {
			t.Fatalf("unexpected team memory: %+v", teamMem)
		}

		if _, err := s.AddMemory(WithWorkspace(ctx, "empresa"), AgentMemory{
			Scope:    "team",
			Kind:     "fact",
			Content:  "Segredo do espaço empresa.",
			Source:   "dono",
			Verified: true,
		}, ActorAdmin, t0.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}

		combined, err := s.SearchMemories(ctx, MemoryFilter{
			WorkspaceID: "pessoal",
			AgentID:     "atlas",
			AgentOrTeam: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(combined) != 2 || combined[0].ID != teamMem.ID {
			t.Fatalf("expected pinned team memory + atlas memory, got: %+v", combined)
		}

		vTrue, archTrue := true, true
		occ := 3
		patched, err := s.PatchMemory(ctx, m1.ID, MemoryPatch{
			Verified:    &vTrue,
			Archived:    &archTrue,
			Occurrences: &occ,
		}, ActorAdmin, t0.Add(3*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if !patched.Verified || patched.ArchivedAt == nil || patched.Occurrences != 3 {
			t.Fatalf("unexpected patched memory: %+v", patched)
		}

		activeList, err := s.ListMemories(ctx, "atlas", "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(activeList) != 0 {
			t.Fatalf("archived memory should not appear in ListMemories: %+v", activeList)
		}

		archivedOnly, err := s.SearchMemories(ctx, MemoryFilter{
			WorkspaceID:  "pessoal",
			AgentID:      "atlas",
			ArchivedOnly: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(archivedOnly) != 1 || archivedOnly[0].ID != m1.ID {
			t.Fatalf("expected archived memory in ArchivedOnly search: %+v", archivedOnly)
		}
	})
}

func TestStoreSectorsAndAgentOrg(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := WithWorkspace(context.Background(), "pessoal")
		sec1, err := s.SaveSector(ctx, Sector{
			ID: "eng", WorkspaceID: "pessoal", Name: "Engenharia", Color: "#7fd1ff", Icon: "code", Position: 0,
		}, ActorAdmin, t0)
		if err != nil {
			t.Fatal(err)
		}
		sec2, err := s.SaveSector(ctx, Sector{
			ID: "ops", WorkspaceID: "pessoal", Name: "Operações", Color: "#86e3bd", Icon: "spark", Position: 1,
		}, ActorAdmin, t0.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}

		ag, err := s.SaveAgent(ctx, Agent{
			ID:          "atlas",
			WorkspaceID: "pessoal",
			Name:        "Atlas",
			Sector:      sec1.ID,
			ReportsTo:   "vega",
			Pinned:      true,
			Autonomy:    AgentAutonomy{CanStartAlone: Bool(false), MaxParallel: 3},
			Apps:        []string{"github", "slack"},
		}, ActorAdmin, t0.Add(2*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if ag.Sector != "eng" || ag.ReportsTo != "vega" || !ag.Pinned || ag.Autonomy.AllowsStartAlone() || ag.Autonomy.LimitParallel() != 3 || len(ag.Apps) != 2 {
			t.Fatalf("agent org/autonomy fields not saved: %+v", ag)
		}

		reordered, err := s.ReorderSectors(ctx, "pessoal", []string{sec2.ID, sec1.ID}, ActorAdmin, t0.Add(3*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if len(reordered) != 2 || reordered[0].ID != "ops" || reordered[1].ID != "eng" {
			t.Fatalf("unexpected sector order: %+v", reordered)
		}

		if err := s.DeleteSector(ctx, "eng", ActorAdmin, t0.Add(4*time.Second)); err != nil {
			t.Fatal(err)
		}
		afterDel, err := s.GetAgent(ctx, "atlas")
		if err != nil {
			t.Fatal(err)
		}
		if afterDel.Sector != "" {
			t.Fatalf("deleting sector should clear agent.Sector, got %q", afterDel.Sector)
		}
	})
}

func TestStoreProposals(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := WithWorkspace(context.Background(), "pessoal")
		if _, err := s.SaveAgent(ctx, Agent{ID: "iris", WorkspaceID: "pessoal", Name: "Iris"}, ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		p, err := s.CreateProposal(ctx, Proposal{
			ID:          "prop-1",
			WorkspaceID: "pessoal",
			FromAgent:   "atlas",
			TargetAgent: "iris",
			Field:       "instructions",
			Before:      "Antigo",
			After:       "Novo",
			Reason:      "Mais claro",
		}, ActorAdmin, t0)
		if err != nil {
			t.Fatal(err)
		}
		if p.Status != "pending" {
			t.Fatalf("expected pending status, got %q", p.Status)
		}
		decided := t0.Add(time.Minute)
		p.Status = "applied"
		p.PreVersion = 1
		p.AppliedVersion = 2
		p.DecidedAt = &decided
		updated, err := s.UpdateProposal(ctx, p, ActorAdmin, decided)
		if err != nil {
			t.Fatal(err)
		}
		if updated.Status != "applied" || updated.PreVersion != 1 || updated.AppliedVersion != 2 || updated.DecidedAt == nil {
			t.Fatalf("unexpected updated proposal: %+v", updated)
		}
		list, err := s.ListProposals(ctx, ProposalFilter{TargetAgent: "iris", Status: "applied"})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != "prop-1" {
			t.Fatalf("unexpected proposal list: %+v", list)
		}
	})
}
