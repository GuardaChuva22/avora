package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GuardaChuva22/avora/internal/store"
)

// AgentIsolation holds the per-agent home directories, git identity and
// computer-wide shared directory prepared before starting an execution.
type AgentIsolation struct {
	AgentID    string
	AgentName  string
	AgentEmail string
	HomeRoot   string
	AvoraHome  string
	TmpDir     string
	GitConfig  string
	SharedDir  string
}

// PrepareIsolation ensures <agentHomeDir>/.avora-home (with tmp/ and
// .gitconfig) and <workspace>/shared exist, and builds the agent's git identity.
func PrepareIsolation(workspace, agentHomeDir, agentID, agentName, agentEmail string) (AgentIsolation, error) {
	if strings.TrimSpace(agentHomeDir) == "" {
		return AgentIsolation{}, errors.New("agent home directory required")
	}
	slug := slugifyAgent(firstNonBlank(agentID, agentName))
	name := cleanGitIdent(firstNonBlank(agentName, humanizeSlug(slug)))
	if name == "" {
		name = "Agent"
	}
	email := cleanGitIdent(firstNonBlank(agentEmail, slug+"@avora.local"))
	if email == "" {
		email = slug + "@avora.local"
	}

	homeRoot := filepath.Clean(agentHomeDir)
	avoraHome := filepath.Join(homeRoot, ".avora-home")
	tmpDir := filepath.Join(avoraHome, "tmp")
	gitConfig := filepath.Join(avoraHome, ".gitconfig")

	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return AgentIsolation{}, fmt.Errorf("create .avora-home: %w", err)
	}
	if _, err := os.Stat(gitConfig); errors.Is(err, os.ErrNotExist) {
		cfgText := fmt.Sprintf("[user]\n\tname = %s\n\temail = %s\n[commit]\n\tgpgsign = false\n", name, email)
		if err := os.WriteFile(gitConfig, []byte(cfgText), 0o600); err != nil {
			return AgentIsolation{}, fmt.Errorf("write .gitconfig: %w", err)
		}
	}
	hideAvoraHomeInGit(homeRoot)

	sharedDir := ""
	if strings.TrimSpace(workspace) != "" {
		sharedDir = filepath.Join(filepath.Clean(workspace), "shared")
		if err := os.MkdirAll(sharedDir, 0o700); err != nil {
			return AgentIsolation{}, fmt.Errorf("create shared dir: %w", err)
		}
	}

	return AgentIsolation{
		AgentID:    slug,
		AgentName:  name,
		AgentEmail: email,
		HomeRoot:   homeRoot,
		AvoraHome:  avoraHome,
		TmpDir:     tmpDir,
		GitConfig:  gitConfig,
		SharedDir:  sharedDir,
	}, nil
}

// hideAvoraHomeInGit keeps .avora-home out of git status when the agent's home
// folder itself is a git repository.
func hideAvoraHomeInGit(dir string) {
	excludePath := filepath.Join(dir, ".git", "info", "exclude")
	st, err := os.Stat(filepath.Dir(excludePath))
	if err != nil || !st.IsDir() {
		return
	}
	cur, _ := os.ReadFile(excludePath)
	for _, line := range strings.Split(strings.ReplaceAll(string(cur), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if t == "/.avora-home/" || t == ".avora-home/" || t == ".avora-home" {
			return
		}
	}
	prefix := ""
	if len(cur) > 0 && cur[len(cur)-1] != '\n' {
		prefix = "\n"
	}
	if f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		_, _ = f.WriteString(prefix + "/.avora-home/\n")
		_ = f.Close()
	}
}

