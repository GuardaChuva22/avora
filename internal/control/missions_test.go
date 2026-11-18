package control

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func setCalls(e *toolEngine, name string, calls ...llm.ToolCall) {
	e.mu.Lock()
	e.calls[name] = calls
	e.mu.Unlock()
}

func waitMission(t *testing.T, s *Server, id string, ok func(store.Mission) bool) store.Mission {
	t.Helper()
	var m store.Mission
	for i := 0; i < 400; i++ {
		m, _ = s.store.GetMission(t.Context(), id)
		if ok(m) {
			return m
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("mission never got there: %+v", m)
	return m
}

func TestMissionRunsInTheBackgroundAndWakesTheAgent(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	setCalls(e, "Cora", call("missao_iniciar", map[string]any{"titulo": "Organizar fotos", "objetivo": "Organize as fotos por ano.",
		"passos": []string{"Listar as fotos", "Mover por ano"}}))
	s := chatServer(t, e)
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)
	c := agentConversation(t, s, "cora")

	// The turn ends at once; the run waits on the node.
	r := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "organiza"}))
	if !strings.Contains(r.Assistant.Content, "começou em segundo plano") {
		t.Fatalf("answer: %s", r.Assistant.Content)
	}
	job := waitClaim(t, s, vps.Token)
	if job.Agent.Engine != "claude" || job.Agent.MaxMinutes != MaxAgentMinutes || job.Agent.ConversationID != "" ||
		!strings.Contains(job.Agent.Prompt, "1. [~] Listar as fotos") || !strings.Contains(job.Agent.Prompt, "2. [ ] Mover por ano") {
		t.Fatalf("job: %+v", job.Agent)
	}
	list := decode[[]store.Mission](t, request(t, s, "GET", "/v1/missions?conversation="+c.ID, adminTestToken, nil))
	if len(list) != 1 || list[0].Status != store.MissionRunning || list[0].Runs != 1 || list[0].Engine != "claude" {
		t.Fatalf("missions: %+v", list)
	}
	id := list[0].ID
	// The round is a run: a thread under Cora, with what the CLI says.
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/steps", vps.Token, AgentStep{Tool: StepText, Summary: "Vou listar as fotos primeiro."})
	runs := decode[[]RunView](t, request(t, s, "GET", "/v1/runs?active=1&agent=cora", adminTestToken, nil))
	if len(runs) != 1 || runs[0].ID != job.ID || runs[0].Mission != id || !strings.Contains(runs[0].Title, "Organizar fotos") {
		t.Fatalf("runs: %+v", runs)
	}
	run := decode[RunView](t, request(t, s, "GET", "/v1/runs/"+job.ID, adminTestToken, nil))
	if len(run.Steps) != 1 || run.Steps[0].Summary != "Vou listar as fotos primeiro." || !strings.Contains(run.Prompt, "Organize as fotos") {
		t.Fatalf("run: %+v", run)
	}
	if steps := s.jobSteps(t.Context(), job.ID); steps != nil {
		t.Fatalf("what the CLI says stays out of the agent's steps: %s", steps)
	}
	if block := s.missionsBlock(t.Context(), store.Agent{ID: "cora"}); !strings.Contains(block, "«Organizar fotos» (id "+id+", rodando") {
		t.Fatalf("block: %s", block)
	}

	// The run ends: Cora is woken with the result, marks step 1 and starts
	// round 2 on the same CLI session.
	setCalls(e, "Cora", call("missao_atualizar", map[string]any{"continuar": "Agora mova as fotos.", "plano": []map[string]any{
		{"texto": "Listar as fotos", "status": "feito"}, {"texto": "Mover por ano", "status": "pendente"}}}))
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", vps.Token,
		JobResult{Success: true, Output: "Listei 40 fotos.", Stats: json.RawMessage(`{"session_id":"sess-1"}`)})
	job2 := waitClaim(t, s, vps.Token)
	if job2.Agent.Resume != "sess-1" || !strings.Contains(job2.Agent.Prompt, "Agora mova as fotos.") ||
		!strings.Contains(job2.Agent.Prompt, "1. [x] Listar as fotos") || !strings.Contains(job2.Agent.Prompt, "2. [~] Mover por ano") {
		t.Fatalf("round 2: %+v", job2.Agent)
	}
	msgs, _ := s.store.Messages(t.Context(), c.ID)
	report := false
	for _, m := range msgs {
		report = report || (m.Role == "user" && strings.HasPrefix(m.Content, missionPrefix) && strings.Contains(m.Content, "Listei 40 fotos."))
	}
	if !report {
		t.Fatalf("no report in the conversation: %+v", msgs)
	}

	// Round 2 ends and Cora finishes the mission.
	waitMission(t, s, id, func(m store.Mission) bool { return !m.WakePending && m.Runs == 2 })
	for i := 0; i < 400 && convBusy(t, s, c.ID); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	setCalls(e, "Cora", call("missao_atualizar", map[string]any{"status": "concluida", "resumo": "Fotos organizadas em 3 pastas."}))
	request(t, s, "POST", "/v1/runtime/jobs/"+job2.ID+"/complete", vps.Token, JobResult{Success: true, Output: "Movi tudo."})
	m := waitMission(t, s, id, func(m store.Mission) bool { return m.Status == store.MissionDone })
	if m.Summary != "Fotos organizadas em 3 pastas." || m.FinishedAt == nil || m.Session != "sess-1" {
		t.Fatalf("done: %+v", m)
	}
	if block := s.missionsBlock(t.Context(), store.Agent{ID: "cora"}); block != "" {
		t.Fatalf("finished missions stay out of the prompt: %s", block)
	}

	// The owner cancels a mission: the run stops and nobody is woken.
	for i := 0; i < 400 && convBusy(t, s, c.ID); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	setCalls(e, "Cora", call("missao_iniciar", map[string]any{"titulo": "Outra", "objetivo": "x", "passos": []string{"a"}}))
	request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "outra"})
	job3 := waitClaim(t, s, vps.Token)
	other, _ := s.store.MissionByJob(t.Context(), job3.ID)
	before, _ := s.store.Messages(t.Context(), c.ID)
	if w := request(t, s, "POST", "/v1/missions/"+other.ID+"/cancel", adminTestToken, nil); w.Code != 200 {
		t.Fatalf("cancel: %d", w.Code)
	}
	if j, _ := s.store.GetJob(t.Context(), job3.ID); j.Status != store.StatusCanceled {
		t.Fatalf("job: %s", j.Status)
	}
	time.Sleep(30 * time.Millisecond)
	after, _ := s.store.Messages(t.Context(), c.ID)
	if got, _ := s.store.GetMission(t.Context(), other.ID); got.Status != store.MissionCanceled || len(after) != len(before) {
		t.Fatalf("canceled: %+v, %d -> %d messages", got, len(before), len(after))
	}
}

func TestMissionEditNeedsTheOwner(t *testing.T) {
	a := toolArgs{Titulo: "X", Objetivo: "y", Passos: []string{"a"}, PodeEditar: true, Executor: "codex"}
	if key, locked := policyOf("missao_iniciar", a); key != "codex.edit" || !locked {
		t.Fatalf("policy: %s %v", key, locked)
	}
	if key, locked := policyOf("missao_iniciar", toolArgs{}); key != "claude_code.read" || locked {
		t.Fatalf("read: %s %v", key, locked)
	}
}

func convBusy(t *testing.T, s *Server, id string) bool {
	c, err := s.store.GetConversation(t.Context(), id)
	return err == nil && c.JobID != ""
}
