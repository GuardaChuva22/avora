package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// AgentStats is what the audit log keeps about a finished run.
type AgentStats struct {
	Engine       string  `json:"engine"`
	Model        string  `json:"model,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
	Turns        int     `json:"turns"`
	DurationMS   int     `json:"duration_ms"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CacheRead    int     `json:"cache_read_tokens"`
	CostUSD      float64 `json:"equivalent_cost_usd,omitempty"`
	Steps        int     `json:"steps"`
	Branch       string  `json:"branch,omitempty"`
	Worktree     string  `json:"worktree,omitempty"`
	Commit       string  `json:"commit,omitempty"`
	FilesChanged int     `json:"files_changed,omitempty"`
	// quota is the last rate limit reading the run reported.
	quota *control.Quota
}

// streamParser reads an agent's JSONL output until it ends, reporting steps.
type streamParser func(r io.Reader, onStep func(control.AgentStep)) (answer string, isError bool, stats AgentStats, err error)

// agentProcess is one agent CLI invocation.
type agentProcess struct {
	name      string // shown in errors: "Claude Code", "Codex"
	bin       string
	args      []string
	argsFor   func(dir string) ([]string, error)
	env       []string
	configDir string // the CLI's own login dir, created if missing
	stdin     string // what goes on stdin; empty sends the prompt
	parse     streamParser
	// gate, when set, runs just before the process starts; the function it
	// returns runs once the process has said something (or gave up), so a
	// shared resource is held only while the CLI starts (agy's login slot).
	gate func(ctx context.Context) (release func(), err error)
}

// RunAgent runs one agent.run job with the official CLI of its engine.
func RunAgent(ctx context.Context, cfg AgentConfig, spec store.AgentSpec, onStep func(control.AgentStep)) control.JobResult {
	cfg, err := cfg.withAccount(spec)
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	switch spec.Engine {
	case "claude":
		return RunClaude(ctx, cfg, spec, onStep)
	case "codex":
		return RunCodex(ctx, cfg, spec, onStep)
	case "antigravity":
		return RunAntigravity(ctx, cfg, spec, onStep)
	case "shell":
		return RunShell(ctx, cfg, spec)
	case "tool":
		return RunTool(ctx, cfg, spec)
	default:
		return control.JobResult{Error: "unsupported agent engine " + spec.Engine}
	}
}

// cleanEnv is the parent environment without the named variables, plus extra.
// API keys are always dropped so runs use the subscription login and never
// bill an API account by accident.
func cleanEnv(drop []string, extra ...string) []string {
	dropped := map[string]bool{"AVORA_PAIRING_CODE": true}
	for _, d := range drop {
		dropped[d] = true
	}
	env := []string{}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !dropped[strings.ToUpper(name)] {
			env = append(env, kv)
		}
	}
	return append(env, extra...)
}

// run starts the process in the job's workdir with the prompt on stdin;
// ctx cancellation (job canceled, time limit) kills it.
func (p agentProcess) run(ctx context.Context, cfg AgentConfig, spec store.AgentSpec, onStep func(control.AgentStep)) control.JobResult {
	fail := func(msg string) control.JobResult {
		return control.JobResult{Success: false, Error: clip(msg, 2000)}
	}
	if _, err := os.Stat(p.bin); err != nil {
		return fail(p.name + " not installed on this node (" + p.bin + ")")
	}
	dir, wt, err := cfg.prepareWorkdir(ctx, spec)
	if err != nil {
		return fail(err.Error())
	}
	startedProc := false
	defer func() {
		if !startedProc && wt != nil {
			cctx, ccancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			wt.Cleanup(cctx)
			ccancel()
		}
	}()
	if p.argsFor != nil {
		p.args, err = p.argsFor(dir)
		if err != nil {
			return fail(err.Error())
		}
	}
	if err := os.MkdirAll(p.configDir, 0o700); err != nil {
		return fail("cannot create the " + p.name + " config dir")
	}
	iso, err := cfg.prepareAgentIsolation(ctx, spec)
	if err != nil {
		return fail("cannot prepare agent home: " + err.Error())
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(spec.MaxMinutes)*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(runCtx, p.bin, p.args...)
	cmd.Dir = dir
	cmd.Env = iso.ApplyEnv(p.env, spec.Engine == "antigravity")
	cmd.Stdin = strings.NewReader(spec.Prompt)
	if p.stdin != "" {
		cmd.Stdin = strings.NewReader(p.stdin)
	}
	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	cmd.WaitDelay = 10 * time.Second
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(err.Error())
	}
	var out io.Reader = stdout
	release := func() {}
	if p.gate != nil {
		r, err := p.gate(runCtx)
		if err != nil {
			return fail(err.Error())
		}
		var once sync.Once
		release = func() { once.Do(r) }
		out = &firstOutput{r: stdout, then: func() { time.AfterFunc(agyReadyGrace, release) }}
		timer := time.AfterFunc(agyReadyWait, release)
		defer timer.Stop()
	}
	defer release()
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return fail("cannot start " + p.name + ": " + err.Error())
	}
	startedProc = true
	answer, isError, stats, parseErr := p.parse(out, onStep)
	_, _ = io.Copy(io.Discard, stdout)
	release()
	waitErr := cmd.Wait()
	if wt != nil {
		fctx, fcancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		if wtRes, werr := wt.Finish(fctx); werr == nil && wtRes.Commit != "" {
			stats.Branch = wtRes.Branch
			stats.Worktree = wtRes.Worktree
			stats.Commit = wtRes.Commit
			stats.FilesChanged = wtRes.FilesChanged
		}
		fcancel()
	}
	if stats.quota != nil && cfg.OnQuota != nil {
		cfg.OnQuota(*stats.quota)
	}
	if stats.DurationMS == 0 {
		stats.DurationMS = int(time.Since(started).Milliseconds())
	}
	if stats.Model == "" {
		stats.Model = spec.Model
	}
	statsJSON, _ := json.Marshal(stats)
	result := control.JobResult{Output: clip(answer, control.MaxJobOutput-8), Stats: statsJSON}

	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		result.Error = fmt.Sprintf("time limit of %d min reached", spec.MaxMinutes)
	case ctx.Err() != nil:
		result.Error = "canceled"
	case parseErr != nil:
		result.Error = parseErr.Error()
		if tail := stderr.String(); tail != "" {
			result.Error += ": " + tail
		}
	case isError:
		result.Error = answer
		if result.Error == "" {
			result.Error = p.name + " reported an error"
		}
	case waitErr != nil:
		result.Error = p.name + " exited: " + waitErr.Error()
		if tail := stderr.String(); tail != "" {
			result.Error += ": " + tail
		}
	default:
		result.Success = true
	}
	result.Error = clip(result.Error, 2000)
	return result
}

// firstOutput calls then once, when the reader first yields something.
type firstOutput struct {
	r    io.Reader
	then func()
	once sync.Once
}

func (f *firstOutput) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if n > 0 || err != nil {
		f.once.Do(f.then)
	}
	return n, err
}
