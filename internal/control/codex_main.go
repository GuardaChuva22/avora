package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/engine"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Codex as the main model. The owner asked for it (2026-11-12) because the
// free Nemotron was too weak: in Settings → Modelos a Codex subscription
// shows up as a provider and its models can be picked. Each chat turn then
// runs as a codex.main job on the node the account lives on: Codex thinks
// with the ChatGPT plan and acts only through the agent's own Avora tools,
// which reach it through the bridge (bridge.go, full mode). Its own sandbox
// is read-only, so every write or send still passes Avora's approval gate.
//
// The node's own Codex login is the provider "codex@<node id>"; an extra
// account keeps its provider id ("codex.xxxxxx").

const codexPrincipalPrefix = "codex@"

// maxCodexPrompt bounds the transcript sent to a turn.
const maxCodexPrompt = 120 << 10

type codexModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// codexEngine is the chat engine when the main model or an agent's brain is a Codex account.
type codexEngine struct {
	provider string
	account  string
	rotate   bool
	speed    string
	context  int
	model    string
	// effort is the agent's (D55): "", low, medium, high or max.
	effort string
}

func (e *codexEngine) Name() string     { return "codex" }
func (e *codexEngine) Model() string    { return e.model }
func (e *codexEngine) Provider() string { return "codex:" + e.provider }

// Reply is never used: turns run through runCodexTurn.
func (e *codexEngine) Reply(context.Context, string, []store.ChatMessage, func(llm.Delta)) (llm.Response, error) {
	return llm.Response{}, errors.New("o Codex responde só pelos turnos do chat")
}

// publicError is an error whose text is safe and useful to show the owner.
type publicError struct{ msg string }

func (e publicError) Error() string { return e.msg }

func isCodexProvider(id string) bool {
	return strings.HasPrefix(id, codexPrincipalPrefix) || strings.HasPrefix(id, "codex.")
}

// codexTarget finds the node and the account a Codex provider runs on
// (account "" = the node's own login). online requires the node up now.
func (s *Server) codexTarget(ctx context.Context, id string, online bool) (store.Node, string, error) {
	nodeID, account := strings.TrimPrefix(id, codexPrincipalPrefix), ""
	if !strings.HasPrefix(id, codexPrincipalPrefix) {
		p, err := s.store.GetProvider(ctx, id)
		if err != nil {
			return store.Node{}, "", err
		}
		if p.Kind != "codex" {
			return store.Node{}, "", errors.New("esse provedor não é uma conta do Codex")
		}
		if online && p.Status != "ok" {
			return store.Node{}, "", publicError{"a conta " + p.Name + " do Codex precisa entrar de novo (Configurações → Modelos)"}
		}
		nodeID, account = p.NodeID, p.ID
	}
	n, err := s.nodeByID(ctx, nodeID)
	if err != nil {
		return store.Node{}, "", err
	}
	if n.RevokedAt != nil {
		return store.Node{}, "", errors.New("o nó dessa conta foi removido")
	}
	if online && !s.online(n) {
		return store.Node{}, "", publicError{"o Codex roda em " + n.Name + ", que está offline agora; ligue-o ou troque o cérebro do agente"}
	}
	return n, account, nil
}

// hasCodex says whether a node reported its own Codex login.
func (s *Server) hasCodex(nodeID string) bool {
	for _, q := range s.quotaBook.get(nodeID) {
		if q.Engine == "codex" && q.Account == "" {
			return true
		}
	}
	return false
}

func (s *Server) cachedCodexModels(id string) []codexModel {
	s.models.mu.Lock()
	defer s.models.mu.Unlock()
	return append([]codexModel{}, s.models.codex[id]...)
}

// codexView is how a Codex provider shows up in Settings: CodexMain says it
// can answer as the main model right now.
func (s *Server) codexView(v ProviderView, online bool) ProviderView {
	if models := s.cachedCodexModels(v.ID); len(models) > 0 {
		v.Models, v.ModelNames = make([]string, 0, len(models)), map[string]string{}
		for _, m := range models {
			v.Models = append(v.Models, m.ID)
			v.ModelNames[m.ID] = m.Name
		}
	}
	if v.Models == nil {
		v.Models = []string{}
	}
	acc := v.ID
	if strings.HasPrefix(v.ID, codexPrincipalPrefix) {
		acc = ""
	}
	limited := s.quotaLimited(v.NodeID, "codex", acc)
	if online && v.Status == "ok" && limited {
		v.LastError = s.quotaLimitReason(v.NodeID, "codex", acc)
	}
	v.CodexMain = online && v.Status == "ok" && !limited
	return v
}

