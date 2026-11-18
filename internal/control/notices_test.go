package control

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

func TestNoticesNoAIOfflineNodeAndPausedAgents(t *testing.T) {
	s, c := makeServer(t)
	ctx := store.WithWorkspace(context.Background(), store.DefaultWorkspaceID)

	// 1. Fresh server without any AI connected -> "no_ai" notice.
	initial := decode[[]Notice](t, request(t, s, "GET", "/v1/notices", adminTestToken, nil))
	if len(initial) != 1 || initial[0].Kind != "no_ai" || initial[0].Text != "Nenhuma IA conectada" || initial[0].Action != "modelos:connect" {
		t.Fatalf("initial notices = %+v", initial)
	}

	// 2. Connect an engine -> no notices at all.
	s.SetChatEngine(&fakeEngine{})
	afterAI := decode[[]Notice](t, request(t, s, "GET", "/v1/notices", adminTestToken, nil))
	if len(afterAI) != 0 {
		t.Fatalf("expected 0 notices after connecting AI, got %+v", afterAI)
	}

	// 3. Pair a node (starts offline until heartbeat) and pin Atlas + Iris to it.
	node := pairNode(t, s, "PC-Escritório", "windows")
	nodeID := node.NodeID
	for _, agID := range []string{"atlas", "iris"} {
		w := request(t, s, "PATCH", "/v1/agents/"+agID, adminTestToken, AgentRequest{HomeNodeID: &nodeID})
		if w.Code != 200 {
			t.Fatalf("patch %s home_node_id: %d %s", agID, w.Code, w.Body.String())
		}
	}

	offline := decode[[]Notice](t, request(t, s, "GET", "/v1/notices", adminTestToken, nil))
	if len(offline) != 1 || offline[0].Kind != "node_offline" || offline[0].Action != "nos" {
		t.Fatalf("expected node_offline notice, got %+v", offline)
	}
	wantText := "O computador PC-Escritório está desligado: Atlas e Iris esperam por ele"
	if offline[0].Text != wantText {
		t.Fatalf("offline notice text = %q, want %q", offline[0].Text, wantText)
	}

	// 4. Node sends heartbeat -> online -> notice disappears.
	request(t, s, "POST", "/v1/runtime/heartbeat", node.Token, HeartbeatRequest{})
	online := decode[[]Notice](t, request(t, s, "GET", "/v1/notices", adminTestToken, nil))
	if len(online) != 0 {
		t.Fatalf("expected 0 notices while node is online, got %+v", online)
	}

	// 5. Advance past OnlineTTL and pause Cora's autonomous execution.
	c.advance(OnlineTTL + 5*time.Second)
	canStart := false
	w := request(t, s, "PATCH", "/v1/agents/cora", adminTestToken, AgentRequest{
		Autonomy: &store.AgentAutonomy{CanStartAlone: &canStart},
	})
	if w.Code != 200 {
		t.Fatalf("pause cora: %d %s", w.Code, w.Body.String())
	}

	both := decode[[]Notice](t, request(t, s, "GET", "/v1/notices", adminTestToken, nil))
	if len(both) != 2 {
		t.Fatalf("expected 2 notices (node_offline + agents_paused), got %+v", both)
	}
	if both[0].Kind != "node_offline" || both[1].Kind != "agents_paused" || !strings.Contains(both[1].Text, "Cora") || both[1].Action != "team" {
		t.Fatalf("unexpected notices: %+v", both)
	}

	// 6. Archived agents do not count as waiting on an offline node or paused.
	if _, err := s.store.ArchiveAgent(ctx, "cora", true, store.ActorAdmin, c.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.ArchiveAgent(ctx, "iris", true, store.ActorAdmin, c.now()); err != nil {
		t.Fatal(err)
	}
	single := decode[[]Notice](t, request(t, s, "GET", "/v1/notices", adminTestToken, nil))
	if len(single) != 1 || single[0].Text != "O computador PC-Escritório está desligado: Atlas espera por ele" {
		t.Fatalf("expected singular offline notice for Atlas only, got %+v", single)
	}
}
