package control

import (
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/store"
)

func TestAgentJobValidation(t *testing.T) {
	s, _ := makeServer(t)
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	bad := []JobRequest{
		{Action: store.ActionAgentRun},
		{Action: store.ActionAgentRun, Agent: &store.AgentSpec{Engine: "gemini", Prompt: "x"}},
		{Action: store.ActionAgentRun, Agent: &store.AgentSpec{Engine: "claude", Prompt: "x", Model: "--dangerous"}},
		{Action: store.ActionAgentRun, Agent: &store.AgentSpec{Engine: "codex", Prompt: "x", Model: "a b"}},
		{Action: store.ActionAgentRun, Agent: &store.AgentSpec{Engine: "claude", Prompt: "   "}},
		{Action: store.ActionAgentRun, Agent: &store.AgentSpec{Engine: "claude", Prompt: "x", Mode: "full"}},
		{Action: store.ActionAgentRun, Agent: &store.AgentSpec{Engine: "claude", Prompt: "x", MaxMinutes: 500}},
		{Action: "ping", Agent: &store.AgentSpec{Engine: "claude", Prompt: "x"}},
	}
	for _, workdir := range []string{"../fora", "/etc", `C:\Windows`, "a/../../b", ".git"} {
		bad = append(bad, JobRequest{Action: store.ActionAgentRun, Agent: &store.AgentSpec{Engine: "claude", Prompt: "x", Workdir: workdir}})
	}
	for _, req := range bad {
		if w := request(t, s, "POST", "/v1/jobs", adminTestToken, req); w.Code != 400 {
			t.Fatalf("accepted %+v %+v: %d", req, req.Agent, w.Code)
		}
	}
	w := request(t, s, "POST", "/v1/jobs", adminTestToken, JobRequest{Action: store.ActionAgentRun,
		Agent: &store.AgentSpec{Engine: "claude", Prompt: strings.Repeat("a", 20000), Mode: "edit", Workdir: `proj\sub`}})
	if w.Code != 201 {
		t.Fatalf("valid agent job: %d %s", w.Code, w.Body.String())
	}
	job := decode[Job](t, w)
	if job.Agent.Workdir != "proj/sub" || job.Agent.Mode != "edit" {
		t.Fatalf("not normalized: %+v", job.Agent)
	}
}
