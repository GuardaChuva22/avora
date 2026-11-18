package control

import (
	"encoding/json"
	"testing"

	"github.com/GuardaChuva22/avora/internal/store"
)

func TestRunCodeResultAndSwitches(t *testing.T) {
	s := chatServer(t, &fakeEngine{})
	ctx := t.Context()

	codeResp := decode[PairingCodeResponse](t, request(t, s, "POST", "/v1/nodes/pairing-codes", adminTestToken,
		PairingCodeRequest{Name: "pc", Kind: "windows"}))
	paired := decode[PairResponse](t, request(t, s, "POST", "/v1/runtime/pair", "", PairRequest{Code: codeResp.Code}))
	request(t, s, "POST", "/v1/runtime/heartbeat", paired.Token, HeartbeatRequest{})

	job := decode[Job](t, request(t, s, "POST", "/v1/jobs", adminTestToken, JobRequest{
		Action: store.ActionAgentRun,
		Agent:  &store.AgentSpec{Engine: "codex", Mode: "edit", Prompt: "implemente login", Owner: "atlas"},
	}))
	if w := request(t, s, "POST", "/v1/runtime/claim", paired.Token, ClaimRequest{Agents: true}); w.Code != 200 {
		t.Fatalf("claim: %d %s", w.Code, w.Body.String())
	}

	stats, _ := json.Marshal(map[string]any{
		"engine":        "codex",
		"turns":         2,
		"branch":        "avora/atlas/a1b2c3d4",
		"worktree":      `C:\Projetos\site\.avora\worktrees\atlas-a1b2c3d4`,
		"commit":        "0123456789abcdef0123456789abcdef01234567",
		"files_changed": 3,
	})
	if w := request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", paired.Token, JobResult{
		Success: true,
		Output:  "Pronto.",
		Stats:   stats,
	}); w.Code != 200 {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}

	got, err := s.RunCodeResult(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch != "avora/atlas/a1b2c3d4" || got.Commit != "0123456789abcdef0123456789abcdef01234567" || got.FilesChanged != 3 {
		t.Fatalf("code result: %+v", got)
	}

	name, email := AgentGitAuthor(store.Agent{ID: "atlas", Name: "Atlas"}, "")
	if name != "Atlas" || email != "atlas@avora.local" {
		t.Fatalf("default author: %s <%s>", name, email)
	}
	_, email = AgentGitAuthor(store.Agent{ID: "atlas", Name: "Atlas"}, "atlas@empresa.dev")
	if email != "atlas@empresa.dev" {
		t.Fatalf("custom author email: %s", email)
	}

	if !s.WorktreeEnabledFor(ctx, "site", "atlas") {
		t.Fatal("worktree should be enabled by default")
	}
	off := false
	if err := s.SetCodeProjectConfig(ctx, "site", CodeProjectConfig{Worktree: &off}, store.ActorAdmin); err != nil {
		t.Fatal(err)
	}
	if s.WorktreeEnabledFor(ctx, "site", "atlas") {
		t.Fatal("project switch did not disable worktree")
	}
}
