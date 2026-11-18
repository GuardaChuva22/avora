package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Quotas come only from the official CLIs, run with Avora's own login dirs:
//   - Claude Code reports a rate_limit_event in every stream-json run. When no
//     run happened for a while, a one-turn Haiku probe refreshes it.
//   - Codex answers account/rateLimits/read on its app-server for free.
const (
	codexQuotaEvery = 5 * time.Minute
	quotaTimeout    = 90 * time.Second
)

// claudeProbeEvery reads AVORA_CLAUDE_QUOTA_PROBE_MIN (default 60; 0 turns
// the probe off, leaving only the readings from real runs).
func claudeProbeEvery() time.Duration {
	if n, err := strconv.Atoi(os.Getenv("AVORA_CLAUDE_QUOTA_PROBE_MIN")); err == nil && n >= 0 {
		return time.Duration(n) * time.Minute
	}
	return 60 * time.Minute
}

// quotaTracker keeps this node's latest reading per login: the node's own
// login of each engine and every extra account (accounts/<id>).
type quotaTracker struct {
	mu     sync.Mutex
	latest map[string]control.Quota
	tried  map[string]time.Time
	stale  map[string]bool
}

func newQuotaTracker() *quotaTracker {
	return &quotaTracker{latest: map[string]control.Quota{}, tried: map[string]time.Time{}, stale: map[string]bool{}}
}

// quotaKey names one login: the engine, or the extra account's id.
func quotaKey(engine, account string) string {
	if account != "" {
		return account
	}
	return engine
}

func (t *quotaTracker) observe(q control.Quota) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := quotaKey(q.Engine, q.Account)
	t.tried[key] = q.CheckedAt
	delete(t.stale, key)
	// A failed check never hides a good reading that is still recent.
	if prev, ok := t.latest[key]; ok && q.Error != "" && prev.Error == "" && q.CheckedAt.Sub(prev.CheckedAt) < 6*time.Hour {
		return
	}
	t.latest[key] = q
}

func (t *quotaTracker) markStale(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stale[key] = true
}

// due says whether the login named key needs a new reading.
func (t *quotaTracker) due(key string, every time.Duration, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	tried, ok := t.tried[key]
	return !ok || t.stale[key] || now.Sub(tried) >= every
}

// keep drops the readings of accounts that are gone from this node.
func (t *quotaTracker) keep(accounts map[string]bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, q := range t.latest {
		if q.Account != "" && !accounts[q.Account] {
			delete(t.latest, key)
			delete(t.tried, key)
		}
	}
}

// snapshot lists the readings: the node's own logins in engine order, then
// the extra accounts by id.
func (t *quotaTracker) snapshot() []control.Quota {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]control.Quota, 0, len(t.latest))
	for _, e := range control.AgentEngines {
		if q, ok := t.latest[e]; ok {
			out = append(out, q)
		}
	}
	extra := []control.Quota{}
	for _, q := range t.latest {
		if q.Account != "" {
			extra = append(extra, q)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].Account < extra[j].Account })
	if room := control.MaxQuotas - len(out); len(extra) > room {
		extra = extra[:room]
	}
	return append(out, extra...)
}

