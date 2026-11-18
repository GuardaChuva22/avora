package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// The test binary doubles as a fake Claude Code when AVORA_FAKE_CLAUDE is set.
func TestMain(m *testing.M) {
	if os.Getenv("AVORA_FAKE_CODEX") != "" && len(os.Args) > 1 && os.Args[1] == "exec" {
		fakeCodex()
		return
	}
	if os.Getenv("AVORA_FAKE_CLAUDE") != "" {
		fakeClaude()
		return
	}
	os.Exit(m.Run())
}

func fakeClaude() {
	prompt, _ := io.ReadAll(os.Stdin)
	if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("CLAUDE_CONFIG_DIR") == "" {
		fmt.Println(`{"type":"result","subtype":"success","is_error":true,"result":"bad environment"}`)
		os.Exit(1)
	}
	args := strings.Join(os.Args[1:], " ")
	fmt.Println(`{"type":"system","subtype":"init","model":"claude-fake","session_id":"s1"}`)
	if string(prompt) == "durma" {
		time.Sleep(30 * time.Second)
	}
	fmt.Println(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"README.md"}}]}}`)
	answer, _ := json.Marshal("feito: " + string(prompt) + " | " + args)
	fmt.Printf(`{"type":"result","subtype":"success","is_error":false,"result":%s,"num_turns":2,"duration_ms":1200,"total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":5}}`+"\n", answer)
}

func agentTestClient(t *testing.T) (*Client, *httptest.Server, string) {
	t.Helper()
	t.Setenv("AVORA_FAKE_CLAUDE", "1")
	t.Setenv("ANTHROPIC_API_KEY", "must-not-reach-the-agent")
	core, err := control.NewServer(testAdmin, store.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(core.Handler())
	t.Cleanup(server.Close)
	var code control.PairingCodeResponse
	adminCall(t, server.URL, "POST", "/v1/nodes/pairing-codes", control.PairingCodeRequest{Name: "pc", Kind: "windows"}, &code)
	state, err := LoadOrPair(context.Background(), filepath.Join(t.TempDir(), "node.json"), server.URL, code.Code)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(server.URL, state.Token)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	c.Agents = AgentConfig{ClaudeBin: os.Args[0], ClaudeConfigDir: filepath.Join(dir, "claude"), Workspace: filepath.Join(dir, "work"), MaxAgents: 1}
	if _, err := c.Heartbeat(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return c, server, state.NodeID
}

func waitJob(t *testing.T, base, id string) control.Job {
	t.Helper()
	var job control.Job
	for i := 0; i < 200; i++ {
		adminCall(t, base, "GET", "/v1/jobs/"+id, nil, &job)
		if job.Status.Terminal() {
			return job
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job never finished: %+v", job)
	return job
}

func TestAgentRunWithClaude(t *testing.T) {
	c, base, _ := agentTestClient(t)
	ctx := context.Background()
	var job control.Job
	adminCall(t, base.URL, "POST", "/v1/jobs", control.JobRequest{Action: store.ActionAgentRun,
		Agent: &store.AgentSpec{Engine: "claude", Prompt: "resuma", Workdir: "projeto"}}, &job)
	if job.Agent == nil || job.Agent.Mode != "read" || job.Agent.MaxMinutes != control.DefaultAgentMinutes {
		t.Fatalf("defaults not applied: %+v", job.Agent)
	}
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	done := waitJob(t, base.URL, job.ID)
	if done.Status != store.StatusSucceeded || !strings.HasPrefix(done.Output, "feito: resuma") {
		t.Fatalf("agent job: %+v", done)
	}
	if !strings.Contains(done.Output, "--disallowedTools Bash,Edit,Write") || !strings.Contains(done.Output, "--permission-mode default") {
		t.Fatalf("read mode not enforced: %s", done.Output)
	}
	if _, err := os.Stat(filepath.Join(c.Agents.Workspace, "projeto")); err != nil {
		t.Fatalf("workdir not created: %v", err)
	}
	var events []store.Event
	adminCall(t, base.URL, "GET", "/v1/events?job="+job.ID, nil, &events)
	var types []string
	for _, e := range events {
		types = append(types, e.Type)
	}
	if got := strings.Join(types, ","); got != "job.created,job.claimed,agent.step,job.succeeded" {
		t.Fatalf("events %s", got)
	}
	if !strings.Contains(string(events[2].Payload), "README.md") || !strings.Contains(string(events[3].Payload), `"turns":2`) {
		t.Fatalf("step/stats payloads: %s %s", events[2].Payload, events[3].Payload)
	}
}

func TestAgentRunCanceled(t *testing.T) {
	c, base, _ := agentTestClient(t)
	ctx := context.Background()
	var job control.Job
	adminCall(t, base.URL, "POST", "/v1/jobs", control.JobRequest{Action: store.ActionAgentRun,
		Agent: &store.AgentSpec{Engine: "claude", Prompt: "durma"}}, &job)
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	// The only slot is busy: a second agent job waits, a ping does not.
	var second, ping control.Job
	adminCall(t, base.URL, "POST", "/v1/jobs", control.JobRequest{Action: store.ActionAgentRun,
		Agent: &store.AgentSpec{Engine: "claude", Prompt: "x"}}, &second)
	adminCall(t, base.URL, "POST", "/v1/jobs", control.JobRequest{Action: "ping"}, &ping)
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := waitJob(t, base.URL, ping.ID); got.Status != store.StatusSucceeded {
		t.Fatalf("ping blocked behind agent: %+v", got)
	}
	adminCall(t, base.URL, "POST", "/v1/jobs/"+job.ID+"/cancel", nil, nil)
	start := time.Now()
	if err := c.Tick(ctx); err != nil { // heartbeat returns stop -> process killed
		t.Fatal(err)
	}
	c.wg.Wait()
	if time.Since(start) > 15*time.Second {
		t.Fatal("canceled agent kept running")
	}
	adminCall(t, base.URL, "GET", "/v1/jobs/"+job.ID, nil, &job)
	if job.Status != store.StatusCanceled {
		t.Fatalf("status %s", job.Status)
	}
	adminCall(t, base.URL, "GET", "/v1/jobs/"+second.ID, nil, &second)
	if second.Status != store.StatusQueued {
		t.Fatalf("second agent job started early: %s", second.Status)
	}
}

func TestClaudeStreamErrors(t *testing.T) {
	answer, isError, _, err := parseClaudeStream(strings.NewReader(
		`{"type":"result","subtype":"success","is_error":true,"result":"Not logged in · Please run /login"}`+"\n"), nil)
	if err != nil || !isError || !strings.Contains(answer, "Not logged in") {
		t.Fatalf("login error not surfaced: %q %v %v", answer, isError, err)
	}
	if _, _, _, err := parseClaudeStream(strings.NewReader(`{"type":"system"}`+"\n"), nil); err == nil {
		t.Fatal("stream without result accepted")
	}
}

func TestWorkdirStaysInWorkspace(t *testing.T) {
	cfg := AgentConfig{Workspace: t.TempDir()}
	if _, err := cfg.workdir("../fora"); err == nil {
		t.Fatal("escaped the workspace")
	}
	if dir, err := cfg.workdir("a/b"); err != nil || !strings.HasPrefix(dir, cfg.Workspace) {
		t.Fatalf("nested workdir: %s %v", dir, err)
	}
}

func fakeCodex() {
	prompt, _ := io.ReadAll(os.Stdin)
	if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("CODEX_HOME") == "" {
		fmt.Println(`{"type":"turn.failed","error":{"message":"bad environment"}}`)
		os.Exit(1)
	}
	args, _ := json.Marshal(strings.Join(os.Args[1:], " "))
	fmt.Println(`{"type":"thread.started","thread_id":"t1"}`)
	fmt.Println(`{"type":"turn.started"}`)
	if string(prompt) == "sem cota" {
		fmt.Println(`{"type":"error","message":"You've hit your usage limit."}`)
		fmt.Println(`{"type":"turn.failed","error":{"message":"You've hit your usage limit."}}`)
		os.Exit(1)
	}
	fmt.Println(`{"type":"item.completed","item":{"id":"i1","type":"command_execution","command":"cat nota.txt","exit_code":0,"status":"completed"}}`)
	fmt.Printf(`{"type":"item.completed","item":{"id":"i2","type":"agent_message","text":%s}}`+"\n", args)
	fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":900,"cached_input_tokens":100,"output_tokens":20}}`)
}

