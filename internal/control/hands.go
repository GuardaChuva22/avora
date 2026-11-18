package control

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
	"github.com/GuardaChuva22/avora/internal/web"
)

// The agents' own hands (Fase A): the web, their folder, a sandboxed shell
// and a memory, all without delegating to Claude Code or Codex.
//
//   - buscar_na_web, ler_pagina: public internet only (internal/web).
//   - listar_arquivos, ler_arquivo, escrever_arquivo: the agent's folder on
//     the VPS; apagar_arquivo is locked.
//   - executar: a command in a bubblewrap sandbox on the VPS node, in the
//     agent's folder, without network; with rede=true it is locked.
//   - lembrar, recordar, esquecer: the agent's memory, shown in its
//     profile; the newest memories go into every conversation.

const (
	// MaxMemoriesPerAgent caps one agent's memory.
	MaxMemoriesPerAgent = 300
	// MaxMemoryChars caps one memory.
	MaxMemoryChars = 1000
	// promptMemories is how many memories every conversation starts with.
	promptMemories = 40
	// promptMemoryChars bounds them in the system prompt.
	promptMemoryChars = 6000
	// MaxShellCommand caps a command's length.
	MaxShellCommand = 8000
)

// SetWeb replaces the web client (tests, or a Brave key).
func (s *Server) SetWeb(c *web.Client) { s.web = c }

func (s *Server) webClient() *web.Client {
	if s.web == nil {
		s.web = web.New(web.Options{})
	}
	return s.web
}

func handTools(files bool) []llm.Tool {
	return handToolsFor(files, files)
}

func handToolsFor(files, shell bool) []llm.Tool {
	tools := []llm.Tool{
		tool("buscar_na_web", "Busca na web (resultados atuais): devolve título, link e trecho de cada resultado. Use para preços, notícias, fatos recentes, encontrar sites. Depois leia as páginas que importam com ler_pagina.", `{"type": "object", "properties": {
	"busca": {"type": "string", "description": "O que buscar, como você digitaria num buscador."},
	"quantidade": {"type": "integer", "minimum": 1, "maximum": 10, "description": "Quantos resultados (padrão 8)."}
}, "required": ["busca"]}`),
		tool("ler_pagina", "Lê uma página da web e devolve o texto dela (e os links, na primeira parte). Páginas longas vêm em partes: peça parte=2, 3...", `{"type": "object", "properties": {
	"url": {"type": "string"},
	"parte": {"type": "integer", "minimum": 1, "description": "Qual parte do texto (padrão 1)."}
}, "required": ["url"]}`),
		tool("lembrar", "Guarda na sua memória (ou na memória do time) algo que vale para conversas futuras: preferências, fatos, decisões, lições ou regras do dono. Uma ideia por memória, escrita numa frase clara. Regras ditas pelo dono devem usar kind='rule'.", `{"type": "object", "properties": {
	"conteudo": {"type": "string", "description": "O que lembrar, numa frase completa."},
	"kind": {"type": "string", "enum": ["decision", "fact", "preference", "lesson", "rule"], "description": "Tipo da memória: decision, fact (padrão), preference, lesson ou rule (regra dita pelo dono: entra verificada e fixada)."},
	"scope": {"type": "string", "enum": ["agent", "team"], "description": "Escopo: agent (só sua, padrão) ou team (memória do time, lida por todos do espaço)."}
}, "required": ["conteudo"]}`),
		tool("recordar", "Procura na sua memória (as mais recentes já estão no seu contexto; use para achar as antigas).", `{"type": "object", "properties": {
	"busca": {"type": "string", "description": "Palavras que a memória contém; vazio lista as mais recentes."}
}}`),
		tool("esquecer", "Apaga uma memória sua que ficou errada ou velha (o id aparece em recordar). Para corrigir, apague e guarde a nova.", `{"type": "object", "properties": {
	"id": {"type": "string"}
}, "required": ["id"]}`),
	}
	if files {
		tools = append(tools,
			tool("listar_arquivos", "Lista os arquivos da sua pasta de trabalho na VPS (a mesma em que o Claude Code e o Codex trabalham quando rodam na VPS).", `{"type": "object", "properties": {
	"pasta": {"type": "string", "description": "Subpasta; vazio = a pasta toda."}
}}`),
			tool("ler_arquivo", "Lê um arquivo de texto da sua pasta. Arquivos longos vêm em partes.", `{"type": "object", "properties": {
	"caminho": {"type": "string", "description": "Caminho relativo à sua pasta, ex.: notas/orcamentos.md"},
	"parte": {"type": "integer", "minimum": 1}
}, "required": ["caminho"]}`),
			tool("escrever_arquivo", "Cria ou substitui um arquivo de texto na sua pasta (até 1 MB por escrita; a pasta toda até 200 MB). Use anexar=true para acrescentar ao fim.", `{"type": "object", "properties": {
	"caminho": {"type": "string"},
	"conteudo": {"type": "string"},
	"anexar": {"type": "boolean"}
}, "required": ["caminho", "conteudo"]}`),
			tool("apagar_arquivo", "Apaga um arquivo ou pasta da sua pasta de trabalho (pede aprovação do dono).", `{"type": "object", "properties": {
	"caminho": {"type": "string"}
}, "required": ["caminho"]}`))
	}
	if shell {
		tools = append(tools,
			tool("executar", "Roda um comando de shell (bash) num sandbox na VPS, dentro da sua pasta (/work): Python 3, ferramentas comuns do Linux. Sem internet por padrão: pip install, curl, wget, git clone e qualquer acesso a sites precisam de rede=true, que pede aprovação do dono. Bom para contas, processar arquivos e testar scripts. Limite de tempo em segundos (padrão 60, máx. 600).", `{"type": "object", "properties": {
	"comando": {"type": "string", "description": "O comando, ex.: python3 analise.py"},
	"rede": {"type": "boolean", "description": "true quando o comando precisa da internet (pip install, curl, git clone, APIs)."},
	"segundos": {"type": "integer", "minimum": 5, "maximum": 600}
}, "required": ["comando"]}`))
	}
	return tools
}

