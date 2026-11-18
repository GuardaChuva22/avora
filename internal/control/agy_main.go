package control

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
)

// The Antigravity pool (D61). The owner has many Google AI accounts; each
// is a provider of kind antigravity on the PC (it signs in with its own
// Google login, kept by the PC's runtime), and together they are one
// provider: "antigravity". A turn, a delegation or a quick question on the
// pool runs on the account that is freest now: not out of quota, running
// the fewest jobs, with the most of its 5-hour and weekly limits left. When
// an account runs out mid-turn, the turn moves to the next one.
//
// The pool can be the main model and any agent's brain, like a Codex
// account: agy runs the whole turn on the PC and acts only through Avora's
// tools (the bridge), its own shell and edits off.

// agyBrain is the provider id of the whole pool.
const agyBrain = "antigravity"

// agyPCAccount marks the account added before the pool: the PC's own agy
// login (D44), checked rather than signed in.
const agyPCAccount = "conta do PC"

// agyModelsTTL is how long the model list fetched from `agy models` stays cached.
const agyModelsTTL = time.Hour

func isAgyProvider(id string) bool { return id == agyBrain || strings.HasPrefix(id, "antigravity.") }

func isAgyPCAccount(p store.Provider) bool { return strings.HasPrefix(p.Account, agyPCAccount) }

func agyQuotaAccount(p store.Provider) string {
	if isAgyPCAccount(p) {
		return ""
	}
	return p.ID
}

// agyEngine answers whole turns with agy on the PC.
type agyEngine struct {
	provider string // agyBrain or one account's id
	account  string // optional preferred/pinned pool account id
	rotate   bool   // true = rotate to the next pool account on quota
	speed    string
	context  int
	model    string
	effort   string
}

func (e *agyEngine) Name() string     { return "antigravity" }
func (e *agyEngine) Model() string    { return firstNonEmpty(e.model, "antigravity") }
func (e *agyEngine) Provider() string { return "antigravity:" + e.provider }

func (e *agyEngine) Reply(context.Context, string, []store.ChatMessage, func(llm.Delta)) (llm.Response, error) {
	return llm.Response{}, errors.New("o Antigravity responde só pelos turnos do chat")
}

// wholeTurns says the engine has no quick call: it runs whole turns on a
// node (Codex, the Antigravity pool), so the router decides by rules and
// conversations compact early (D52, D53).
func wholeTurns(eng ChatEngine) bool {
	switch eng.(type) {
	case *codexEngine, *agyEngine:
		return true
	}
	return false
}

// agyBusy counts the jobs each account runs now, to spread new ones, and
// remembers which ran out of quota mid-run (until agyOutFor passes: the
// node reads each account's quota every 5 minutes).
type agyBusy struct {
	mu  sync.Mutex
	n   map[string]int
	out map[string]time.Time
}

const agyOutFor = 10 * time.Minute

func (b *agyBusy) markOut(id string, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.out == nil {
		b.out = map[string]time.Time{}
	}
	b.out[id] = now
}

func (b *agyBusy) isOut(id string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	at, ok := b.out[id]
	return ok && now.Sub(at) < agyOutFor
}

func (b *agyBusy) take(id string) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.n == nil {
		b.n = map[string]int{}
	}
	b.n[id]++
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.n[id]--; b.n[id] <= 0 {
				delete(b.n, id)
			}
		})
	}
}

func (b *agyBusy) count(id string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.n[id]
}

// agyLoad is how used an account is now, 0 to 100 (the fuller of its two
// windows), and whether it is out.
func (s *Server) agyLoad(p store.Provider) (int, bool) {
	if s.agyJobs.isOut(p.ID, s.now()) {
		return 100, true
	}
	acc := p.ID
	if isAgyPCAccount(p) {
		acc = "" // the PC's own login reports as the node's
	}
	for _, q := range s.quotaBook.get(p.NodeID) {
		if q.Engine != "antigravity" || q.Account != acc || q.Error != "" {
			continue
		}
		used := 0
		for _, w := range []*QuotaWindow{q.FiveHour, q.Weekly} {
			if w != nil && int(w.UsedPercent) > used {
				used = int(w.UsedPercent)
			}
		}
		return used, q.Limited
	}
	return 0, false
}

