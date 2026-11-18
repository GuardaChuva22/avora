package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
)

// The agents' tools. Everything the owner could set up in a panel, an agent
// can do from the conversation: the panels are the second way, not the first.
//
//   - claude_code, codex: delegate heavy work to a CLI on a node; the turn
//     waits for the result and the agent reports it.
//   - rotinas and responsabilidades: create, change, remove, run now.
//   - consultar_colega: ask a colleague and get the answer back (direct
//     conversations; in #time colleagues are called with @).
//   - estado_do_avora: nodes, recent work, the team's routines.

const maxToolResult = 8000

func tool(name, description, params string) llm.Tool {
	return llm.Tool{Type: "function", Function: llm.ToolFunction{Name: name, Description: description, Parameters: json.RawMessage(params)}}
}

const scheduleParams = `
	"horario": {"type": "string", "description": "Hora local HH:MM (horário de Brasília) para rodar em horário fixo, ex.: 08:00."},
	"dias": {"type": "array", "items": {"type": "integer", "minimum": 0, "maximum": 6}, "description": "Dias da semana para o horário fixo: 0=domingo, 1=segunda ... 6=sábado. Vazio = todo dia."},
	"data": {"type": "string", "description": "AAAA-MM-DD para rodar uma vez só, nesse dia e horário, e desligar sozinha: lembretes como 'amanhã às 15h'. Sem dias."},
	"a_cada_minutos": {"type": "integer", "minimum": 15, "description": "Em vez de horário fixo: rodar a cada N minutos (mínimo 15)."},
	"entregar": {"type": "string", "enum": ["time", "conversa"], "description": "Onde entregar o resultado: no canal #time (padrão) ou numa conversa própria da rotina."},
	"skill": {"type": "string", "description": "Opcional: uma skill que a rotina segue passo a passo (vira um workflow); vazio em alterar_rotina tira a skill."},
	"gatilho": {"type": "object", "description": "Em vez de horário: rodar quando algo acontecer. A rotina recebe o que aconteceu junto com a instrução.", "properties": {
		"tipo": {"type": "string", "enum": ["email", "clickup", "no"], "description": "email: chegou e-mail novo que casa com a busca; clickup: apareceu tarefa nova; no: o PC ou a VPS ligou/desligou."},
		"busca": {"type": "string", "description": "email: busca do Gmail (ex.: 'from:fornecedor is:unread'); clickup: texto no nome da tarefa."},
		"lista_id": {"type": "string", "description": "clickup: só tarefas desta lista."},
		"no": {"type": "string", "enum": ["pc", "vps"]},
		"evento": {"type": "string", "enum": ["online", "offline"]},
		"a_cada_minutos": {"type": "integer", "description": "De quanto em quanto tempo conferir (padrão 5)."}}}`

var delegateParams = `{"type": "object", "properties": {
	"tarefa": {"type": "string", "description": "A tarefa completa e autocontida: o que fazer, com todo o contexto necessário, e o que entregar no fim."},
	"pode_editar": {"type": "boolean", "description": "true se ele precisa criar ou editar arquivos na sua pasta de trabalho; false (padrão) para só ler, pesquisar e responder."},
	"conta": {"type": "string", "description": "Opcional: o nome da conta da assinatura a usar, quando o dono pedir uma. Vazio usa a principal (e outra conta sozinho se a principal chegou no limite)."},
	"projeto": {"type": "string", "description": "Opcional, para funcionários universais: o projeto em cuja pasta principal ele trabalha. Quem é de um projeto já trabalha na pasta dele."},
	"pasta": {"type": "string", "description": "Opcional: outra pasta do projeto (o nome dela); vazio = a principal."}
}, "required": ["tarefa"]}`