// codexViews lists the nodes' own Codex logins (the "principal" ones) and
// marks the extra accounts in list; active is the main model's provider,
// listed even while its node is away.
func (s *Server) codexViews(ctx context.Context, list []ProviderView, active string) []ProviderView {
	nodes, _ := s.store.ListNodes(ctx)
	online := map[string]bool{}
	out := []ProviderView{}
	for _, n := range nodes {
		online[n.ID] = s.online(n)
		id := codexPrincipalPrefix + n.ID
		if n.RevokedAt != nil || !s.hasCodex(n.ID) && id != active {
			continue
		}
		v := ProviderView{Builtin: true, Provider: store.Provider{ID: id, Kind: "codex", Name: "Codex principal · " + n.Name,
			NodeID: n.ID, Status: "ok"}}
		if !s.hasCodex(n.ID) || !online[n.ID] {
			v.Status, v.LastError = "error", n.Name+" está offline"
		}
		out = append(out, s.codexView(v, online[n.ID]))
	}
	for i, v := range list {
		if v.Kind == "codex" {
			list[i] = s.codexView(v, online[v.NodeID])
		}
	}
	return append(out, list...)
}

// refreshCodex asks the account's node which models it can use.
func (s *Server) refreshCodex(ctx context.Context, id string) (ProviderView, error) {
	n, account, err := s.codexTarget(ctx, id, true)
	if err != nil {
		return ProviderView{}, err
	}
	spec := &store.AgentSpec{Engine: "codex", Mode: store.CodexModelsMode, MaxMinutes: 2, Account: account,
		Prompt: "listar modelos"}
	jctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	end, err := s.runNodeJobAs(jctx, nil, spec, n.Kind, n.ID, "modelos do Codex", store.ActorAdmin, "")
	if err != nil {
		return ProviderView{}, err
	}
	r := end.result
	switch {
	case r == nil:
		return ProviderView{}, publicError{"o nó não respondeu a tempo"}
	case !r.Success:
		return ProviderView{}, publicError{codexProblem(r.Error)}
	}
	var models []codexModel
	if err := json.Unmarshal([]byte(r.Output), &models); err != nil || len(models) == 0 {
		return ProviderView{}, publicError{"o Codex não listou nenhum modelo"}
	}
	s.models.mu.Lock()
	if s.models.codex == nil {
		s.models.codex = map[string][]codexModel{}
	}
	s.models.codex[id] = models
	s.models.mu.Unlock()
	if account != "" {
		if p, err := s.store.GetProvider(ctx, id); err == nil {
			p.Models = make([]string, 0, len(models))
			for _, m := range models {
				p.Models = append(p.Models, m.ID)
			}
			_, _ = s.store.SaveProvider(ctx, p, "", s.now())
		}
	}
	return s.codexProvider(ctx, id)
}

// codexProvider is one Codex provider's view.
func (s *Server) codexProvider(ctx context.Context, id string) (ProviderView, error) {
	n, _, err := s.codexTarget(ctx, id, false)
	if err != nil {
		return ProviderView{}, err
	}
	if strings.HasPrefix(id, codexPrincipalPrefix) {
		return s.codexView(ProviderView{Builtin: true, Provider: store.Provider{ID: id, Kind: "codex",
			Name: "Codex principal · " + n.Name, NodeID: n.ID, Status: "ok"}}, s.online(n)), nil
	}
	p, err := s.store.GetProvider(ctx, id)
	if err != nil {
		return ProviderView{}, err
	}
	return s.codexView(ProviderView{Provider: p}, s.online(n)), nil
}

