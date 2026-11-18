package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestHomeWaitingWorkingDeliveredAndLastHomeAt(t *testing.T) {
	s, c := makeServer(t)
	eng := &fakeEngine{}
	s.SetChatEngine(eng)

	ana := signupPerson(t, s, "", SignupRequest{
		Email:    "ana@exemplo.com",
		Name:     "Ana Silva",
		Password: "senha-muito-forte",
	})

	about := "Cuido de produto, operações e clientes do estúdio."
	patched := decode[store.Person](t, request(t, s, "PATCH", "/v1/me", ana.Token, PatchMeRequest{About: &about}))
	if patched.About != about {
		t.Fatalf("about not saved: %+v", patched)
	}

	ctx := store.WithWorkspace(context.Background(), store.DefaultWorkspaceID)

	// Direct conversation with Iris so she becomes last_agent, and owner profile enters instructions.
	conv := decode[store.Conversation](t, request(t, s, "POST", "/v1/conversations", ana.Token, ConversationRequest{AgentID: "iris"}))
	request(t, s, "POST", "/v1/conversations/"+conv.ID+"/messages", ana.Token, MessageRequest{Content: "oi"})
	if !strings.Contains(eng.system, "Ana Silva") || !strings.Contains(eng.system, about) {
		t.Fatalf("owner profile missing from agent instructions: %q", eng.system)
	}

	// Unanswered perguntar step on Iris's latest message -> waiting question.
	askJSON, _ := json.Marshal([]AgentStep{{
		Tool:    StepAsk,
		Summary: "Prefere tabela ou resumo?",
		Detail:  `{"opcoes":["Tabela","Resumo curto"],"multipla":false,"outra":true}`,
	}})
	if _, err := s.store.AppendMessage(ctx, store.ChatMessage{
		ConversationID: conv.ID,
		Role:           "assistant",
		AgentID:        "iris",
		Content:        "Me diga como prefere:",
		Steps:          askJSON,
		CreatedAt:      c.now(),
	}, "agent:iris"); err != nil {
		t.Fatal(err)
	}

	// Pending approval for Atlas -> waiting approval.
	if _, err := s.store.CreateApproval(ctx, store.Approval{
		ID:             "ap-home-1",
		AgentID:        "atlas",
		ConversationID: conv.ID,
		TurnID:         "turn-1",
		Action:         "claude_code.edit",
		Summary:        "Claude Code vai editar internal/server.go",
		CreatedAt:      c.now(),
		ExpiresAt:      c.now().Add(15 * time.Minute),
	}, "agent:atlas"); err != nil {
		t.Fatal(err)
	}

	// Waiting mission for Vega -> waiting decision.
	if _, err := s.store.SaveMission(ctx, store.Mission{
		ID:             "mis-home-1",
		AgentID:        "vega",
		ConversationID: conv.ID,
		Title:          "Organizar contratos",
		Objective:      "Separar contratos por cliente",
		Status:         store.MissionWaiting,
		Summary:        "Devo arquivar os contratos de 2024?",
		Plan:           []store.MissionStep{{Text: "Listar arquivos", Status: store.StepDone}},
		CreatedAt:      c.now(),
		UpdatedAt:      c.now(),
	}, "agent:vega", c.now()); err != nil {
		t.Fatal(err)
	}

	// Active CLI run for Cora -> working.
	node := pairNode(t, s, "pc-ana", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", node.Token, HeartbeatRequest{})
	startedAt := c.now().Add(-2 * time.Minute)
	if _, err := s.store.CreateJob(ctx, Job{
		ID:        "run-working-cora",
		Action:    store.ActionAgentRun,
		Target:    scheduler.Windows,
		DeviceID:  node.NodeID,
		CreatedAt: startedAt,
		Agent: &store.AgentSpec{
			Engine:     "claude",
			Mode:       "read",
			MaxMinutes: 20,
			Prompt:     "Conferir planilha de despesas",
			Owner:      "cora",
			Title:      "Conferir planilha de despesas",
		},
	}, "teste", "agent:cora"); err != nil {
		t.Fatal(err)
	}

	// Completed CLI run for Nico (1 hour ago) -> delivered on first visit.
	doneAt := c.now().Add(-1 * time.Hour)
	if _, err := s.store.CreateJob(ctx, Job{
		ID:        "run-done-nico",
		Action:    store.ActionAgentRun,
		Target:    scheduler.Windows,
		DeviceID:  node.NodeID,
		CreatedAt: doneAt.Add(-5 * time.Minute),
		Agent: &store.AgentSpec{
			Engine:     "codex",
			Mode:       "read",
			MaxMinutes: 20,
			Prompt:     "Levantar canais parceiros",
			Owner:      "nico",
			Title:      "Levantar canais parceiros",
		},
	}, "teste", "agent:nico"); err != nil {
		t.Fatal(err)
	}
	// Claim until the runtime holds Nico's run (Cora's queued run may come first).
	for i := 0; i < 5; i++ {
		j, err := s.store.ClaimJob(ctx, node.NodeID, doneAt.Add(-4*time.Minute), JobLease, store.ClaimSlots{Agents: true, CodexMain: true, CodexNested: true})
		if err != nil {
			t.Fatal(err)
		}
		if j == nil || j.ID == "run-done-nico" {
			break
		}
	}
	if _, err := s.store.CompleteJob(ctx, "run-done-nico", node.NodeID, store.Result{Success: true, Output: "12 canais encontrados."}, doneAt); err != nil {
		t.Fatal(err)
	}

	// First visit to GET /v1/home.
	home1 := decode[HomeResponse](t, request(t, s, "GET", "/v1/home", ana.Token, nil))
	if home1.GreetingName != "Ana" {
		t.Fatalf("greeting_name = %q, want Ana", home1.GreetingName)
	}
	if home1.LastAgent != "iris" {
		t.Fatalf("last_agent = %q, want iris", home1.LastAgent)
	}
	if len(home1.Waiting) != 3 {
		t.Fatalf("waiting = %+v, want 3 items", home1.Waiting)
	}
	if len(home1.Working) != 1 || home1.Working[0].AgentID != "cora" || home1.Working[0].RunID != "run-working-cora" {
		t.Fatalf("working = %+v", home1.Working)
	}
	if len(home1.Delivered) != 1 || home1.Delivered[0].AgentID != "nico" {
		t.Fatalf("delivered on first visit = %+v", home1.Delivered)
	}
	if len(home1.NextRoutines) > 3 {
		t.Fatalf("next_routines = %+v", home1.NextRoutines)
	}

	// Second visit 1 minute later: last_home_at moved forward, so nothing new was delivered.
	c.advance(time.Minute)
	home2 := decode[HomeResponse](t, request(t, s, "GET", "/v1/home", ana.Token, nil))
	if len(home2.Delivered) != 0 {
		t.Fatalf("expected 0 delivered after last_home_at bump, got %+v", home2.Delivered)
	}

	// Complete Cora's run after second visit -> shows up on third visit!
	c.advance(30 * time.Second)
	request(t, s, "POST", "/v1/runtime/heartbeat", node.Token, HeartbeatRequest{})
	// Cora's run may already be held from the earlier claim loop.
	if claimed, err := s.store.ClaimJob(ctx, node.NodeID, c.now(), JobLease, store.ClaimSlots{Agents: true, CodexMain: true, CodexNested: true}); err != nil || claimed != nil && claimed.ID != "run-working-cora" {
		t.Fatalf("claim cora run: %v %+v", err, claimed)
	}
	c.advance(10 * time.Second)
	if _, err := s.store.CompleteJob(ctx, "run-working-cora", node.NodeID, store.Result{Success: true, Output: "Planilha conferida sem divergências."}, c.now()); err != nil {
		t.Fatal(err)
	}
	c.advance(time.Second)
	home3 := decode[HomeResponse](t, request(t, s, "GET", "/v1/home", ana.Token, nil))
	if len(home3.Delivered) != 1 || home3.Delivered[0].AgentID != "cora" {
		t.Fatalf("expected cora delivery on third visit, got %+v", home3.Delivered)
	}
}

func TestAgentStatusesAndStopRun(t *testing.T) {
	s, c := makeServer(t)
	ctx := store.WithWorkspace(context.Background(), store.DefaultWorkspaceID)

	node := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", node.Token, HeartbeatRequest{})

	// Atlas waits on approval.
	if _, err := s.store.CreateApproval(ctx, store.Approval{
		ID:        "ap-status-1",
		AgentID:   "atlas",
		TurnID:    "t1",
		Action:    "claude_code.edit",
		Summary:   "Editar main.go",
		CreatedAt: c.now(),
		ExpiresAt: c.now().Add(15 * time.Minute),
	}, "agent:atlas"); err != nil {
		t.Fatal(err)
	}

	// Cora runs a CLI job.
	if _, err := s.store.CreateJob(ctx, Job{
		ID:        "run-stop-cora",
		Action:    store.ActionAgentRun,
		Target:    scheduler.Windows,
		DeviceID:  node.NodeID,
		CreatedAt: c.now(),
		Agent: &store.AgentSpec{
			Engine:     "claude",
			Mode:       "read",
			MaxMinutes: 20,
			Prompt:     "Gerar relatório mensal",
			Owner:      "cora",
			Title:      "Gerar relatório mensal",
		},
	}, "teste", "agent:cora"); err != nil {
		t.Fatal(err)
	}

	// Vega is answering in #time.
	s.channel.mu.Lock()
	s.runsLocked(TeamChannelID)["vega"] = &channelRun{By: ownerName, Since: c.now(), cancel: func() {}}
	s.channel.mu.Unlock()

	team := decode[[]AgentStatus](t, request(t, s, "GET", "/v1/agents", adminTestToken, nil))
	byID := map[string]AgentStatus{}
	for _, a := range team {
		byID[a.ID] = a
	}
	if byID["atlas"].Status != "waiting" {
		t.Fatalf("atlas status = %+v, want waiting", byID["atlas"])
	}
	if byID["cora"].Status != "working" || !byID["cora"].Working {
		t.Fatalf("cora status = %+v, want working", byID["cora"])
	}
	if byID["vega"].Status != "working" || byID["vega"].StatusChannel != "#time" {
		t.Fatalf("vega status = %+v, want working in #time", byID["vega"])
	}
	if byID["nico"].Status != "free" || byID["nico"].Working {
		t.Fatalf("nico status = %+v, want free", byID["nico"])
	}

	// Stop Cora's run via POST /v1/runs/{id}/stop.
	w := request(t, s, "POST", "/v1/runs/run-stop-cora/stop", adminTestToken, nil)
	if w.Code != 200 {
		t.Fatalf("stop run status=%d: %s", w.Code, w.Body.String())
	}
	stopped := decode[RunView](t, w)
	if stopped.Status != store.StatusCanceled || stopped.Active {
		t.Fatalf("stopped run = %+v", stopped)
	}
	detail := decode[RunView](t, request(t, s, "GET", "/v1/runs/run-stop-cora", adminTestToken, nil))
	if len(detail.Steps) == 0 || !strings.Contains(detail.Steps[len(detail.Steps)-1].Summary, "parada") {
		t.Fatalf("expected stop step recorded on run: %+v", detail.Steps)
	}
}

func TestGetMyUsageTool(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{
		"Atlas": {call("get_my_usage", map[string]any{})},
	}}
	s := chatServer(t, e)

	node := pairNode(t, s, "pc-cota", "windows")
	reset5h := s.now().Add(2 * time.Hour).Unix()
	resetWeek := s.now().Add(48 * time.Hour).Unix()
	w := request(t, s, "POST", "/v1/runtime/heartbeat", node.Token, HeartbeatRequest{
		Quotas: []Quota{{
			Engine:    "claude",
			Plan:      "max",
			FiveHour:  &QuotaWindow{UsedPercent: 35, ResetsAt: reset5h},
			Weekly:    &QuotaWindow{UsedPercent: 60, ResetsAt: resetWeek},
			CheckedAt: s.now(),
		}},
	})
	if w.Code != 200 {
		t.Fatalf("heartbeat: %d %s", w.Code, w.Body.String())
	}

	ctx := store.WithWorkspace(context.Background(), store.DefaultWorkspaceID)
	atlas, err := s.store.GetAgent(ctx, "atlas")
	if err != nil {
		t.Fatal(err)
	}
	atlas.Brain = store.AgentBrain{Provider: claudeBrain, Model: "sonnet"}
	if _, err := s.store.SaveAgent(ctx, atlas, store.ActorAdmin, s.now()); err != nil {
		t.Fatal(err)
	}

	// Ensure get_my_usage is exposed and returns both agent usage and Claude quota.
	conv := agentConversation(t, s, "atlas")
	if _, err := s.store.AppendMessage(ctx, store.ChatMessage{
		ConversationID:   conv.ID,
		Role:             "assistant",
		AgentID:          "atlas",
		Content:          "resposta anterior",
		Model:            "sonnet",
		PromptTokens:     1200,
		CompletionTokens: 300,
		CreatedAt:        s.now(),
	}, "agent:atlas"); err != nil {
		t.Fatal(err)
	}

	// Switch Atlas back to Automático so toolEngine runs the turn while still checking tool output directly.
	out, err := s.toolGetMyUsage(ctx, &turn{agent: atlas, id: "t-usage", conv: conv.ID})
	if err != nil {
		t.Fatalf("toolGetMyUsage error: %v", err)
	}
	for _, want := range []string{"Uso de Atlas", "1500 tokens", "35% usado", "60% usado", "MAX"} {
		if !strings.Contains(out, want) {
			t.Fatalf("get_my_usage output missing %q:\n%s", want, out)
		}
	}

	// Also verify get_my_usage is declared in toolsFor and callable through runTurn.
	atlas.Brain = store.AgentBrain{}
	_, _ = s.store.SaveAgent(ctx, atlas, store.ActorAdmin, s.now())
	resp := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+conv.ID+"/messages", adminTestToken,
		MessageRequest{Content: "como está meu limite?"}))
	if !contains(e.tools, "get_my_usage") || !strings.Contains(resp.Assistant.Content, "Uso de Atlas") {
		t.Fatalf("get_my_usage not called in turn: tools=%v content=%q", e.tools, resp.Assistant.Content)
	}
}