func (s *Server) toolsFor(t *turn) []llm.Tool {
	tools := append(append(append(handToolsFor(s.workspace != "", s.workspace != "" || strings.TrimSpace(t.agent.HomeNodeID) != ""), s.browserToolsFor(t.agent)...), s.connectorTools(context.Background(), t.agent.ID, t.appLoaded)...),
		tool("pesquisar", "Pesquisa a fundo na internet: um pesquisador (Codex, modelo Sol com raciocínio alto e busca ao vivo) faz dezenas de buscas, abre as páginas, cruza fontes (portais, sites locais, redes sociais públicas, grupos, notícias) e devolve um relatório com links e contatos. Use para qualquer pergunta que precise de pesquisa de verdade, em vez de buscar_web. Demora de 2 a 15 minutos.", `{"type": "object", "properties": {
	"assunto": {"type": "string", "description": "O que pesquisar, com todo o contexto e os critérios (ex.: cidade, faixa de preço, o que precisa ter, o que descartar)."},
	"formato": {"type": "string", "description": "Opcional: o formato do resultado (ex.: tabela com preço, endereço, contato e link de cada opção)."}
}, "required": ["assunto"]}`),
		tool("claude_code", "Delega um trabalho pesado ao Claude Code, que roda no PC do dono ou na VPS: programar projetos, mexer em muitos arquivos no PC do dono, pesquisas longas com dezenas de fontes. Para uma busca ou uma conta, use suas próprias ferramentas. Demora de 1 a 30 minutos; você recebe a resposta dele e conta ao dono.", delegateParams),
		tool("codex", "Delega um trabalho pesado ao Codex (OpenAI), como alternativa ao Claude Code: programar, revisar código, analisar arquivos. Demora de 1 a 30 minutos.", delegateParams),
		tool("antigravity", "Delega um trabalho ao Antigravity (o agente do Google, com Gemini) no PC do dono, pela conta Google dele: programar, analisar arquivos e documentos, uma segunda opinião. Só roda com o PC ligado e não usa os apps conectados do dono. Demora de 1 a 30 minutos.", delegateParams),
		tool("missao_iniciar", "Começa uma missão em segundo plano: um trabalho longo ou de várias etapas (programar, pesquisar a fundo, organizar muitos arquivos) que o Claude Code ou o Codex executa no PC ou na VPS enquanto a conversa segue. Prefira a claude_code/codex quando o trabalho passa de alguns minutos. Você escreve o objetivo e o plano; quando cada rodada termina, você é acordado nesta conversa com o resultado, atualiza o plano com missao_atualizar e continua ou conclui.", missionStartParams),
		tool("missao_atualizar", "Atualiza uma missão sua: marca os passos do plano, começa a próxima rodada (continuar), pausa para perguntar ao dono (aguardando_dono) ou encerra (concluida, cancelada).", missionUpdateParams),
		tool("estado_do_avora", "Mostra o estado atual: nós online, tarefas das últimas 24 h, rotinas e responsabilidades do time.", `{"type": "object", "properties": {}}`),
		tool("get_my_usage", "Consulta o seu próprio uso (respostas, tokens e execuções de hoje e dos últimos 30 dias) e a cota atual da conta que você usa (porcentagem usada na janela de 5 h e na semana, e horário de renovação). Use quando perguntarem como está seu limite, cota ou gasto.", `{"type": "object", "properties": {}}`),
		tool("ver_rotinas", "Lista as rotinas (tarefas agendadas) com id, horário, próxima execução e último resultado.", `{"type": "object", "properties": {
	"funcionario": {"type": "string", "description": "Nome de um funcionário para filtrar; vazio = o time todo."}}}`),
		tool("criar_rotina", "Cria uma rotina: uma instrução que um funcionário roda sozinho no horário, todo dia, em dias da semana ou a cada N minutos. Use quando o dono pedir algo recorrente ou um lembrete.", `{"type": "object", "properties": {
	"titulo": {"type": "string", "description": "Nome curto, ex.: Revisão de gastos."},
	"instrucao": {"type": "string", "description": "O que o funcionário deve fazer a cada execução, escrito como um pedido completo."},
	"funcionario": {"type": "string", "description": "Quem roda; vazio = você."},
	"responsabilidade_id": {"type": "string", "description": "Id da responsabilidade que esta rotina acompanha, se houver."},`+scheduleParams+`
}, "required": ["titulo", "instrucao"]}`),
		tool("alterar_rotina", "Altera uma rotina existente: só os campos enviados mudam. Use ativa=false para pausar e true para religar.", `{"type": "object", "properties": {
	"id": {"type": "string"},
	"titulo": {"type": "string"},
	"instrucao": {"type": "string"},
	"funcionario": {"type": "string"},
	"ativa": {"type": "boolean"},`+scheduleParams+`
}, "required": ["id"]}`),
		tool("apagar_rotina", "Apaga uma rotina de vez. Para só pausar, use alterar_rotina com ativa=false.", `{"type": "object", "properties": {"id": {"type": "string"}}, "required": ["id"]}`),
		tool("rodar_rotina", "Roda uma rotina agora, fora do horário.", `{"type": "object", "properties": {"id": {"type": "string"}}, "required": ["id"]}`),
		tool("definir_responsabilidade", "Cria ou altera uma responsabilidade permanente de um funcionário: uma meta que ele acompanha, com métrica e cadência.", `{"type": "object", "properties": {
	"objetivo": {"type": "string", "description": "A meta, ex.: Gastos do mês abaixo do orçamento."},
	"metrica": {"type": "string", "description": "Como medir, ex.: gasto acumulado / orçamento."},
	"cadencia": {"type": "string", "description": "Com que frequência checar, ex.: toda segunda."},
	"funcionario": {"type": "string", "description": "De quem é; vazio = você."},
	"id": {"type": "string", "description": "Id de uma responsabilidade existente para alterá-la; vazio = nova."}
}, "required": ["objetivo"]}`),
		tool("remover_responsabilidade", "Remove uma responsabilidade de um funcionário.", `{"type": "object", "properties": {
	"id": {"type": "string"},
	"funcionario": {"type": "string", "description": "De quem é; vazio = você."}
}, "required": ["id"]}`),
	)
	tools = append(tools, s.activateTool(context.Background(), t)...)
	tools = append(tools, s.pluginTools()...)
	tools = append(tools, s.whatsappTools()...)
	tools = append(tools, s.connectTools()...)
	tools = append(tools, nightTools()...)
	tools = append(tools, docTools(s.workspace != "")...)
	tools = append(tools,
		tool("skill_ler", "Carrega as instruções completas de uma skill da lista de skills, para seguir. Sem nome, lista as skills.", `{"type": "object", "properties": {
	"nome": {"type": "string", "description": "O nome da skill, ex.: resumir-video."}
}}`),
		tool("skill_criar", "Guarda (ou atualiza) uma skill: um jeito de fazer algo que o time vai repetir, com passos numerados. O dono aprova antes. Funcionário de projeto guarda no projeto dele.", `{"type": "object", "properties": {
	"nome": {"type": "string", "description": "Nome curto, em minúsculas com hífen, ex.: relatorio-mensal."},
	"descricao": {"type": "string", "description": "Quando usar: o que ela faz e em que pedidos, numa frase."},
	"instrucoes": {"type": "string", "description": "Os passos em Markdown, numerados, com as ferramentas a usar e o que entregar no fim."},
	"projeto": {"type": "string", "description": "Opcional, para universais: guardar num projeto; vazio = para todos."}
}, "required": ["nome", "descricao", "instrucoes"]}`),
		proposeChangeTool())
	if t.parent == nil {
		tools = append(tools, tellAgentTool())
	}
	if nt := s.nodeToolsToolFor(context.Background(), t.agent); nt != nil {
		tools = append(tools, *nt)
	}
	if !t.channel && t.parent == nil {
		tools = append(tools, goalTools()...)
		tools = append(tools, tool(StepAsk, askToolDesc, askParams))
		tools = append(tools, tool("consultar_colega", "Pergunta algo a um colega do time e espera a resposta dele, para usar a especialidade dele nesta resposta. O dono vê a resposta do colega dentro da sua.", `{"type": "object", "properties": {
	"funcionario": {"type": "string", "description": "Nome do colega."},
	"pergunta": {"type": "string", "description": "A pergunta completa, com o contexto que ele precisa."}
}, "required": ["funcionario", "pergunta"]}`))
		tools = append(tools, tool("avisar_colega", "Passa um trabalho a um colega sem esperar: abre uma conversa dele com o dono, onde ele começa na hora e entrega o resultado direto ao dono. Use quando o dono pede algo que é da área do colega (\"pede pra Cora...\"); para usar a resposta dele agora na sua, use consultar_colega.", `{"type": "object", "properties": {
	"funcionario": {"type": "string", "description": "Nome do colega."},
	"recado": {"type": "string", "description": "O que ele deve fazer ou saber, completo, com o contexto."}
}, "required": ["funcionario", "recado"]}`))
	}
	return tools
}