func codexOutOfQuota(msg string) bool {
	low := strings.ToLower(msg)
	for _, k := range []string{"usage limit", "rate limit", "quota", "limit reached", "exhausted", "429", "too many requests"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

type codexCandidate struct {
	id      string
	name    string
	node    store.Node
	account string
	limited bool
}

func (s *Server) codexAccountLabel(ctx context.Context, id string, n store.Node) string {
	if strings.HasPrefix(id, codexPrincipalPrefix) {
		return "conta principal (" + n.Name + ")"
	}
	if p, err := s.store.GetProvider(ctx, id); err == nil && p.Name != "" {
		return p.Name
	}
	return id
}

func (s *Server) codexLimitText(name, nodeID, acc string) string {
	if rc := s.quotaResetClause(nodeID, "codex", acc); rc != "" {
		return "a conta " + name + " do Codex chegou no limite (" + rc + ")"
	}
	return "a conta " + name + " do Codex chegou no limite"
}

// codexTargetsFor resolves candidate Codex accounts, rotating to another online
// Codex account when rotate is true and the chosen account hit its quota limit.
func (s *Server) codexTargetsFor(ctx context.Context, agentName, provider, preferredAccount string, rotate bool) ([]codexCandidate, string, error) {
	startID := provider
	if preferredAccount != "" {
		startID = preferredAccount
	}
	n, account, err := s.codexTarget(ctx, startID, true)
	if err != nil {
		return nil, "", err
	}
	startName := s.codexAccountLabel(ctx, startID, n)
	startLimited := s.quotaLimited(n.ID, "codex", account)
	start := codexCandidate{id: startID, name: startName, node: n, account: account, limited: startLimited}
	if !rotate {
		if startLimited {
			return nil, "", publicError{s.codexLimitText(startName, n.ID, account)}
		}
		return []codexCandidate{start}, "", nil
	}

	var others []codexCandidate
	nodes, _ := s.store.ListNodes(ctx)
	byNode := map[string]store.Node{}
	for _, nd := range nodes {
		if nd.RevokedAt == nil && s.online(nd) {
			byNode[nd.ID] = nd
			pid := codexPrincipalPrefix + nd.ID
			if pid != startID && s.hasCodex(nd.ID) && !s.quotaLimited(nd.ID, "codex", "") {
				others = append(others, codexCandidate{
					id:   pid,
					name: "conta principal (" + nd.Name + ")",
					node: nd,
				})
			}
		}
	}
	if list, err := s.store.ListProviders(ctx); err == nil {
		for _, p := range list {
			if p.Kind != "codex" || p.Status != "ok" || p.ID == startID {
				continue
			}
			nd, ok := byNode[p.NodeID]
			if !ok || s.quotaLimited(nd.ID, "codex", p.ID) {
				continue
			}
			others = append(others, codexCandidate{
				id:      p.ID,
				name:    firstNonEmpty(p.Name, p.ID),
				node:    nd,
				account: p.ID,
			})
		}
	}

	if startLimited {
		if len(others) == 0 {
			return nil, "", publicError{s.codexLimitText(startName, n.ID, account)}
		}
		note := ""
		if agentName != "" {
			note = s.formatRotationLine(agentName, "Codex", startName, others[0].name, n.ID, "codex", account)
		}
		return others, note, nil
	}
	return append([]codexCandidate{start}, others...), "", nil
}

// codexProblem turns a node's error into something the owner can act on.
func codexProblem(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "not installed"):
		return "o Codex não está instalado nesse nó"
	case codexOutOfQuota(msg):
		return "a conta do Codex atingiu o limite de uso; espere renovar ou troque o cérebro do agente"
	case strings.Contains(low, "login") || strings.Contains(low, "logged in") || strings.Contains(low, "401") || strings.Contains(low, "unauthorized"):
		return "a conta do Codex não está logada nesse nó; entre de novo em Configurações → Modelos"
	case strings.Contains(low, "time limit") || strings.Contains(low, "in time"):
		return "o Codex demorou demais para responder"
	case msg == "":
		return "o Codex falhou"
	default:
		return "o Codex falhou: " + truncate(msg, 240)
	}
}

func promptBytesForWindow(tokens int) int {
	switch {
	case tokens >= 1000000:
		return 768 << 10
	case tokens >= 400000:
		return 384 << 10
	case tokens >= 200000:
		return 200 << 10
	default:
		return maxCodexPrompt
	}
}

