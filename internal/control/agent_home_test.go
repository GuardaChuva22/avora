package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/browser"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func homeStrPtr(s string) *string { return &s }

func TestAgentHomeRoutesExecutionsToConfiguredNodeAndDir(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{
		"Atlas": {call("codex", map[string]any{"tarefa": "Revise os testes"})},
	}}
	s := chatServer(t, e)
	pc1 := pairNode(t, s, "PC Sala", "windows")
	pc2 := pairNode(t, s, "PC Estúdio", "windows")
	vps := pairNode(t, s, "VPS", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc1.Token, nil)
	request(t, s, "POST", "/v1/runtime/heartbeat", pc2.Token, nil)
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)

	// Without home_node_id, Atlas uses standard routing (the first online PC).
	conv := agentConversation(t, s, "atlas")
	ans1 := sendAsync(t, s, conv.ID, "rode sem casa fixa")
	job1 := waitClaim(t, s, pc1.Token)
	if job1.DeviceID != pc1.NodeID || job1.Agent.Workdir != "agents/atlas" {
		t.Fatalf("expected default routing to pc1 with agents/atlas, got device=%s spec=%+v", job1.DeviceID, job1.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job1.ID+"/complete", pc1.Token, JobResult{Success: true, Output: "ok pc1"})
	<-ans1

	// Pin Atlas to pc2 with default home_dir ("": <workspace>/agents/atlas).
	cur, err := s.store.GetAgent(context.Background(), "atlas")
	if err != nil {
		t.Fatal(err)
	}
	w := request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, AgentRequest{
		Name:       cur.Name,
		Role:       cur.Role,
		Persona:    cur.Persona,
		Color:      cur.Color,
		HomeNodeID: homeStrPtr(pc2.NodeID),
		HomeDir:    homeStrPtr(""),
	})
	if w.Code != 200 {
		t.Fatalf("update agent home: %d %s", w.Code, w.Body.String())
	}

	ans2 := sendAsync(t, s, conv.ID, "rode na casa padrão")
	if claimWrong := request(t, s, "POST", "/v1/runtime/claim", pc1.Token, ClaimRequest{Agents: true}); claimWrong.Code != 204 {
		t.Fatalf("pc1 should not receive Atlas job: %d", claimWrong.Code)
	}
	job2 := waitClaim(t, s, pc2.Token)
	if job2.DeviceID != pc2.NodeID || job2.Agent.Workdir != "agents/atlas" || job2.Agent.HomeDir != "" ||
		job2.Agent.AgentName != "Atlas" || job2.Agent.AgentEmail != "atlas@avora.local" {
		t.Fatalf("unexpected home spec: device=%s spec=%+v", job2.DeviceID, job2.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job2.ID+"/complete", pc2.Token, JobResult{Success: true, Output: "ok pc2"})
	<-ans2

	// Custom home_dir overrides the default folder, and shell + headless browser follow home_node_id too.
	w = request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, AgentRequest{
		Name:       cur.Name,
		Role:       cur.Role,
		Persona:    cur.Persona,
		Color:      cur.Color,
		HomeNodeID: homeStrPtr(vps.NodeID),
		HomeDir:    homeStrPtr("casas/atlas-dev"),
	})
	if w.Code != 200 {
		t.Fatalf("update custom home_dir: %d %s", w.Code, w.Body.String())
	}

	e.mu.Lock()
	e.calls["Atlas"] = []llm.ToolCall{call("executar", map[string]any{"comando": "pwd"})}
	e.mu.Unlock()

	ans3 := sendAsync(t, s, conv.ID, "rode shell na casa customizada")
	job3 := waitClaim(t, s, vps.Token)
	if job3.DeviceID != vps.NodeID || job3.Agent.Engine != "shell" || job3.Agent.HomeDir != "casas/atlas-dev" || job3.Agent.Workdir != "" {
		t.Fatalf("shell did not use custom home: device=%s spec=%+v", job3.DeviceID, job3.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job3.ID+"/complete", vps.Token, JobResult{Success: true, Output: "/work"})
	<-ans3

	// Headless browser goes to Atlas's home computer even if AppBrowser is polling.
	s.touchPolled(AppBrowser, "")
	e.mu.Lock()
	e.calls["Atlas"] = []llm.ToolCall{call("navegador", map[string]any{"acao": "abrir", "url": "https://exemplo.com"})}
	e.mu.Unlock()

	ans4 := sendAsync(t, s, conv.ID, "abra o site")
	var bcall BrowserCall
	for i := 0; i < 200; i++ {
		s.browsers.mu.Lock()
		n := s.browsers.node(vps.NodeID)
		call, ok := n.takeNext("atlas", time.Now())
		s.browsers.mu.Unlock()
		if ok {
			bcall = call
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if bcall.ID == "" {
		t.Fatal("browser call was not routed to home node")
	}
	request(t, s, "POST", "/v1/runtime/browser/result", vps.Token, BrowserAnswer{
		ID:     bcall.ID,
		Agent:  "atlas",
		Result: browser.Result{OK: true, URL: "https://exemplo.com", Title: "Exemplo", Text: "Página carregada"},
	})
	res4 := <-ans4
	if !strings.Contains(res4.Assistant.Content, "Página carregada") {
		t.Fatalf("unexpected browser answer: %+v", res4.Assistant)
	}
}

func TestAgentHomeOfflineQueuesJobAndShowsChatNotice(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{
		"Atlas": {call("codex", map[string]any{"tarefa": "Analise o repositório"})},
	}}
	s := chatServer(t, e)
	// Pair an online VPS (so another computer IS online) and an offline home computer for Atlas.
	vps := pairNode(t, s, "VPS Sempre Ligada", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)
	homePC := pairNode(t, s, "Notebook de Casa", "windows") // never sent heartbeat -> offline

	cur, err := s.store.GetAgent(context.Background(), "atlas")
	if err != nil {
		t.Fatal(err)
	}
	if w := request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, AgentRequest{
		Name:       cur.Name,
		Role:       cur.Role,
		Persona:    cur.Persona,
		Color:      cur.Color,
		HomeNodeID: homeStrPtr(homePC.NodeID),
	}); w.Code != 200 {
		t.Fatalf("set offline home_node_id: %d %s", w.Code, w.Body.String())
	}

	conv := agentConversation(t, s, "atlas")
	ansCh := sendAsync(t, s, conv.ID, "analise agora")

	wantNotice := "O computador Notebook de Casa está desligado; a tarefa começa quando ele voltar"

	// Wait until the job is queued on homePC and the offline step is published.
	var queuedJob *Job
	for i := 0; i < 200; i++ {
		jobs, _ := s.store.ListJobs(context.Background(), 10)
		for j := range jobs {
			if jobs[j].DeviceID == homePC.NodeID && jobs[j].Status == store.StatusQueued {
				queuedJob = &jobs[j]
				break
			}
		}
		if queuedJob != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if queuedJob == nil {
		t.Fatal("expected execution to wait queued on offline home computer")
	}

	// Online VPS must NOT steal the job.
	if w := request(t, s, "POST", "/v1/runtime/claim", vps.Token, ClaimRequest{Agents: true}); w.Code != 204 {
		t.Fatalf("online VPS claimed offline home job: %d", w.Code)
	}

	// Verify the offline message was recorded in the audit/chat stream right away.
	foundEvent := false
	for i := 0; i < 100 && !foundEvent; i++ {
		evs, _ := s.store.Events(context.Background(), store.EventFilter{Limit: 100})
		for _, ev := range evs {
			if ev.Type == store.EventAgentStep && strings.Contains(string(ev.Payload), wantNotice) {
				foundEvent = true
				break
			}
		}
		if !foundEvent {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !foundEvent {
		t.Fatalf("expected chat step %q while home node is offline", wantNotice)
	}

	// Now Notebook de Casa comes back online, claims the queued job and finishes it.
	request(t, s, "POST", "/v1/runtime/heartbeat", homePC.Token, nil)
	claimed := waitClaim(t, s, homePC.Token)
	if claimed.ID != queuedJob.ID {
		t.Fatalf("claimed job %s, want queued job %s", claimed.ID, queuedJob.ID)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+claimed.ID+"/complete", homePC.Token, JobResult{
		Success: true,
		Output:  "Análise concluída quando o computador voltou.",
	})

	resp := <-ansCh
	if !strings.Contains(resp.Assistant.Content, "Análise concluída quando o computador voltou.") {
		t.Fatalf("unexpected final answer: %+v", resp.Assistant)
	}
	var steps []AgentStep
	_ = json.Unmarshal(resp.Assistant.Steps, &steps)
	hasStep := false
	for _, st := range steps {
		if st.Summary == wantNotice {
			hasStep = true
			break
		}
	}
	if !hasStep {
		t.Fatalf("assistant steps missing %q: %+v", wantNotice, steps)
	}
}
