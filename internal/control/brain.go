package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/engine"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
)

// What each agent thinks with (D55). An agent's brain is a provider of
// Settings → Modelos (an API, a Codex account, Antigravity or Claude Code with
// the owner's subscription), its model, effort, fallback policy, account
// rotation, context window and speed. Whatever the brain, the agent acts only
// through Avora's tools (the bridge for Codex, Antigravity and Claude), so its
// hands, approvals and memory stay the same. An empty brain is the main model.

// claudeBrain is the provider id of the owner's own Claude Code login, on
// whichever computer runs it (like delegation).
const claudeBrain = "claude"

// Efforts an agent can think with; "" is the engine's default.
var brainEfforts = []string{"low", "medium", "high", "max"}

// Speeds offered where the provider supports a fast mode.
var brainSpeeds = []string{"standard", "fast"}

// Valid context window overrides in tokens (0 = default).
var brainContextChoices = []int{200000, 400000, 1000000}

// claudeModels are the aliases Claude Code takes for --model.
var claudeModels = []codexModel{{ID: "opus", Name: "Opus"}, {ID: "sonnet", Name: "Sonnet"}, {ID: "haiku", Name: "Haiku"}}

// claudeEngine answers a turn with Claude Code on a node.
type claudeEngine struct {
	provider string // claudeBrain or a claude account's provider id
	account  string
	rotate   bool
	speed    string
	context  int
	model    string
	effort   string
}

func (e *claudeEngine) Name() string     { return "claude" }
func (e *claudeEngine) Model() string    { return firstNonEmpty(e.model, "claude") }
func (e *claudeEngine) Provider() string { return "claude:" + e.provider }

func (e *claudeEngine) Reply(context.Context, string, []store.ChatMessage, func(llm.Delta)) (llm.Response, error) {
	return llm.Response{}, errors.New("o Claude responde só pelos turnos do chat")
}

func firstNonEmpty(v ...string) string {
	for _, x := range v {
		if x != "" {
			return x
		}
	}
	return ""
}

func isClaudeProvider(id string) bool {
	return id == claudeBrain || strings.HasPrefix(id, "claude.")
}

// codexEffort is the Codex name of an effort ("max" is its xhigh).
func codexEffort(e string) string {
	if e == "max" {
		return "xhigh"
	}
	return e
}

func providerSupportsSpeed(kind, providerID string) bool {
	if isClaudeProvider(providerID) || isCodexProvider(providerID) || isAgyProvider(providerID) {
		return true
	}
	switch kind {
	case "openai_api", "anthropic", "openrouter", "groq", "cerebras", "fireworks", "gemini", "xai":
		return true
	}
	return false
}

// modelMaxContext estimates the maximum context window (in tokens) accepted by
// model on provider/kind, capped at 1M.
func modelMaxContext(provider, kind, model string) int {
	low := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(low, "1m"):
		return 1000000
	case strings.Contains(low, "haiku"):
		return 200000
	case strings.Contains(low, "gemini") || strings.Contains(low, "claude-opus") || strings.Contains(low, "claude-sonnet") ||
		low == "opus" || low == "sonnet" || strings.Contains(low, "gpt-4.1") || strings.Contains(low, "gpt-6"):
		return 1000000
	case strings.Contains(low, "gpt-5") || strings.Contains(low, "codex") || strings.HasPrefix(low, "o3") ||
		strings.HasPrefix(low, "o4") || strings.Contains(low, "grok"):
		return 400000
	case strings.Contains(low, "llama") || strings.Contains(low, "mistral") || strings.Contains(low, "qwen") ||
		strings.Contains(low, "deepseek") || strings.Contains(low, "nemotron") || strings.Contains(low, "gemma"):
		return 200000
	}
	switch {
	case isAgyProvider(provider) || kind == "antigravity" || kind == "gemini":
		return 1000000
	case isClaudeProvider(provider) || kind == "claude" || kind == "anthropic":
		return 1000000
	case isCodexProvider(provider) || kind == "codex" || kind == "openai_api":
		return 400000
	case low == "":
		return 1000000
	default:
		return 200000
	}
}

func (s *Server) maxBrainContext(ctx context.Context, b store.AgentBrain) int {
	kind := ""
	if b.Provider != "" && b.Provider != claudeBrain && b.Provider != agyBrain &&
		!strings.HasPrefix(b.Provider, codexPrincipalPrefix) && b.Provider != builtinProviderID {
		if p, err := s.store.GetProvider(ctx, b.Provider); err == nil {
			kind = p.Kind
			if b.Model != "" {
				if mm, ok := p.ModelMeta[b.Model]; ok && mm.Context > 0 {
					return mm.Context
				}
			}
		}
	}
	return modelMaxContext(b.Provider, kind, b.Model)
}