func TestAgentRunWithCodex(t *testing.T) {
	c, base, _ := agentTestClient(t)
	t.Setenv("AVORA_FAKE_CODEX", "1")
	t.Setenv("OPENAI_API_KEY", "must-not-reach-the-agent")
	c.Agents.CodexBin, c.Agents.CodexHome = os.Args[0], filepath.Join(t.TempDir(), "codex")
	ctx := context.Background()
	for _, tc := range []struct{ prompt, mode, wantStatus, want string }{
		{"resuma", "read", "succeeded", "--sandbox read-only"},
		{"edite", "edit", "succeeded", "--sandbox workspace-write"},
		{"sem cota", "read", "failed", "usage limit"},
	} {
		var job control.Job
		adminCall(t, base.URL, "POST", "/v1/jobs", control.JobRequest{Action: store.ActionAgentRun,
			Agent: &store.AgentSpec{Engine: "codex", Model: "gpt-6-luna", Prompt: tc.prompt, Mode: tc.mode}}, &job)
		if err := c.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		done := waitJob(t, base.URL, job.ID)
		got := done.Output + done.Error
		if string(done.Status) != tc.wantStatus || !strings.Contains(got, tc.want) {
			t.Fatalf("%s: %s %q", tc.prompt, done.Status, got)
		}
		if tc.wantStatus == "succeeded" && !strings.Contains(got, "--model gpt-6-luna") {
			t.Fatalf("model not passed: %s", got)
		}
	}
	var events []store.Event
	adminCall(t, base.URL, "GET", "/v1/events", nil, &events)
	found := false
	for _, e := range events {
		if e.Type == store.EventAgentStep && strings.Contains(string(e.Payload), "cat nota.txt") {
			found = true
		}
	}
	if !found {
		t.Fatal("codex command not audited")
	}
}

