package control

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/GuardaChuva22/avora/internal/store"
)

// AgentTemplate is a ready-made starting point for NewAgentSheet (Phase 5).
type AgentTemplate struct {
	ID               string                 `json:"id"`
	Title            string                 `json:"title"`
	Summary          string                 `json:"summary"`
	DefaultName      string                 `json:"default_name"`
	Role             string                 `json:"role"`
	Color            string                 `json:"color"`
	Persona          string                 `json:"persona"`
	Responsibilities []store.Responsibility `json:"responsibilities"`
	SuggestedSkills  []string               `json:"suggested_skills"`
}

type WelcomeRequest struct {
	TemplateID      string   `json:"template_id,omitempty"`
	SuggestedSkills []string `json:"suggested_skills,omitempty"`
}

type WelcomeResponse struct {
	Conversation store.Conversation `json:"conversation"`
	Message      store.ChatMessage  `json:"message"`
}

type OnboardingGreetResponse struct {
	ChannelID string `json:"channel_id"`
	Agents    int    `json:"agents"`
	Live      bool   `json:"live"`
}

func agentTemplatesCatalog() []AgentTemplate {
	return []AgentTemplate{
		{
			ID:          "pm",
			Title:       "PM",
			Summary:     "Organiza prioridades, acompanha entregas e mantém o time alinhado.",
			DefaultName: "Lia",
			Role:        "Produto e Projetos",
			Color:       "#c9b8ff",
			Persona: `Você cuida de produto, prioridades e acompanhamento de entregas.
- Transforme pedidos abertos em planos curtos com escopo claro, dono e próximo passo.
- Destaque riscos e dependências cedo, sem burocracia.
- Escreva de forma objetiva, sempre em português simples e direto.`,
			Responsibilities: []store.Responsibility{
				{ID: "prioridades-semana", Goal: "Manter as prioridades da semana claras e em dia", Metric: "entregas concluídas vs. planejadas", Cadence: "toda segunda"},
				{ID: "bloqueios", Goal: "Destravar pendências do time antes de virarem atraso", Metric: "itens parados há mais de 2 dias", Cadence: "todo dia útil"},
			},
			SuggestedSkills: []string{"priorizar-backlog", "resumo-semanal"},
		},
		{
			ID:          "dev",
			Title:       "Dev",
			Summary:     "Escreve, revisa e conserta código delegando tarefas pesadas aos executores.",
			DefaultName: "Atlas",
			Role:        "Desenvolvedor",
			Color:       "#7fd1ff",
			Persona: `Você é o desenvolvedor do time. Para escrever ou mudar código, delegue ao Claude Code ou ao Codex com pode_editar=true.
- Antes de mudanças grandes, explique o plano em uma linha e delegue a tarefa completa.
- Prefira alterações pequenas, claras e fáceis de testar.
- Ao terminar, resuma o que mudou e em quais arquivos.`,
			Responsibilities: []store.Responsibility{
				{ID: "qualidade-codigo", Goal: "Entregar mudanças limpas e sem regressões", Metric: "builds e testes passando", Cadence: "a cada entrega"},
				{ID: "revisao-tecnica", Goal: "Apontar dívidas técnicas e melhorias simples", Metric: "pontos críticos resolvidos", Cadence: "toda sexta"},
			},
			SuggestedSkills: []string{"revisar-codigo", "corrigir-bug"},
		},
		{
			ID:          "pesquisa",
			Title:       "Pesquisa",
			Summary:     "Investiga assuntos a fundo, compara fontes e entrega relatórios verificados.",
			DefaultName: "Iris",
			Role:        "Pesquisadora",
			Color:       "#ffc48a",
			Persona: `Você pesquisa a fundo e responde apenas com informações verificadas.
- Cruze várias fontes, aponte divergências e diga o grau de confiança.
- Entregue em tópicos curtos, sempre com os links das fontes ao final.
- Nunca invente números ou fatos: quando não encontrar, diga claramente.`,
			Responsibilities: []store.Responsibility{
				{ID: "fontes-confiaveis", Goal: "Trazer dados verificados com fontes claras para decisões", Metric: "fontes citadas por pesquisa", Cadence: "a cada pedido"},
				{ID: "radar-novidades", Goal: "Acompanhar novidades importantes da área", Metric: "resumo objetivo com links", Cadence: "toda semana"},
			},
			SuggestedSkills: []string{"pesquisa-fontes", "comparativo-mercado"},
		},
		{
			ID:          "ops",
			Title:       "Ops",
			Summary:     "Cuida de servidores, rotinas recorrentes, checklists e saúde da operação.",
			DefaultName: "Caio",
			Role:        "Operações",
			Color:       "#86e3bd",
			Persona: `Você cuida da operação, automações, rotinas e estabilidade do ambiente.
- Monitore o que está rodando, avise logo quando algo sair do esperado e sugira a correção.
- Documente passos repetitivos em checklists simples.
- Seja direto: o que aconteceu, o impacto e o que fazer agora.`,
			Responsibilities: []store.Responsibility{
				{ID: "saude-operacao", Goal: "Garantir que computadores, integrações e rotinas estejam saudáveis", Metric: "falhas pendentes = 0", Cadence: "todo dia"},
				{ID: "automacao-rotinas", Goal: "Automatizar tarefas repetitivas com rotinas confiáveis", Metric: "tempo economizado na semana", Cadence: "quinzenal"},
			},
			SuggestedSkills: []string{"checar-servidores", "relatorio-operacional"},
		},
		{
			ID:          "assistente",
			Title:       "Assistente pessoal",
			Summary:     "Ajuda no dia a dia: agenda, e-mails, lembretes, documentos e organização.",
			DefaultName: "Clara",
			Role:        "Assistente pessoal",
			Color:       "#ff9db8",
			Persona: `Você organiza o dia a dia de quem trabalha com você: compromissos, mensagens, lembretes e documentos.
- Antecipe o que vence hoje, resuma o que importa e sugira respostas prontas quando ajudar.
- Respeite o jeito e as prioridades da pessoa; na dúvida antes de enviar algo externo, pergunte.
- Fale com leveza, clareza e sem enrolação.`,
			Responsibilities: []store.Responsibility{
				{ID: "agenda-em-dia", Goal: "Manter compromissos, prazos e lembretes organizados", Metric: "nenhum prazo esquecido", Cadence: "todo dia às 08:00"},
				{ID: "caixa-entrada", Goal: "Destacar mensagens e pendências que precisam de resposta", Metric: "itens urgentes triados", Cadence: "dias úteis"},
			},
			SuggestedSkills: []string{"triagem-emails", "resumo-do-dia"},
		},
		{
			ID:               "em-branco",
			Title:            "Em branco",
			Summary:          "Comece do zero e defina função, instruções e rotinas no seu ritmo.",
			DefaultName:      "",
			Role:             "",
			Color:            "#9fb4ff",
			Persona:          "",
			Responsibilities: []store.Responsibility{},
			SuggestedSkills:  []string{},
		},
	}
}

