package control

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/mcp"
)

const (
	installMCPTool = "instalar_mcp"
	createMCPTool  = "criar_mcp"
)

func isMCPTool(name string) bool { return name == installMCPTool || name == createMCPTool }

type mcpToolArgs struct {
	Nome        string `json:"nome"`
	URL         string `json:"url"`
	Repositorio string `json:"repositorio"`
	Revisao     string `json:"revisao"`
	Entrada     string `json:"entrada"`
	Linguagem   string `json:"linguagem"`
	Codigo      string `json:"codigo"`
	Rede        bool   `json:"rede"`
}

func (s *Server) pluginTools() []llm.Tool {
	if s.connectors == nil || s.vault == nil {
		return nil
	}
	description := "Quando o dono pedir, instala um servidor MCP e registra em Plugins. Para um servidor remoto informe url (endereço HTTPS do MCP). OAuth e chaves são concluídos pelo dono em Plugins."
	if s.workspace != "" {
		description += " Para um repositório público GitHub de MCP Node.js, confira o README e informe repositorio e entrada (arquivo JavaScript compilado que fala MCP via stdio). A Avora instala e compila no sandbox da VPS com a aprovação do dono."
	}
	tools := []llm.Tool{tool(installMCPTool, description, `{"type":"object","properties":{
		"nome":{"type":"string","description":"Nome opcional para a página Plugins."},
		"url":{"type":"string","description":"Endpoint HTTPS de um servidor MCP remoto."},
		"repositorio":{"type":"string","description":"URL HTTPS do repositório GitHub de um servidor Node.js stdio com package-lock.json."},
		"entrada":{"type":"string","description":"Arquivo JS relativo à raiz do repositório, ex.: build/index.js."},
		"revisao":{"type":"string","description":"SHA completo do commit, opcional. Vazio usa o branch padrão."}
	}}`)}
	if s.workspace != "" {
		tools = append(tools, tool(createMCPTool, "Quando o dono pedir um MCP novo, instala em Plugins um servidor que você escreveu. Forneça um arquivo completo sem dependências externas, com JSON-RPC MCP via stdio (initialize, tools/list, tools/call e notifications/initialized). Emita somente mensagens MCP no stdout e mande logs ao stderr. A Avora testa no sandbox da VPS após aprovação.", `{"type":"object","properties":{
			"nome":{"type":"string"},
			"linguagem":{"type":"string","enum":["python3","node"]},
			"codigo":{"type":"string","description":"Código-fonte completo em um arquivo, sem blocos Markdown."},
			"rede":{"type":"boolean","description":"Ative quando o servidor precisar consultar uma API pública."}
		},"required":["nome","linguagem","codigo"]}`))
	}
	return tools
}

func localMCPDraft(name string, a mcpToolArgs) (localMCPConfig, string, error) {
	cfg := localMCPConfig{Network: a.Rede}
	label := truncate(strings.TrimSpace(a.Nome), 60)
	mode := "list"
	switch name {
	case installMCPTool:
		repo, err := url.Parse(strings.TrimSpace(a.Repositorio))
		if err != nil || repo.Scheme != "https" || !strings.EqualFold(repo.Host, "github.com") || repo.User != nil ||
			repo.RawQuery != "" || repo.Fragment != "" {
			return cfg, "", errors.New("informe a URL HTTPS do repositório GitHub, no formato https://github.com/usuario/repositorio")
		}
		repoPath := strings.TrimSuffix(strings.ToLower(strings.TrimRight(repo.Path, "/")), ".git")
		cfg.Repository = "https://github.com" + repoPath
		cfg.Revision = strings.ToLower(strings.TrimSpace(a.Revisao))
		cfg.Entry = strings.TrimSpace(a.Entrada)
		cfg.Runtime, cfg.Network = "node", true
		mode = "install"
		a.Codigo = ""
		if label == "" {
			label = truncate(repoPath[strings.LastIndexByte(repoPath, '/')+1:], 60)
		}
	case createMCPTool:
		cfg.Runtime = strings.TrimSpace(a.Linguagem)
		switch cfg.Runtime {
		case "node":
			cfg.Entry = "server.mjs"
		case "python3":
			cfg.Entry = "server.py"
		default:
			return cfg, "", errors.New("use python3 ou node como linguagem do MCP")
		}
		if label == "" || strings.TrimSpace(a.Codigo) == "" || len(a.Codigo) > 48<<10 || strings.ContainsRune(a.Codigo, 0) {
			return cfg, "", errors.New("informe o nome e o código-fonte do MCP, com até 48 KB")
		}
	}
	job := mcp.PluginJob{Mode: mode, Repository: cfg.Repository, Revision: cfg.Revision, Runtime: cfg.Runtime, Entry: cfg.Entry}
	if err := job.Validate(); err != nil {
		return cfg, "", err
	}
	identity, _ := json.Marshal(cfg)
	cfg.Identity = fmt.Sprintf("%x", sha256.Sum256(append(identity, a.Codigo...)))
	return cfg, label, nil
}