// formatRotationLine builds the single-line chat notice emitted when an agent
// rotates from a quota-limited account to the next account of the same kind.
func (s *Server) formatRotationLine(agentName, brand, fromName, toName, nodeID, engineName, account string) string {
	suffix := ""
	if rc := s.quotaResetClause(nodeID, engineName, account); rc != "" {
		suffix = ", " + rc
	}
	return fmt.Sprintf("%s passou para %s do %s: %s chegou no limite%s", agentName, toName, brand, fromName, suffix)
}

// recordBrainSwitch logs a fallback or account rotation both on the turn's run
// steps and as a dedicated brain.switched live event for the chat view.
func (s *Server) recordBrainSwitch(t *turn, kind, message string) {
	if t == nil || strings.TrimSpace(message) == "" {
		return
	}
	s.step(t, AgentStep{Tool: "Cérebro", Summary: message})
	payload, _ := json.Marshal(map[string]string{
		"conversation_id": t.conv,
		"agent_id":        t.agent.ID,
		"agent_name":      t.agent.Name,
		"kind":            kind,
		"message":         message,
	})
	_ = s.store.RecordEvent(context.Background(), store.Event{
		At: s.now(), Type: store.EventBrainSwitched, Actor: "agent:" + t.agent.ID, JobID: t.id, Payload: payload,
	})
	s.hub.publish(t.id, hubEvent{"brain", map[string]string{
		"kind":            kind,
		"message":         message,
		"agent_id":        t.agent.ID,
		"conversation_id": t.conv,
	}})
}

// brainFor is the engine an agent thinks with now, and a note when its own
// brain could not answer and the automatic model stands in.
func (s *Server) brainFor(ctx context.Context, a store.Agent) (ChatEngine, string) {
	eng, note, hardErr := s.selectBrain(ctx, a)
	if hardErr != nil {
		return nil, hardErr.Error()
	}
	return eng, note
}

func (s *Server) applyBrainOptionsToMain(ctx context.Context, main ChatEngine, a store.Agent) ChatEngine {
	if main == nil {
		return nil
	}
	b := a.Brain
	win := 0
	if b.Context > 0 {
		win = min(b.Context, s.maxBrainContext(ctx, b))
	}
	switch me := main.(type) {
	case *codexEngine:
		c := *me
		if b.Effort != "" {
			c.effort = b.Effort
		}
		if b.Speed != "" {
			c.speed = b.Speed
		}
		if win > 0 {
			c.context = win
		}
		return &c
	case *agyEngine:
		c := *me
		if b.Effort != "" {
			c.effort = b.Effort
		}
		if b.Speed != "" {
			c.speed = b.Speed
		}
		if win > 0 {
			c.context = win
		}
		return &c
	case *engine.Native:
		if b.Speed != "" || win > 0 {
			clone := *me
			clone.WithSpeed(b.Speed).WithContextTokens(win)
			return &clone
		}
	}
	return main
}

// selectBrain resolves the engine for agent a, returning a hard error when
// fallback is "none" and the configured brain cannot answer.
func (s *Server) selectBrain(ctx context.Context, a store.Agent) (ChatEngine, string, error) {
	s.repick(ctx, false) // a computer that came online since
	main := s.chatEngine()
	b := a.Brain
	if b.Provider == "" {
		return s.applyBrainOptionsToMain(ctx, main, a), "", nil
	}
	eng, err := s.brainEngine(ctx, b)
	if err == nil {
		return eng, "", nil
	}
	why := "não está disponível"
	var pe publicError
	if errors.As(err, &pe) {
		why = pe.msg
	} else if errors.Is(err, store.ErrNotFound) {
		why = "a conta foi removida"
	}
	if b.FallbackMode() == "none" {
		return nil, "", publicError{"o cérebro de " + a.Name + " (" + s.brainName(ctx, b) + ") " + why}
	}
	if main == nil {
		return nil, "", nil
	}
	return s.applyBrainOptionsToMain(ctx, main, store.Agent{Brain: store.AgentBrain{Context: b.Context, Speed: b.Speed}}),
		"o cérebro de " + a.Name + " (" + s.brainName(ctx, b) + ") " + why + "; respondi com o modelo automático", nil
}