// toolArgs are the fields any tool may receive.
type toolArgs struct {
	Tarefa             string             `json:"tarefa"`
	PodeEditar         bool               `json:"pode_editar"`
	Conta              string             `json:"conta"`
	Projeto            string             `json:"projeto"`
	Funcionario        string             `json:"funcionario"`
	ID                 string             `json:"id"`
	Titulo             string             `json:"titulo"`
	Instrucao          string             `json:"instrucao"`
	ResponsabilidadeID string             `json:"responsabilidade_id"`
	Horario            string             `json:"horario"`
	Data               string             `json:"data"`
	Dias               []int              `json:"dias"`
	ACadaMinutos       int                `json:"a_cada_minutos"`
	Entregar           string             `json:"entregar"`
	Assunto            string             `json:"assunto"`
	Formato            string             `json:"formato"`
	Ativa              *bool              `json:"ativa"`
	Objetivo           string             `json:"objetivo"`
	Metrica            string             `json:"metrica"`
	Cadencia           string             `json:"cadencia"`
	Pergunta           string             `json:"pergunta"`
	Recado             string             `json:"recado"`
	Opcoes             []string           `json:"opcoes"`
	Multipla           bool               `json:"multipla"`
	Outra              bool               `json:"outra"`
	Busca              string             `json:"busca"`
	Quantidade         int                `json:"quantidade"`
	URL                string             `json:"url"`
	Parte              int                `json:"parte"`
	Conteudo           string             `json:"conteudo"`
	Pasta              string             `json:"pasta"`
	Caminho            string             `json:"caminho"`
	Anexar             bool               `json:"anexar"`
	Comando            string             `json:"comando"`
	Rede               bool               `json:"rede"`
	Segundos           int                `json:"segundos"`
	Nome               string             `json:"nome"`
	Descricao          string             `json:"descricao"`
	Instrucoes         string             `json:"instrucoes"`
	Skill              string             `json:"skill"`
	Ferramenta         string             `json:"ferramenta"`
	Acao               string             `json:"acao"`
	No                 string             `json:"no"`
	Gatilho            *triggerArgs       `json:"gatilho"`
	Missao             string             `json:"missao"`
	Passos             []string           `json:"passos"`
	Executor           string             `json:"executor"`
	Plano              []missionStepInput `json:"plano"`
	Continuar          string             `json:"continuar"`
	Status             string             `json:"status"`
	Resumo             string             `json:"resumo"`

	Args map[string]any `json:"args"`

	mcp mcpToolArgs
	has map[string]bool
	raw string // the arguments as sent (connector tools read them)
}

// callTool runs one tool call and returns what the model reads back. Errors
// go back to the model as text, so it can fix the call or tell the owner.
func (s *Server) callTool(ctx context.Context, t *turn, c llm.ToolCall) string {
	var a toolArgs
	raw := strings.TrimSpace(c.Function.Arguments)
	if raw == "" {
		raw = "{}"
	}
	var input any = &a
	if isMCPTool(c.Function.Name) {
		input = &a.mcp
	}
	if err := json.Unmarshal([]byte(raw), input); err != nil {
		return "Erro: os argumentos não são um JSON válido: " + err.Error()
	}
	a.has, a.raw = map[string]bool{}, raw
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) == nil {
		for k := range fields {
			a.has[k] = true
		}
	}
	if refusal, ok := s.gate(ctx, t, c.Function.Name, a); !ok {
		return refusal
	}
	out, err := s.runTool(ctx, t, c.Function.Name, a)
	if err != nil {
		s.step(t, AgentStep{Tool: c.Function.Name, Summary: "erro: " + err.Error()})
		return "Erro: " + err.Error()
	}
	return truncate(out, maxToolResult)
}