// agyAccounts lists the pool's accounts that can run now, freest first;
// out-of-quota ones come last.
func (s *Server) agyAccounts(ctx context.Context) ([]store.Provider, error) {
	list, err := s.store.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	nodes, _ := s.store.ListNodes(ctx)
	online := map[string]bool{}
	for _, n := range nodes {
		online[n.ID] = n.RevokedAt == nil && s.online(n)
	}
	type cand struct {
		p       store.Provider
		busy    int
		used    int
		limited bool
	}
	var cands []cand
	for _, p := range list {
		if p.Kind != "antigravity" || p.Status != "ok" || !online[p.NodeID] {
			continue
		}
		used, limited := s.agyLoad(p)
		cands = append(cands, cand{p: p, busy: s.agyJobs.count(p.ID), used: used, limited: limited})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.limited != b.limited {
			return !a.limited
		}
		if a.busy != b.busy {
			return a.busy < b.busy
		}
		return a.used < b.used
	})
	out := make([]store.Provider, len(cands))
	for i, c := range cands {
		out[i] = c.p
	}
	return out, nil
}

func (s *Server) agyLimitText(p store.Provider) string {
	if rc := s.quotaResetClause(p.NodeID, "antigravity", agyQuotaAccount(p)); rc != "" {
		return "a conta " + p.Name + " chegou no limite (" + rc + ")"
	}
	return "a conta " + p.Name + " chegou no limite"
}

// agyTargets are the accounts a turn on provider may run on, in order: the
// pool's, freest first, or the one account picked.
func (s *Server) agyTargets(ctx context.Context, provider string) ([]store.Provider, error) {
	list, _, err := s.agyTargetsFor(ctx, "", provider, "", provider == agyBrain)
	return list, err
}

// agyTargetsFor resolves candidate Antigravity accounts for an agent's brain,
// honoring a chosen account and the rotate flag. rotatedNote is non-empty when
// the preferred account was already at its quota limit and another took over.
func (s *Server) agyTargetsFor(ctx context.Context, agentName, provider, account string, rotate bool) ([]store.Provider, string, error) {
	startID := provider
	if account != "" {
		startID = account
	}
	if startID != agyBrain {
		p, err := s.store.GetProvider(ctx, startID)
		if err != nil {
			return nil, "", err
		}
		if p.Kind != "antigravity" {
			return nil, "", errors.New("esse provedor não é uma conta do Antigravity")
		}
		if p.Status != "ok" {
			return nil, "", publicError{"a conta " + p.Name + " do Antigravity precisa entrar de novo (Configurações → Modelos)"}
		}
		n, err := s.nodeByID(ctx, p.NodeID)
		if err != nil {
			return nil, "", err
		}
		if n.RevokedAt != nil || !s.online(n) {
			return nil, "", publicError{"o Antigravity roda em " + n.Name + ", que está offline agora"}
		}
		_, limited := s.agyLoad(p)
		if !rotate {
			if limited {
				return nil, "", publicError{s.agyLimitText(p)}
			}
			return []store.Provider{p}, "", nil
		}
		pool, _ := s.agyAccounts(ctx)
		var others []store.Provider
		for _, cand := range pool {
			if cand.ID == p.ID {
				continue
			}
			if _, l := s.agyLoad(cand); !l {
				others = append(others, cand)
			}
		}
		if limited {
			if len(others) == 0 {
				return nil, "", publicError{s.agyLimitText(p)}
			}
			note := ""
			if agentName != "" {
				note = s.formatRotationLine(agentName, "Antigravity", p.Name, others[0].Name, p.NodeID, "antigravity", agyQuotaAccount(p))
			}
			return others, note, nil
		}
		return append([]store.Provider{p}, others...), "", nil
	}

	list, err := s.agyAccounts(ctx)
	if err != nil {
		return nil, "", err
	}
	if len(list) == 0 {
		return nil, "", publicError{"nenhuma conta do Antigravity pronta agora: o PC precisa estar ligado e com contas em Configurações → Modelos"}
	}
	var avail []store.Provider
	for _, cand := range list {
		if _, l := s.agyLoad(cand); !l {
			avail = append(avail, cand)
		}
	}
	if len(avail) == 0 {
		return nil, "", publicError{s.agyLimitText(list[0])}
	}
	if !rotate {
		return avail[:1], "", nil
	}
	return avail, "", nil
}