// extraAccounts lists the account folders logged in on this node.
func extraAccounts(cfg AgentConfig) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, def := range []string{cfg.ClaudeConfigDir, cfg.CodexHome, cfg.AgyHome} {
		if def == "" {
			continue
		}
		dir := filepath.Join(filepath.Dir(def), "accounts")
		if seen[dir] {
			continue
		}
		seen[dir] = true
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() && accountID.MatchString(e.Name()) {
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// refresh reads whichever quotas are due. It runs them one at a time.
func (t *quotaTracker) refresh(ctx context.Context, cfg AgentConfig, now time.Time) {
	if installed(cfg.CodexBin) && t.due("codex", codexQuotaEvery, now) {
		t.observe(readCodexQuota(ctx, cfg))
	}
	if runtime.GOOS == "windows" && installed(cfg.AgyBin) && t.due("antigravity", agyQuotaEvery, now) {
		t.observe(readAgyQuota(ctx, cfg, ""))
	}
	every := claudeProbeEvery()
	if every > 0 && installed(cfg.ClaudeBin) && t.due("claude", every, now) {
		t.observe(probeClaudeQuota(ctx, cfg))
	}
	have := map[string]bool{}
	for _, id := range extraAccounts(cfg) {
		have[id] = true
		engine, _, _ := strings.Cut(id, ".")
		acfg, err := cfg.withAccount(store.AgentSpec{Engine: engine, Account: id})
		if err != nil || ctx.Err() != nil {
			continue
		}
		var q control.Quota
		switch {
		case engine == "codex" && installed(cfg.CodexBin) && t.due(id, codexQuotaEvery, now):
			q = readCodexQuota(ctx, acfg)
		case engine == "claude" && every > 0 && installed(cfg.ClaudeBin) && t.due(id, every, now):
			q = probeClaudeQuota(ctx, acfg)
		case engine == "antigravity" && agyKeyringSupported && installed(cfg.AgyBin) && t.due(id, agyQuotaEvery, now):
			if _, _, _, pool, _ := agyPoolAccount(cfg, id); !pool {
				continue
			}
			q = readAgyQuota(ctx, acfg, id)
		default:
			continue
		}
		q.Account = id
		t.observe(q)
	}
	t.keep(have)
}

// loop refreshes quotas every minute until ctx ends.
func (t *quotaTracker) loop(ctx context.Context, cfg AgentConfig) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		t.refresh(ctx, cfg, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func installed(bin string) bool {
	if bin == "" {
		return false
	}
	_, err := os.Stat(bin)
	return err == nil
}

func percent(v float64) float64 { return math.Round(v*10) / 10 }

// claudeRateLimit is Claude Code's rate_limit_event payload.
type claudeRateLimit struct {
	Status         string  `json:"status"`
	ResetsAt       int64   `json:"resetsAt"`
	RateLimitType  string  `json:"rateLimitType"`
	Utilization    float64 `json:"utilization"`
	UnifiedWindows map[string]struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    int64   `json:"resetsAt"`
	} `json:"unifiedWindows"`
}

func (r claudeRateLimit) quota(now time.Time) *control.Quota {
	q := control.Quota{Engine: "claude", Limited: r.Status == "rejected", CheckedAt: now}
	window := func(name string) *control.QuotaWindow {
		if w, ok := r.UnifiedWindows[name]; ok {
			return &control.QuotaWindow{UsedPercent: percent(w.Utilization * 100), ResetsAt: w.ResetsAt}
		}
		if r.RateLimitType == name && r.Utilization > 0 {
			return &control.QuotaWindow{UsedPercent: percent(r.Utilization * 100), ResetsAt: r.ResetsAt}
		}
		return nil
	}
	q.FiveHour, q.Weekly = window("five_hour"), window("seven_day")
	if q.FiveHour == nil && q.Weekly == nil {
		return nil
	}
	return &q
}

// probeClaudeQuota runs the smallest possible Claude Code turn (Haiku, no
// tools) just to read the rate_limit_event it emits.
func probeClaudeQuota(ctx context.Context, cfg AgentConfig) control.Quota {
	fail := func(msg string) control.Quota {
		return control.Quota{Engine: "claude", CheckedAt: time.Now(), Error: clip(msg, 280)}
	}
	dir, err := cfg.workdir("")
	if err != nil {
		return fail(err.Error())
	}
	if err := os.MkdirAll(cfg.ClaudeConfigDir, 0o700); err != nil {
		return fail("cannot create the Claude Code config dir")
	}
	runCtx, cancel := context.WithTimeout(ctx, quotaTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, cfg.ClaudeBin, "-p", "--model", "haiku",
		"--output-format", "stream-json", "--verbose", "--max-turns", "1", "--tools", "")
	cmd.Dir = dir
	cmd.Env = claudeEnv(cfg.ClaudeConfigDir)
	cmd.Stdin = strings.NewReader("Reply with: ok")
	cmd.WaitDelay = 5 * time.Second
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(err.Error())
	}
	if err := cmd.Start(); err != nil {
		return fail("cannot start Claude Code: " + err.Error())
	}
	answer, isError, stats, _ := parseClaudeStream(stdout, nil)
	_, _ = io.Copy(io.Discard, stdout)
	_ = cmd.Wait()
	if stats.quota != nil {
		return *stats.quota
	}
	if isError && answer != "" {
		return fail(answer)
	}
	return fail("Claude Code did not report its limits")
}

// codexLimits is the rate limit part of account/rateLimits/read.
type codexLimits struct {
	OrdinaryUsageAllowed *bool `json:"ordinaryUsageAllowed"`
	RateLimits           *struct {
		Primary              *codexWindow `json:"primary"`
		Secondary            *codexWindow `json:"secondary"`
		PlanType             string       `json:"planType"`
		RateLimitReachedType string       `json:"rateLimitReachedType"`
	} `json:"rateLimits"`
}

type codexWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins int     `json:"windowDurationMins"`
	ResetsAt           int64   `json:"resetsAt"`
}

