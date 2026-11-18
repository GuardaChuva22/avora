package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// AgentConfig says where the agent CLI lives and where it may work.
type AgentConfig struct {
	// ClaudeBin is the official Claude Code binary.
	ClaudeBin string
	// ClaudeConfigDir is Avora's own Claude Code login, kept apart from any
	// other Claude installation on the machine. Avora never reads it.
	ClaudeConfigDir string
	// CodexBin and CodexHome are the same for the official Codex CLI.
	CodexBin  string
	CodexHome string
	// AgyBin is the Antigravity CLI; its login lives in the OS keyring.
	AgyBin string
	// AgyHome is Avora's Antigravity folder: run homes, and the owner's own
	// login while a pool account holds the keyring (D61). Pool accounts live
	// next to it, in accounts/<id>.
	AgyHome string
	// Workspace is the only directory tree agent runs may use.
	Workspace string
	// MaxAgents is how many agent runs this node accepts at once.
	MaxAgents int
	// OnQuota receives the subscription limits a run reported, if any.
	OnQuota func(control.Quota)
	// Bwrap is the bubblewrap binary that sandboxes executar (Linux only);
	// empty means /usr/bin/bwrap.
	Bwrap string
	// Pasta gives executar with network its own network namespace.
	Pasta string
	// ServerURL is the control plane this node talks to; delegated runs
	// reach the owner's Plugins through its bridge.
	ServerURL string
	// FoldersFile lists the project folders the owner picked in the app on
	// this machine (a JSON array of absolute paths); a run with
	// AgentSpec.Folder works only inside one of them.
	FoldersFile string
	// Worktrees controls isolated git worktrees for edit runs in git repos.
	Worktrees WorktreePolicy
}

// DefaultAgentConfig reads AVORA_CLAUDE_BIN, AVORA_CLAUDE_CONFIG_DIR,
// AVORA_WORKSPACE and AVORA_MAX_AGENTS, with per-OS defaults.
func DefaultAgentConfig() AgentConfig {
	cfg := AgentConfig{
		ClaudeBin:       os.Getenv("AVORA_CLAUDE_BIN"),
		ClaudeConfigDir: os.Getenv("AVORA_CLAUDE_CONFIG_DIR"),
		Workspace:       os.Getenv("AVORA_WORKSPACE"),
		CodexBin:        os.Getenv("AVORA_CODEX_BIN"),
		CodexHome:       os.Getenv("AVORA_CODEX_HOME"),
		AgyBin:          os.Getenv("AVORA_AGY_BIN"),
		MaxAgents:       2,
	}
	if runtime.GOOS == "linux" {
		cfg.MaxAgents = 3 // D11: at most 3 heavy agents on the VPS.
	}
	if n, err := strconv.Atoi(os.Getenv("AVORA_MAX_AGENTS")); err == nil && n >= 0 {
		cfg.MaxAgents = n
	}
	if cfg.ClaudeBin == "" {
		cfg.ClaudeBin = findClaude()
	}
	if cfg.CodexBin == "" {
		cfg.CodexBin = findCodex()
	}
	if cfg.AgyBin == "" {
		cfg.AgyBin = findAgy()
	}
	base, _ := os.UserConfigDir()
	if cfg.ClaudeConfigDir == "" && base != "" {
		cfg.ClaudeConfigDir = filepath.Join(base, "Avora", "claude")
	}
	if cfg.CodexHome == "" && base != "" {
		cfg.CodexHome = filepath.Join(base, "Avora", "codex")
	}
	if cfg.AgyHome == "" && base != "" {
		cfg.AgyHome = filepath.Join(base, "Avora", "agy")
	}
	if base != "" {
		cfg.FoldersFile = filepath.Join(base, "Avora", "folders.json")
	}
	if cfg.Workspace == "" {
		if cache, err := os.UserCacheDir(); err == nil {
			cfg.Workspace = filepath.Join(cache, "Avora", "work")
		}
	}
	return cfg
}

func findClaude() string {
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	name := "claude"
	if runtime.GOOS == "windows" {
		name = "claude.exe"
	}
	return filepath.Join(home, ".local", "bin", name)
}