// brainEngine builds the engine of a brain, checking it can answer now.
func (s *Server) brainEngine(ctx context.Context, b store.AgentBrain) (ChatEngine, error) {
	rot := b.Rotates()
	win := 0
	if b.Context > 0 {
		win = min(b.Context, s.maxBrainContext(ctx, b))
	}
	switch {
	case isAgyProvider(b.Provider):
		if _, _, err := s.agyTargetsFor(ctx, "", b.Provider, b.Account, rot); err != nil {
			return nil, err
		}
		return &agyEngine{provider: b.Provider, account: b.Account, rotate: rot, speed: b.Speed, context: win, model: b.Model, effort: b.Effort}, nil
	case isCodexProvider(b.Provider):
		if _, _, err := s.codexTargetsFor(ctx, "", b.Provider, b.Account, rot); err != nil {
			return nil, err
		}
		return &codexEngine{provider: b.Provider, account: b.Account, rotate: rot, speed: b.Speed, context: win, model: b.Model, effort: b.Effort}, nil
	case isClaudeProvider(b.Provider):
		if _, _, err := s.claudeTargetsFor(ctx, "", b.Provider, b.Account, rot); err != nil {
			return nil, err
		}
		return &claudeEngine{provider: b.Provider, account: b.Account, rotate: rot, speed: b.Speed, context: win, model: b.Model, effort: b.Effort}, nil
	}
	c, err := s.providerClient(ctx, b.Provider)
	if err != nil {
		return nil, err
	}
	if b.Provider != builtinProviderID {
		if p, perr := s.store.GetProvider(ctx, b.Provider); perr == nil && p.Status != "ok" {
			return nil, publicError{firstNonEmpty(p.LastError, "não respondeu no último teste")}
		}
	}
	model := b.Model
	if model == "" {
		model = s.firstModel(ctx, b.Provider)
	}
	if model == "" {
		return nil, publicError{"escolha um modelo"}
	}
	return engine.NewNative(c, model).WithSpeed(b.Speed).WithContextTokens(win), nil
}

func (s *Server) firstModel(ctx context.Context, provider string) string {
	if provider == builtinProviderID {
		s.models.mu.Lock()
		defer s.models.mu.Unlock()
		return s.models.builtinModel
	}
	if p, err := s.store.GetProvider(ctx, provider); err == nil && len(p.Models) > 0 {
		return p.Models[0]
	}
	return ""
}

// claudeTarget finds where a Claude brain runs now: the node, its kind and
// the account ("" = the node's own login).
func (s *Server) claudeTarget(ctx context.Context, id string) (string, scheduler.Target, string, error) {
	if id == claudeBrain {
		r, err := s.routeEngine(ctx, "claude")
		if err != nil {
			return "", "", "", publicError{"nenhum computador com o Claude Code está online agora"}
		}
		return r.node, r.target, "", nil
	}
	p, err := s.store.GetProvider(ctx, id)
	if err != nil {
		return "", "", "", err
	}
	if p.Kind != "claude" {
		return "", "", "", errors.New("esse provedor não é uma conta do Claude")
	}
	if p.Status != "ok" {
		return "", "", "", publicError{"a conta " + p.Name + " do Claude precisa entrar de novo (Configurações → Modelos)"}
	}
	n, err := s.nodeByID(ctx, p.NodeID)
	if err != nil {
		return "", "", "", err
	}
	if n.RevokedAt != nil || !s.online(n) {
		return "", "", "", publicError{"o Claude dessa conta roda em " + n.Name + ", que está offline agora"}
	}
	return n.ID, n.Kind, p.ID, nil
}