func (l codexLimits) quota(now time.Time) (control.Quota, error) {
	q := control.Quota{Engine: "codex", CheckedAt: now}
	if l.RateLimits == nil {
		return q, errors.New("Codex did not report its limits")
	}
	q.Plan = clip(l.RateLimits.PlanType, 32)
	q.Limited = l.RateLimits.RateLimitReachedType != "" || (l.OrdinaryUsageAllowed != nil && !*l.OrdinaryUsageAllowed)
	for _, w := range []*codexWindow{l.RateLimits.Primary, l.RateLimits.Secondary} {
		if w == nil {
			continue
		}
		win := &control.QuotaWindow{UsedPercent: percent(w.UsedPercent), ResetsAt: w.ResetsAt}
		if w.WindowDurationMins > 0 && w.WindowDurationMins <= 6*60 {
			q.FiveHour = win
		} else {
			q.Weekly = win
		}
	}
	return q, nil
}

// readCodexQuota asks Codex's app-server for the account's rate limits; it
// costs nothing and runs no model.
func readCodexQuota(ctx context.Context, cfg AgentConfig) control.Quota {
	fail := func(msg string) control.Quota {
		return control.Quota{Engine: "codex", CheckedAt: time.Now(), Error: clip(msg, 280)}
	}
	raw, err := codexAppServer(ctx, cfg, "account/rateLimits/read", nil)
	if err != nil {
		return fail(err.Error())
	}
	var limits codexLimits
	if err := json.Unmarshal(raw, &limits); err != nil {
		return fail("unexpected Codex answer")
	}
	q, err := limits.quota(time.Now())
	if err != nil {
		return fail(err.Error())
	}
	return q
}

// codexAppServer makes one request to Codex's app-server (JSON-RPC over
// stdio) and returns its result.
func codexAppServer(ctx context.Context, cfg AgentConfig, method string, params any) (json.RawMessage, error) {
	if err := os.MkdirAll(cfg.CodexHome, 0o700); err != nil {
		return nil, errors.New("cannot create the Codex home")
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, cfg.CodexBin, "app-server")
	cmd.Env = codexEnv(cfg.CodexHome)
	cmd.WaitDelay = 2 * time.Second
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.New("cannot start Codex: " + err.Error())
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	send := func(msg any) error {
		b, _ := json.Marshal(msg)
		_, err := stdin.Write(append(b, '\n'))
		return err
	}
	if err := send(map[string]any{"method": "initialize", "id": 0, "params": map[string]any{"clientInfo": map[string]string{"name": "avora", "version": "0.1"}}}); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		var msg struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil || msg.ID == nil {
			continue
		}
		if msg.Error != nil {
			return nil, errors.New("Codex: " + msg.Error.Message)
		}
		switch *msg.ID {
		case 0:
			req := map[string]any{"method": method, "id": 1}
			if params != nil {
				req["params"] = params
			}
			if send(map[string]any{"method": "initialized"}) != nil || send(req) != nil {
				return nil, errors.New("Codex app-server closed")
			}
		case 1:
			return msg.Result, nil
		}
	}
	if runCtx.Err() != nil {
		return nil, errors.New("Codex did not answer in time")
	}
	return nil, errors.New("Codex app-server ended without answering")
}
