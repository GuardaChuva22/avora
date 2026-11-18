package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/browser"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// The agents' browser (D29): each agent has its own browser session and queue
// inside the Avora app (isolated WebEngineProfile per agent) or on a paired PC
// runtime. Commands travel by long poll; while a person takes control of an
// agent's browser, that agent's queued commands wait and new tool calls tell
// the agent that the person is using the browser.

// BrowserCall is one browser command for a node.
type BrowserCall struct {
	ID        string          `json:"id"`
	Command   browser.Command `json:"command"`
	Agent     string          `json:"agent,omitempty"`      // agent ID that asked
	AgentName string          `json:"agent_name,omitempty"` // display name for the app banner
	ownerCmd  bool
}

// BrowserAnswer is a node's answer to a BrowserCall.
type BrowserAnswer struct {
	ID     string         `json:"id"`
	Agent  string         `json:"agent,omitempty"`
	Result browser.Result `json:"result"`
}

// AppBrowser is the hub key of the app's embedded browser.
const AppBrowser = "app"

const (
	browserPoll  = 25 * time.Second
	appPoll      = 15 * time.Second // the app's requests time out at 20 s
	browserLease = 3 * time.Minute
	browserLog   = 40
)

type browserLine struct {
	At    time.Time `json:"at"`
	Agent string    `json:"agent"`
	Text  string    `json:"text"`
}

type browserSlot struct {
	polled   time.Time
	shot     []byte
	shotAt   time.Time
	url      string
	title    string
	agent    string // agent ID that holds or last used this slot
	usedAt   time.Time
	owner    bool // a person took over this agent's browser
	log      []browserLine
	elements []browser.Element
	queue    []BrowserCall
}

type browserPending struct {
	agent string
	done  chan browser.Result
}

type browserNode struct {
	anyPolled      time.Time
	wildcardPolled time.Time
	owner          bool // node-wide takeover when no agent is specified
	legacyOwner    bool // taken via legacy {"owner": true} without agent/take
	log            []browserLine
	slots          map[string]*browserSlot
	waiting        map[string]browserPending
	notify         chan struct{}
}

type browserHub struct {
	mu    sync.Mutex
	nodes map[string]*browserNode
}

func (h *browserHub) node(id string) *browserNode {
	if h.nodes == nil {
		h.nodes = map[string]*browserNode{}
	}
	n := h.nodes[id]
	if n == nil {
		n = &browserNode{
			slots:   map[string]*browserSlot{},
			waiting: map[string]browserPending{},
			notify:  make(chan struct{}),
		}
		h.nodes[id] = n
	}
	return n
}

func (n *browserNode) slot(agentID string) *browserSlot {
	if n.slots == nil {
		n.slots = map[string]*browserSlot{}
	}
	sl := n.slots[agentID]
	if sl == nil {
		sl = &browserSlot{}
		n.slots[agentID] = sl
	}
	return sl
}

func (n *browserNode) wake() {
	if n.notify != nil {
		close(n.notify)
	}
	n.notify = make(chan struct{})
}

func (n *browserNode) ready(now time.Time) bool {
	return now.Sub(n.anyPolled) < browserPoll+10*time.Second || len(n.waiting) > 0
}

func (n *browserNode) readyFor(agentID string, now time.Time) bool {
	if agentID == "" {
		return n.ready(now)
	}
	if sl := n.slots[agentID]; sl != nil && now.Sub(sl.polled) < browserPoll+10*time.Second {
		return true
	}
	if now.Sub(n.wildcardPolled) < browserPoll+10*time.Second {
		return true
	}
	for _, p := range n.waiting {
		if p.agent == agentID {
			return true
		}
	}
	return false
}

func (sl *browserSlot) addLog(now time.Time, agent, text string) {
	sl.log = append(sl.log, browserLine{At: now, Agent: agent, Text: text})
	if len(sl.log) > browserLog {
		sl.log = sl.log[len(sl.log)-browserLog:]
	}
}

func (n *browserNode) addLog(now time.Time, agent, text string) {
	n.log = append(n.log, browserLine{At: now, Agent: agent, Text: text})
	if len(n.log) > browserLog {
		n.log = n.log[len(n.log)-browserLog:]
	}
}