func claudeOutOfQuota(msg string) bool {
	low := strings.ToLower(msg)
	for _, k := range []string{"rate limit", "usage limit", "hit your limit", "quota", "limit reached", "exhausted", "429", "too many requests"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

type claudeCandidate struct {
	id      string
	name    string
	nodeID  string
	target  scheduler.Target
	account string
	limited bool
}

func (s *Server) claudeAccountLabel(ctx context.Context, id string) string {
	if id == claudeBrain {
		return "a conta principal"
	}
	if p, err := s.store.GetProvider(ctx, id); err == nil && p.Name != "" {
		return p.Name
	}
	return id
}

func (s *Server) claudeLimitText(name, nodeID, acc string) string {
	if rc := s.quotaResetClause(nodeID, "claude", acc); rc != "" {
		return name + " do Claude chegou no limite (" + rc + ")"
	}
	return name + " do Claude chegou no limite"
}

// claudeTargetsFor resolves candidate Claude accounts, rotating to the next
// online account when rotate is true and the chosen account hit its limit.
func (s *Server) claudeTargetsFor(ctx context.Context, agentName, provider, preferredAccount string, rotate bool) ([]claudeCandidate, string, error) {
	startID := provider
	if preferredAccount != "" {
		startID = preferredAccount
	}
	nodeID, target, acc, err := s.claudeTarget(ctx, startID)
	if err != nil {
		return nil, "", err
	}
	startName := s.claudeAccountLabel(ctx, startID)
	if q, ok := s.quotaEntry(nodeID, "claude", acc); ok && q.Error != "" {
		nodeName := nodeID
		if nd, nerr := s.nodeByID(ctx, nodeID); nerr == nil && nd.Name != "" {
			nodeName = nd.Name
		}
		if reason := cliErrorReason("Claude Code", nodeName, q.Error); reason != "" {
			return nil, "", publicError{strings.TrimSuffix(reason, ".")}
		}
	}
	startLimited := s.quotaLimited(nodeID, "claude", acc)
	start := claudeCandidate{id: startID, name: startName, nodeID: nodeID, target: target, account: acc, limited: startLimited}
	if !rotate {
		if startLimited {
			return nil, "", publicError{s.claudeLimitText(startName, nodeID, acc)}
		}
		return []claudeCandidate{start}, "", nil
	}

	var others []claudeCandidate
	if startID != claudeBrain {
		if r, rerr := s.routeEngine(ctx, "claude"); rerr == nil && !s.quotaLimited(r.node, "claude", "") {
			others = append(others, claudeCandidate{
				id:     claudeBrain,
				name:   "a conta principal",
				nodeID: r.node,
				target: r.target,
			})
		}
	}
	if list, lerr := s.store.ListProviders(ctx); lerr == nil {
		for _, p := range list {
			if p.Kind != "claude" || p.Status != "ok" || p.ID == startID {
				continue
			}
			nd, nerr := s.nodeByID(ctx, p.NodeID)
			if nerr != nil || nd.RevokedAt != nil || !s.online(nd) || s.quotaLimited(nd.ID, "claude", p.ID) {
				continue
			}
			others = append(others, claudeCandidate{
				id:      p.ID,
				name:    firstNonEmpty(p.Name, p.ID),
				nodeID:  nd.ID,
				target:  nd.Kind,
				account: p.ID,
			})
		}
	}

	if startLimited {
		if len(others) == 0 {
			return nil, "", publicError{s.claudeLimitText(startName, nodeID, acc)}
		}
		note := ""
		if agentName != "" {
			note = s.formatRotationLine(agentName, "Claude", startName, others[0].name, nodeID, "claude", acc)
		}
		return others, note, nil
	}
	return append([]claudeCandidate{start}, others...), "", nil
}

// brainName is how a brain reads to the owner.
func (s *Server) brainName(ctx context.Context, b store.AgentBrain) string {
	if b.Provider == claudeBrain {
		return "Claude"
	}
	if b.Provider == agyBrain {
		return "Antigravity"
	}
	if strings.HasPrefix(b.Provider, codexPrincipalPrefix) {
		return "Codex"
	}
	if b.Provider == builtinProviderID {
		return builtinProviderName
	}
	if p, err := s.store.GetProvider(ctx, b.Provider); err == nil {
		return p.Name
	}
	return b.Provider
}

// runClaudeTurn answers one turn with Claude Code, rotating to the next Claude
// account when enabled and the current account runs out of quota.
func (s *Server) runClaudeTurn(ctx context.Context, t *turn, e *claudeEngine, system string, history []store.ChatMessage) (llm.Response, error) {
	targets, rotatedNote, err := s.claudeTargetsFor(ctx, t.agent.Name, e.provider, e.account, e.rotate)
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
	key, closeBridge := s.bridges.openRun(bridgeRun{t: t, full: true}, time.Duration(minutes+5)*time.Minute, s.now())
	defer closeBridge()
	prompt := codexTurnPromptWithBudget(s.now().In(s.zone()), system, history, promptBytesForWindow(e.context))
	var lastErr error
	for i, cand := range targets {
		if i >= 3 {
			break
		}
		spec := &store.AgentSpec{Engine: "claude", Model: e.model, Mode: "read", MaxMinutes: minutes, Account: cand.account,
			Effort: e.effort, Speed: e.speed, Workdir: "agents/" + t.agent.ID, Bridge: key, Prompt: prompt}
		end, err := s.runNodeJobAs(ctx, t, spec, cand.target, cand.nodeID, t.agent.Name+": resposta pelo Claude", "agent:"+t.agent.ID, "Claude · ")
		if err != nil {
			return llm.Response{}, err
		}
		r := end.result
		switch {
		case r != nil && r.Success:
			resp := llm.Response{Content: strings.TrimSpace(r.Output), Model: e.Model()}
			var stats struct {
				Input  int `json:"input_tokens"`
				Output int `json:"output_tokens"`
			}
			if json.Unmarshal(r.Stats, &stats) == nil {
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens = stats.Input, stats.Output
			}
			return resp, nil
		case r != nil && claudeOutOfQuota(r.Error) && e.rotate && i+1 < len(targets):
			line := s.formatRotationLine(t.agent.Name, "Claude", cand.name, targets[i+1].name, cand.nodeID, "claude", cand.account)
			s.recordBrainSwitch(t, "rotate", line)
			lastErr = publicError{"o Claude atingiu o limite de uso: " + truncate(r.Error, 240)}
			continue
		case r != nil:
			return llm.Response{}, publicError{"o Claude não conseguiu responder: " + truncate(r.Error, 300)}
		case ctx.Err() != nil:
			return llm.Response{}, ctx.Err()
		case end.job.Status == store.StatusTimedOut:
			return llm.Response{}, publicError{"o Claude parou de responder"}
		default:
			return llm.Response{}, publicError{"o Claude foi parado antes de terminar"}
		}
	}
	if lastErr != nil {
		return llm.Response{}, lastErr
	}
	return llm.Response{}, publicError{"o Claude falhou"}
}

// validBrain checks a brain the owner picked.
func (s *Server) validBrain(ctx context.Context, b store.AgentBrain) (store.AgentBrain, error) {
	b.Provider = strings.TrimSpace(b.Provider)
	b.Model = strings.TrimSpace(b.Model)
	b.Effort = strings.TrimSpace(b.Effort)
	b.Fallback = strings.TrimSpace(b.Fallback)
	b.Account = strings.TrimSpace(b.Account)
	b.Speed = strings.TrimSpace(b.Speed)

	if b.Effort != "" && !contains(brainEfforts, b.Effort) {
		return b, errors.New("brain.effort must be low, medium, high or max")
	}
	if b.Model != "" && !validModel(b.Model) {
		return b, errors.New("brain.model must be a plain model name")
	}
	switch b.Fallback {
	case "", "auto", "none":
	default:
		return b, errors.New("brain.fallback must be auto or none")
	}
	switch b.Speed {
	case "", "standard", "fast":
	default:
		return b, errors.New("brain.speed must be standard or fast")
	}
	if b.Context != 0 {
		okCtx := false
		for _, c := range brainContextChoices {
			if b.Context == c {
				okCtx = true
				break
			}
		}
		if !okCtx {
			return b, errors.New("brain.context must be 200000, 400000 or 1000000")
		}
	}

	switch {
	case b.Provider == "":
		b.Model = "" // the main model's own
		b.Account = ""
	case b.Provider == claudeBrain || b.Provider == agyBrain || strings.HasPrefix(b.Provider, codexPrincipalPrefix) || b.Provider == builtinProviderID:
	default:
		p, err := s.store.GetProvider(ctx, b.Provider)
		if err != nil {
			return b, errors.New("brain.provider: unknown provider")
		}
		if k, ok := providerKind(p.Kind); !ok || !k.Main && p.Kind != "codex" && p.Kind != "claude" && p.Kind != "antigravity" {
			return b, errors.New("brain.provider: this provider cannot think for an agent")
		}
	}

	if b.Account != "" {
		switch {
		case isClaudeProvider(b.Provider):
			if b.Account != claudeBrain {
				ap, err := s.store.GetProvider(ctx, b.Account)
				if err != nil || ap.Kind != "claude" {
					return b, errors.New("brain.account: unknown Claude account")
				}
			}
		case isCodexProvider(b.Provider):
			if !strings.HasPrefix(b.Account, codexPrincipalPrefix) {
				ap, err := s.store.GetProvider(ctx, b.Account)
				if err != nil || ap.Kind != "codex" {
					return b, errors.New("brain.account: unknown Codex account")
				}
			}
		case isAgyProvider(b.Provider):
			ap, err := s.store.GetProvider(ctx, b.Account)
			if err != nil || ap.Kind != "antigravity" {
				return b, errors.New("brain.account: unknown Antigravity account")
			}
		default:
			b.Account = ""
		}
	}

	if b.Context > 0 {
		if maxWin := s.maxBrainContext(ctx, b); maxWin > 0 && b.Context > maxWin {
			b.Context = maxWin
		}
	}
	return b.Clone(), nil
}

// BrainAccount describes one selectable subscription/pool account inside a brain.
type BrainAccount struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// BrainOption is one brain the agent editor and ModelPicker offer.
type BrainOption struct {
	Provider     string                     `json:"provider"`
	Name         string                     `json:"name"`
	Kind         string                     `json:"kind"`
	Models       []string                   `json:"models"`
	ModelNames   map[string]string          `json:"model_names,omitempty"`
	ModelMeta    map[string]store.ModelMeta `json:"model_meta,omitempty"`
	MaxContext   int                        `json:"max_context,omitempty"`
	ModelContext map[string]int             `json:"model_context,omitempty"`
	// Efforts it takes; empty = it does not take one.
	Efforts  []string       `json:"efforts"`
	Speeds   []string       `json:"speeds,omitempty"`
	Accounts []BrainAccount `json:"accounts,omitempty"`
	// Available says it can answer now; Reason says why not in one sentence.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	// Ready and Note mirror Available and Reason for compatibility.
	Ready bool   `json:"ready"`
	Note  string `json:"note,omitempty"`
}

func (o *BrainOption) setStatus(avail bool, reason string) {
	if avail {
		reason = ""
	}
	o.Available, o.Ready = avail, avail
	o.Reason, o.Note = reason, reason
}

func (s *Server) cliSubscriptionStatus(ctx context.Context, progName, nodeID, engineName, acc, status, lastErr string) (bool, string) {
	nodeName := nodeID
	online := false
	if nodeID != "" {
		if nd, err := s.nodeByID(ctx, nodeID); err == nil {
			nodeName = firstNonEmpty(nd.Name, nd.ID)
			online = nd.RevokedAt == nil && s.online(nd)
		}
	}
	if !online {
		if nodeName != "" {
			return false, "O computador " + nodeName + " está offline agora."
		}
		return false, "Nenhum computador com o " + progName + " está online agora."
	}
	if reason := cliErrorReason(progName, nodeName, lastErr); reason != "" {
		return false, reason
	}
	if q, ok := s.quotaEntry(nodeID, engineName, acc); ok {
		if reason := cliErrorReason(progName, nodeName, q.Error); reason != "" {
			return false, reason
		}
		if q.Limited {
			return false, s.quotaLimitReason(nodeID, engineName, acc)
		}
	}
	if status != "" && status != "ok" {
		if status == "login" {
			return false, "A conta do " + progName + " ainda está aguardando login."
		}
		if lastErr != "" {
			return false, ensureSentence(lastErr)
		}
		return false, "A conta do " + progName + " precisa entrar de novo."
	}
	return true, ""
}

func apiProviderReason(lastErr string) string {
	low := strings.ToLower(strings.TrimSpace(lastErr))
	switch {
	case strings.Contains(low, "chave") || strings.Contains(low, "401") || strings.Contains(low, "403") ||
		strings.Contains(low, "invalid") || strings.Contains(low, "unauthorized") || strings.Contains(low, "api key"):
		return "Chave inválida ou recusada pelo provedor."
	case strings.Contains(low, "offline") || strings.Contains(low, "escutando") || strings.Contains(low, "connection refused"):
		return "O servidor desse provedor está offline agora."
	case strings.Contains(low, "versão") || strings.Contains(low, "version"):
		return "A versão do servidor desse provedor está velha."
	case strings.Contains(low, "429") || strings.Contains(low, "quota") || strings.Contains(low, "limite"):
		return "Limite atingido nesse provedor; espere renovar."
	case lastErr != "":
		return ensureSentence(lastErr)
	default:
		return "O provedor não respondeu no último teste."
	}
}

func ensureSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] = r[0] - 'a' + 'A'
	}
	out := string(r)
	if !strings.HasSuffix(out, ".") && !strings.HasSuffix(out, "!") && !strings.HasSuffix(out, "?") {
		out += "."
	}
	return out
}

