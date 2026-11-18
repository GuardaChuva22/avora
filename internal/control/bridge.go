package control

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/llm"
)

// The bridge: while an agent delegates a task to Claude Code or Codex, the
// run gets Avora as an MCP server with the agent's Plugins (Gmail, ClickUp,
// MCP apps). Each call comes back here and runs exactly like the agent's
// own: same accounts and "conta", same "Quem usa", same approval gate. The
// credentials never leave the vault; the CLI only holds a key that works
// for this one run and dies with it.

const bridgeProtocol = "2025-06-18"

type bridgeRun struct {
	t *turn
	// full gives every tool of the turn, not only the Plugins: the run is
	// the agent's own brain (Codex as the main model).
	full    bool
	expires time.Time
}

type bridges struct {
	mu   sync.Mutex
	runs map[string]bridgeRun // sha256 of the key -> run
}

func bridgeHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// open issues a key for one run; close revokes it.
func (b *bridges) open(t *turn, ttl time.Duration, now time.Time) (key string, close func()) {
	return b.openRun(bridgeRun{t: t}, ttl, now)
}

func (b *bridges) openRun(run bridgeRun, ttl time.Duration, now time.Time) (key string, close func()) {
	key = "avb_" + randomString(32)
	h := bridgeHash(key)
	b.mu.Lock()
	if b.runs == nil {
		b.runs = map[string]bridgeRun{}
	}
	for k, r := range b.runs {
		if now.After(r.expires) {
			delete(b.runs, k)
		}
	}
	run.expires = now.Add(ttl)
	b.runs[h] = run
	b.mu.Unlock()
	return key, func() {
		b.mu.Lock()
		delete(b.runs, h)
		b.mu.Unlock()
	}
}

func (b *bridges) find(key string, now time.Time) (bridgeRun, bool) {
	if !strings.HasPrefix(key, "avb_") {
		return bridgeRun{}, false
	}
	h := bridgeHash(key)
	b.mu.Lock()
	defer b.mu.Unlock()
	for k, r := range b.runs {
		if subtle.ConstantTimeCompare([]byte(k), []byte(h)) == 1 && now.Before(r.expires) {
			return r, true
		}
	}
	return bridgeRun{}, false
}

// bridgeApps names the apps (and accounts) the agent can reach through the
// bridge, or "" when it has none.
func (s *Server) bridgeApps(ctx context.Context, agentID string) string {
	if s.connectors == nil {
		return ""
	}
	conns, err := s.store.ListConnections(ctx)
	if err != nil {
		return ""
	}
	kinds, by := accountsOf(conns)
	out := []string{}
	for _, kind := range kinds {
		con, ok := s.connectors.Get(kind)
		usable := usableOnly(by[kind], agentID)
		if !ok || len(usable) == 0 {
			continue
		}
		name := con.Info().Name
		if apps := con.Info().Apps; len(apps) > 0 {
			name = strings.Join(apps, ", ")
		}
		out = append(out, name+" ("+strings.Join(accountLabels(usable), ", ")+")")
	}
	return strings.Join(out, "; ")
}

// bridgeTools are the Plugins the agent may use, as MCP tools; a full run
// gets every tool of the turn.
func (s *Server) bridgeTools(ctx context.Context, run bridgeRun) []llm.Tool {
	if run.full {
		return s.toolsFor(run.t)
	}
	return s.connectorTools(ctx, run.t.agent.ID, nil)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) bridgeMCP(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	run, ok := s.bridges.find(key, s.now())
	t := run.t
	if !ok {
		http.Error(w, "unknown or finished run", http.StatusUnauthorized)
		return
	}
	var req rpcRequest
	if !readJSONMax(w, r, &req, 1<<20) {
		return
	}
	if len(req.ID) == 0 {
		// A notification (initialized, cancelled): nothing to answer.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	reply := func(result any, rerr *rpcError) {
		out := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rerr != nil {
			out["error"] = rerr
		} else {
			out["result"] = result
		}
		writeJSON(w, http.StatusOK, out)
	}
	ctx := r.Context()
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := bridgeProtocol
		if p.ProtocolVersion != "" && p.ProtocolVersion < bridgeProtocol {
			version = p.ProtocolVersion
		}
		instructions := "Apps conectados do dono no Avora. Enviar, comentar, apagar ou pagar espera a aprovação do dono no app; a chamada fica aberta até ele decidir."
		if run.full {
			instructions = "As ferramentas do funcionário no Avora: memória, rotinas, colegas, Claude Code, Codex, arquivos e os apps conectados. Ações travadas esperam a aprovação do dono no app; a chamada fica aberta até ele decidir."
		}
		reply(map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "avora", "title": "Avora", "version": "1"},
			"instructions":    instructions,
		}, nil)
	case "ping":
		reply(map[string]any{}, nil)
	case "tools/list":
		tools := []map[string]any{}
		for _, tl := range s.bridgeTools(ctx, run) {
			schema := tl.Function.Parameters
			if len(schema) == 0 || string(schema) == "null" {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			item := map[string]any{"name": tl.Function.Name, "description": tl.Function.Description, "inputSchema": schema}
			if s.connectors == nil {
				// no Plugins: no risk annotations
			} else if _, def, ok := s.connectors.Tool(tl.Function.Name); ok {
				risk := def.Risk
				item["annotations"] = map[string]any{"readOnlyHint": risk == connect.Read, "destructiveHint": risk == connect.Delete || risk == connect.Spend}
			}
			tools = append(tools, item)
		}
		reply(map[string]any{"tools": tools}, nil)
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &p) != nil || p.Name == "" {
			reply(nil, &rpcError{Code: -32602, Message: "invalid params"})
			return
		}
		allowed := false
		for _, tl := range s.bridgeTools(ctx, run) {
			allowed = allowed || tl.Function.Name == p.Name
		}
		if !allowed {
			reply(nil, &rpcError{Code: -32602, Message: "ferramenta desconhecida ou não liberada para este funcionário: " + p.Name})
			return
		}
		// A locked action waits for the owner; keep the answer open that long.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(s.approvalWait() + connectorTimeout + time.Minute))
		args := string(p.Arguments)
		if strings.TrimSpace(args) == "" || args == "null" {
			args = "{}"
		}
		out := s.callTool(ctx, t, llm.ToolCall{Type: "function", Function: llm.FunctionCall{Name: p.Name, Arguments: args}})
		isError := strings.HasPrefix(out, "Erro:") || strings.HasPrefix(out, "O dono ")
		reply(map[string]any{"content": []map[string]any{{"type": "text", "text": out}}, "isError": isError}, nil)
	default:
		reply(nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method})
	}
}
