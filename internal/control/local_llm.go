package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/engine"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

const (
	localLLMPoll       = 25 * time.Second
	localLLMFirstWait  = 2 * time.Minute
	localLLMStreamIdle = 2 * time.Minute
)

// LocalModelReport is one local model server (Ollama or LM Studio) detected by
// a runner on its own machine and reported in its heartbeat.
type LocalModelReport struct {
	Kind   string   `json:"kind"`
	URL    string   `json:"url"`
	Models []string `json:"models"`
}

// LocalLLMCall is one OpenAI-compatible request queued for a node's runner.
type LocalLLMCall struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"` // "chat" or "models"
	Service string          `json:"service,omitempty"`
	BaseURL string          `json:"base_url,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// LocalLLMResult is one streamed chunk or the final result of a LocalLLMCall.
type LocalLLMResult struct {
	ID     string `json:"id"`
	Seq    int    `json:"seq,omitempty"`
	Status int    `json:"status,omitempty"`
	Chunk  string `json:"chunk,omitempty"`
	Error  string `json:"error,omitempty"`
	Done   bool   `json:"done"`
}

// ConnectLocalModelRequest connects a detected local model on a computer as a
// workspace provider with node_id set.
type ConnectLocalModelRequest struct {
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	BaseURL    string   `json:"base_url"`
	SharedWith []string `json:"shared_with"`
}

// ShareProviderRequest updates who may use a local provider.
type ShareProviderRequest struct {
	SharedWith []string `json:"shared_with"`
}

type nodeLocalInfo struct {
	models    []LocalModelReport
	sharedDir string
}

type localModelBook struct {
	mu     sync.Mutex
	byNode map[string]nodeLocalInfo
}

func (b *localModelBook) set(nodeID string, models []LocalModelReport, sharedDir string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byNode == nil {
		b.byNode = map[string]nodeLocalInfo{}
	}
	prev := b.byNode[nodeID]
	cp := make([]LocalModelReport, len(models))
	for i, m := range models {
		cp[i] = LocalModelReport{
			Kind:   m.Kind,
			URL:    m.URL,
			Models: append([]string{}, m.Models...),
		}
	}
	dir := strings.TrimSpace(sharedDir)
	if dir == "" {
		dir = prev.sharedDir
	}
	b.byNode[nodeID] = nodeLocalInfo{models: cp, sharedDir: dir}
}

func (b *localModelBook) info(nodeID string) ([]LocalModelReport, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	in, ok := b.byNode[nodeID]
	if !ok {
		return nil, ""
	}
	cp := make([]LocalModelReport, len(in.models))
	for i, m := range in.models {
		cp[i] = LocalModelReport{
			Kind:   m.Kind,
			URL:    m.URL,
			Models: append([]string{}, m.Models...),
		}
	}
	return cp, in.sharedDir
}

func validLocalModels(list []LocalModelReport) bool {
	if len(list) > 8 {
		return false
	}
	for _, m := range list {
		if (m.Kind != "ollama" && m.Kind != "lmstudio" && m.Kind != "local" && m.Kind != "openai") ||
			len(m.URL) > 300 || len(m.Models) > 200 {
			return false
		}
		for _, id := range m.Models {
			if len(id) == 0 || len(id) > 160 || !utf8.ValidString(id) {
				return false
			}
		}
	}
	return true
}

type localLLMPending struct {
	ch     chan LocalLLMResult
	closed chan struct{}
	once   sync.Once
}

func newLocalLLMPending() *localLLMPending {
	return &localLLMPending{
		ch:     make(chan LocalLLMResult, 256),
		closed: make(chan struct{}),
	}
}

func (p *localLLMPending) close() {
	p.once.Do(func() { close(p.closed) })
}

type localLLMNode struct {
	polled  time.Time
	queue   []LocalLLMCall
	waiting map[string]*localLLMPending
	notify  chan struct{}
}

func (n *localLLMNode) wake() {
	if n.notify != nil {
		close(n.notify)
	}
	n.notify = make(chan struct{})
}

type localLLMHub struct {
	mu    sync.Mutex
	nodes map[string]*localLLMNode
}

func (h *localLLMHub) node(id string) *localLLMNode {
	if h.nodes == nil {
		h.nodes = map[string]*localLLMNode{}
	}
	n := h.nodes[id]
	if n == nil {
		n = &localLLMNode{
			waiting: map[string]*localLLMPending{},
			notify:  make(chan struct{}),
		}
		h.nodes[id] = n
	}
	return n
}

func (h *localLLMHub) ready(nodeID string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := h.nodes[nodeID]
	return n != nil && !n.polled.IsZero() && now.Sub(n.polled) < localLLMPoll+10*time.Second
}

func (s *Server) registerLocalLLM(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/runtime/llm/next", s.node(s.llmNext))
	mux.HandleFunc("POST /v1/runtime/llm/result", s.node(s.llmResult))
	mux.HandleFunc("POST /v1/nodes/{id}/local-models", s.admin(s.connectNodeLocalModel))
	mux.HandleFunc("POST /v1/nodes/{id}/providers", s.admin(s.connectNodeLocalModel))
	mux.HandleFunc("PUT /v1/nodes/providers/{id}/share", s.admin(s.shareLocalProvider))
	mux.HandleFunc("PATCH /v1/nodes/providers/{id}/share", s.admin(s.shareLocalProvider))
}

func (s *Server) llmNext(w http.ResponseWriter, r *http.Request, node store.Node) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(localLLMPoll + 10*time.Second))
	timer := time.NewTimer(localLLMPoll)
	defer timer.Stop()

	for {
		now := s.now()
		s.localLLM.mu.Lock()
		n := s.localLLM.node(node.ID)
		n.polled = now
		if len(n.queue) > 0 {
			call := n.queue[0]
			n.queue = n.queue[1:]
			s.localLLM.mu.Unlock()
			writeJSON(w, http.StatusOK, call)
			return
		}
		ch := n.notify
		s.localLLM.mu.Unlock()

		select {
		case <-ch:
		case <-timer.C:
			s.touchLLMPolled(node.ID)
			w.WriteHeader(http.StatusNoContent)
			return
		case <-r.Context().Done():
			s.touchLLMPolled(node.ID)
			return
		}
	}
}

func (s *Server) touchLLMPolled(nodeID string) {
	now := s.now()
	s.localLLM.mu.Lock()
	n := s.localLLM.node(nodeID)
	n.polled = now
	s.localLLM.mu.Unlock()
}

func (s *Server) llmResult(w http.ResponseWriter, r *http.Request, node store.Node) {
	var res LocalLLMResult
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(time.Minute))
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&res); err != nil {
		http.Error(w, "bad result", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(res.ID) == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	s.localLLM.mu.Lock()
	n := s.localLLM.node(node.ID)
	n.polled = s.now()
	p := n.waiting[res.ID]
	if res.Done {
		delete(n.waiting, res.ID)
	}
	s.localLLM.mu.Unlock()

	if p != nil {
		select {
		case p.ch <- res:
		case <-p.closed:
		case <-r.Context().Done():
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// LocalLLMClient talks to a local OpenAI-compatible server on one computer by
// sending requests over that computer's runner queue instead of direct HTTP.
type LocalLLMClient struct {
	provider *llm.Provider
}

func (c *LocalLLMClient) Provider() *llm.Provider { return c.provider }

func (c *LocalLLMClient) Chat(ctx context.Context, req llm.Request, onDelta func(llm.Delta)) (llm.Response, error) {
	return c.provider.Chat(ctx, req, onDelta)
}

func (c *LocalLLMClient) Models(ctx context.Context) ([]string, error) {
	return c.provider.Models(ctx)
}

func defaultLocalBaseURL(kind, raw string) string {
	if strings.TrimSpace(raw) != "" {
		return strings.TrimRight(strings.TrimSpace(raw), "/")
	}
	switch kind {
	case "ollama":
		return "http://127.0.0.1:11434/v1"
	case "lmstudio":
		return "http://127.0.0.1:1234/v1"
	default:
		return "http://127.0.0.1:8080/v1"
	}
}

// NewLocalLLMClient builds a queue-backed LLM client for provider p.
func (s *Server) NewLocalLLMClient(p store.Provider) *LocalLLMClient {
	base := defaultLocalBaseURL(p.Kind, p.BaseURL)
	rt := &localLLMTransport{
		server:  s,
		nodeID:  p.NodeID,
		service: p.Kind,
		baseURL: base,
	}
	name := p.Kind
	if name == "" {
		name = "local"
	}
	return &LocalLLMClient{
		provider: llm.NewProviderWithTransport(name, base, "", 0, rt),
	}
}

func (s *Server) localProviderClient(p store.Provider) *llm.Provider {
	return s.NewLocalLLMClient(p).Provider()
}

type localLLMTransport struct {
	server  *Server
	nodeID  string
	service string
	baseURL string
}

func (t *localLLMTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.TrimSpace(t.nodeID) == "" {
		return nil, errors.New("local provider missing node_id")
	}
	kind := "chat"
	var body []byte
	if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/models") {
		kind = "models"
	} else if req.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(req.Body, 4<<20))
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	call := LocalLLMCall{
		ID:      newID()[:16],
		Kind:    kind,
		Service: t.service,
		BaseURL: t.baseURL,
		Body:    body,
	}
	pending := newLocalLLMPending()

	t.server.localLLM.mu.Lock()
	n := t.server.localLLM.node(t.nodeID)
	if len(n.queue) >= 32 {
		t.server.localLLM.mu.Unlock()
		pending.close()
		return nil, errors.New("a fila do modelo local neste computador está cheia; tente em instantes")
	}
	n.queue = append(n.queue, call)
	n.waiting[call.ID] = pending
	n.wake()
	t.server.localLLM.mu.Unlock()

	cleanup := func() {
		pending.close()
		t.server.localLLM.mu.Lock()
		delete(n.waiting, call.ID)
		for i, q := range n.queue {
			if q.ID == call.ID {
				n.queue = append(n.queue[:i], n.queue[i+1:]...)
				break
			}
		}
		t.server.localLLM.mu.Unlock()
	}

	firstTimer := time.NewTimer(localLLMFirstWait)
	var first LocalLLMResult
	select {
	case first = <-pending.ch:
		firstTimer.Stop()
	case <-firstTimer.C:
		cleanup()
		return nil, context.DeadlineExceeded
	case <-req.Context().Done():
		cleanup()
		return nil, req.Context().Err()
	}

	if first.Error != "" && first.Status == 0 {
		cleanup()
		return nil, errors.New(first.Error)
	}
	status := first.Status
	if status == 0 {
		status = http.StatusOK
	}
	contentType := "text/event-stream"
	if kind == "models" || status != http.StatusOK {
		contentType = "application/json"
	}

	pr, pw := io.Pipe()
	go func() {
		defer cleanup()
		if first.Chunk != "" {
			if _, err := pw.Write([]byte(first.Chunk)); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}
		if first.Done {
			if first.Error != "" && status == http.StatusOK {
				_ = pw.CloseWithError(errors.New(first.Error))
				return
			}
			_ = pw.Close()
			return
		}
		idle := time.NewTimer(localLLMStreamIdle)
		defer idle.Stop()
		for {
			select {
			case piece := <-pending.ch:
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(localLLMStreamIdle)
				if piece.Chunk != "" {
					if _, err := pw.Write([]byte(piece.Chunk)); err != nil {
						_ = pw.CloseWithError(err)
						return
					}
				}
				if piece.Error != "" {
					_ = pw.CloseWithError(errors.New(piece.Error))
					return
				}
				if piece.Done {
					_ = pw.Close()
					return
				}
			case <-idle.C:
				_ = pw.CloseWithError(context.DeadlineExceeded)
				return
			case <-req.Context().Done():
				_ = pw.CloseWithError(req.Context().Err())
				return
			}
		}
	}()

	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       pr,
		Request:    req,
	}, nil
}

// ── Access control (who may use a local provider) ───────────────

func isLocalNodeProvider(p store.Provider) bool {
	if strings.TrimSpace(p.NodeID) == "" {
		return false
	}
	k, ok := providerKind(p.Kind)
	if ok && k.Login {
		return false
	}
	return p.Kind != "claude" && p.Kind != "codex" && p.Kind != "antigravity"
}

func localProviderRefusal(ownerName string) string {
	return fmt.Sprintf("Este modelo é do computador de %s e não foi dividido com este espaço", ownerName)
}

func (s *Server) localProviderOwnerName(ctx context.Context, p store.Provider) string {
	provWS := p.WorkspaceID
	if provWS == "" {
		provWS = store.DefaultWorkspaceID
	}
	if ws, err := s.store.GetWorkspace(ctx, provWS); err == nil {
		if ws.OwnerID != "" {
			if owner, perr := s.store.GetPerson(ctx, ws.OwnerID); perr == nil && strings.TrimSpace(owner.Name) != "" {
				return strings.TrimSpace(owner.Name)
			}
		}
		if members, merr := s.store.ListMembers(ctx, provWS); merr == nil {
			for _, m := range members {
				if m.Role == store.RoleOwner && strings.TrimSpace(m.Name) != "" {
					return strings.TrimSpace(m.Name)
				}
			}
			for _, m := range members {
				if m.Status == store.MemberActive && strings.TrimSpace(m.Name) != "" {
					return strings.TrimSpace(m.Name)
				}
			}
		}
		if strings.TrimSpace(p.Account) != "" {
			return strings.TrimSpace(p.Account)
		}
		if strings.TrimSpace(ws.Name) != "" {
			return strings.TrimSpace(ws.Name)
		}
	}
	if strings.TrimSpace(p.Account) != "" {
		return strings.TrimSpace(p.Account)
	}
	return "outra pessoa"
}

// checkLocalProviderAccess enforces that a local computer provider is used only
// by agents of the same workspace or workspaces/people listed in shared_with.
func (s *Server) checkLocalProviderAccess(ctx context.Context, p store.Provider, targetWorkspaceID string) error {
	if !isLocalNodeProvider(p) {
		return nil
	}
	provWS := p.WorkspaceID
	if provWS == "" {
		provWS = store.DefaultWorkspaceID
	}
	targetWS := targetWorkspaceID
	if targetWS == "" {
		targetWS = store.DefaultWorkspaceID
	}
	if provWS == targetWS {
		return nil
	}

	allowed := map[string]bool{targetWS: true}
	if ws, err := s.store.GetWorkspace(ctx, targetWS); err == nil {
		if ws.OwnerID != "" {
			allowed[ws.OwnerID] = true
		}
		if members, merr := s.store.ListMembers(ctx, targetWS); merr == nil {
			for _, m := range members {
				if m.Status == store.MemberActive && (m.Role == store.RoleOwner || ws.Mode == store.WorkspacePersonal) {
					allowed[m.PersonID] = true
				}
			}
		}
	}
	for _, entry := range p.SharedWith {
		id := strings.TrimSpace(entry)
		if id != "" && (id == "*" || allowed[id]) {
			return nil
		}
	}
	return publicError{msg: localProviderRefusal(s.localProviderOwnerName(ctx, p))}
}

func (s *Server) checkBrainProviderAccess(ctx context.Context, workspaceID, providerID string) error {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || providerID == builtinProviderID || isAgyProvider(providerID) || isCodexProvider(providerID) || isClaudeProvider(providerID) {
		return nil
	}
	p, err := s.store.GetProvider(context.Background(), providerID)
	if err != nil {
		return nil
	}
	return s.checkLocalProviderAccess(ctx, p, workspaceID)
}

func (s *Server) resolveLocalTurnEngine(ctx context.Context, t *turn) (ChatEngine, bool, error) {
	provID := strings.TrimSpace(t.agent.Brain.Provider)
	if provID == "" {
		s.models.mu.Lock()
		active := s.models.active
		s.models.mu.Unlock()
		if active != "" && active != builtinProviderID {
			provID = active
		}
	}
	if provID == "" || provID == builtinProviderID || isAgyProvider(provID) || isCodexProvider(provID) || isClaudeProvider(provID) {
		return nil, false, nil
	}
	p, err := s.store.GetProvider(context.Background(), provID)
	if err != nil || !isLocalNodeProvider(p) {
		return nil, false, nil
	}
	if err := s.checkLocalProviderAccess(ctx, p, t.agent.WorkspaceID); err != nil {
		return nil, true, err
	}
	model := strings.TrimSpace(t.agent.Brain.Model)
	if model == "" && len(p.Models) > 0 {
		model = p.Models[0]
	}
	if model == "" && t.agent.Brain.Provider == "" {
		if ce := s.currentEngine(); ce != nil {
			model = ce.Model()
		}
	}
	if model == "" {
		return nil, true, publicError{msg: "escolha um modelo"}
	}
	if n, nerr := s.nodeByID(context.Background(), p.NodeID); nerr == nil && !s.online(n) && !s.localLLM.ready(n.ID, s.now()) {
		if t.agent.HomeNodeID == n.ID {
			s.step(t, AgentStep{Tool: "computador", Summary: offlineNodeMessage(n.Name)})
		}
	}
	return engine.NewNative(s.localProviderClient(p), model), true, nil
}

// ── HTTP: connect & share a computer's local model ──────────────

func cleanSharedWith(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" || len(v) > 120 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func (s *Server) connectNodeLocalModel(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	if !isAdminToken(r.Context()) {
		if _, _, ok := s.requireWorkspacePerm(w, r, wsID, "manage"); !ok {
			return
		}
	}
	var req ConnectLocalModelRequest
	if !readJSON(w, r, &req) {
		return
	}
	req.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	if req.Kind == "" {
		req.Kind = "ollama"
	}
	kind, ok := providerKind(req.Kind)
	if !ok || !kind.Main || kind.Login {
		http.Error(w, "tipo de modelo local inválido", http.StatusBadRequest)
		return
	}
	nodeID := r.PathValue("id")
	node, err := s.nodeByID(r.Context(), nodeID)
	if err != nil || node.RevokedAt != nil || node.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}

	reported, _ := s.localModels.info(node.ID)
	var detected *LocalModelReport
	for i := range reported {
		if reported[i].Kind == req.Kind {
			detected = &reported[i]
			break
		}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if baseURL == "" && detected != nil && detected.URL != "" {
		baseURL = strings.TrimRight(detected.URL, "/")
	}
	baseURL = defaultLocalBaseURL(req.Kind, baseURL)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = kind.Name + " (" + node.Name + ")"
	}
	if utf8.RuneCountInString(name) > 60 {
		http.Error(w, "nome longo demais", http.StatusBadRequest)
		return
	}

	ctx := store.WithWorkspace(r.Context(), wsID)
	list, err := s.store.ListProviders(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	var p store.Provider
	found := false
	for _, existing := range list {
		if existing.NodeID == node.ID && existing.Kind == req.Kind {
			p, found = existing, true
			break
		}
	}
	statusCode := http.StatusCreated
	if found {
		statusCode = http.StatusOK
	} else {
		if len(list) >= maxProviders {
			http.Error(w, "limite de provedores atingido", http.StatusBadRequest)
			return
		}
		p = store.Provider{
			ID:          req.Kind + "." + node.ID,
			WorkspaceID: wsID,
			Kind:        req.Kind,
			NodeID:      node.ID,
		}
		if _, err := s.store.GetProvider(context.Background(), p.ID); err == nil {
			p.ID = req.Kind + "." + node.ID + "." + strings.ToLower(randomString(4))
		}
	}
	p.Name = name
	p.BaseURL = baseURL
	if req.SharedWith != nil {
		p.SharedWith = cleanSharedWith(req.SharedWith)
	}
	if person := personFrom(r.Context()); person != nil && strings.TrimSpace(person.Name) != "" && p.Account == "" {
		p.Account = strings.TrimSpace(person.Name)
	}

	models := []string{}
	if detected != nil && len(detected.Models) > 0 {
		models = append(models, detected.Models...)
	}
	if s.localLLM.ready(node.ID, s.now()) {
		pctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		if probed, perr := s.NewLocalLLMClient(p).Models(pctx); perr == nil && len(probed) > 0 {
			models = probed
		}
		cancel()
	}
	p.Models = models
	p.Status, p.LastError = "ok", ""
	if len(p.Models) == 0 && !s.online(node) {
		p.Status = "error"
		p.LastError = "o computador " + node.Name + " está desligado"
	}

	saved, err := s.store.SaveProvider(ctx, p, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	if s.chatEngine() == nil && len(saved.Models) > 0 {
		s.SetChatEngine(engine.NewNative(s.localProviderClient(saved), saved.Models[0]))
		s.models.mu.Lock()
		s.models.active = saved.ID
		s.models.mu.Unlock()
	}
	writeJSON(w, statusCode, ProviderView{Provider: saved})
}

func (s *Server) shareLocalProvider(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	if !isAdminToken(r.Context()) {
		if _, _, ok := s.requireWorkspacePerm(w, r, wsID, "manage"); !ok {
			return
		}
	}
	var req ShareProviderRequest
	if !readJSON(w, r, &req) {
		return
	}
	p, err := s.store.GetProvider(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if p.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	p.SharedWith = cleanSharedWith(req.SharedWith)
	saved, err := s.store.SaveProvider(store.WithWorkspace(r.Context(), wsID), p, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ProviderView{Provider: saved, HasKey: len(saved.Secret) > 0})
}