// listBrains: GET /v1/brains, what an agent can think with.
func (s *Server) listBrains(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	list, err := s.store.ListProviders(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	views := []ProviderView{}
	s.models.mu.Lock()
	if s.models.builtin != nil {
		views = append(views, ProviderView{Builtin: true, Provider: store.Provider{ID: builtinProviderID, Kind: s.models.builtin.Name,
			Name: builtinProviderName, Status: "ok", Models: append([]string{}, s.models.builtinModels...)}})
	}
	active := s.models.active
	s.models.mu.Unlock()
	for _, p := range list {
		views = append(views, ProviderView{Provider: p})
	}
	views = s.codexViews(ctx, views, active)
	views = s.agyViews(ctx, views)

	// Build account rosters per CLI family so the UI can let an agent pick a specific account.
	var claudeAccounts, codexAccounts, agyAccountsList []BrainAccount
	claudeRoute, claudeRouteErr := s.routeEngine(ctx, "claude")
	claudeAvail, claudeReason := false, "Nenhum computador com o Claude Code está online agora."
	if claudeRouteErr == nil {
		claudeAvail, claudeReason = s.cliSubscriptionStatus(ctx, "Claude Code", claudeRoute.node, "claude", "", "ok", "")
		claudeAccounts = append(claudeAccounts, BrainAccount{
			ID: claudeBrain, Name: "Conta principal", Available: claudeAvail, Reason: claudeReason,
		})
	}
	for _, v := range views {
		switch {
		case v.Kind == "claude":
			av, rs := s.cliSubscriptionStatus(ctx, "Claude Code", v.NodeID, "claude", v.ID, v.Status, v.LastError)
			claudeAccounts = append(claudeAccounts, BrainAccount{ID: v.ID, Name: v.Name, Available: av, Reason: rs})
		case v.Kind == "codex":
			acc := v.ID
			if strings.HasPrefix(v.ID, codexPrincipalPrefix) {
				acc = ""
			}
			av, rs := s.cliSubscriptionStatus(ctx, "Codex", v.NodeID, "codex", acc, v.Status, v.LastError)
			codexAccounts = append(codexAccounts, BrainAccount{ID: v.ID, Name: v.Name, Available: av, Reason: rs})
		case v.Kind == "antigravity" && v.ID != agyBrain:
			acc := agyQuotaAccount(v.Provider)
			av, rs := s.cliSubscriptionStatus(ctx, "Antigravity CLI (agy)", v.NodeID, "antigravity", acc, v.Status, v.LastError)
			if av {
				if _, lim := s.agyLoad(v.Provider); lim {
					av, rs = false, s.quotaLimitReason(v.NodeID, "antigravity", acc)
				}
			}
			agyAccountsList = append(agyAccountsList, BrainAccount{ID: v.ID, Name: v.Name, Available: av, Reason: rs})
		}
	}

	out := []BrainOption{}
	claudeOpt := BrainOption{
		Provider:   claudeBrain,
		Name:       "Claude (sua assinatura)",
		Kind:       "claude",
		Models:     ids(claudeModels),
		ModelNames: names(claudeModels),
		MaxContext: 1000000,
		ModelContext: map[string]int{
			"opus":   1000000,
			"sonnet": 1000000,
			"haiku":  200000,
		},
		Efforts:  brainEfforts,
		Speeds:   brainSpeeds,
		Accounts: claudeAccounts,
	}
	claudeOpt.setStatus(claudeAvail, claudeReason)
	out = append(out, claudeOpt)

	for _, v := range views {
		o := BrainOption{
			Provider:   v.ID,
			Name:       v.Name,
			Kind:       v.Kind,
			Models:     v.Models,
			ModelNames: v.ModelNames,
			ModelMeta:  v.ModelMeta,
			MaxContext: modelMaxContext(v.ID, v.Kind, ""),
			Efforts:    []string{},
		}
		if o.Models == nil {
			o.Models = []string{}
		}
		if len(o.Models) > 0 {
			mc := make(map[string]int, len(o.Models))
			maxC := 0
			for _, m := range o.Models {
				c := modelMaxContext(v.ID, v.Kind, m)
				if mm, ok := v.ModelMeta[m]; ok && mm.Context > 0 {
					c = mm.Context
				}
				mc[m] = c
				if c > maxC {
					maxC = c
				}
			}
			o.ModelContext = mc
			if maxC > 0 {
				o.MaxContext = maxC
			}
		}
		if providerSupportsSpeed(v.Kind, v.ID) {
			o.Speeds = brainSpeeds
		}

		switch v.Kind {
		case "codex":
			o.Efforts = brainEfforts
			o.Accounts = codexAccounts
			acc := v.ID
			if strings.HasPrefix(v.ID, codexPrincipalPrefix) {
				acc = ""
			}
			av, rs := s.cliSubscriptionStatus(ctx, "Codex", v.NodeID, "codex", acc, v.Status, v.LastError)
			o.setStatus(av && v.CodexMain, firstNonEmpty(rs, ensureSentence(v.LastError), "A conta do Codex não está pronta agora."))
		case "antigravity":
			o.Efforts = brainEfforts
			if v.ID == agyBrain {
				o.Name = "Antigravity (todas as contas)"
				o.Accounts = agyAccountsList
				if v.CodexMain {
					o.setStatus(true, "")
				} else {
					reason := ensureSentence(firstNonEmpty(v.LastError, "Nenhuma conta do Antigravity está disponível agora."))
					for _, acc := range agyAccountsList {
						if strings.Contains(acc.Reason, "instalado") || strings.Contains(acc.Reason, "versão") {
							reason = acc.Reason
							break
						}
					}
					o.setStatus(false, reason)
				}
			} else {
				acc := agyQuotaAccount(v.Provider)
				av, rs := s.cliSubscriptionStatus(ctx, "Antigravity CLI (agy)", v.NodeID, "antigravity", acc, v.Status, v.LastError)
				if av {
					if _, lim := s.agyLoad(v.Provider); lim {
						av, rs = false, s.quotaLimitReason(v.NodeID, "antigravity", acc)
					}
				}
				o.setStatus(av && v.CodexMain, firstNonEmpty(rs, ensureSentence(v.LastError), "A conta do Antigravity não está pronta agora."))
			}
		case "claude":
			o.Models, o.ModelNames, o.Efforts = ids(claudeModels), names(claudeModels), brainEfforts
			o.ModelContext = map[string]int{"opus": 1000000, "sonnet": 1000000, "haiku": 200000}
			o.MaxContext = 1000000
			o.Accounts = claudeAccounts
			av, rs := s.cliSubscriptionStatus(ctx, "Claude Code", v.NodeID, "claude", v.ID, v.Status, v.LastError)
			o.setStatus(av, firstNonEmpty(rs, "A conta do Claude não está pronta agora."))
		default:
			k, ok := providerKind(v.Kind)
			if v.ID != builtinProviderID && (!ok || !k.Main) {
				continue
			}
			if v.Status == "ok" {
				o.setStatus(true, "")
			} else {
				o.setStatus(false, apiProviderReason(v.LastError))
			}
		}
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, out)
}

func defaultBrainSettingKey(wsID string) string {
	if wsID == "" || wsID == store.DefaultWorkspaceID {
		return "default_brain"
	}
	return "default_brain:" + wsID
}

func (s *Server) defaultBrain(ctx context.Context, wsID string) store.AgentBrain {
	raw, err := s.store.GetSetting(ctx, defaultBrainSettingKey(wsID))
	if err != nil || len(raw) == 0 {
		return store.AgentBrain{}
	}
	var b store.AgentBrain
	if json.Unmarshal(raw, &b) != nil {
		return store.AgentBrain{}
	}
	if valid, verr := s.validBrain(ctx, b); verr == nil {
		return valid
	}
	return store.AgentBrain{}
}

// getDefaultBrain: GET /v1/brain/default
func (s *Server) getDefaultBrain(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	writeJSON(w, http.StatusOK, s.defaultBrain(ctx, wsID))
}

// setDefaultBrain: PUT /v1/brain/default
func (s *Server) setDefaultBrain(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	var b store.AgentBrain
	if !readJSON(w, r, &b) {
		return
	}
	clean, err := s.validBrain(ctx, b)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	raw, _ := json.Marshal(clean)
	if err := s.store.SetSetting(ctx, defaultBrainSettingKey(wsID), raw, actorFrom(r), s.now()); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clean)
}