func (s *Server) findLocalMCP(cfg localMCPConfig, label string) *localMCPApp {
	for _, con := range s.connectors.All() {
		if app, ok := con.(*localMCPApp); ok && app.backend.config.Identity == cfg.Identity &&
			(cfg.Repository != "" || strings.EqualFold(app.Name, label)) {
			return app
		}
	}
	return nil
}

func (s *Server) installLocalMCP(ctx context.Context, t *turn, actor string, cfg localMCPConfig, label, source string) (connect.Connector, bool, error) {
	if s.workspace == "" {
		return nil, false, errors.New("o sandbox da VPS não está configurado")
	}
	if existing := s.findLocalMCP(cfg, label); existing != nil {
		return existing, false, nil
	}
	cfg.Workdir = "plugins/" + newID()
	root, err := os.OpenRoot(s.workspace)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	if err := root.MkdirAll(cfg.Workdir, 0o700); err != nil {
		return nil, false, err
	}
	keep := false
	defer func() {
		if !keep {
			s.removeMCPWorkdir(cfg.Workdir)
		}
	}()
	mode := "install"
	if cfg.Repository == "" {
		mode = "list"
		if err := root.WriteFile(cfg.Workdir+"/"+cfg.Entry, []byte(source), 0o600); err != nil {
			return nil, false, err
		}
	}
	backend := &localMCP{server: s, name: label, config: cfg}
	res, err := backend.run(ctx, t, actor, mode, "", nil)
	if err != nil {
		return nil, false, err
	}
	if len(res.Tools) == 0 {
		return nil, false, errors.New("o servidor MCP iniciou, mas não publicou ferramentas")
	}
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if existing := s.findLocalMCP(cfg, label); existing != nil {
		return existing, false, nil
	}
	slug := strings.Trim(slugChars.ReplaceAllString(strings.ToLower(label), "-"), "-")
	prefix := "mcp-local-" + orDefault(truncate(slug, 36), "plugin")
	kind := prefix
	for n := 2; ; n++ {
		if _, exists := s.connectors.Get(kind); !exists {
			break
		}
		kind = fmt.Sprintf("%s-%d", prefix, n)
	}
	app := s.localMCPApp(kind, label, cfg)
	account := app.LoadTools(res.Server, res.Tools)
	if _, err := s.saveCheckedCredential(ctx, app, connect.Cred{}, "", actor, account); err != nil {
		return nil, false, err
	}
	s.connectors.Add(app)
	keep = true
	return app, true, nil
}

func (s *Server) toolMCP(ctx context.Context, t *turn, name string, a mcpToolArgs) (string, error) {
	if s.connectors == nil || s.vault == nil {
		return "", errors.New("os plugins não estão configurados")
	}
	var con connect.Connector
	var created bool
	var err error
	if name == installMCPTool && strings.TrimSpace(a.URL) != "" {
		if strings.TrimSpace(a.Repositorio) != "" || strings.TrimSpace(a.Entrada) != "" || strings.TrimSpace(a.Revisao) != "" {
			return "", errors.New("informe o endpoint MCP remoto ou o repositório GitHub")
		}
		if addr, parseErr := url.Parse(strings.TrimSpace(a.URL)); parseErr == nil && strings.EqualFold(addr.Hostname(), "github.com") {
			return "", errors.New("para um repositório do GitHub, consulte o README e use repositorio e entrada")
		}
		con, created, err = s.installRemoteMCP(ctx, AddMCPRequest{URL: a.URL, Name: a.Nome}, "agent:"+t.agent.ID)
	} else {
		cfg, label, draftErr := localMCPDraft(name, a)
		if draftErr != nil {
			return "", draftErr
		}
		con, created, err = s.installLocalMCP(ctx, t, "agent:"+t.agent.ID, cfg, label, a.Codigo)
	}
	if err != nil {
		return "", err
	}
	view := s.connectorView(ctx, con)
	summary := "«" + view.Name + "» já está em Plugins"
	if created {
		summary = "«" + view.Name + "» foi adicionado em Plugins"
	}
	s.step(t, AgentStep{Tool: name, Summary: summary})
	if len(view.Connections) == 0 {
		return summary + ". " + view.Help, nil
	}
	if len(usableOnly(view.Connections, t.agent.ID)) == 0 {
		return summary + ". Confira o acesso deste agente e o estado da conexão em Plugins.", nil
	}
	names := make([]string, 0, len(con.Tools()))
	for _, tool := range con.Tools() {
		names = append(names, tool.Name)
	}
	data, _ := json.Marshal(names)
	return fmt.Sprintf("%s e está conectado. Ferramentas disponíveis para você: %s.", summary, data), nil
}