func (s *Server) cachedAgyModels(id string) ([]codexModel, bool) {
	now := s.now()
	s.models.mu.Lock()
	defer s.models.mu.Unlock()
	if c, ok := s.models.agy[id]; ok && len(c.models) > 0 {
		return append([]codexModel{}, c.models...), now.Sub(c.at) < agyModelsTTL
	}
	if id != agyBrain {
		if c, ok := s.models.agy[agyBrain]; ok && len(c.models) > 0 {
			return append([]codexModel{}, c.models...), now.Sub(c.at) < agyModelsTTL
		}
	}
	return nil, false
}

func (s *Server) agyModelsForProvider(p store.Provider) ([]string, map[string]string) {
	if models, _ := s.cachedAgyModels(p.ID); len(models) > 0 {
		return ids(models), names(models)
	}
	if len(p.Models) > 0 {
		return append([]string{}, p.Models...), nil
	}
	return []string{}, nil
}

func (s *Server) agyKnownModelIDs(ctx context.Context, provider string) []string {
	if models, _ := s.cachedAgyModels(provider); len(models) > 0 {
		return ids(models)
	}
	if provider != agyBrain {
		if p, err := s.store.GetProvider(ctx, provider); err == nil && len(p.Models) > 0 {
			return append([]string{}, p.Models...)
		}
	} else if list, err := s.store.ListProviders(ctx); err == nil {
		for _, p := range list {
			if p.Kind == "antigravity" && len(p.Models) > 0 {
				return append([]string{}, p.Models...)
			}
		}
	}
	return nil
}

// refreshAgy runs `agy models` on the account's (or pool's) Windows node and
// caches the returned model list for 1 hour.
func (s *Server) refreshAgy(ctx context.Context, id string) (ProviderView, error) {
	if models, fresh := s.cachedAgyModels(id); fresh && len(models) > 0 {
		return s.agyProviderView(ctx, id)
	}
	targets, err := s.agyTargets(ctx, id)
	if err != nil {
		return ProviderView{}, err
	}
	p := targets[0]
	n, err := s.nodeByID(ctx, p.NodeID)
	if err != nil {
		return ProviderView{}, err
	}
	acc := p.ID
	if isAgyPCAccount(p) {
		acc = ""
	}
	spec := &store.AgentSpec{Engine: "antigravity", Mode: store.CodexModelsMode, MaxMinutes: 2, Account: acc, Prompt: "listar modelos"}
	jctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	end, err := s.runNodeJobAs(jctx, nil, spec, n.Kind, n.ID, "modelos do Antigravity", store.ActorAdmin, "")
	if err != nil {
		return ProviderView{}, err
	}
	r := end.result
	switch {
	case r == nil:
		return ProviderView{}, publicError{"o PC não respondeu a tempo"}
	case !r.Success:
		return ProviderView{}, publicError{agyProblem(r.Error)}
	}
	var models []codexModel
	if err := json.Unmarshal([]byte(r.Output), &models); err != nil || len(models) == 0 {
		return ProviderView{}, publicError{"o agy não listou nenhum modelo"}
	}
	now := s.now()
	s.models.mu.Lock()
	if s.models.agy == nil {
		s.models.agy = map[string]agyCachedModelSet{}
	}
	entry := agyCachedModelSet{models: models, at: now}
	s.models.agy[id] = entry
	s.models.agy[p.ID] = entry
	s.models.agy[agyBrain] = entry
	s.models.mu.Unlock()

	modelIDs := ids(models)
	if list, lerr := s.store.ListProviders(ctx); lerr == nil {
		for _, item := range list {
			if item.Kind == "antigravity" && (id == agyBrain || item.ID == p.ID) {
				item.Models = modelIDs
				_, _ = s.store.SaveProvider(ctx, item, "", now)
			}
		}
	}
	return s.agyProviderView(ctx, id)
}