func (s *Server) registerTemplates(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agent-templates", s.admin(s.listAgentTemplates))
	mux.HandleFunc("POST /v1/agents/{id}/welcome", s.admin(s.welcomeAgent))
	mux.HandleFunc("POST /v1/onboarding/greet", s.admin(s.onboardingGreet))
}

func (s *Server) listAgentTemplates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, agentTemplatesCatalog())
}

func (s *Server) welcomeAgent(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	var req WelcomeRequest
	if r.ContentLength != 0 && !readJSON(w, r, &req) {
		return
	}
	a, err := s.store.GetAgent(ctx, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if a.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}

	skills := req.SuggestedSkills
	if len(skills) == 0 && req.TemplateID != "" {
		for _, tpl := range agentTemplatesCatalog() {
			if tpl.ID == req.TemplateID {
				skills = tpl.SuggestedSkills
				break
			}
		}
	}

	rawID, err := randomBytes(12)
	if err != nil {
		http.Error(w, "failed to create conversation", http.StatusInternalServerError)
		return
	}
	model := ""
	if eng := s.chatEngine(); eng != nil {
		model = eng.Model()
	}
	conv, err := s.store.CreateConversation(ctx, store.Conversation{
		ID:          hex.EncodeToString(rawID),
		WorkspaceID: wsID,
		AgentID:     a.ID,
		Title:       "Pra eu começar",
		Engine:      "native",
		Model:       model,
		CreatedAt:   s.now(),
	}, actorFrom(r))
	if err != nil {
		storeError(w, err)
		return
	}

	brainItem := fmt.Sprintf("- [Escolher meu cérebro](avora:setup/brain/%s) — ajustar qual conta ou modelo pensa por mim", a.ID)
	if s.chatEngine() == nil && a.Brain.Provider == "" {
		brainItem = fmt.Sprintf("- [Conectar meu cérebro](avora:setup/connect) — ligar uma conta de IA (ou [escolher na minha ficha](avora:setup/brain/%s))", a.ID)
	}
	toolsHint := "ligar plugins, pastas e permissões"
	if len(skills) > 0 {
		toolsHint = "ligar plugins e skills sugeridas (" + strings.Join(skills, ", ") + ")"
	}
	toolsItem := fmt.Sprintf("- [Configurar ferramentas](avora:setup/tools/%s) — %s", a.ID, toolsHint)
	channelsItem := fmt.Sprintf("- [Escolher canais](avora:setup/channels/%s) — definir em quais conversas de grupo participo", a.ID)
	routineItem := fmt.Sprintf("- [Criar minha primeira rotina](avora:setup/routine/%s) — combinar algo recorrente para eu fazer sozinho", a.ID)

	intro := fmt.Sprintf("Oi! Eu sou **%s**", a.Name)
	if a.Role != "" {
		intro += " (" + a.Role + ")"
	}
	intro += "."

	content := strings.Join([]string{
		intro,
		"",
		"**Pra eu começar:**",
		brainItem,
		toolsItem,
		channelsItem,
		routineItem,
		"",
		"Ou pode me pedir algo direto aqui embaixo!",
	}, "\n")

	msg, err := s.store.AppendMessage(ctx, store.ChatMessage{
		ConversationID: conv.ID,
		Role:           "assistant",
		AgentID:        a.ID,
		Content:        content,
		CreatedAt:      s.now(),
	}, "agent:"+a.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	if updated, gerr := s.store.GetConversation(ctx, conv.ID); gerr == nil {
		conv = updated
	}
	writeJSON(w, http.StatusCreated, WelcomeResponse{Conversation: conv, Message: msg})
}

func (s *Server) ensureWorkspaceTeamChannel(w http.ResponseWriter, r *http.Request, wsID string) (store.Channel, bool) {
	ctx := store.WithWorkspace(r.Context(), wsID)
	channels, err := s.store.ListChannels(ctx)
	if err != nil {
		storeError(w, err)
		return store.Channel{}, false
	}
	for _, c := range channels {
		if c.ArchivedAt == nil && (c.Name == "time" || c.Everyone) {
			return c, true
		}
	}
	if len(channels) > 0 {
		return channels[0], true
	}
	chID := TeamChannelID
	if wsID != store.DefaultWorkspaceID {
		chID = wsID + "-time"
	}
	created, err := s.store.SaveChannel(ctx, store.Channel{
		ID:          chID,
		WorkspaceID: wsID,
		Name:        "time",
		Everyone:    true,
		Purpose:     "O grupo do time todo: papo, avisos e pedidos rápidos.",
	}, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return store.Channel{}, false
	}
	return created, true
}

func (s *Server) onboardingGreet(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ch, ok := s.ensureWorkspaceTeamChannel(w, r, wsID)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	if len(agents) == 0 {
		writeJSON(w, http.StatusOK, OnboardingGreetResponse{ChannelID: ch.ID, Agents: 0, Live: false})
		return
	}

	owner := s.greetingNameOf(ctx, wsID)
	hello := "Oi, time! Acabamos de abrir nosso espaço."
	if owner != "" {
		hello = fmt.Sprintf("Oi, time! Aqui é %s, acabamos de abrir nosso espaço.", owner)
	}
	_, _ = s.store.AppendMessage(ctx, store.ChatMessage{
		ConversationID: ch.ID,
		Role:           "user",
		Content:        hello,
		CreatedAt:      s.now(),
	}, actorFrom(r))

	limit := len(agents)
	if limit > 3 {
		limit = 3
	}
	if s.chatEngine() != nil {
		for i := 0; i < limit; i++ {
			s.callInChannel(ch.ID, agents[i].ID, ownerName)
		}
		writeJSON(w, http.StatusOK, OnboardingGreetResponse{ChannelID: ch.ID, Agents: limit, Live: true})
		return
	}

	for i := 0; i < limit; i++ {
		a := agents[i]
		roleText := ""
		if a.Role != "" {
			roleText = " — cuido de " + strings.ToLower(a.Role)
		}
		line := fmt.Sprintf("Oi%s! Eu sou %s%s. Assim que conectar uma conta de IA, é só chamar a gente aqui no #time ou conversar comigo no privado!",
			func() string {
				if owner != "" {
					return ", " + owner
				}
				return ""
			}(), a.Name, roleText)
		_, _ = s.store.AppendMessage(ctx, store.ChatMessage{
			ConversationID: ch.ID,
			Role:           "assistant",
			AgentID:        a.ID,
			Content:        line,
			CreatedAt:      s.now(),
		}, "agent:"+a.ID)
	}
	writeJSON(w, http.StatusOK, OnboardingGreetResponse{ChannelID: ch.ID, Agents: limit, Live: false})
}
