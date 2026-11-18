package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestAccountFolders(t *testing.T) {
	base := filepath.Join("srv", "avora")
	var seen control.Quota
	cfg := AgentConfig{ClaudeConfigDir: filepath.Join(base, "claude"), CodexHome: filepath.Join(base, "codex"), OnQuota: func(q control.Quota) { seen = q }}
	got, err := cfg.withAccount(store.AgentSpec{Engine: "codex", Account: "codex.a1b2c3"})
	if err != nil || got.CodexHome != filepath.Join(base, "accounts", "codex.a1b2c3") || got.ClaudeConfigDir != cfg.ClaudeConfigDir || got.OnQuota == nil {
		t.Fatalf("codex account: %+v %v", got, err)
	}
	// A run on the account reports that account's limits.
	got.OnQuota(control.Quota{Engine: "codex"})
	if seen.Account != "codex.a1b2c3" {
		t.Fatalf("quota of the account: %+v", seen)
	}
	same, _ := cfg.withAccount(store.AgentSpec{Engine: "claude"})
	if same.ClaudeConfigDir != cfg.ClaudeConfigDir || same.OnQuota == nil {
		t.Fatal("the principal changed")
	}
	for _, bad := range []store.AgentSpec{
		{Engine: "claude", Account: "../../etc"},
		{Engine: "claude", Account: "codex.a1b2c3"},
		{Engine: "claude", Account: "claude.A1B2C3"},
	} {
		if _, err := cfg.withAccount(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if r := RunAgent(t.Context(), cfg, store.AgentSpec{Engine: "claude", Account: "../x"}, func(control.AgentStep) {}); r.Error == "" {
		t.Fatal("run on a bad account")
	}
	if tail := loginTail("visit https://x.example/a?b=c now\ncode ABCD-12345 ok"); tail != "visit … now code … ok" {
		t.Fatalf("tail: %q", tail)
	}
}

func TestProjectFoldersNeedTheOwnersPick(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "Trabalho")
	inner := filepath.Join(proj, "site")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := AgentConfig{Workspace: filepath.Join(base, "work"), FoldersFile: filepath.Join(base, "folders.json")}
	if _, err := cfg.dirFor(store.AgentSpec{Folder: proj}); err == nil {
		t.Fatal("a folder nobody picked was used")
	}
	data, _ := json.Marshal([]string{proj})
	if err := os.WriteFile(cfg.FoldersFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if dir, err := cfg.dirFor(store.AgentSpec{Folder: inner}); err != nil || dir != inner {
		t.Fatalf("inside a picked folder: %q %v", dir, err)
	}
	if _, err := cfg.dirFor(store.AgentSpec{Folder: filepath.Join(proj, "..", "outro")}); err == nil {
		t.Fatal("escaped the picked folder")
	}
	if _, err := cfg.dirFor(store.AgentSpec{Folder: proj + "-irmao"}); err == nil {
		t.Fatal("a sibling with the same prefix was used")
	}
	if dir, err := cfg.dirFor(store.AgentSpec{Workdir: "agents/atlas"}); err != nil || !strings.HasPrefix(dir, cfg.Workspace) {
		t.Fatalf("workdir: %q %v", dir, err)
	}
}
