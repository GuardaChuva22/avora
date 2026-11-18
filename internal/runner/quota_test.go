package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
)

// Captured from Claude Code 2.1.322 on the VPS.
const claudeRateLine = `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","resetsAt":1794585600,"rateLimitType":"seven_day","utilization":0.81,"overageStatus":"rejected","isUsingOverage":false,"surpassedThreshold":0.75,"unifiedWindows":{"five_hour":{"utilization":0.01,"resetsAt":1794358200,"status":"allowed"},"seven_day":{"utilization":0.81,"resetsAt":1794585600,"status":"allowed_warning"}}},"session_id":"s"}`

func TestClaudeStreamReportsQuota(t *testing.T) {
	stream := claudeRateLine + "\n" + `{"type":"result","subtype":"success","result":"ok","num_turns":1}` + "\n"
	_, isError, stats, err := parseClaudeStream(strings.NewReader(stream), nil)
	if err != nil || isError || stats.quota == nil {
		t.Fatalf("err=%v isError=%v quota=%v", err, isError, stats.quota)
	}
	q := stats.quota
	if q.Engine != "claude" || q.Limited || q.FiveHour.UsedPercent != 1 || q.Weekly.UsedPercent != 81 ||
		q.FiveHour.ResetsAt != 1794358200 || q.Weekly.ResetsAt != 1794585600 {
		t.Fatalf("quota %+v five=%+v week=%+v", q, q.FiveHour, q.Weekly)
	}
}

// Captured from codex-cli 0.167.0 (account/rateLimits/read).
const codexLimitsJSON = `{"ordinaryUsageAllowed":false,"rateLimits":{"limitId":"codex","primary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":1794371890},"secondary":{"usedPercent":100,"windowDurationMins":10080,"resetsAt":1794431948},"planType":"plus","rateLimitReachedType":"rate_limit_reached"}}`

func TestCodexLimitsToQuota(t *testing.T) {
	var l codexLimits
	if err := json.Unmarshal([]byte(codexLimitsJSON), &l); err != nil {
		t.Fatal(err)
	}
	q, err := l.quota(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if q.Plan != "plus" || !q.Limited || q.FiveHour.UsedPercent != 0 || q.Weekly.UsedPercent != 100 || q.Weekly.ResetsAt != 1794431948 {
		t.Fatalf("quota %+v five=%+v week=%+v", q, q.FiveHour, q.Weekly)
	}
	if _, err := (codexLimits{}).quota(time.Now()); err == nil {
		t.Fatal("empty answer accepted")
	}
}

func TestTrackerKeepsGoodReadingOverFailure(t *testing.T) {
	tr := newQuotaTracker()
	now := time.Now()
	var l codexLimits
	if err := json.Unmarshal([]byte(codexLimitsJSON), &l); err != nil {
		t.Fatal(err)
	}
	good, _ := l.quota(now)
	tr.observe(good)
	later := now.Add(time.Minute)
	tr.observe(control.Quota{Engine: "codex", CheckedAt: later, Error: "timeout"})
	if s := tr.snapshot(); len(s) != 1 || s[0].Error != "" {
		t.Fatalf("snapshot %+v", s)
	}
	if tr.due("codex", time.Hour, later) {
		t.Fatal("a failed check right now must not be retried at once")
	}
	tr.markStale("codex")
	if !tr.due("codex", time.Hour, later) {
		t.Fatal("stale reading not due")
	}
}

func TestResumeArgs(t *testing.T) {
	args, err := codexArgs("read", "", "/w", true, "abc-123")
	joined := strings.Join(args, " ")
	if err != nil || strings.Contains(joined, "--ephemeral") || !strings.HasSuffix(joined, "resume abc-123 -") {
		t.Fatalf("codex resume args: %v %v", args, err)
	}
	if args, _ := codexArgs("read", "", "/w", false, ""); !strings.Contains(strings.Join(args, " "), "--ephemeral") {
		t.Fatalf("one-off codex run must stay ephemeral: %v", args)
	}
	args, err = claudeArgs("edit", "", "", "sess-9", "")
	if err != nil || args[0] != "--resume" || args[1] != "sess-9" {
		t.Fatalf("claude resume args: %v %v", args, err)
	}
}

func TestQuotaTrackerKeepsOneReadingPerAccount(t *testing.T) {
	tr := newQuotaTracker()
	now := time.Now()
	tr.observe(control.Quota{Engine: "claude", CheckedAt: now})
	tr.observe(control.Quota{Engine: "claude", Account: "claude.bbbbbb", CheckedAt: now})
	tr.observe(control.Quota{Engine: "claude", Account: "claude.aaaaaa", CheckedAt: now})
	tr.observe(control.Quota{Engine: "codex", CheckedAt: now})
	s := tr.snapshot()
	if len(s) != 4 || s[0].Engine != "claude" || s[0].Account != "" || s[1].Engine != "codex" || s[2].Account != "claude.aaaaaa" {
		t.Fatalf("snapshot: %+v", s)
	}
	if tr.due("claude.aaaaaa", time.Hour, now) || !tr.due("claude.cccccc", time.Hour, now) {
		t.Fatal("due per account")
	}
	tr.keep(map[string]bool{"claude.aaaaaa": true})
	if s := tr.snapshot(); len(s) != 3 {
		t.Fatalf("removed account kept: %+v", s)
	}
}

func TestExtraAccountsFromFolders(t *testing.T) {
	base := t.TempDir()
	for _, d := range []string{"claude.abc123", "codex.def456", "junk", "claude.UPPER1"} {
		if err := os.MkdirAll(filepath.Join(base, "accounts", d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got := extraAccounts(AgentConfig{ClaudeConfigDir: filepath.Join(base, "claude"), CodexHome: filepath.Join(base, "codex")})
	if strings.Join(got, ",") != "claude.abc123,codex.def456" {
		t.Fatalf("accounts: %v", got)
	}
}

func TestCodexArgsSearchAndEffort(t *testing.T) {
	args, err := codexArgs("read", "gpt-6.1-sol", "/w", false, "", "search", "effort=high")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	if args[0] != "--search" || args[1] != "exec" || !strings.Contains(got, `-c model_reasoning_effort="high"`) || !strings.Contains(got, "--model gpt-6.1-sol") {
		t.Fatalf("args %q", got)
	}
}