type copyBrainRequest struct {
	To []string `json:"to"`
}

// copyAgentBrain: POST /v1/agents/{id}/brain/copy {to:[ids]}
func (s *Server) copyAgentBrain(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	srcID := r.PathValue("id")
	src, err := s.store.GetAgent(ctx, srcID)
	if err != nil {
		storeError(w, err)
		return
	}
	if src.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	var req copyBrainRequest
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.To) == 0 {
		http.Error(w, "informe ao menos um agente em to", http.StatusBadRequest)
		return
	}
	seen := map[string]bool{}
	targets := make([]store.Agent, 0, len(req.To))
	for _, rawID := range req.To {
		tid := strings.TrimSpace(rawID)
		if tid == "" || tid == src.ID || seen[tid] {
			continue
		}
		seen[tid] = true
		dst, err := s.store.GetAgent(ctx, tid)
		if err != nil {
			storeError(w, err)
			return
		}
		if dst.WorkspaceID != wsID {
			storeError(w, store.ErrNotFound)
			return
		}
		targets = append(targets, dst)
	}
	actor := actorFrom(r)
	now := s.now()
	updated := make([]store.Agent, 0, len(targets))
	idsCopied := make([]string, 0, len(targets))
	for _, dst := range targets {
		dst.Brain = src.Brain.Clone()
		saved, err := s.store.SaveAgent(ctx, dst, actor, now)
		if err != nil {
			storeError(w, err)
			return
		}
		updated = append(updated, saved)
		idsCopied = append(idsCopied, saved.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"copied": len(updated),
		"to":     idsCopied,
		"brain":  src.Brain,
		"agents": updated,
	})
}

func ids(list []codexModel) []string {
	out := make([]string, len(list))
	for i, m := range list {
		out[i] = m.ID
	}
	return out
}

func names(list []codexModel) map[string]string {
	out := map[string]string{}
	for _, m := range list {
		out[m.ID] = m.Name
	}
	return out
}