func (s *Server) runTool(ctx context.Context, t *turn, name string, a toolArgs) (string, error) {
	if isMCPTool(name) {
		return s.toolMCP(ctx, t, name, a.mcp)
	}
	if out, err, ok := s.runConnector(ctx, t, name, a.raw); ok {
		return out, err
	}
	if out, err, ok := s.runHandTool(ctx, t, name, a); ok {
		return out, err
	}
	if out, err, ok := s.runGoalTool(ctx, t, name, a.raw); ok {
		return out, err
	}
	if out, err, ok := s.runWhatsAppTool(ctx, t, name, a.raw); ok {
		return out, err
	}
	if out, err, ok := s.runConnectTool(ctx, t, name, a.raw); ok {
		return out, err
	}
	if out, err, ok := s.runNightTool(ctx, t, name, a.raw); ok {
		return out, err
	}
	if out, err, ok := s.runDocTool(ctx, t, name, a.raw); ok {
		return out, err
	}
	switch name {
	case "claude_code":
		return s.delegate(ctx, t, "claude", a)
	case "codex":
		return s.delegate(ctx, t, "codex", a)
	case "antigravity":
		return s.delegate(ctx, t, "antigravity", a)
	case "pesquisar":
		return s.research(ctx, t, a)
	case "navegador":
		if refusal := s.browserForApp(ctx, t, a.raw); refusal != "" {
			s.step(t, AgentStep{Tool: name, Summary: "trocou o navegador pelo conector"})
			return refusal, nil
		}
		return s.toolBrowser(ctx, t, a.raw)
	case activateAppTool:
		return s.toolActivateApp(ctx, t, a.raw)
	case "missao_iniciar":
		return s.toolStartMission(ctx, t, a)
	case "missao_atualizar":
		return s.toolUpdateMission(ctx, t, a)
	case "skill_ler":
		return s.toolReadSkill(ctx, t, a)
	case "skill_criar":
		return s.toolCreateSkill(ctx, t, a)
	case "ferramenta":
		return s.toolNodeTool(ctx, t, a)
	case "estado_do_avora":
		s.step(t, AgentStep{Tool: name, Summary: "olhou o estado do Avora"})
		return s.stateText(ctx, ""), nil
	case "get_my_usage":
		return s.toolGetMyUsage(ctx, t)
	case "ver_rotinas":
		return s.toolListRoutines(ctx, t, a)
	case "criar_rotina":
		return s.toolCreateRoutine(ctx, t, a)
	case "alterar_rotina":
		return s.toolEditRoutine(ctx, t, a)
	case "apagar_rotina":
		rt, err := s.store.GetRoutine(ctx, a.ID)
		if err != nil {
			return "", errors.New("rotina não encontrada; use ver_rotinas para ver os ids")
		}
		if err := s.store.DeleteRoutine(ctx, rt.ID, "agent:"+t.agent.ID, s.now()); err != nil {
			return "", err
		}
		s.step(t, AgentStep{Tool: name, Summary: "«" + rt.Title + "»"})
		return "Rotina «" + rt.Title + "» apagada.", nil
	case "rodar_rotina":
		rt, err := s.store.GetRoutine(ctx, a.ID)
		if err != nil {
			return "", errors.New("rotina não encontrada; use ver_rotinas para ver os ids")
		}
		if !s.startRoutine(context.WithoutCancel(ctx), rt, true, "") {
			return "", errors.New("essa rotina já está rodando")
		}
		s.step(t, AgentStep{Tool: name, Summary: "«" + rt.Title + "»"})
		where := "no #time"
		if rt.Deliver == "conversation" {
			where = "na conversa da rotina"
		}
		return "Rotina «" + rt.Title + "» começou a rodar; o resultado sai " + where + ".", nil
	case "definir_responsabilidade":
		return s.toolSetResponsibility(ctx, t, a)
	case "remover_responsabilidade":
		return s.toolRemoveResponsibility(ctx, t, a)
	case StepProposal:
		return s.toolProposeChange(ctx, t, a)
	case StepTellAgent:
		if t.parent != nil {
			return "", errors.New("aqui não: responda você mesmo")
		}
		return s.toolTellAgent(ctx, t, a)
	case StepAsk:
		if t.channel || t.parent != nil {
			return "", errors.New("aqui não: pergunte no texto")
		}
		return s.ask(t, a)
	case "consultar_colega", "avisar_colega":
		if t.channel {
			return "", errors.New("aqui não: no #time chame o colega com @Nome")
		}
		if t.parent != nil {
			return "", errors.New("aqui não: responda você mesmo")
		}
		if name == "avisar_colega" {
			return s.note(ctx, t, a)
		}
		return s.consult(ctx, t, a)
	}
	return "", fmt.Errorf("ferramenta desconhecida: %s", name)
}

// who resolves a funcionário by name or id; empty means the caller.
func (s *Server) who(ctx context.Context, t *turn, name string) (store.Agent, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "@")
	if name == "" {
		return t.agent, nil
	}
	team, err := s.store.ListAgents(ctx)
	if err != nil {
		return store.Agent{}, err
	}
	key := slug(name)
	names := []string{}
	for _, a := range team {
		if !sees(t.agent, a) {
			if a.ID == key || slug(a.Name) == key {
				return store.Agent{}, fmt.Errorf("%s é de outro projeto: você não trabalha com ele", a.Name)
			}
			continue
		}
		if a.ID == key || slug(a.Name) == key {
			return a, nil
		}
		names = append(names, a.Name)
	}
	return store.Agent{}, fmt.Errorf("não há funcionário %q no time (o time: %s)", name, strings.Join(names, ", "))
}

func agentName(team []store.Agent, id string) string {
	for _, a := range team {
		if a.ID == id {
			return a.Name
		}
	}
	return id
}

// ── Delegation ──────────────────────────────────────────────────

func engineLabel(engine string) string {
	switch engine {
	case "codex":
		return "Codex"
	case "antigravity":
		return "Antigravity"
	}
	return "Claude Code"
}