func (n *browserNode) popSlot(sl *browserSlot) (BrowserCall, bool) {
	if sl == nil {
		return BrowserCall{}, false
	}
	for i, call := range sl.queue {
		if call.ownerCmd || (!sl.owner && !n.owner) {
			sl.queue = append(sl.queue[:i], sl.queue[i+1:]...)
			return call, true
		}
	}
	return BrowserCall{}, false
}

// takeNext pops the next runnable call for a poller. A wildcard poller (empty
// wantAgent) serves unagented calls and any agent slot without its own active
// dedicated poller.
func (n *browserNode) takeNext(wantAgent string, now time.Time) (BrowserCall, bool) {
	if wantAgent != "" {
		return n.popSlot(n.slots[wantAgent])
	}
	if call, ok := n.popSlot(n.slots[""]); ok {
		return call, true
	}
	keys := make([]string, 0, len(n.slots))
	for k := range n.slots {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		sl := n.slots[k]
		if now.Sub(sl.polled) < appPoll+10*time.Second {
			continue
		}
		if call, ok := n.popSlot(sl); ok {
			return call, true
		}
	}
	return BrowserCall{}, false
}

func (n *browserNode) latestSlot() *browserSlot {
	var best *browserSlot
	for _, sl := range n.slots {
		if sl == nil {
			continue
		}
		if best == nil || sl.usedAt.After(best.usedAt) || (sl.usedAt.Equal(best.usedAt) && sl.shotAt.After(best.shotAt)) {
			best = sl
		}
	}
	return best
}

func (n *browserNode) latestShot() ([]byte, time.Time) {
	var shot []byte
	var at time.Time
	for _, sl := range n.slots {
		if sl != nil && len(sl.shot) > 0 && (shot == nil || sl.shotAt.After(at)) {
			shot, at = sl.shot, sl.shotAt
		}
	}
	return shot, at
}

// browserNodeID is the node whose browser the agents use: the app first.
func (s *Server) browserNodeID(ctx context.Context) string {
	return s.browserNodeIDFor(ctx, "")
}

func (s *Server) browserNodeIDFor(ctx context.Context, agentID string) string {
	now := time.Now()
	s.browsers.mu.Lock()
	ready := map[string]bool{}
	for id, n := range s.browsers.nodes {
		if n.readyFor(agentID, now) {
			ready[id] = true
		}
	}
	s.browsers.mu.Unlock()
	if len(ready) == 0 {
		return ""
	}
	if ready[AppBrowser] {
		return AppBrowser // the app's panel, where the person sees it
	}
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return ""
	}
	best := ""
	for _, n := range nodes {
		if !ready[n.ID] || n.RevokedAt != nil {
			continue
		}
		if best == "" || n.Kind == "windows" {
			best = n.ID
		}
	}
	return best
}

// browse runs one command on a node's browser for agentID and waits for the answer.
func (s *Server) browse(ctx context.Context, nodeID, agentID, agentName string, cmd browser.Command, ownerCmd bool) (browser.Result, error) {
	call := BrowserCall{ID: newID()[:16], Command: cmd, Agent: agentID, AgentName: agentName, ownerCmd: ownerCmd}
	done := make(chan browser.Result, 1)
	s.browsers.mu.Lock()
	n := s.browsers.node(nodeID)
	sl := n.slot(agentID)
	if len(sl.queue) >= 8 {
		s.browsers.mu.Unlock()
		return browser.Result{}, errors.New("o navegador está ocupado; tente de novo em instantes")
	}
	sl.queue = append(sl.queue, call)
	n.waiting[call.ID] = browserPending{agent: agentID, done: done}
	n.wake()
	s.browsers.mu.Unlock()
	defer func() {
		s.browsers.mu.Lock()
		delete(n.waiting, call.ID)
		for i, q := range sl.queue {
			if q.ID == call.ID {
				sl.queue = append(sl.queue[:i], sl.queue[i+1:]...)
				break
			}
		}
		s.browsers.mu.Unlock()
	}()
	timer := time.NewTimer(3 * time.Minute)
	defer timer.Stop()
	select {
	case r := <-done:
		return r, nil
	case <-timer.C:
		return browser.Result{}, errors.New("o navegador não respondeu a tempo")
	case <-ctx.Done():
		return browser.Result{}, ctx.Err()
	}
}