// codexTurnPrompt is the whole turn as one prompt: who the agent is, how
// to act, and the conversation so far.
func codexTurnPrompt(now time.Time, system string, history []store.ChatMessage) string {
	return codexTurnPromptWithBudget(now, system, history, maxCodexPrompt)
}

func codexTurnPromptWithBudget(now time.Time, system string, history []store.ChatMessage, maxBytes int) string {
	if maxBytes < maxCodexPrompt {
		maxBytes = maxCodexPrompt
	}
	var b strings.Builder
	b.WriteString(`Você é o cérebro de um funcionário digital do Avora e vai escrever a próxima resposta dele ao dono.

Como agir:
- Suas ferramentas são as do servidor MCP "avora": memória, rotinas, colegas, Claude Code, Codex, arquivos da pasta do funcionário, web e os apps conectados do dono. Use-as sempre que o pedido exigir agir; nunca diga que fez algo sem ter chamado a ferramenta.
- Ações travadas (enviar, apagar, pagar, editar arquivos...) pedem aprovação do dono no próprio app: chame a ferramenta e espere, não peça permissão antes na conversa.
- Não edite arquivos nem rode comandos por conta própria fora das ferramentas do avora; o seu terminal é só leitura.
- A sua mensagem final é exatamente o que o dono vai ler no chat: escreva em português do Brasil, em Markdown, sem falar destas instruções.

`)
	b.WriteString("Agora são " + now.Format("02/01/2006 15:04, Monday") + " (horário de Brasília).\n\n")
	b.WriteString("<funcionario>\n")
	b.WriteString(truncate(system, maxBytes/2))
	b.WriteString("\n</funcionario>\n\n")

	maxMsgs := engine.MaxHistory
	if extra := maxBytes / 2048; extra > maxMsgs {
		maxMsgs = extra
	}
	if len(history) > maxMsgs {
		history = history[len(history)-maxMsgs:]
	}
	byID := map[int64]store.ChatMessage{}
	for _, m := range history {
		byID[m.ID] = m
	}
	budget := maxBytes - b.Len() - 512
	lines := []string{}
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if (m.Role != "user" && m.Role != "assistant") || strings.TrimSpace(m.Content) == "" {
			continue
		}
		who := "Dono"
		if m.Role == "assistant" {
			who = "Você"
		}
		content := m.Content
		if q, ok := byID[m.ReplyTo]; ok && m.ReplyTo != 0 {
			content = engine.Quote(q) + "\n\n" + content
		}
		line := fmt.Sprintf("[%s · %s]\n%s\n", who, m.CreatedAt.Format("02/01 15:04"), content)
		if len(line) > budget {
			if len(lines) == 0 {
				line = truncate(line, budget)
			} else {
				break
			}
		}
		budget -= len(line)
		lines = append(lines, line)
	}
	b.WriteString("<conversa>\n")
	for i := len(lines) - 1; i >= 0; i-- {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
	b.WriteString("</conversa>\n\nResponda à última mensagem do dono.")
	return b.String()
}