// routeEngine picks the node that runs a delegation to engine.
func (s *Server) routeEngine(ctx context.Context, engine string) (agentRoute, error) {
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return agentRoute{}, err
	}
	if engine == "antigravity" {
		// agy keeps its login in the PC's keyring: it runs there only.
		pc := s.onlineNode(nodes, scheduler.Windows)
		if pc == "" {
			return agentRoute{}, errNoNode
		}
		return agentRoute{node: pc, target: scheduler.Windows, reason: "Antigravity no PC"}, nil
	}
	want := scheduler.Claude
	if engine == "codex" {
		want = scheduler.Codex
	}
	windows, vps := s.onlineNode(nodes, scheduler.Windows), s.onlineNode(nodes, scheduler.VPS)
	d := scheduler.Route(scheduler.Request{Engine: want}, scheduler.Availability{WindowsOnline: windows != "", VPSReady: vps != ""})
	if !d.Allowed {
		return agentRoute{}, errNoNode
	}
	r := agentRoute{node: windows, target: d.Target, reason: d.Reason}
	if d.Target == scheduler.VPS {
		r.node = vps
	}
	return r, nil
}

func delegatePrompt(a store.Agent, task, apps string, project *store.Project, folder *store.ProjectFolder) string {
	bridge := ""
	if project != nil {
		bridge += "\nA tarefa é do projeto " + project.Name + "."
		if folder != nil {
			bridge += " A pasta atual é uma pasta do projeto."
		}
		if project.Instructions != "" {
			bridge += "\nInstruções do projeto (" + AgentsFile + "), siga-as:\n" + truncate(project.Instructions, 6000)
		}
	}
	if apps != "" {
		bridge += "\nVocê tem os apps conectados do dono pelo servidor MCP \"avora\" (ferramentas mcp__avora__… no Claude Code, avora no Codex): " + apps +
			". Use-os quando a tarefa pedir. Ler e criar é livre; enviar, comentar, apagar ou pagar espera a aprovação do dono, e a chamada fica aberta até ele decidir."
	}
	return fmt.Sprintf(`Você está trabalhando para %s (%s), um funcionário digital do Avora, que delegou esta tarefa a você.
Trabalhe só dentro da pasta atual. Ao terminar, responda com o resultado completo e objetivo: o que fez, o que encontrou e onde ficaram os arquivos. Responda em português do Brasil.%s

Tarefa:
%s`, a.Name, a.Role, bridge, task)
}

// delegation is where and how a Claude Code or Codex run goes: the node,
// the account, the folder and the mode. The spec has no prompt yet.
type delegation struct {
	spec    *store.AgentSpec
	route   agentRoute
	label   string
	project *store.Project
	folder  *store.ProjectFolder
}

func (s *Server) planDelegation(ctx context.Context, t *turn, engine string, a toolArgs) (delegation, error) {
	label := engineLabel(engine)
	project, folder, ferr := s.projectFolder(ctx, t.agent, a.Projeto, a.Pasta)
	if ferr != nil {
		return delegation{}, ferr
	}
	mode := "read"
	if a.PodeEditar {
		mode = "edit"
	}

	homeNode, hasHome, herr := s.resolveHomeNode(ctx, t.agent)
	if herr != nil {
		return delegation{}, herr
	}
	if hasHome {
		if folder != nil && folder.Node != homeNode.ID {
			if strings.TrimSpace(a.Projeto) != "" || strings.TrimSpace(a.Pasta) != "" {
				return delegation{}, fmt.Errorf("a pasta do projeto fica em outro computador; %s trabalha em %s", t.agent.Name, homeNode.Name)
			}
			folder = nil
		}
		account, aerr := s.accountFor(ctx, engine, a.Conta, homeNode.ID)
		if aerr != nil && s.online(homeNode) {
			return delegation{}, aerr
		}
		if account.ID != "" && account.NodeID != homeNode.ID {
			if strings.TrimSpace(a.Conta) != "" {
				return delegation{}, fmt.Errorf("a conta %s fica em outro computador; %s trabalha em %s", account.Name, t.agent.Name, homeNode.Name)
			}
			account = store.Provider{}
		}
		if account.ID != "" {
			label += " (" + account.Name + ")"
		}
		spec := &store.AgentSpec{
			Engine: engine, Model: s.delegateModels[engine], Mode: mode, MaxMinutes: AgentReplyMinutes,
			Account: account.ID, Owner: t.agent.ID,
		}
		if folder != nil {
			if homeNode.Kind == scheduler.Windows {
				spec.Folder = folder.Path
			} else {
				spec.Workdir = folder.Path
			}
			applyAgentIdentity(spec, t.agent)
		} else {
			applyAgentHome(spec, t.agent)
		}
		route := agentRoute{node: homeNode.ID, target: homeNode.Kind, reason: "casa de " + t.agent.Name}
		return delegation{spec: spec, route: route, label: label, project: project, folder: folder}, nil
	}

	route, err := s.routeEngine(ctx, engine)
	var folderNode *store.Node
	if folder != nil {
		// The project's folder is on one node: the run goes there.
		n, nerr := s.nodeByID(ctx, folder.Node)
		if nerr != nil {
			return delegation{}, nerr
		}
		if !s.online(n) {
			return delegation{}, fmt.Errorf("a pasta do projeto fica em %s, que está offline agora", n.Name)
		}
		folderNode = &n
		route, err = agentRoute{node: n.ID, target: n.Kind, reason: "pasta do projeto " + project.Name}, nil
	}
	account, aerr := s.accountFor(ctx, engine, a.Conta, route.node)
	if aerr != nil {
		return delegation{}, aerr
	}
	if folderNode != nil && account.ID != "" && account.NodeID != folderNode.ID {
		if strings.TrimSpace(a.Conta) != "" {
			return delegation{}, fmt.Errorf("a conta %s fica em outro nó, e a pasta do projeto em %s", account.Name, folderNode.Name)
		}
		account = store.Provider{} // the folder's node uses its own login
	}
	if account.ID != "" {
		// The account lives on one node: the run goes there.
		n, nerr := s.nodeByID(ctx, account.NodeID)
		if nerr != nil {
			return delegation{}, nerr
		}
		route, err = agentRoute{node: n.ID, target: n.Kind, reason: "conta " + account.Name}, nil
		label += " (" + account.Name + ")"
	}
	if err != nil {
		return delegation{}, fmt.Errorf("nenhum nó online com o %s agora; tente de novo mais tarde", label)
	}
	spec := &store.AgentSpec{
		Engine: engine, Model: s.delegateModels[engine], Mode: mode, MaxMinutes: AgentReplyMinutes,
		Workdir: "agents/" + t.agent.ID, Account: account.ID, Owner: t.agent.ID,
	}
	applyAgentIdentity(spec, t.agent)
	if folder != nil {
		spec.Workdir = ""
		if folderNode.Kind == scheduler.Windows {
			spec.Folder = folder.Path
		} else {
			spec.Workdir = folder.Path
		}
	}
	return delegation{spec: spec, route: route, label: label, project: project, folder: folder}, nil
}