// Tools per mode. Anything not listed is denied: claude -p never prompts.
var claudeTools = map[string]struct{ allowed, denied, permission string }{
	"read": {"Read,Grep,Glob,WebSearch,WebFetch,TodoWrite", "Bash,Edit,Write,NotebookEdit", "default"},
	"edit": {"Read,Grep,Glob,WebSearch,WebFetch,TodoWrite,Edit,Write,NotebookEdit", "Bash", "acceptEdits"},
}

// claudeArgs builds `claude -p`. mcpConfig, when set, is the bridge's config
// file: only its server is loaded, and its tools are allowed.
func claudeArgs(mode, model, effort, resume, mcpConfig string) ([]string, error) {
	tools, ok := claudeTools[mode]
	if !ok {
		return nil, fmt.Errorf("unsupported mode %q", mode)
	}
	allowed := tools.allowed
	args := []string{}
	if mcpConfig != "" {
		args = append(args, "--mcp-config", mcpConfig, "--strict-mcp-config")
		allowed += ",mcp__" + bridgeServer
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	return append(args,
		"-p", "--output-format", "stream-json", "--verbose",
		"--permission-mode", tools.permission,
		"--allowedTools", allowed, "--disallowedTools", tools.denied,
		"--max-turns", "60",
	), nil
}

// claudeEnv is the parent environment with Avora's login directory and
// without API keys, so a run always uses the subscription login and never
// bills an API account by accident.
func claudeEnv(configDir string) []string {
	return cleanEnv([]string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CONFIG_DIR", bridgeKeyEnv}, "CLAUDE_CONFIG_DIR="+configDir)
}

// workdir resolves a job's workdir inside the workspace.
// dirFor is where a run works: a project folder the owner allowed on this
// machine, or a folder inside the workspace.
func (cfg AgentConfig) dirFor(spec store.AgentSpec) (string, error) {
	if strings.TrimSpace(spec.HomeDir) != "" {
		return cfg.resolveHomeDir(spec.HomeDir)
	}
	if spec.Folder == "" {
		return cfg.workdir(spec.Workdir)
	}
	if !filepath.IsAbs(spec.Folder) {
		return "", errors.New("folder must be absolute")
	}
	want := filepath.Clean(spec.Folder)
	if cfg.FoldersFile != "" {
		if data, err := os.ReadFile(cfg.FoldersFile); err == nil {
			var allowed []string
			_ = json.Unmarshal(data, &allowed)
			for _, a := range allowed {
				if a == "" || !filepath.IsAbs(a) {
					continue
				}
				in, err := filepath.Rel(filepath.Clean(a), want)
				if err == nil && in != ".." && !strings.HasPrefix(in, ".."+string(filepath.Separator)) && !filepath.IsAbs(in) {
					if st, err := os.Stat(want); err != nil || !st.IsDir() {
						return "", errors.New("a pasta do projeto não existe neste PC: " + want)
					}
					return want, nil
				}
			}
		}
	}
	return "", errors.New("pasta não liberada neste PC (" + want + "): abra o projeto no app do Avora deste PC e escolha a pasta de novo")
}

func (cfg AgentConfig) workdir(rel string) (string, error) {
	if cfg.Workspace == "" {
		return "", errors.New("no workspace configured (AVORA_WORKSPACE)")
	}
	dir := cfg.Workspace
	if rel != "" {
		dir = filepath.Join(cfg.Workspace, filepath.FromSlash(rel))
		inside, err := filepath.Rel(cfg.Workspace, dir)
		if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return "", errors.New("workdir escapes the workspace")
		}
	}
	return dir, os.MkdirAll(dir, 0o700)
}

// claudeEvent is the subset of Claude Code's stream-json lines Avora reads.
type claudeEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	Model     string `json:"model"`
	SessionID string `json:"session_id"`
	Message   *struct {
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Text  string          `json:"text"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
	RateLimit  *claudeRateLimit `json:"rate_limit_info"`
	Result     string           `json:"result"`
	IsError    bool             `json:"is_error"`
	NumTurns   int              `json:"num_turns"`
	DurationMS int              `json:"duration_ms"`
	CostUSD    float64          `json:"total_cost_usd"`
	Usage      *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		CacheRead    int `json:"cache_read_input_tokens"`
		CacheWrite   int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// parseClaudeStream reads stream-json until the result line, reporting tool
// calls as steps. It returns the final answer and the run's stats.
func parseClaudeStream(r io.Reader, onStep func(control.AgentStep)) (answer string, isError bool, stats AgentStats, err error) {
	stats.Engine = "claude"
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	gotResult := false
	for scanner.Scan() {
		var ev claudeEvent
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "system":
			if ev.Model != "" {
				stats.Model = ev.Model
			}
			if ev.SessionID != "" {
				stats.SessionID = ev.SessionID
			}
		case "assistant":
			if ev.Message == nil {
				continue
			}
			for _, c := range ev.Message.Content {
				if c.Type == "text" && strings.TrimSpace(c.Text) != "" && onStep != nil {
					onStep(control.AgentStep{Tool: control.StepText, Summary: clip(strings.TrimSpace(c.Text), control.MaxStepText)})
				}
				if c.Type == "tool_use" {
					stats.Steps++
					if onStep != nil {
						onStep(control.AgentStep{Tool: c.Name, Summary: summarizeInput(c.Input)})
					}
				}
			}
		case "rate_limit_event":
			if ev.RateLimit != nil {
				if q := ev.RateLimit.quota(time.Now()); q != nil {
					stats.quota = q
				}
			}
		case "result":
			gotResult = true
			answer, isError = ev.Result, ev.IsError || ev.Subtype != "success"
			stats.Turns, stats.DurationMS, stats.CostUSD = ev.NumTurns, ev.DurationMS, ev.CostUSD
			if ev.Usage != nil {
				stats.InputTokens = ev.Usage.InputTokens + ev.Usage.CacheWrite
				stats.OutputTokens, stats.CacheRead = ev.Usage.OutputTokens, ev.Usage.CacheRead
			}
			if isError && answer == "" {
				answer = ev.Subtype
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return answer, true, stats, err
	}
	if !gotResult {
		return answer, true, stats, errors.New("claude ended without a result")
	}
	return answer, isError, stats, nil
}

// summarizeInput picks the most telling field of a tool call for the audit log.
func summarizeInput(input json.RawMessage) string {
	var fields map[string]any
	if json.Unmarshal(input, &fields) != nil {
		return ""
	}
	for _, key := range []string{"file_path", "notebook_path", "path", "pattern", "url", "query", "command", "description"} {
		if v, ok := fields[key].(string); ok && v != "" {
			return clip(key+": "+v, 300)
		}
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

// tailBuffer keeps the last bytes written to it (stderr of the agent).
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(bytes.ToValidUTF8(t.buf, nil)))
}

// RunClaude runs one agent.run job with the official Claude Code CLI.
func RunClaude(ctx context.Context, cfg AgentConfig, spec store.AgentSpec, onStep func(control.AgentStep)) control.JobResult {
	mcpConfig := ""
	env := claudeEnv(cfg.ClaudeConfigDir)
	if spec.Bridge != "" && cfg.ServerURL != "" {
		path, cleanup, err := claudeBridge(cfg.ServerURL, spec.Bridge)
		if err != nil {
			return control.JobResult{Error: "cannot prepare the Plugins bridge: " + err.Error()}
		}
		defer cleanup()
		mcpConfig = path
		env = append(env, "MCP_TOOL_TIMEOUT="+itoa(bridgeTimeoutSec*1000))
	}
	args, err := claudeArgs(spec.Mode, spec.Model, spec.Effort, spec.Resume, mcpConfig)
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	return agentProcess{
		name: "Claude Code", bin: cfg.ClaudeBin, args: args, configDir: cfg.ClaudeConfigDir,
		env:   env,
		parse: parseClaudeStream,
	}.run(ctx, cfg, spec, onStep)
}
