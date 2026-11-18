package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/store"
)

func requireGit(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	return bin
}

func initTempRepo(t *testing.T, gitBin string) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := gitExec(ctx, gitBin, dir, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitExec(ctx, gitBin, dir, "checkout", "-B", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Projeto\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := gitExec(ctx, gitBin, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitExec(ctx, gitBin, dir,
		"-c", "user.name=Dono",
		"-c", "user.email=dono@avora.local",
		"-c", "commit.gpgsign=false",
		"commit", "-m", "init",
	); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWorktreeCommitAuthorBranchAndCoAuthor(t *testing.T) {
	gitBin := requireGit(t)
	ctx := context.Background()
	repo := initTempRepo(t, gitBin)

	wt, err := StartWorktree(ctx, WorktreeOptions{
		ProjectDir: repo,
		RunID:      "f0e1d2c3b4a59687",
		AgentID:    "atlas",
		AgentName:  "Atlas",
		CoAuthor:   "Maria Silva <maria@exemplo.com>",
		CommitMsg:  "Implementa autenticação",
	})
	if err != nil || wt == nil {
		t.Fatalf("start worktree: %+v %v", wt, err)
	}
	wantPath := filepath.Join(repo, ".avora", "worktrees", "atlas-f0e1d2c3b4a59687")
	if wt.Path() != wantPath || wt.Branch() != "avora/atlas/f0e1d2c3" {
		t.Fatalf("worktree %q branch %q, want %q", wt.Path(), wt.Branch(), wantPath)
	}

	if err := os.WriteFile(filepath.Join(wt.Dir(), "README.md"), []byte("# Projeto\n\nCom login.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Dir(), "auth.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := wt.Finish(ctx)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if res.Branch != "avora/atlas/f0e1d2c3" || res.Worktree != wantPath || res.Commit == "" || res.FilesChanged != 2 {
		t.Fatalf("result: %+v", res)
	}
	if _, err := os.Stat(res.Worktree); err != nil {
		t.Fatalf("worktree removed despite changes: %v", err)
	}

	logOut, err := gitExec(ctx, gitBin, repo, "log", "-1", "--format=%an <%ae>%n%B", res.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(logOut, "Atlas <atlas@avora.local>\n") {
		t.Fatalf("unexpected commit author: %q", logOut)
	}
	if !strings.Contains(logOut, "Implementa autenticação") || !strings.Contains(logOut, "Co-authored-by: Maria Silva <maria@exemplo.com>") {
		t.Fatalf("unexpected commit message: %q", logOut)
	}

	// Main repository stays on main with a clean working tree.
	curBranch, err := gitExec(ctx, gitBin, repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(curBranch) != "main" {
		t.Fatalf("main repo branch %q (%v)", curBranch, err)
	}
	mainStatus, err := gitExec(ctx, gitBin, repo, "status", "--porcelain")
	if err != nil || strings.TrimSpace(mainStatus) != "" {
		t.Fatalf("main repo dirty: %q (%v)", mainStatus, err)
	}

	// Custom configured e-mail on the agent overrides @avora.local.
	res2, err := RunInWorktree(ctx, WorktreeOptions{
		ProjectDir: repo,
		RunID:      "998877665544",
		AgentID:    "cora",
		AgentName:  "Cora",
		AgentEmail: "cora@equipe.dev",
	}, func(dir string) error {
		return os.WriteFile(filepath.Join(dir, "orcamento.md"), []byte("ok\n"), 0o600)
	})
	if err != nil || res2.Commit == "" {
		t.Fatalf("custom email run: %+v %v", res2, err)
	}
	authorOut, err := gitExec(ctx, gitBin, repo, "log", "-1", "--format=%an <%ae>", res2.Commit)
	if err != nil || strings.TrimSpace(authorOut) != "Cora <cora@equipe.dev>" {
		t.Fatalf("custom author: %q (%v)", authorOut, err)
	}
}

func TestWorktreeCleanupWithoutChanges(t *testing.T) {
	gitBin := requireGit(t)
	ctx := context.Background()
	repo := initTempRepo(t, gitBin)

	wt, err := StartWorktree(ctx, WorktreeOptions{
		ProjectDir: repo,
		RunID:      "001122334455",
		AgentID:    "iris",
		AgentName:  "Iris",
	})
	if err != nil || wt == nil {
		t.Fatalf("start: %+v %v", wt, err)
	}
	wtPath := wt.Path()
	branch := wt.Branch()
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree missing before finish: %v", err)
	}

	res, err := wt.Finish(ctx)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if res.Commit != "" || res.Branch != "" || res.FilesChanged != 0 {
		t.Fatalf("expected empty result, got %+v", res)
	}
	if _, err := os.Stat(wtPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree still on disk: %v", err)
	}
	if _, err := gitExec(ctx, gitBin, repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		t.Fatalf("branch %s was not deleted", branch)
	}
}

func TestWorktreeRunnerIntegrationAndSwitches(t *testing.T) {
	gitBin := requireGit(t)
	ctx := context.Background()

	// Non-git folder: runs in place without error.
	plainWork := t.TempDir()
	cfg := AgentConfig{Workspace: plainWork}
	dir, wt, err := cfg.prepareWorkdir(ctx, store.AgentSpec{Engine: "codex", Mode: "edit", Workdir: "sem-git", Owner: "atlas"})
	if err != nil || wt != nil || dir != filepath.Join(plainWork, "sem-git") {
		t.Fatalf("non-git fallback: dir=%q wt=%+v err=%v", dir, wt, err)
	}

	// Git project folder with per-agent and per-project switches.
	repo := initTempRepo(t, gitBin)
	foldersFile := filepath.Join(t.TempDir(), "folders.json")
	allowedJSON, _ := json.Marshal([]string{repo})
	if err := os.WriteFile(foldersFile, allowedJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	disabledCfg := AgentConfig{
		FoldersFile: foldersFile,
		Worktrees:   WorktreePolicy{Agents: map[string]bool{"atlas": false}},
	}
	dir, wt, err = disabledCfg.prepareWorkdir(ctx, store.AgentSpec{Engine: "codex", Mode: "edit", Folder: repo, Owner: "atlas"})
	if err != nil || wt != nil || dir != filepath.Clean(repo) {
		t.Fatalf("agent switch did not disable worktree: dir=%q wt=%+v err=%v", dir, wt, err)
	}

	// Enabled (default): RunCodex receives the worktree path via --cd and cleans up when unchanged.
	t.Setenv("AVORA_FAKE_CODEX", "1")
	runCfg := AgentConfig{
		CodexBin:    os.Args[0],
		CodexHome:   filepath.Join(t.TempDir(), "codex"),
		FoldersFile: foldersFile,
	}
	runCtx := WithRunMeta(ctx, RunMeta{JobID: "abcd1234ef56", AgentID: "atlas", AgentName: "Atlas"})
	jobRes := RunAgent(runCtx, runCfg, store.AgentSpec{
		Engine:     "codex",
		Mode:       "edit",
		Folder:     repo,
		Owner:      "atlas",
		Prompt:     "verifique",
		MaxMinutes: 2,
	}, nil)
	if !jobRes.Success {
		t.Fatalf("RunAgent failed: %+v", jobRes)
	}
	wantWT := filepath.Join(repo, ".avora", "worktrees", "atlas-abcd1234ef56")
	if !strings.Contains(jobRes.Output, "--cd "+wantWT) {
		t.Fatalf("codex did not run inside worktree %q: %s", wantWT, jobRes.Output)
	}
	if _, err := os.Stat(wantWT); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unchanged RunAgent worktree not cleaned up: %v", err)
	}
}
