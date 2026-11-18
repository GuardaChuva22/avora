package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/scheduler"
)

func TestAgentHomeNodeWorkspaceAndProviderShare(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()

		// Nodes carry workspace_id from pairing codes and filter by workspace context.
		if err := s.CreatePairingCode(ctx, PairingCode{
			Hash:        []byte("code-pessoal"),
			Name:        "pc-casa",
			Kind:        scheduler.Windows,
			CreatedAt:   t0,
			ExpiresAt:   t0.Add(10 * time.Minute),
		}, ActorAdmin); err != nil {
			t.Fatal(err)
		}
		if err := s.CreatePairingCode(ctx, PairingCode{
			Hash:        []byte("code-equipe"),
			WorkspaceID: "equipe",
			Name:        "pc-estudio",
			Kind:        scheduler.Windows,
			CreatedAt:   t0,
			ExpiresAt:   t0.Add(10 * time.Minute),
		}, ActorAdmin); err != nil {
			t.Fatal(err)
		}

		n1, err := s.RedeemPairingCode(ctx, []byte("code-pessoal"), "node-1", []byte("tok-1"), t0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if n1.WorkspaceID != DefaultWorkspaceID {
			t.Fatalf("default workspace_id = %q, want %q", n1.WorkspaceID, DefaultWorkspaceID)
		}
		n2, err := s.RedeemPairingCode(ctx, []byte("code-equipe"), "node-2", []byte("tok-2"), t0.Add(2*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if n2.WorkspaceID != "equipe" {
			t.Fatalf("workspace_id = %q, want equipe", n2.WorkspaceID)
		}

		allNodes, err := s.ListNodes(ctx)
		if err != nil || len(allNodes) != 2 {
			t.Fatalf("unscoped ListNodes: %+v %v", allNodes, err)
		}
		teamNodes, err := s.ListNodes(WithWorkspace(ctx, "equipe"))
		if err != nil || len(teamNodes) != 1 || teamNodes[0].ID != "node-2" {
			t.Fatalf("scoped ListNodes(equipe): %+v %v", teamNodes, err)
		}

		// Agents persist home_node_id and home_dir across versions.
		saved, err := s.SaveAgent(ctx, Agent{
			ID:          "atlas",
			WorkspaceID: "equipe",
			Name:        "Atlas",
			Role:        "Dev",
			Engine:      "native",
			HomeNodeID:  n2.ID,
			HomeDir:     "projetos/atlas",
		}, ActorAdmin, t0)
		if err != nil {
			t.Fatal(err)
		}
		if saved.HomeNodeID != n2.ID || saved.HomeDir != "projetos/atlas" {
			t.Fatalf("saved agent home: %+v", saved)
		}

		got, err := s.GetAgent(ctx, "atlas")
		if err != nil {
			t.Fatal(err)
		}
		if got.HomeNodeID != n2.ID || got.HomeDir != "projetos/atlas" {
			t.Fatalf("get agent home: %+v", got)
		}

		saved2, err := s.SaveAgent(ctx, Agent{
			ID:          "atlas",
			WorkspaceID: "equipe",
			Name:        "Atlas",
			Role:        "Dev",
			Engine:      "native",
			HomeNodeID:  "",
			HomeDir:     "",
		}, ActorAdmin, t0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if saved2.HomeNodeID != "" || saved2.HomeDir != "" || saved2.Version != 2 {
			t.Fatalf("updated agent home: %+v", saved2)
		}
		versions, err := s.AgentVersions(ctx, "atlas")
		if err != nil || len(versions) != 2 || versions[1].Agent.HomeNodeID != n2.ID {
			t.Fatalf("versions: %+v %v", versions, err)
		}

		// Providers persist node_id and shared_with.
		prov, err := s.SaveProvider(ctx, Provider{
			ID:          "ollama.node-1",
			WorkspaceID: DefaultWorkspaceID,
			Kind:        "ollama",
			Name:        "Ollama (pc-casa)",
			BaseURL:     "http://127.0.0.1:11434/v1",
			NodeID:      n1.ID,
			Status:      "ok",
			Models:      []string{"llama3.2"},
			SharedWith:  []string{"equipe", "person-2"},
		}, ActorAdmin, t0)
		if err != nil {
			t.Fatal(err)
		}
		if prov.NodeID != n1.ID || !reflect.DeepEqual(prov.SharedWith, []string{"equipe", "person-2"}) {
			t.Fatalf("saved provider: %+v", prov)
		}
		gotProv, err := s.GetProvider(ctx, "ollama.node-1")
		if err != nil {
			t.Fatal(err)
		}
		if gotProv.NodeID != n1.ID || !reflect.DeepEqual(gotProv.SharedWith, []string{"equipe", "person-2"}) {
			t.Fatalf("get provider: %+v", gotProv)
		}
	})
}