// The Codex model of pesquisar is AVORA_RESEARCH_MODEL; empty uses Codex's
// own default, so a new Codex release needs no code change.

// research runs a deep web research on Codex: live search, high effort.
func (s *Server) research(ctx context.Context, t *turn, a toolArgs) (string, error) {
	topic := strings.TrimSpace(a.Assunto)
	if topic == "" {
		return "", errors.New("diga o que pesquisar")
	}
	task := `Faça uma pesquisa a fundo na internet sobre o assunto abaixo. Seja proativo e exaustivo, como um pesquisador profissional:
- faça muitas buscas (dezenas), com variações de termos, sinônimos e grafias, e abra as páginas em vez de confiar só no resumo da busca;
- vá além dos primeiros resultados: sites locais e regionais, classificados, portais menores, páginas e grupos públicos do Facebook, perfis e posts públicos do Instagram, OLX, Marketplace, notícias, fóruns;
- confira cada item contra os critérios e descarte o que não serve, dizendo por quê;
- junte os contatos (telefone, WhatsApp, e-mail, perfil) de quem pode ajudar, como lojas, imobiliárias ou vendedores;
- cite o link de cada informação e diga a data quando houver; nunca invente.
Termine com: o que encontrou (organizado), os contatos, o que não conseguiu verificar e os próximos passos sugeridos.

Assunto:
` + topic
	if e := strings.TrimSpace(a.Formato); e != "" {
		task += "\n\nEntregue assim: " + e
	}
	a.Tarefa, a.PodeEditar = task, false
	return s.delegateWith(ctx, t, "codex", a, "pesquisar", topic, func(spec *store.AgentSpec) {
		spec.Model, spec.Effort, spec.Search = s.researchModel, "high", true
		spec.Title = "Pesquisa: " + runTitle(topic)
	})
}

// delegate runs a task on Claude Code or Codex and waits for the result.
func (s *Server) delegate(ctx context.Context, t *turn, engine string, a toolArgs) (string, error) {
	return s.delegateWith(ctx, t, engine, a, engine, strings.TrimSpace(a.Tarefa), nil)
}

// delegateWith is delegate with a last say on the spec (model, search...).
// stepTool and shown are the step the chat shows for it.
func (s *Server) delegateWith(ctx context.Context, t *turn, engine string, a toolArgs, stepTool, shown string, tweak func(*store.AgentSpec)) (string, error) {
	task := strings.TrimSpace(a.Tarefa)
	if task == "" {
		return "", errors.New("diga a tarefa")
	}
	d, err := s.planDelegation(ctx, t, engine, a)
	if err != nil {
		return "", err
	}
	spec, route, label := d.spec, d.route, d.label
	// The run reaches the agent's Plugins through the bridge, if it has any
	// (Antigravity has no bridge yet).
	apps := ""
	if engine != "antigravity" {
		apps = s.bridgeApps(ctx, t.agent.ID)
	}
	if apps != "" {
		var closeBridge func()
		spec.Bridge, closeBridge = s.bridges.open(t, time.Duration(AgentReplyMinutes)*time.Minute+s.approvalWait()+5*time.Minute, s.now())
		defer closeBridge()
	}
	spec.Prompt = truncate(delegatePrompt(t.agent, task, apps, d.project, d.folder), MaxAgentPrompt-8)
	spec.Title = runTitle(task)
	if tweak != nil {
		tweak(spec)
	}
	if err := validateAgent(spec); err != nil {
		return "", err
	}
	started := time.Now()
	end, err := s.runNodeJob(ctx, t, spec, route.target, route.node, "delegou: "+route.reason, label+" · ", func(id string) {
		s.step(t, AgentStep{Tool: stepTool, Summary: shown, Job: id})
	})
	if err != nil {
		return "", err
	}
	took := time.Since(started).Round(time.Second)
	r := end.result
	switch {
	case r != nil && r.Success:
		return fmt.Sprintf("%s terminou em %s. A resposta dele:\n%s", label, took, strings.TrimSpace(r.Output)), nil
	case r != nil:
		why := r.Error
		if why == "" {
			why = "a execução falhou"
		}
		out := fmt.Sprintf("%s falhou depois de %s: %s", label, took, why)
		if o := strings.TrimSpace(r.Output); o != "" {
			out += "\nO que ele chegou a responder:\n" + o
		}
		return out, nil
	case end.job.Status == store.StatusTimedOut:
		return label + " parou de responder e a tarefa foi encerrada.", nil
	case end.job.Status == store.StatusCanceled && ctx.Err() != nil:
		return label + " foi parado.", nil
	default:
		return label + " foi parado antes de terminar.", nil
	}
}