// runHandTool runs the Fase A tools; ok is false for any other tool.
func (s *Server) runHandTool(ctx context.Context, t *turn, name string, a toolArgs) (out string, err error, ok bool) {
	switch name {
	case "buscar_na_web":
		out, err = s.toolSearch(ctx, t, a)
	case "ler_pagina":
		out, err = s.toolReadPage(ctx, t, a)
	case "lembrar", "remember":
		out, err = s.toolRemember(ctx, t, a)
	case "recordar":
		out, err = s.toolRecall(ctx, t, a)
	case "esquecer":
		out, err = s.toolForget(ctx, t, a)
	case "listar_arquivos":
		out, err = s.listFiles(t.agent.ID, a.Pasta)
		if err == nil {
			s.step(t, AgentStep{Tool: name, Summary: "pasta " + orDefault(strings.TrimSpace(a.Pasta), "/")})
		}
	case "ler_arquivo":
		out, err = s.readFile(t.agent.ID, a.Caminho, a.Parte)
		if err == nil {
			s.step(t, AgentStep{Tool: name, Summary: a.Caminho})
		}
	case "escrever_arquivo":
		out, err = s.writeFile(t.agent.ID, a.Caminho, a.Conteudo, a.Anexar)
		if err == nil {
			s.step(t, AgentStep{Tool: name, Summary: out})
		}
	case "apagar_arquivo":
		out, err = s.removeFile(t.agent.ID, a.Caminho)
		if err == nil {
			s.step(t, AgentStep{Tool: name, Summary: out})
		}
	case "executar":
		out, err = s.toolShell(ctx, t, a)
	default:
		return "", nil, false
	}
	return out, err, true
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// ── Web ─────────────────────────────────────────────────────────

func (s *Server) toolSearch(ctx context.Context, t *turn, a toolArgs) (string, error) {
	q := strings.TrimSpace(a.Busca)
	res, err := s.webClient().Search(ctx, q, a.Quantidade)
	if err != nil {
		return "", err
	}
	s.step(t, AgentStep{Tool: "buscar_na_web", Summary: fmt.Sprintf("«%s» · %d resultados", q, len(res))})
	if len(res) == 0 {
		return "Nenhum resultado. Tente outras palavras.", nil
	}
	var b strings.Builder
	for i, r := range res {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", truncate(r.Snippet, 300))
		}
	}
	return b.String(), nil
}

func (s *Server) toolReadPage(ctx context.Context, t *turn, a toolArgs) (string, error) {
	p, err := s.webClient().Read(ctx, a.URL)
	if err != nil {
		return "", err
	}
	n := max(a.Parte, 1)
	text, total := part(p.Text, n)
	if text == "" {
		return "", fmt.Errorf("a página tem só %d partes", total)
	}
	title := orDefault(p.Title, p.URL)
	s.step(t, AgentStep{Tool: "ler_pagina", Summary: truncate(title, 120) + " · " + p.URL})
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n", title, p.URL)
	if total > 1 {
		fmt.Fprintf(&b, "(parte %d de %d)\n", n, total)
	}
	b.WriteString("\n" + text + "\n")
	if n == 1 && len(p.Links) > 0 {
		b.WriteString("\nLinks:\n")
		room := maxToolResult - b.Len() - 200
		for _, l := range p.Links {
			line := fmt.Sprintf("- %s: %s\n", l.Text, l.URL)
			if room -= len(line); room < 0 {
				break
			}
			b.WriteString(line)
		}
	}
	return b.String(), nil
}

// ── Shell ───────────────────────────────────────────────────────