func TestClaudeModelFlag(t *testing.T) {
	args, err := claudeArgs("read", "haiku", "", "", "")
	if err != nil || args[0] != "--model" || args[1] != "haiku" {
		t.Fatalf("args %v %v", args, err)
	}
}

func TestBridgeArgs(t *testing.T) {
	path, cleanup, err := claudeBridge("http://127.0.0.1:3190/", "avb_key")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"url":"http://127.0.0.1:3190/v1/bridge/mcp"`) || !strings.Contains(string(b), "Bearer avb_key") {
		t.Fatalf("config: %s", b)
	}
	args, _ := claudeArgs("read", "", "", "", path)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--mcp-config "+path+" --strict-mcp-config") || !strings.Contains(joined, "WebFetch,TodoWrite,mcp__avora") || strings.Contains(joined, "avb_key") {
		t.Fatalf("claude args: %s", joined)
	}
	cleanup()
	if _, err := os.Stat(path); err == nil {
		t.Fatal("config left behind")
	}
	over := strings.Join(codexBridge("https://x.ts.net:8443"), " ")
	if !strings.Contains(over, `mcp_servers.avora.url="https://x.ts.net:8443/v1/bridge/mcp"`) || !strings.Contains(over, `bearer_token_env_var="AVORA_BRIDGE_KEY"`) {
		t.Fatalf("codex overrides: %s", over)
	}
}
