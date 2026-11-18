package control

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBridgeRealClaude runs the real Claude Code against the bridge. Opt-in:
// AVORA_E2E_CLAUDE=path/to/claude.
func TestBridgeRealClaude(t *testing.T) {
	bin := os.Getenv("AVORA_E2E_CLAUDE")
	if bin == "" {
		t.Skip("AVORA_E2E_CLAUDE not set")
	}
	s, f := pluginServer(t, nil)
	connectClickUp(t, s)
	agent, _ := s.store.GetAgent(t.Context(), "atlas")
	key, closeBridge := s.bridges.open(&turn{agent: agent, id: "e2e"}, time.Hour, s.now())
	defer closeBridge()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := `{"mcpServers":{"avora":{"type":"http","url":"` + srv.URL + `/v1/bridge/mcp","headers":{"Authorization":"Bearer ` + key + `"}}}}`
	path := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(path, []byte(cfg), 0o600)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-p", "--model", "haiku", "--mcp-config", path, "--strict-mcp-config",
		"--allowedTools", "mcp__avora", "--max-turns", "4", "--output-format", "text")
	cmd.Stdin = strings.NewReader("Use a ferramenta clickup_criar_tarefa do servidor avora para criar a tarefa 'Teste da ponte' na lista l1. Depois responda só OK.")
	out, err := cmd.CombinedOutput()
	t.Logf("claude: %s", out)
	if err != nil || len(f.created) != 1 || !strings.Contains(f.created[0], "Teste da ponte") {
		t.Fatalf("err=%v created=%v", err, f.created)
	}
}

// TestBridgeRealCodex is the same with the real Codex. Opt-in:
// AVORA_E2E_CODEX=path/to/codex.
func TestBridgeRealCodex(t *testing.T) {
	bin := os.Getenv("AVORA_E2E_CODEX")
	if bin == "" {
		t.Skip("AVORA_E2E_CODEX not set")
	}
	s, f := pluginServer(t, nil)
	connectClickUp(t, s)
	agent, _ := s.store.GetAgent(t.Context(), "atlas")
	key, closeBridge := s.bridges.open(&turn{agent: agent, id: "e2e"}, time.Hour, s.now())
	defer closeBridge()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "exec", "-c", `mcp_servers.avora.url="`+srv.URL+`/v1/bridge/mcp"`,
		"-c", `mcp_servers.avora.bearer_token_env_var="AVORA_BRIDGE_KEY"`, "-c", "mcp_servers.avora.tool_timeout_sec=1800", "-c", `mcp_servers.avora.default_tools_approval_mode="approve"`,
		"--json", "--skip-git-repo-check", "--sandbox", "read-only", "-c", `approval_policy="never"`, "--ephemeral", "--cd", t.TempDir(), "-")
	cmd.Env = append(os.Environ(), "AVORA_BRIDGE_KEY="+key)
	cmd.Stdin = strings.NewReader("Use a ferramenta clickup_criar_tarefa do servidor MCP avora para criar a tarefa 'Teste da ponte' na lista l1. Depois responda só OK.")
	out, err := cmd.CombinedOutput()
	if len(out) > 3000 {
		out = out[len(out)-3000:]
	}
	t.Logf("codex: %s", out)
	if err != nil || len(f.created) != 1 || !strings.Contains(f.created[0], "Teste da ponte") {
		t.Fatalf("err=%v created=%v", err, f.created)
	}
}