// ── Routines ────────────────────────────────────────────────────

func (s *Server) toolListRoutines(ctx context.Context, t *turn, a toolArgs) (string, error) {
	filter := ""
	if strings.TrimSpace(a.Funcionario) != "" {
		ag, err := s.who(ctx, t, a.Funcionario)
		if err != nil {
			return "", err
		}
		filter = ag.ID
	}
	list, err := s.store.ListRoutines(ctx, filter)
	if err != nil {
		return "", err
	}
	s.step(t, AgentStep{Tool: "ver_rotinas", Summary: fmt.Sprintf("%d rotinas", len(list))})
	if len(list) == 0 {
		return "Nenhuma rotina.", nil
	}
	team, _ := s.store.ListAgents(ctx)
	var b strings.Builder
	for _, rt := range list {
		b.WriteString(s.routineLine(rt, team) + "\n")
	}
	return b.String(), nil
}

func (s *Server) routineLine(rt store.Routine, team []store.Agent) string {
	loc := s.zone()
	where := "#time"
	if rt.Deliver == "conversation" {
		where = "conversa própria"
	}
	line := fmt.Sprintf("- id=%s «%s» de %s, %s, entrega em %s", rt.ID, rt.Title, agentName(team, rt.AgentID), whenOf(rt), where)
	if !rt.Enabled {
		line += ", PAUSADA"
	} else if rt.NextRun != nil {
		line += ", próxima " + rt.NextRun.In(loc).Format("02/01 15:04")
	}
	if rt.LastRun != nil && rt.LastStatus != "" {
		line += fmt.Sprintf("; última %s (%s): %s", rt.LastStatus, rt.LastRun.In(loc).Format("02/01 15:04"), rt.LastPreview)
	}
	line += "\n  instrução: " + truncate(rt.Prompt, 300)
	return line
}

// scheduleOf reads the schedule fields; ok is false when none was sent.
func scheduleOf(a toolArgs) (store.Schedule, bool) {
	if a.has["a_cada_minutos"] && a.ACadaMinutos > 0 {
		return store.Schedule{Every: a.ACadaMinutos}, true
	}
	if a.has["horario"] || a.has["dias"] || a.has["data"] {
		sc := store.Schedule{At: strings.TrimSpace(a.Horario), Days: a.Dias, Date: strings.TrimSpace(a.Data)}
		if sc.Date != "" {
			sc.Days = nil
		}
		return sc, true
	}
	return store.Schedule{}, false
}

func deliverOf(v string) string {
	if v == "conversa" || v == "conversation" {
		return "conversation"
	}
	return "time"
}

func (s *Server) toolCreateRoutine(ctx context.Context, t *turn, a toolArgs) (string, error) {
	owner, err := s.who(ctx, t, a.Funcionario)
	if err != nil {
		return "", err
	}
	sc, ok := scheduleOf(a)
	trigger, err := s.triggerFromArgs(ctx, a.Gatilho)
	if err != nil {
		return "", err
	}
	if !ok && trigger == nil {
		return "", errors.New("diga quando: horario (e dias), a_cada_minutos ou um gatilho")
	}
	on := true
	req := RoutineRequest{AgentID: owner.ID, ResponsibilityID: a.ResponsabilidadeID, Title: a.Titulo, Prompt: a.Instrucao,
		Schedule: sc, Deliver: deliverOf(a.Entregar), Enabled: &on, Skill: &a.Skill, Trigger: trigger}
	rt, _, err := s.toRoutine(ctx, req, store.Routine{})
	if err != nil {
		return "", err
	}
	existing, err := s.store.ListRoutines(ctx, owner.ID)
	if err != nil {
		return "", err
	}
	if len(existing) >= maxRoutinesPerAgent {
		return "", fmt.Errorf("%s já tem %d rotinas, o máximo", owner.Name, maxRoutinesPerAgent)
	}
	rt.ID = newID()[:16]
	saved, err := s.store.SaveRoutine(ctx, rt, "agent:"+t.agent.ID, s.now())
	if err != nil {
		return "", err
	}
	team, _ := s.store.ListAgents(ctx)
	s.step(t, AgentStep{Tool: "criar_rotina", Summary: routineCard(saved, team, s.zone())})
	return "Rotina criada:\n" + s.routineLine(saved, team), nil
}

// routineCard is the one line the app shows for a routine an agent touched.
func routineCard(rt store.Routine, team []store.Agent, loc *time.Location) string {
	where := "#time"
	if rt.Deliver == "conversation" {
		where = "conversa"
	}
	card := fmt.Sprintf("«%s» · %s · %s · %s", rt.Title, agentName(team, rt.AgentID), whenOf(rt), where)
	if !rt.Enabled {
		card += " · pausada"
	} else if rt.NextRun != nil {
		card += " · próxima " + rt.NextRun.In(loc).Format("02/01 15:04")
	}
	return card
}