func (s *Server) toolShell(ctx context.Context, t *turn, a toolArgs) (string, error) {
	cmd := strings.TrimSpace(a.Comando)
	if cmd == "" {
		return "", errors.New("diga o comando")
	}
	if len(cmd) > MaxShellCommand || !utf8.ValidString(cmd) || strings.ContainsRune(cmd, 0) {
		return "", errors.New("comando inválido ou longo demais")
	}
	secs := a.Segundos
	if secs <= 0 {
		secs = 60
	}
	secs = min(max(secs, 5), 600)
	homeNode, hasHome, herr := s.resolveHomeNode(ctx, t.agent)
	if herr != nil {
		return "", herr
	}
	node := ""
	target := scheduler.VPS
	if hasHome {
		node, target = homeNode.ID, homeNode.Kind
	} else {
		nodes, err := s.store.ListNodes(ctx)
		if err != nil {
			return "", err
		}
		node = s.onlineNode(nodes, scheduler.VPS)
		if node == "" {
			return "", errors.New("o nó da VPS está offline; não dá para executar agora")
		}
	}
	mode := "offline"
	if a.Rede {
		mode = "net"
	}
	spec := &store.AgentSpec{Engine: "shell", Prompt: cmd, Mode: mode, Workdir: "agents/" + t.agent.ID,
		MaxMinutes: (secs + 59) / 60}
	if hasHome {
		applyAgentHome(spec, t.agent)
	} else {
		applyAgentIdentity(spec, t.agent)
	}
	label := cmd
	if a.Rede {
		label += " · com internet"
	}
	s.step(t, AgentStep{Tool: "executar", Summary: label})
	end, err := s.runNodeJob(ctx, t, spec, target, node, "executar no sandbox da VPS", "")
	if err != nil {
		return "", err
	}
	r := end.result
	switch {
	case r != nil && r.Success:
		return truncate(r.Output, maxToolResult), nil
	case r != nil:
		out := "Falhou: " + orDefault(r.Error, "a execução falhou")
		if o := strings.TrimSpace(r.Output); o != "" {
			out += "\nSaída até ali:\n" + o
		}
		return truncate(out, maxToolResult), nil
	case end.job.Status == store.StatusTimedOut:
		return "O nó parou de responder e o comando foi encerrado.", nil
	}
	return "O comando foi parado antes de terminar.", nil
}

// runNodeJob creates a job for a node and waits for it to end, forwarding
// its steps; stopping the turn cancels it.
// onCreated, if given, runs with the job's id once it exists.
func (s *Server) runNodeJob(ctx context.Context, t *turn, spec *store.AgentSpec, target scheduler.Target, node, reason, stepPrefix string, onCreated ...func(string)) (jobEnd, error) {
	return s.runNodeJobAs(ctx, t, spec, target, node, t.agent.Name+": "+reason, "agent:"+t.agent.ID, stepPrefix, onCreated...)
}

func (s *Server) runNodeJobAs(ctx context.Context, t *turn, spec *store.AgentSpec, target scheduler.Target, node, reason, actor, stepPrefix string, onCreated ...func(string)) (jobEnd, error) {
	jobID := newID()
	ended := s.waits.add(jobID)
	defer s.waits.drop(jobID)
	events, unsubscribe := s.hub.subscribe(jobID)
	defer unsubscribe()
	action := store.ActionAgentRun
	switch {
	case spec.Engine == "mcp":
		action = store.ActionMCPRun
	case spec.Engine == "codex" && (spec.Mode == store.CodexChatMode || spec.Mode == store.CodexConsultMode || spec.Mode == store.CodexModelsMode):
		action = store.ActionCodexMain
	case spec.Engine == "antigravity" && (spec.Mode == store.CodexChatMode || spec.Mode == store.CodexConsultMode || spec.Mode == store.CodexModelsMode):
		// The main model's lane (its own slots on the node), like Codex.
		action = store.ActionCodexMain
	}
	if t != nil {
		applyAgentIdentity(spec, t.agent)
	}
	if _, err := s.store.CreateJob(ctx, Job{ID: jobID, Action: action, Target: target, DeviceID: node,
		CreatedAt: s.now(), Agent: spec}, reason, actor); err != nil {
		return jobEnd{}, errors.New("não consegui criar a tarefa")
	}
	for _, f := range onCreated {
		f(jobID)
	}
	if t != nil && strings.TrimSpace(t.agent.HomeNodeID) == node {
		if n, nerr := s.nodeByID(context.Background(), node); nerr == nil && !s.online(n) {
			s.step(t, AgentStep{Tool: "computador", Summary: offlineNodeMessage(n.Name), Job: jobID})
		}
	}
	root := t
	for root != nil && root.parent != nil {
		root = root.parent
	}
	if root != nil {
		root.mu.Lock()
		root.job = jobID
		root.mu.Unlock()
	}
	for {
		select {
		case ev := <-events:
			if st, ok := ev.data.(AgentStep); t != nil && ok && ev.kind == "step" && st.Tool != StepText {
				st.Tool = stepPrefix + st.Tool
				s.step(t, st)
			}
		case end := <-ended:
			return end, nil
		case <-ctx.Done():
			if job, err := s.store.CancelJob(context.Background(), jobID, actor, s.now()); err == nil {
				s.finishAgentReply(job, nil)
			}
			return jobEnd{job: Job{Status: store.StatusCanceled}}, nil
		}
	}
}