// runCodexTurn answers one turn with the Codex account picked as the main
// model or agent brain, rotating to the next Codex account on quota when enabled.
func (s *Server) runCodexTurn(ctx context.Context, t *turn, e *codexEngine, system string, history []store.ChatMessage) (llm.Response, error) {
	targets, rotatedNote, err := s.codexTargetsFor(ctx, t.agent.Name, e.provider, e.account, e.rotate)
	if err != nil {
		return llm.Response{}, err
	}
	if rotatedNote != "" {
		s.recordBrainSwitch(t, "rotate", rotatedNote)
	}
	minutes := TurnMinutes
	if minutes > MaxAgentMinutes {
		minutes = MaxAgentMinutes
	}
	// Codex says nothing while it draws (its JSONL has no event for it), so
	// a turn that asks for an image shows the image frame from the start.
	// Not in #time: there the whole channel is one prompt, not the owner's words.
	if !t.channel && wantsImage(history) {
		s.step(t, AgentStep{Tool: StepImage, Summary: "gerando"})
	}
	key, closeBridge := s.bridges.openRun(bridgeRun{t: t, full: true}, time.Duration(minutes+5)*time.Minute, s.now())
	defer closeBridge()
	mode := store.CodexChatMode
	if t.parent != nil {
		mode = store.CodexConsultMode
	}
	prompt := codexTurnPromptWithBudget(s.now().In(s.zone()), system, history, promptBytesForWindow(e.context))
	imgs := s.turnImages(ctx, history)
	var lastErr error
	for i, cand := range targets {
		if i >= 3 {
			break
		}
		spec := &store.AgentSpec{Engine: "codex", Model: e.model, Mode: mode, MaxMinutes: minutes, Account: cand.account,
			Effort: codexEffort(e.effort), Speed: e.speed, Workdir: "agents/" + t.agent.ID, Bridge: key,
			Prompt: prompt, Images: imgs}
		end, err := s.runNodeJobAs(ctx, t, spec, cand.node.Kind, cand.node.ID, t.agent.Name+": resposta pelo Codex", "agent:"+t.agent.ID, "Codex · ")
		if err != nil {
			return llm.Response{}, err
		}
		r := end.result
		switch {
		case r != nil && r.Success:
			resp := llm.Response{Content: strings.TrimSpace(r.Output), Model: e.model}
			var stats struct {
				Input  int `json:"input_tokens"`
				Output int `json:"output_tokens"`
			}
			if json.Unmarshal(r.Stats, &stats) == nil {
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens = stats.Input, stats.Output
			}
			return resp, nil
		case r != nil && codexOutOfQuota(r.Error) && e.rotate && i+1 < len(targets):
			line := s.formatRotationLine(t.agent.Name, "Codex", cand.name, targets[i+1].name, cand.node.ID, "codex", cand.account)
			s.recordBrainSwitch(t, "rotate", line)
			lastErr = publicError{codexProblem(r.Error)}
			continue
		case r != nil:
			return llm.Response{}, publicError{codexProblem(r.Error)}
		case ctx.Err() != nil:
			return llm.Response{}, ctx.Err()
		case end.job.Status == store.StatusTimedOut:
			return llm.Response{}, publicError{"o Codex parou de responder"}
		default:
			return llm.Response{}, publicError{"o Codex foi parado antes de terminar"}
		}
	}
	if lastErr != nil {
		return llm.Response{}, lastErr
	}
	return llm.Response{}, publicError{"o Codex falhou"}
}

var (
	// Whole words only: Go's word boundary does not know accented letters.
	imageVerb  = regexp.MustCompile(`(?i)(^|[^\p{L}])(ger[ae]r?|cri[ae]r?|fa[çc]a|faz|fazer|desenh[ae]r?|ilustr[ae]r?|mont[ae]r?|edit[ae]r?|refa[çc]a|refaz)([^\p{L}]|$)`)
	imageNoun  = regexp.MustCompile(`(?i)(^|[^\p{L}])(imagem|imagens|fotos?|desenhos?|ilustra[çc][ãa]o|ilustra[çc][õo]es|logos?|logotipos?|arte|wallpaper|[íi]cones?|avatar|banner|thumbnail|thumb|figurinha|sticker|pintura|retrato)([^\p{L}]|$)`)
	imageAgain = regexp.MustCompile(`(?i)(^|[^\p{L}])(outra|outro|mais uma|mais um|de novo|novamente|varia[çc][ãa]o|vers[ãa]o)([^\p{L}]|$)`)
)

// wantsImage guesses whether the owner's last message asks for an image:
// "gere uma imagem de…", or "gere outra" right after an image.
func wantsImage(history []store.ChatMessage) bool {
	last := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			last = i
			break
		}
	}
	if last < 0 {
		return false
	}
	msg := history[last].Content
	// A long message is a report or a prompt the server wrote (heartbeat,
	// mission), not a request for a picture.
	if len([]rune(msg)) > 600 || strings.HasPrefix(msg, "💓") || strings.HasPrefix(msg, "🎯") {
		return false
	}
	if !imageVerb.MatchString(msg) {
		return false
	}
	if imageNoun.MatchString(msg) {
		return true
	}
	if !imageAgain.MatchString(msg) {
		return false
	}
	for i := last - 1; i >= 0; i-- {
		if history[i].Role == "assistant" {
			return strings.Contains(history[i].Content, "](avora:file/")
		}
	}
	return false
}