func (s *Server) agyProviderView(ctx context.Context, id string) (ProviderView, error) {
	if id == agyBrain {
		list, err := s.store.ListProviders(ctx)
		if err != nil {
			return ProviderView{}, err
		}
		views := make([]ProviderView, 0, len(list))
		for _, p := range list {
			views = append(views, ProviderView{Provider: p})
		}
		v, ok := s.agyPoolView(ctx, views)
		if !ok {
			return ProviderView{}, store.ErrNotFound
		}
		return v, nil
	}
	p, err := s.store.GetProvider(ctx, id)
	if err != nil {
		return ProviderView{}, err
	}
	n, _ := s.nodeByID(ctx, p.NodeID)
	v := ProviderView{Provider: p, CodexMain: p.Status == "ok" && n.RevokedAt == nil && s.online(n)}
	v.Models, v.ModelNames = s.agyModelsForProvider(p)
	return v, nil
}

// agyPoolView is the pool as one provider in Settings and the brain list.
func (s *Server) agyPoolView(ctx context.Context, list []ProviderView) (ProviderView, bool) {
	n, ready := 0, 0
	var sample store.Provider
	for _, v := range list {
		if v.Kind == "antigravity" {
			n++
			if len(sample.Models) == 0 && len(v.Models) > 0 {
				sample = v.Provider
			}
			if v.Status == "ok" {
				ready++
			}
		}
	}
	if n == 0 {
		return ProviderView{}, false
	}
	accounts, _ := s.agyAccounts(ctx)
	availCount := 0
	for _, acc := range accounts {
		if _, l := s.agyLoad(acc); !l {
			availCount++
		}
	}
	sample.ID = agyBrain
	mIDs, mNames := s.agyModelsForProvider(sample)
	v := ProviderView{Builtin: true, Provider: store.Provider{ID: agyBrain, Kind: "antigravity", Name: "Antigravity", Status: "ok",
		Models: mIDs}, ModelNames: mNames, CodexMain: availCount > 0}
	switch {
	case len(accounts) == 0 && ready == 0:
		v.Status, v.LastError = "error", "Nenhuma conta do Antigravity entrou ainda."
	case len(accounts) == 0:
		v.Status, v.LastError = "error", "O computador das contas está offline."
	case availCount == 0:
		v.Status, v.LastError = "error", s.quotaLimitReason(accounts[0].NodeID, "antigravity", agyQuotaAccount(accounts[0]))
	}
	v.Account = plural(len(accounts), "conta pronta", "contas prontas")
	return v, true
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// agyOutOfQuota says a failed run hit its account's limit, so the next
// account should take the turn.
func agyOutOfQuota(msg string) bool {
	low := strings.ToLower(msg)
	for _, k := range []string{"resource_exhausted", "resource exhausted", "quota", "rate limit", "limit reached", "exhausted", "429", "too many requests"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

// agyProblem turns a node's error into something the owner can act on.
func agyProblem(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "not installed") || strings.Contains(low, "não está instalado"):
		return "o Antigravity CLI (agy) não está instalado no PC"
	case agyOutOfQuota(msg):
		return "as contas do Antigravity atingiram o limite de uso; espere renovar ou troque o modelo"
	case strings.Contains(low, "authentication required") || strings.Contains(low, "not logged") || strings.Contains(low, "sign in") || strings.Contains(low, "login"):
		return "uma conta do Antigravity saiu do login; entre de novo em Configurações → Modelos"
	case strings.Contains(low, "time limit"):
		return "o Antigravity demorou demais para responder"
	case msg == "":
		return "o Antigravity falhou"
	default:
		return "o Antigravity falhou: " + truncate(msg, 240)
	}
}

// agyRun runs one main-lane job (a turn or a quick question) on the pool,
// moving to the next account when one runs out of quota.
func (s *Server) agyRun(ctx context.Context, t *turn, provider, account string, rotate bool, spec store.AgentSpec, reason, actor, prefix string) (*JobResult, error) {
	agentName := ""
	if t != nil {
		agentName = t.agent.Name
	}
	targets, rotatedNote, err := s.agyTargetsFor(ctx, agentName, provider, account, rotate)
	if err != nil {
		return nil, err
	}
	if rotatedNote != "" && t != nil {
		s.recordBrainSwitch(t, "rotate", rotatedNote)
	}
	var last *JobResult
	for i, p := range targets {
		if i >= 3 {
			break
		}
		if limited := func() bool { _, l := s.agyLoad(p); return l }(); limited && i > 0 {
			break // the rest are out too
		}
		n, err := s.nodeByID(ctx, p.NodeID)
		if err != nil {
			return nil, err
		}
		run := spec
		run.Account = p.ID
		if isAgyPCAccount(p) {
			run.Account = ""
		}
		done := s.agyJobs.take(p.ID)
		end, err := s.runNodeJobAs(ctx, t, &run, n.Kind, n.ID, reason+" ("+p.Name+")", actor, prefix)
		done()
		if err != nil {
			return nil, err
		}
		r := end.result
		switch {
		case r != nil && r.Success:
			return r, nil
		case r != nil && agyOutOfQuota(r.Error) && rotate && i+1 < len(targets):
			s.markAgyLimited(p)
			if t != nil {
				s.step(t, AgentStep{Tool: "Antigravity", Summary: p.Name + " sem cota; passando para a próxima conta"})
				line := s.formatRotationLine(t.agent.Name, "Antigravity", p.Name, targets[i+1].Name, p.NodeID, "antigravity", agyQuotaAccount(p))
				s.recordBrainSwitch(t, "rotate", line)
			}
			last = r
			continue
		case r != nil:
			return r, nil
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case end.job.Status == store.StatusTimedOut:
			return nil, publicError{"o Antigravity parou de responder"}
		default:
			return nil, publicError{"o Antigravity foi parado antes de terminar"}
		}
	}
	return last, nil
}

// markAgyLimited notes an account ran out, for a while.
func (s *Server) markAgyLimited(p store.Provider) { s.agyJobs.markOut(p.ID, s.now()) }

// runAgyTurn answers one turn with the Antigravity pool (or one account),
// like runCodexTurn: the whole conversation in one prompt, Avora's tools
// through the bridge.
func (s *Server) runAgyTurn(ctx context.Context, t *turn, e *agyEngine, system string, history []store.ChatMessage) (llm.Response, error) {
	minutes := TurnMinutes
	if minutes > MaxAgentMinutes {
		minutes = MaxAgentMinutes
	}
	key, closeBridge := s.bridges.openRun(bridgeRun{t: t, full: true}, time.Duration(minutes+5)*time.Minute, s.now())
	defer closeBridge()
	mode := store.CodexChatMode
	if t.parent != nil {
		mode = store.CodexConsultMode
	}
	spec := store.AgentSpec{Engine: "antigravity", Model: e.model, Mode: mode, MaxMinutes: minutes,
		Effort: agyEffort(e.effort), Speed: e.speed, Workdir: "agents/" + t.agent.ID, Bridge: key,
		Prompt: codexTurnPromptWithBudget(s.now().In(s.zone()), system, history, promptBytesForWindow(e.context))}
	r, err := s.agyRun(ctx, t, e.provider, e.account, e.rotate, spec, t.agent.Name+": resposta pelo Antigravity", "agent:"+t.agent.ID, "Antigravity · ")
	if err != nil {
		return llm.Response{}, err
	}
	if r == nil || !r.Success {
		msg := ""
		if r != nil {
			msg = r.Error
		}
		return llm.Response{}, publicError{agyProblem(msg)}
	}
	resp := llm.Response{Content: strings.TrimSpace(r.Output), Model: e.Model()}
	var stats struct {
		Input  int `json:"input_tokens"`
		Output int `json:"output_tokens"`
	}
	if json.Unmarshal(r.Stats, &stats) == nil {
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens = stats.Input, stats.Output
	}
	return resp, nil
}

// agyQuick answers a quick question (summary, overseer) on the pool, with
// no bridge and agy's read-only default.
func (s *Server) agyQuick(ctx context.Context, e *agyEngine, system, question, reason string) (string, error) {
	minutes := 5
	if d, ok := ctx.Deadline(); ok {
		minutes = max(1, int(time.Until(d).Minutes())+1)
	}
	prompt := "Siga as instruções abaixo e responda só o que elas pedem, sem usar ferramentas nem ler arquivos.\n\n<instrucoes>\n" +
		system + "\n</instrucoes>\n\n" + truncate(question, maxCodexPrompt-len(system)-512)
	spec := store.AgentSpec{Engine: "antigravity", Model: e.model, Mode: store.CodexConsultMode, MaxMinutes: minutes,
		Workdir: "avora-quick", Prompt: prompt}
	r, err := s.agyRun(ctx, nil, e.provider, e.account, true, spec, reason, store.ActorSystem, "")
	if err != nil {
		return "", err
	}
	if r == nil || !r.Success {
		msg := ""
		if r != nil {
			msg = r.Error
		}
		return "", publicError{agyProblem(msg)}
	}
	return strings.TrimSpace(r.Output), nil
}

// agyEffort is agy's name of an effort.
func agyEffort(e string) string {
	switch e {
	case "low", "medium", "high", "max":
		return e
	}
	return ""
}

// agyRoute is where a delegation to the pool goes: the freest account and
// its node; ok is false when the pool has no account (the PC's own agy).
func (s *Server) agyRoute(ctx context.Context) (store.Provider, agentRoute, bool) {
	list, err := s.agyAccounts(ctx)
	if err != nil || len(list) == 0 {
		return store.Provider{}, agentRoute{}, false
	}
	p := list[0]
	return p, agentRoute{node: p.NodeID, target: scheduler.Windows, reason: "conta " + p.Name}, true
}

// agyViews puts the pool first among the Antigravity accounts in a
// provider list and gives each account the models it can answer with.
func (s *Server) agyViews(ctx context.Context, list []ProviderView) []ProviderView {
	pool, ok := s.agyPoolView(ctx, list)
	if !ok {
		return list
	}
	nodes, _ := s.store.ListNodes(ctx)
	online := map[string]bool{}
	for _, n := range nodes {
		online[n.ID] = n.RevokedAt == nil && s.online(n)
	}
	out := make([]ProviderView, 0, len(list)+1)
	added := false
	for _, v := range list {
		if v.Kind == "antigravity" {
			if !added {
				out, added = append(out, pool), true
			}
			v.Models, v.ModelNames = s.agyModelsForProvider(v.Provider)
			_, limited := s.agyLoad(v.Provider)
			v.CodexMain = v.Status == "ok" && online[v.NodeID] && !limited
		}
		out = append(out, v)
	}
	return out
}