func (s *Server) toolEditRoutine(ctx context.Context, t *turn, a toolArgs) (string, error) {
	cur, err := s.store.GetRoutine(ctx, a.ID)
	if err != nil {
		return "", errors.New("rotina não encontrada; use ver_rotinas para ver os ids")
	}
	req := RoutineRequest{AgentID: cur.AgentID, ResponsibilityID: cur.ResponsibilityID, Title: cur.Title, Prompt: cur.Prompt,
		Schedule: cur.Schedule, Deliver: cur.Deliver, Enabled: a.Ativa, Trigger: cur.Trigger}
	if a.has["funcionario"] && strings.TrimSpace(a.Funcionario) != "" {
		owner, err := s.who(ctx, t, a.Funcionario)
		if err != nil {
			return "", err
		}
		req.AgentID = owner.ID
		if owner.ID != cur.AgentID {
			req.ResponsibilityID = ""
		}
	}
	if a.has["titulo"] {
		req.Title = a.Titulo
	}
	if a.has["instrucao"] {
		req.Prompt = a.Instrucao
	}
	if a.has["entregar"] {
		req.Deliver = deliverOf(a.Entregar)
	}
	if a.has["skill"] {
		req.Skill = &a.Skill
	}
	if sc, ok := scheduleOf(a); ok {
		req.Schedule, req.Trigger = sc, nil
	}
	if a.Gatilho != nil {
		tr, err := s.triggerFromArgs(ctx, a.Gatilho)
		if err != nil {
			return "", err
		}
		req.Trigger = tr
	}
	rt, _, err := s.toRoutine(ctx, req, cur)
	if err != nil {
		return "", err
	}
	saved, err := s.store.SaveRoutine(ctx, rt, "agent:"+t.agent.ID, s.now())
	if err != nil {
		return "", err
	}
	team, _ := s.store.ListAgents(ctx)
	s.step(t, AgentStep{Tool: "alterar_rotina", Summary: routineCard(saved, team, s.zone())})
	return "Rotina alterada:\n" + s.routineLine(saved, team), nil
}

// ── Responsibilities ────────────────────────────────────────────

func (s *Server) toolSetResponsibility(ctx context.Context, t *turn, a toolArgs) (string, error) {
	owner, err := s.who(ctx, t, a.Funcionario)
	if err != nil {
		return "", err
	}
	list := append([]store.Responsibility{}, owner.Responsibilities...)
	resp := store.Responsibility{ID: strings.TrimSpace(a.ID), Goal: a.Objetivo, Metric: a.Metrica, Cadence: a.Cadencia}
	found := false
	for i, r := range list {
		if resp.ID != "" && r.ID == resp.ID {
			if !a.has["metrica"] {
				resp.Metric = r.Metric
			}
			if !a.has["cadencia"] {
				resp.Cadence = r.Cadence
			}
			list[i], found = resp, true
		}
	}
	if resp.ID != "" && !found {
		return "", errors.New("responsabilidade não encontrada; deixe o id vazio para criar uma nova")
	}
	if !found {
		list = append(list, resp)
	}
	clean, err := validResponsibilities(list)
	if err != nil {
		return "", err
	}
	owner.Responsibilities = clean
	saved, err := s.store.SaveAgent(ctx, owner, "agent:"+t.agent.ID, s.now())
	if err != nil {
		return "", err
	}
	if owner.ID == t.agent.ID {
		t.agent = saved
	}
	var made store.Responsibility
	for _, r := range saved.Responsibilities {
		if (found && r.ID == resp.ID) || (!found && r.Goal == strings.TrimSpace(resp.Goal)) {
			made = r
		}
	}
	verb := "nova"
	if found {
		verb = "alterada"
	}
	s.step(t, AgentStep{Tool: "definir_responsabilidade", Summary: saved.Name + ": " + responsibilityText(made)})
	return fmt.Sprintf("Responsabilidade %s de %s: id=%s, %s. Se precisar de checagem automática, crie uma rotina com responsabilidade_id=%s.",
		verb, saved.Name, made.ID, responsibilityText(made), made.ID), nil
}

func (s *Server) toolRemoveResponsibility(ctx context.Context, t *turn, a toolArgs) (string, error) {
	owner, err := s.who(ctx, t, a.Funcionario)
	if err != nil {
		return "", err
	}
	list := []store.Responsibility{}
	var gone *store.Responsibility
	for _, r := range owner.Responsibilities {
		if r.ID == a.ID {
			r := r
			gone = &r
			continue
		}
		list = append(list, r)
	}
	if gone == nil {
		return "", errors.New("responsabilidade não encontrada")
	}
	owner.Responsibilities = list
	if _, err := s.store.SaveAgent(ctx, owner, "agent:"+t.agent.ID, s.now()); err != nil {
		return "", err
	}
	s.step(t, AgentStep{Tool: "remover_responsabilidade", Summary: owner.Name + ": " + gone.Goal})
	return "Responsabilidade removida: " + gone.Goal, nil
}

// ── Colleagues ──────────────────────────────────────────────────

func (s *Server) consult(ctx context.Context, t *turn, a toolArgs) (string, error) {
	col, err := s.who(ctx, t, a.Funcionario)
	if err != nil {
		return "", err
	}
	if col.ID == t.agent.ID || strings.TrimSpace(a.Funcionario) == "" {
		return "", errors.New("diga qual colega consultar (não você mesmo)")
	}
	q := strings.TrimSpace(a.Pergunta)
	if q == "" {
		return "", errors.New("diga a pergunta")
	}
	s.step(t, AgentStep{Tool: "consultar_colega", Summary: col.Name + ": " + q})
	all, _ := s.store.ListAgents(ctx)
	colTeam := visibleTeam(col, all)
	sub := &turn{agent: col, team: colTeam, id: t.id, conv: t.conv, parent: t}
	history := []store.ChatMessage{{Role: "user", Content: fmt.Sprintf(
		"%s, seu colega no time, está conversando com o dono e pergunta para você:\n%s\n\nResponda direto para %s, curto e com o que ele precisa.",
		t.agent.Name, q, t.agent.Name)}}
	answer := s.runTurn(ctx, sub, nativeSystem(col, colTeam), history)
	if answer.Error != "" {
		return "", fmt.Errorf("%s não conseguiu responder: %s", col.Name, answer.Error)
	}
	// The owner sees the colleague's answer as its message inside this one.
	s.step(t, AgentStep{Tool: StepColleague, Summary: col.ID, Detail: answer.Content})
	return col.Name + " respondeu:\n" + answer.Content, nil
}
