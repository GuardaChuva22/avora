package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

func envMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			out[k] = v
		}
	}
	return out
}

func TestPrepareIsolationVariablesAndDirectories(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "node-work")
	atlasHome := filepath.Join(workspace, "agents", "atlas")
	coraHome := filepath.Join(workspace, "agents", "cora")

	isoAtlas, err := PrepareIsolation(workspace, atlasHome, "atlas", "Atlas", "")
	if err != nil {
		t.Fatalf("PrepareIsolation(atlas): %v", err)
	}
	isoCora, err := PrepareIsolation(workspace, coraHome, "cora", "Cora", "cora@time.dev")
	if err != nil {
		t.Fatalf("PrepareIsolation(cora): %v", err)
	}

	wantAvoraHome := filepath.Join(atlasHome, ".avora-home")
	wantTmp := filepath.Join(wantAvoraHome, "tmp")
	wantGitCfg := filepath.Join(wantAvoraHome, ".gitconfig")
	wantShared := filepath.Join(workspace, "shared")

	for _, p := range []string{wantAvoraHome, wantTmp, wantGitCfg, wantShared} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s to be created: %v", p, err)
		}
	}
	cfgBytes, err := os.ReadFile(wantGitCfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgBytes), "name = Atlas") || !strings.Contains(string(cfgBytes), "email = atlas@avora.local") {
		t.Fatalf("unexpected .gitconfig: %s", cfgBytes)
	}

	// Parent environment values are replaced by the agent's isolated variables.
	base := []string{
		"PATH=/usr/bin",
		"HOME=/Users/HostUser",
		"USERPROFILE=C:\\Users\\HostUser",
		"TMP=C:\\Temp",
		"TEMP=C:\\Temp",
		"GIT_AUTHOR_NAME=Host",
		"GIT_AUTHOR_EMAIL=host@example.com",
		"GIT_COMMITTER_NAME=Host",
		"GIT_COMMITTER_EMAIL=host@example.com",
		"GIT_CONFIG_GLOBAL=C:\\Users\\HostUser\\.gitconfig",
		"AVORA_SHARED=C:\\OldShared",
	}
	applied := envMap(isoAtlas.ApplyEnv(base, false))
	checks := map[string]string{
		"HOME":                wantAvoraHome,
		"USERPROFILE":         wantAvoraHome,
		"TMP":                 wantTmp,
		"TEMP":                wantTmp,
		"TMPDIR":              wantTmp,
		"GIT_AUTHOR_NAME":     "Atlas",
		"GIT_AUTHOR_EMAIL":    "atlas@avora.local",
		"GIT_COMMITTER_NAME":  "Atlas",
		"GIT_COMMITTER_EMAIL": "atlas@avora.local",
		"GIT_CONFIG_GLOBAL":   wantGitCfg,
		"AVORA_SHARED":        wantShared,
		"PATH":                "/usr/bin",
	}
	for k, want := range checks {
		if got := applied[k]; got != want {
			t.Fatalf("env[%s] = %q, want %q", k, got, want)
		}
	}

	// Two agents have separate homes/gitconfigs while sharing AVORA_SHARED.
	coraEnv := envMap(isoCora.ApplyEnv(nil, false))
	if coraEnv["HOME"] == applied["HOME"] || coraEnv["GIT_CONFIG_GLOBAL"] == applied["GIT_CONFIG_GLOBAL"] {
		t.Fatalf("agents shared HOME or GIT_CONFIG_GLOBAL: atlas=%v cora=%v", applied, coraEnv)
	}
	if coraEnv["GIT_AUTHOR_EMAIL"] != "cora@time.dev" || coraEnv["GIT_COMMITTER_EMAIL"] != "cora@time.dev" {
		t.Fatalf("cora git email: %v", coraEnv)
	}
	if coraEnv["AVORA_SHARED"] != wantShared {
		t.Fatalf("cora AVORA_SHARED = %q, want %q", coraEnv["AVORA_SHARED"], wantShared)
	}
}

func TestAgentProcessRunsWithIsolatedGitIdentityAndHome(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	workspace := filepath.Join(t.TempDir(), "ws")
	customHome := filepath.Join(t.TempDir(), "casa-atlas")
	cfg := AgentConfig{Workspace: workspace}
	spec := store.AgentSpec{
		Engine:     "claude",
		HomeDir:    customHome,
		Owner:      "atlas",
		AgentName:  "Atlas",
		AgentEmail: "atlas@avora.local",
		MaxMinutes: 1,
		Prompt:     "noop",
	}

	res := agentProcess{
		name:      "git-ident",
		bin:       gitBin,
		args:      []string{"var", "GIT_AUTHOR_IDENT"},
		configDir: filepath.Join(t.TempDir(), "cfg"),
		env:       os.Environ(),
		parse: func(r io.Reader, _ func(control.AgentStep)) (string, bool, AgentStats, error) {
			b, err := io.ReadAll(r)
			return strings.TrimSpace(string(b)), false, AgentStats{}, err
		},
	}.run(context.Background(), cfg, spec, nil)

	if !res.Success || !strings.HasPrefix(res.Output, "Atlas <atlas@avora.local>") {
		t.Fatalf("unexpected git author ident from isolated run: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(customHome, ".avora-home", ".gitconfig")); err != nil {
		t.Fatalf("missing .avora-home/.gitconfig in custom HomeDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "shared")); err != nil {
		t.Fatalf("missing <workspace>/shared: %v", err)
	}
}