func (s *Server) browserNext(w http.ResponseWriter, r *http.Request, node store.Node) {
	s.nextBrowserCall(w, r, node.ID, browserPoll)
}

func (s *Server) appBrowserNext(w http.ResponseWriter, r *http.Request) {
	s.nextBrowserCall(w, r, AppBrowser, appPoll)
}

func (s *Server) appBrowserResult(w http.ResponseWriter, r *http.Request) {
	s.browserAnswer(w, r, AppBrowser)
}

func (s *Server) nextBrowserCall(w http.ResponseWriter, r *http.Request, id string, wait time.Duration) {
	var body struct {
		Agent string `json:"agent"`
	}
	if r.Body != nil {
		var raw json.RawMessage
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&raw); err == nil && len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
	}
	wantAgent := strings.TrimSpace(body.Agent)

	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(wait + 10*time.Second))
	timer := time.NewTimer(wait)
	defer timer.Stop()

	for {
		now := time.Now()
		s.browsers.mu.Lock()
		n := s.browsers.node(id)
		n.anyPolled = now
		if wantAgent == "" {
			n.wildcardPolled = now
		} else {
			n.slot(wantAgent).polled = now
		}
		if call, ok := n.takeNext(wantAgent, now); ok {
			s.browsers.mu.Unlock()
			writeJSON(w, http.StatusOK, call)
			return
		}
		ch := n.notify
		s.browsers.mu.Unlock()

		select {
		case <-ch:
		case <-timer.C:
			s.touchPolled(id, wantAgent)
			w.WriteHeader(http.StatusNoContent)
			return
		case <-r.Context().Done():
			s.touchPolled(id, wantAgent)
			return
		}
	}
}

func (s *Server) touchPolled(nodeID, wantAgent string) {
	now := time.Now()
	s.browsers.mu.Lock()
	n := s.browsers.node(nodeID)
	n.anyPolled = now
	if wantAgent == "" {
		n.wildcardPolled = now
	} else {
		n.slot(wantAgent).polled = now
	}
	s.browsers.mu.Unlock()
}

func (s *Server) browserResult(w http.ResponseWriter, r *http.Request, node store.Node) {
	s.browserAnswer(w, r, node.ID)
}

