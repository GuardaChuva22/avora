package runner

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Manual: AVORA_AGY_LIVE=1 checks agy calls Avora's tools through the bridge
// config of a run home without asking.
func TestAgyLiveBridge(t *testing.T) {
	if os.Getenv("AVORA_AGY_LIVE") == "" {
		t.Skip("set AVORA_AGY_LIVE=1")
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k123" {
			http.Error(w, "no", 401)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "", 405)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(b, &req)
		t.Logf("mcp %s", req.Method)
		if req.ID == nil {
			w.WriteHeader(202)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "avora", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "senha_do_dia", "description": "Devolve a senha do dia.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "tools/call":
			calls.Add(1)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "abacaxi-azul-77"}}}
		default:
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()
	cfg := DefaultAgentConfig()
	cfg.AgyHome = t.TempDir()
	cfg.Workspace = t.TempDir()
	cfg.ServerURL = srv.URL
	r := RunAgyMain(t.Context(), cfg, store.AgentSpec{Engine: "antigravity", Mode: "chat", MaxMinutes: 4, Bridge: "k123",
		Prompt: "Use a ferramenta senha_do_dia do servidor MCP avora e responda só a senha que ela devolver."},
		func(st control.AgentStep) { t.Logf("step %s: %s", st.Tool, st.Summary) })
	t.Logf("success=%v out=%q err=%q calls=%d", r.Success, r.Output, r.Error, calls.Load())
	if calls.Load() == 0 || !strings.Contains(r.Output, "abacaxi") {
		t.Errorf("the tool was not called")
	}
}
