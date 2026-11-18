package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/mcp"
	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
)

var pluginWorkdir = regexp.MustCompile(`^plugins/[a-f0-9]{24}$`)

const localMCPInstallTimeout = 6 * time.Minute

type localMCPConfig struct {
	Workdir    string `json:"workdir"`
	Repository string `json:"repository,omitempty"`
	Revision   string `json:"revision,omitempty"`
	Runtime    string `json:"runtime"`
	Entry      string `json:"entry"`
	Network    bool   `json:"network,omitempty"`
	Identity   string `json:"identity"`
}

type localMCPState struct {
	Local localMCPConfig  `json:"local"`
	MCP   json.RawMessage `json:"mcp"`
}

// localMCPApp exposes a stdio server through the same registry, tool schemas
// and approvals as a remote MCP server.
type localMCPApp struct {
	*connect.MCPApp
	backend *localMCP
}

func (a *localMCPApp) State() json.RawMessage {
	state, _ := json.Marshal(localMCPState{Local: a.backend.config, MCP: a.MCPApp.State()})
	return state
}

func (a *localMCPApp) Restore(data []byte) {
	var st localMCPState
	if json.Unmarshal(data, &st) == nil {
		a.MCPApp.Restore(st.MCP)
	}
}

func (s *Server) localMCPApp(kind, name string, cfg localMCPConfig) *localMCPApp {
	backend := &localMCP{server: s, name: name, config: cfg}
	description := "Servidor MCP criado no Avora"
	if cfg.Repository != "" {
		description = "Servidor MCP de " + strings.TrimPrefix(cfg.Repository, "https://")
	}
	return &localMCPApp{MCPApp: &connect.MCPApp{
		Kind: kind, Name: name, URL: cfg.Repository, Custom: true, Auth: mcp.AuthNone,
		Category: "Seus apps", Description: description, Local: backend,
	}, backend: backend}
}

func (s *Server) restoreLocalMCP(c store.Connection) bool {
	var st localMCPState
	if json.Unmarshal(c.Meta, &st) != nil || st.Local.Workdir == "" {
		return false
	}
	var saved connect.MCPState
	if !pluginWorkdir.MatchString(st.Local.Workdir) || json.Unmarshal(st.MCP, &saved) != nil || saved.Name == "" {
		log.Printf("plugin %s: estado do MCP local inválido", c.Kind)
		return true
	}
	app := s.localMCPApp(c.Kind, saved.Name, st.Local)
	app.MCPApp.Restore(st.MCP)
	s.connectors.Add(app)
	return true
}

func (s *Server) removeLocalMCP(con connect.Connector) {
	if app, ok := con.(*localMCPApp); ok {
		backend := app.backend
		backend.removed.Store(true)
		remove := func() {
			defer backend.mu.Unlock()
			s.removeMCPWorkdir(backend.config.Workdir)
		}
		if backend.mu.TryLock() {
			remove()
		} else {
			// The registration is gone. Clean up once in-flight jobs finish.
			go func() {
				backend.mu.Lock()
				remove()
			}()
		}
	}
}

func (s *Server) removeMCPWorkdir(dir string) {
	if s.workspace == "" || !pluginWorkdir.MatchString(dir) {
		return
	}
	root, err := os.OpenRoot(s.workspace)
	if err == nil {
		defer root.Close()
		err = root.RemoveAll(dir)
	}
	if err != nil {
		log.Printf("plugin workspace %s: %v", dir, err)
	}
}

type localMCP struct {
	server  *Server
	name    string
	config  localMCPConfig
	mu      sync.RWMutex
	removed atomic.Bool
}

func (m *localMCP) List(ctx context.Context) (mcp.ServerInfo, []mcp.Tool, error) {
	res, err := m.run(ctx, nil, store.ActorSystem, "list", "", nil)
	return res.Server, res.Tools, err
}

func (m *localMCP) Call(ctx context.Context, name string, args json.RawMessage) (mcp.CallResult, error) {
	res, err := m.run(ctx, nil, store.ActorSystem, "call", name, args)
	return res.Result, err
}

func (m *localMCP) run(ctx context.Context, t *turn, actor, mode, toolName string, args json.RawMessage) (mcp.PluginResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res mcp.PluginResult
	if m.removed.Load() {
		return res, errors.New("o plugin foi removido")
	}
	s := m.server
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return res, err
	}
	node := s.onlineNode(nodes, scheduler.VPS)
	if node == "" {
		return res, errors.New("o nó da VPS está offline")
	}
	cfg := m.config
	payload, err := json.Marshal(mcp.PluginJob{
		Mode: mode, Repository: cfg.Repository, Revision: cfg.Revision, Runtime: cfg.Runtime,
		Entry: cfg.Entry, Name: toolName, Arguments: args,
	})
	if err != nil {
		return res, err
	}
	spec := &store.AgentSpec{Engine: "mcp", Prompt: string(payload), Mode: "offline", Workdir: cfg.Workdir, MaxMinutes: 1}
	if cfg.Network || mode == "install" {
		spec.Mode = "net"
	}
	limit := 75 * time.Second
	if mode == "install" {
		spec.MaxMinutes = 5
		limit = localMCPInstallTimeout
	}
	jobCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	label := "Plugin MCP " + m.name
	end, err := s.runNodeJobAs(jobCtx, t, spec, scheduler.VPS, node, label, actor, "")
	if err != nil {
		return res, err
	}
	if jobCtx.Err() != nil {
		return res, jobCtx.Err()
	}
	if end.result == nil {
		return res, fmt.Errorf("a tarefa do MCP terminou com status %s", end.job.Status)
	}
	if !end.result.Success {
		return res, errors.New(orDefault(end.result.Error, "a execução do MCP falhou"))
	}
	if err := json.Unmarshal([]byte(end.result.Output), &res); err != nil {
		return res, errors.New("o nó da VPS devolveu uma resposta MCP inválida")
	}
	return res, nil
}