func (s *Server) browserAnswer(w http.ResponseWriter, r *http.Request, id string) {
	var a BrowserAnswer
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(time.Minute))
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&a); err != nil {
		http.Error(w, "bad answer", http.StatusBadRequest)
		return
	}
	now := time.Now()
	s.browsers.mu.Lock()
	n := s.browsers.node(id)
	p, hasPending := n.waiting[a.ID]
	agentID := strings.TrimSpace(a.Agent)
	if agentID == "" && hasPending {
		agentID = p.agent
	}
	sl := n.slot(agentID)
	if len(a.Result.Shot) > 0 {
		sl.shot, sl.shotAt = a.Result.Shot, now
	}
	if a.Result.URL != "" {
		sl.url, sl.title = a.Result.URL, a.Result.Title
	}
	if a.Result.Elements != nil {
		sl.elements = a.Result.Elements
	}
	s.browsers.mu.Unlock()
	if hasPending && p.done != nil {
		select {
		case p.done <- a.Result:
		default:
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── The agents' tool ────────────────────────────────────────────

const browserToolDesc = "Um navegador de verdade (Chromium) no painel ao lado da conversa no app, com perfil e sessão próprios deste agente. Use para o que a busca não alcança: Marketplace, grupos e perfis, anúncios, páginas com login ou que carregam ao rolar. abrir e ler devolvem a página como texto com os elementos numerados ([12] link «...»); clicar e digitar usam esse número (ref). Leia de novo depois de mudar de página: os números mudam. Senha só a pessoa digita. Clicar em enviar, publicar, comprar, pagar ou apagar pede aprovação sozinho. print tira uma foto da tela para mostrar na resposta."

var browserParams = `{"type": "object", "properties": {
	"acao": {"type": "string", "enum": ["abrir", "ler", "clicar", "digitar", "rolar", "tecla", "voltar", "abas", "print"], "description": "abrir: ir a um endereço; ler: ler a página atual (parte 2, 3... nas longas); clicar: clicar no elemento ref; digitar: escrever texto no campo ref (enviar=true aperta Enter); rolar: rolar a página (carrega mais itens em feeds); tecla: apertar uma tecla; voltar: página anterior; abas: listar abas ou trocar (aba=N); print: foto da tela para mostrar ao dono."},
	"url": {"type": "string", "description": "abrir: o endereço, ex.: https://www.facebook.com/marketplace/saosebastiaodoparaiso/search?query=casa"},
	"ref": {"type": "integer", "description": "clicar/digitar: o número do elemento na última leitura."},
	"texto": {"type": "string", "description": "digitar: o texto; tecla: enter, esc, tab, pagedown, pageup, baixo, cima."},
	"enviar": {"type": "boolean", "description": "digitar: apertar Enter depois (buscar, mandar)."},
	"direcao": {"type": "string", "enum": ["baixo", "cima"], "description": "rolar: para onde (padrão baixo)."},
	"vezes": {"type": "integer", "description": "rolar: quantas telas (1 a 10, padrão 2)."},
	"parte": {"type": "integer", "description": "ler: a parte do texto das páginas longas."},
	"aba": {"type": "integer", "description": "abas: o número da aba para trocar; vazio lista."}
}, "required": ["acao"]}`

type browserArgs struct {
	Acao    string `json:"acao"`
	URL     string `json:"url"`
	Ref     int    `json:"ref"`
	Texto   string `json:"texto"`
	Enviar  bool   `json:"enviar"`
	Direcao string `json:"direcao"`
	Vezes   int    `json:"vezes"`
	Parte   int    `json:"parte"`
	Aba     int    `json:"aba"`
}

func parseBrowserArgs(raw string) browserArgs {
	var b browserArgs
	_ = json.Unmarshal([]byte(raw), &b)
	b.Acao = strings.ToLower(strings.TrimSpace(b.Acao))
	return b
}

// browserTools offers the browser while a node has one.
func (s *Server) browserTools() []llm.Tool {
	if s.browserNodeID(context.Background()) == "" {
		return nil
	}
	return []llm.Tool{tool("navegador", browserToolDesc, browserParams)}
}

func (s *Server) browserToolsFor(a store.Agent) []llm.Tool {
	if strings.TrimSpace(a.HomeNodeID) != "" {
		return []llm.Tool{tool("navegador", browserToolDesc, browserParams)}
	}
	return s.browserTools()
}

func host(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return strings.TrimPrefix(p.Host, "www.")
	}
	return u
}

// riskyLabel is a button that sends, posts, buys or deletes.
var riskyLabel = regexp.MustCompile(`(?i)\b(enviar|envie|send|publicar|publique|postar|post|compartilhar|share|comprar|compre|buy|pagar|pague|pay|checkout|finalizar|confirmar|confirm|assinar|subscribe|excluir|apagar|delete|remover|remove|seguir|follow|curtir|like|comentar|comment|responder|reply|fazer oferta|ofertar|reservar|candidatar|transferir|pix|doar|donate)\b`)

func (s *Server) browserElementFor(agentID string, ref int) (browser.Element, string, bool) {
	now := time.Now()
	s.browsers.mu.Lock()
	defer s.browsers.mu.Unlock()
	if agentID != "" {
		for _, n := range s.browsers.nodes {
			if sl := n.slots[agentID]; sl != nil && ref >= 1 && ref <= len(sl.elements) && n.readyFor(agentID, now) {
				return sl.elements[ref-1], sl.url, true
			}
		}
	}
	var best *browserSlot
	for _, n := range s.browsers.nodes {
		if !n.ready(now) {
			continue
		}
		for _, sl := range n.slots {
			if sl != nil && ref >= 1 && ref <= len(sl.elements) && (best == nil || sl.usedAt.After(best.usedAt)) {
				best = sl
			}
		}
	}
	if best != nil {
		return best.elements[ref-1], best.url, true
	}
	return browser.Element{}, "", false
}

func (s *Server) browserElement(ref int) (browser.Element, string, bool) {
	return s.browserElementFor("", ref)
}

// browserRisk locks the clicks and sends that act on someone else.
func (s *Server) browserRisk(raw string) (string, bool) {
	a := parseBrowserArgs(raw)
	switch a.Acao {
	case "clicar":
		if el, _, ok := s.browserElement(a.Ref); ok && riskyLabel.MatchString(el.Label) {
			return "navegador.enviar", true
		}
	case "digitar":
		if el, _, ok := s.browserElement(a.Ref); ok && a.Enviar && el.Kind == "caixa de texto" {
			return "navegador.enviar", true
		}
	}
	return "navegador", false
}

func (s *Server) browserDescribe(raw string) string {
	a := parseBrowserArgs(raw)
	el, u, _ := s.browserElement(a.Ref)
	if a.Acao == "digitar" {
		return fmt.Sprintf("Escrever e enviar em %s («%s»): %s", host(u), truncate(el.Label, 80), truncate(a.Texto, 400))
	}
	return fmt.Sprintf("Clicar em «%s» em %s", truncate(el.Label, 120), host(u))
}

func (s *Server) toolBrowser(ctx context.Context, t *turn, raw string) (string, error) {
	a := parseBrowserArgs(raw)
	agentID := t.agent.ID
	homeNode, hasHome, herr := s.resolveHomeNode(ctx, t.agent)
	if herr != nil {
		return "", herr
	}
	nodeID := ""
	if hasHome {
		nodeID = homeNode.ID
	} else {
		nodeID = s.browserNodeIDFor(ctx, agentID)
		if nodeID == "" {
			return "", errors.New("o navegador fica no app Avora, que está fechado agora; peça para abrir o app")
		}
	}
	now := time.Now()
	s.browsers.mu.Lock()
	n := s.browsers.node(nodeID)
	browserReady := n.readyFor(agentID, now)
	sl := n.slot(agentID)
	if sl.owner || n.owner {
		msg := "a pessoa está usando o navegador"
		if n.legacyOwner && !sl.owner {
			msg = "o dono assumiu o navegador agora; espere ele devolver (ou pergunte a ele)"
		}
		s.browsers.mu.Unlock()
		return "", errors.New(msg)
	}
	sl.agent, sl.usedAt = agentID, now
	prevURL := sl.url
	s.browsers.mu.Unlock()

	cmd := browser.Command{Action: a.Acao, URL: a.URL, Ref: a.Ref, Text: a.Texto, Enter: a.Enviar, Down: a.Direcao != "cima",
		Times: a.Vezes, Part: a.Parte, Tab: a.Aba}
	var shown string
	switch a.Acao {
	case "abrir":
		shown = "abriu " + host(a.URL)
	case "ler":
		shown = "leu a página"
	case "clicar", "digitar":
		el, _, _ := s.browserElementFor(agentID, a.Ref)
		if a.Acao == "clicar" {
			shown = "clicou em «" + truncate(el.Label, 60) + "»"
		} else {
			shown = "digitou «" + truncate(a.Texto, 60) + "»"
			if el.Kind == "campo de senha" {
				return "", errors.New("senha é a pessoa quem digita: peça para ela entrar no site pelo painel do navegador")
			}
		}
	case "rolar":
		if cmd.Times == 0 {
			cmd.Times = 2
		}
		shown = "rolou a página"
	case "tecla":
		shown = "apertou " + a.Texto
	case "voltar":
		shown = "voltou uma página"
	case "abas":
		shown = "olhou as abas"
	case "print":
		shown = "tirou um print"
	default:
		return "", fmt.Errorf("ação %q não existe", a.Acao)
	}

	if hasHome && !s.online(homeNode) && !browserReady {
		s.step(t, AgentStep{Tool: "navegador", Summary: offlineNodeMessage(homeNode.Name)})
	}

	// Publish a live stream hint right away so the chat shows "Navegando em <site>" while loading.
	if liveHost := orDefault(host(a.URL), host(prevURL)); liveHost != "" {
		root := t
		for root.parent != nil {
			root = root.parent
		}
		s.hub.publish(root.id, hubEvent{"step", AgentStep{Tool: "navegador", Summary: "navegando em " + liveHost, Detail: liveHost}})
	}

	res, err := s.browse(ctx, nodeID, agentID, t.agent.Name, cmd, false)
	now = time.Now()
	s.browsers.mu.Lock()
	sl.usedAt = now
	where := host(sl.url)
	if where == "" {
		where = host(a.URL)
	}
	sl.addLog(now, t.agent.Name, shown)
	n.addLog(now, t.agent.Name, shown)
	s.browsers.mu.Unlock()
	if err != nil {
		return "", err
	}
	if a.Acao != "abrir" && where != "" {
		shown += " em " + where
	}
	s.step(t, AgentStep{Tool: "navegador", Summary: shown, Detail: where})
	if !res.OK {
		return "", errors.New(orDefault(res.Error, "a ação falhou"))
	}
	if a.Acao == "print" {
		if len(res.Shot) == 0 {
			return "", errors.New("não consegui tirar o print")
		}
		f, err := s.store.SaveFile(ctx, store.File{ID: newID(), ConversationID: t.conv, AgentID: t.agent.ID,
			Name: "print " + where + ".jpg", Mime: "image/jpeg", Data: res.Shot, CreatedAt: s.now()})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Print de %s salvo. Para mostrar ao dono, ponha na resposta (numa linha só): ![%s](avora:file/%s)", where, res.Title, f.ID), nil
	}
	return res.Text, nil
}

// ── The person's view ───────────────────────────────────────────

// BrowserView is a node's browser for the app's live view.
type BrowserView struct {
	NodeID   string        `json:"node_id"`
	NodeName string        `json:"node_name"`
	Ready    bool          `json:"ready"`
	URL      string        `json:"url"`
	Title    string        `json:"title"`
	AgentID  string        `json:"agent_id"`
	UsedAt   *time.Time    `json:"used_at,omitempty"`
	Active   bool          `json:"active"` // an agent used it in the last minute
	Owner    bool          `json:"owner"`
	ShotAt   *time.Time    `json:"shot_at,omitempty"`
	Log      []browserLine `json:"log"`
}

func (s *Server) registerBrowser(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/runtime/browser/next", s.node(s.browserNext))
	mux.HandleFunc("POST /v1/runtime/browser/result", s.node(s.browserResult))
	mux.HandleFunc("POST /v1/browser/app/next", s.admin(s.appBrowserNext))
	mux.HandleFunc("POST /v1/browser/app/result", s.admin(s.appBrowserResult))
	mux.HandleFunc("GET /v1/browser/script", s.admin(s.browserScript))
	mux.HandleFunc("GET /v1/browser", s.admin(s.listBrowsers))
	mux.HandleFunc("GET /v1/browser/{node}/screen", s.admin(s.browserScreen))
	mux.HandleFunc("POST /v1/browser/{node}/control", s.admin(s.browserControl))
	mux.HandleFunc("POST /v1/browser/{node}/command", s.admin(s.browserCommand))
	mux.HandleFunc("GET /v1/files/{id}", s.admin(s.getFile))
	mux.HandleFunc("POST /v1/files", s.admin(s.uploadFile))
}

func (s *Server) listBrowsers(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListNodes(r.Context())
	if err != nil {
		storeError(w, err)
		return
	}
	wantAgent := strings.TrimSpace(r.URL.Query().Get("agent"))
	now := time.Now()
	s.browsers.mu.Lock()
	defer s.browsers.mu.Unlock()
	out := []BrowserView{}
	nodes = append([]store.Node{{ID: AppBrowser, Name: "Avora (app)"}}, nodes...)
	for _, nd := range nodes {
		n := s.browsers.nodes[nd.ID]
		if n == nil || nd.RevokedAt != nil {
			continue
		}
		if wantAgent != "" {
			sl := n.slots[wantAgent]
			v := BrowserView{
				NodeID:   nd.ID,
				NodeName: nd.Name,
				Ready:    n.readyFor(wantAgent, now),
				Owner:    n.owner,
				Log:      []browserLine{},
			}
			if sl != nil {
				v.URL, v.Title, v.AgentID = sl.url, sl.title, sl.agent
				v.Owner = sl.owner || n.owner
				v.Active = sl.agent != "" && now.Sub(sl.usedAt) < time.Minute
				v.Log = append([]browserLine{}, sl.log...)
				if !sl.usedAt.IsZero() {
					at := sl.usedAt.UTC()
					v.UsedAt = &at
				}
				if !sl.shotAt.IsZero() {
					at := sl.shotAt.UTC()
					v.ShotAt = &at
				}
			}
			out = append(out, v)
			continue
		}
		sl := n.latestSlot()
		v := BrowserView{
			NodeID:   nd.ID,
			NodeName: nd.Name,
			Ready:    n.ready(now),
			Owner:    n.owner,
			Log:      append([]browserLine{}, n.log...),
		}
		if sl != nil {
			v.URL, v.Title, v.AgentID = sl.url, sl.title, sl.agent
			v.Owner = n.owner || sl.owner
			v.Active = sl.agent != "" && now.Sub(sl.usedAt) < time.Minute
			if !sl.usedAt.IsZero() {
				at := sl.usedAt.UTC()
				v.UsedAt = &at
			}
		}
		if _, shotAt := n.latestShot(); !shotAt.IsZero() {
			at := shotAt.UTC()
			v.ShotAt = &at
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) browserScreen(w http.ResponseWriter, r *http.Request) {
	wantAgent := strings.TrimSpace(r.URL.Query().Get("agent"))
	s.browsers.mu.Lock()
	var shot []byte
	if n := s.browsers.nodes[r.PathValue("node")]; n != nil {
		if wantAgent != "" {
			if sl := n.slots[wantAgent]; sl != nil {
				shot = sl.shot
			}
		} else {
			shot, _ = n.latestShot()
		}
	}
	s.browsers.mu.Unlock()
	if len(shot) == 0 {
		http.Error(w, "no screen yet", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(shot)
}

// browserControl hands an agent's browser (or the node's when agent is empty)
// to the person and back.
func (s *Server) browserControl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent string `json:"agent"`
		Take  *bool  `json:"take"`
		Owner *bool  `json:"owner"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	agent := strings.TrimSpace(body.Agent)
	take := false
	legacy := false
	switch {
	case body.Take != nil:
		take = *body.Take
	case body.Owner != nil:
		take = *body.Owner
		legacy = agent == ""
	}
	now := time.Now()
	s.browsers.mu.Lock()
	n := s.browsers.node(r.PathValue("node"))
	if agent == "" {
		n.owner = take
		n.legacyOwner = take && legacy
	} else {
		n.slot(agent).owner = take
	}
	text := "a pessoa devolveu o navegador"
	if take {
		text = "a pessoa assumiu o navegador"
	}
	if legacy {
		if take {
			text = "o dono assumiu o navegador"
		} else {
			text = "o dono devolveu o navegador"
		}
	}
	if agent != "" {
		n.slot(agent).addLog(now, "Você", text)
	}
	n.addLog(now, "Você", text)
	if !take {
		n.wake()
	}
	s.browsers.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// browserCommand lets the person drive the browser from the app: open a site
// to sign in, refresh the view, go back.
func (s *Server) browserCommand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		browser.Command
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad command", http.StatusBadRequest)
		return
	}
	cmd := body.Command
	switch cmd.Action {
	case "abrir", "print", "voltar", "fechar", "rolar":
	default:
		http.Error(w, "not allowed from the app", http.StatusBadRequest)
		return
	}
	node := r.PathValue("node")
	agent := strings.TrimSpace(body.Agent)
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(4 * time.Minute))
	res, err := s.browse(r.Context(), node, agent, "", cmd, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if cmd.Action == "abrir" {
		now := time.Now()
		s.browsers.mu.Lock()
		n := s.browsers.node(node)
		if agent != "" {
			n.slot(agent).addLog(now, "Você", "abriu "+host(cmd.URL))
		}
		n.addLog(now, "Você", "abriu "+host(cmd.URL))
		s.browsers.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": res.OK, "error": res.Error, "url": res.URL, "title": res.Title})
}

// browserScript is the page reader the app runs in its browser.
func (s *Server) browserScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": browser.SnapshotJS, "max": browser.MaxText})
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.GetFile(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	w.Header().Set("Content-Type", f.Mime)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(f.Data)
}
