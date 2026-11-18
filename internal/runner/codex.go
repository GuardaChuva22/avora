package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

func findCodex() string {
	if runtime.GOOS == "windows" {
		// winget's standalone build; the npm shim (codex.ps1/.cmd) is not
		// directly executable.
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			matches, _ := filepath.Glob(filepath.Join(base, "Microsoft", "WinGet", "Packages", "OpenAI.Codex_*", "codex-*-windows-*.exe"))
			if len(matches) > 0 {
				return matches[len(matches)-1]
			}
		}
	}
	if p, err := exec.LookPath("codex"); err == nil && filepath.Ext(p) != ".ps1" && filepath.Ext(p) != ".cmd" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "codex")
}

// Codex runs every model action as a shell command inside its own OS sandbox,
// so the modes map onto sandbox policies: read-only, or writes limited to the
// workdir (no network). Approvals are off: codex exec never prompts.
var codexSandbox = map[string]string{"read": "read-only", "edit": "workspace-write"}

// codexArgs builds `codex exec`. Runs that answer a conversation keep their
// session so the next message can resume it; one-off jobs stay ephemeral.
func codexArgs(mode, model, dir string, keep bool, resume string, opts ...string) ([]string, error) {
	sandbox, ok := codexSandbox[mode]
	if !ok {
		return nil, fmt.Errorf("unsupported mode %q", mode)
	}
	args := []string{"exec", "--json", "--color", "never", "--skip-git-repo-check",
		"--sandbox", sandbox, "-c", `approval_policy="never"`, "--cd", dir}
	if !keep {
		args = append(args, "--ephemeral")
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	// opts: "search" turns on live web search (a top-level flag, before
	// exec); "effort=<level>" sets the reasoning effort.
	for _, o := range opts {
		switch {
		case o == "search":
			args = append([]string{"--search"}, args...)
		case strings.HasPrefix(o, "effort="):
			args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", strings.TrimPrefix(o, "effort=")))
		case strings.HasPrefix(o, "image="):
			args = append(args, "--image", strings.TrimPrefix(o, "image="))
		}
	}
	if resume != "" {
		args = append(args, "resume", resume)
	}
	return append(args, "-"), nil
}

func codexEnv(home string) []string {
	return cleanEnv([]string{"OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_HOME", bridgeKeyEnv}, "CODEX_HOME="+home)
}

// codexEvent is the subset of `codex exec --json` lines Avora reads.
type codexEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Message  string `json:"message"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
	Item *struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Command string `json:"command"`
		Query   string `json:"query"`
		Server  string `json:"server"`
		Tool    string `json:"tool"`
		Changes []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"changes"`
	} `json:"item"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		CachedInput  int `json:"cached_input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// parseCodexStream reads codex exec JSONL until the turn ends.
func parseCodexStream(r io.Reader, onStep func(control.AgentStep)) (answer string, isError bool, stats AgentStats, err error) {
	stats.Engine = "codex"
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	finished := false
	step := func(tool, summary string) {
		stats.Steps++
		if onStep != nil {
			onStep(control.AgentStep{Tool: tool, Summary: clip(summary, 300)})
		}
	}
	for scanner.Scan() {
		var ev codexEvent
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "thread.started":
			stats.SessionID = ev.ThreadID
		case "item.started":
			// Codex's own image generation: the app shows it being drawn.
			if ev.Item != nil && ev.Item.Type == "image_generation" && onStep != nil {
				onStep(control.AgentStep{Tool: control.StepImage, Summary: "gerando"})
			}
		case "item.completed":
			if ev.Item == nil {
				continue
			}
			switch it := ev.Item; it.Type {
			case "agent_message":
				answer = it.Text
				if t := strings.TrimSpace(it.Text); t != "" && onStep != nil {
					onStep(control.AgentStep{Tool: control.StepText, Summary: clip(t, control.MaxStepText)})
				}
			case "command_execution":
				step("Shell", "command: "+it.Command)
			case "file_change":
				paths := []string{}
				for _, c := range it.Changes {
					paths = append(paths, c.Kind+" "+c.Path)
				}
				step("Edit", strings.Join(paths, ", "))
			case "web_search":
				step("WebSearch", "query: "+it.Query)
			case "mcp_tool_call":
				step("MCP", it.Server+"/"+it.Tool)
			case "image_generation":
				step(control.StepImage, "pronta")
			case "error":
				if it.Text != "" {
					answer = it.Text
				}
			}
		case "turn.completed":
			finished = true
			stats.Turns++
			if ev.Usage != nil {
				stats.InputTokens += ev.Usage.InputTokens
				stats.CacheRead += ev.Usage.CachedInput
				stats.OutputTokens += ev.Usage.OutputTokens
			}
		case "turn.failed":
			finished, isError = true, true
			if ev.Error != nil {
				answer = ev.Error.Message
			}
		case "error":
			isError = true
			if ev.Message != "" {
				answer = ev.Message
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return answer, true, stats, err
	}
	if !finished && !isError {
		return answer, true, stats, errors.New("codex ended without finishing the turn")
	}
	return answer, isError, stats, nil
}

// RunCodex runs one agent.run job with the official Codex CLI.
func RunCodex(ctx context.Context, cfg AgentConfig, spec store.AgentSpec, onStep func(control.AgentStep)) control.JobResult {
	var opts []string
	if spec.Search {
		opts = append(opts, "search")
	}
	if spec.Effort != "" {
		opts = append(opts, "effort="+spec.Effort)
	}
	if len(spec.Images) > 0 {
		imgDir, err := os.MkdirTemp("", "avora-img-")
		if err != nil {
			return control.JobResult{Error: err.Error()}
		}
		defer os.RemoveAll(imgDir)
		for i, im := range spec.Images {
			ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}[im.Mime]
			if ext == "" {
				continue
			}
			p := filepath.Join(imgDir, fmt.Sprintf("imagem-%d%s", i+1, ext))
			if err := os.WriteFile(p, im.Data, 0o600); err == nil {
				opts = append(opts, "image="+p)
			}
		}
	}
	env := codexEnv(cfg.CodexHome)
	if spec.Bridge != "" && cfg.ServerURL != "" {
		env = append(env, bridgeKeyEnv+"="+spec.Bridge)
	}
	return agentProcess{
		name: "Codex", bin: cfg.CodexBin, configDir: cfg.CodexHome,
		argsFor: func(dir string) ([]string, error) {
			args, err := codexArgs(spec.Mode, spec.Model, dir, spec.ConversationID != "", spec.Resume, opts...)
			if err != nil {
				return nil, err
			}
			if spec.Bridge != "" && cfg.ServerURL != "" {
				// exec's own flags come first; the overrides go right after "exec".
				args = append(append([]string{args[0]}, codexBridge(cfg.ServerURL)...), args[1:]...)
			}
			return args, nil
		},
		env:   env,
		parse: parseCodexStream,
	}.run(ctx, cfg, spec, onStep)
}
