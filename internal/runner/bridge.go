package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// The bridge gives a delegated Claude Code or Codex run the owner's Plugins
// as an MCP server on Avora (internal/control/bridge.go). The run's key is
// handed over in a private file (Claude Code) or an environment variable
// (Codex), never on the command line, where other users could read it.

const (
	bridgeServer = "avora"
	bridgeKeyEnv = "AVORA_BRIDGE_KEY"
	// bridgeTimeoutSec covers a locked action waiting for the owner.
	bridgeTimeoutSec = 1800
)

func bridgeURL(server string) string {
	return strings.TrimRight(server, "/") + "/v1/bridge/mcp"
}

// claudeBridge writes the MCP config of a run to a file only this user can
// read; cleanup removes it.
func claudeBridge(server, key string) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "avora-bridge-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	cfg := map[string]any{"mcpServers": map[string]any{bridgeServer: map[string]any{
		"type": "http", "url": bridgeURL(server), "headers": map[string]string{"Authorization": "Bearer " + key},
	}}}
	b, _ := json.Marshal(cfg)
	path = filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

// codexBridge is the -c overrides that add the bridge to codex exec; the
// key itself goes in AVORA_BRIDGE_KEY.
func codexBridge(server string) []string {
	url, _ := json.Marshal(bridgeURL(server))
	return []string{
		"-c", "mcp_servers." + bridgeServer + ".url=" + string(url),
		"-c", "mcp_servers." + bridgeServer + `.bearer_token_env_var="` + bridgeKeyEnv + `"`,
		"-c", "mcp_servers." + bridgeServer + ".tool_timeout_sec=" + itoa(bridgeTimeoutSec),
		// Avora's own gate asks the owner; Codex must not ask again (and
		// with approvals off it would refuse every write).
		"-c", "mcp_servers." + bridgeServer + `.default_tools_approval_mode="approve"`,
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