// Vars returns the isolated KEY=VALUE pairs. When keepHome is true (used only
// when Antigravity needs its own keyring/profile HOME), HOME and USERPROFILE
// are omitted so the caller's values remain in place.
func (iso AgentIsolation) Vars(keepHome bool) []string {
	vars := make([]string, 0, 11)
	if !keepHome {
		vars = append(vars,
			"HOME="+iso.AvoraHome,
			"USERPROFILE="+iso.AvoraHome,
		)
	}
	vars = append(vars,
		"TMP="+iso.TmpDir,
		"TEMP="+iso.TmpDir,
		"TMPDIR="+iso.TmpDir,
		"GIT_AUTHOR_NAME="+iso.AgentName,
		"GIT_AUTHOR_EMAIL="+iso.AgentEmail,
		"GIT_COMMITTER_NAME="+iso.AgentName,
		"GIT_COMMITTER_EMAIL="+iso.AgentEmail,
		"GIT_CONFIG_GLOBAL="+iso.GitConfig,
	)
	if iso.SharedDir != "" {
		vars = append(vars, "AVORA_SHARED="+iso.SharedDir)
	}
	return vars
}

// ApplyEnv returns base with all managed isolation variables replaced by iso's.
func (iso AgentIsolation) ApplyEnv(base []string, keepHome bool) []string {
	overrides := iso.Vars(keepHome)
	managed := make(map[string]bool, len(overrides))
	for _, kv := range overrides {
		k, _, _ := strings.Cut(kv, "=")
		managed[strings.ToUpper(k)] = true
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !managed[strings.ToUpper(k)] {
			out = append(out, kv)
		}
	}
	return append(out, overrides...)
}

func (cfg AgentConfig) resolveHomeDir(raw string) (string, error) {
	dir := strings.TrimSpace(raw)
	if dir == "" {
		return "", errors.New("empty home_dir")
	}
	if strings.ContainsRune(dir, 0) {
		return "", errors.New("invalid home_dir")
	}
	for _, seg := range strings.Split(strings.ReplaceAll(dir, `\`, "/"), "/") {
		if seg == ".." {
			return "", errors.New("home_dir cannot contain ..")
		}
	}
	if filepath.IsAbs(dir) {
		clean := filepath.Clean(dir)
		if err := os.MkdirAll(clean, 0o700); err != nil {
			return "", err
		}
		return clean, nil
	}
	return cfg.workdir(dir)
}

func (cfg AgentConfig) prepareAgentIsolation(ctx context.Context, spec store.AgentSpec) (AgentIsolation, error) {
	meta := runMetaFrom(ctx)
	agentID := firstNonBlank(meta.AgentID, spec.Owner, spec.Engine)
	slug := slugifyAgent(agentID)
	name := firstNonBlank(meta.AgentName, spec.AgentName, cfg.Worktrees.AgentNames[slug], humanizeSlug(slug))
	email := firstNonBlank(meta.AgentEmail, spec.AgentEmail, cfg.Worktrees.AgentEmails[slug], slug+"@avora.local")

	var homeRoot string
	var err error
	switch {
	case strings.TrimSpace(spec.HomeDir) != "":
		homeRoot, err = cfg.resolveHomeDir(spec.HomeDir)
	case spec.Folder == "" && cfg.Workspace != "":
		if spec.Workdir != "" && (strings.HasPrefix(filepath.ToSlash(spec.Workdir), "agents/") || (meta.AgentID == "" && spec.Owner == "")) {
			homeRoot, err = cfg.workdir(spec.Workdir)
		} else {
			homeRoot = filepath.Join(cfg.Workspace, "agents", slug)
		}
	case cfg.Workspace != "":
		homeRoot = filepath.Join(cfg.Workspace, "agents", slug)
	case spec.Folder != "":
		// Keep .avora-home inside .avora/ when running in a standalone git folder without Workspace.
		homeRoot = filepath.Join(filepath.Clean(spec.Folder), ".avora", "agents", slug)
	default:
		homeRoot = filepath.Join(os.TempDir(), "avora-agents", slug)
	}
	if err != nil {
		return AgentIsolation{}, err
	}
	return PrepareIsolation(cfg.Workspace, homeRoot, agentID, name, email)
}
