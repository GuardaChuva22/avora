// Package control implements Avora's authenticated control plane.
// Executable jobs are allowlisted: ping, diagnostics, and agent.run, which runs
// an agent CLI under a bounded permission mode. Arbitrary commands are not supported.
package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
	"github.com/GuardaChuva22/avora/internal/vault"
	"github.com/GuardaChuva22/avora/internal/web"
)

const (
	OnlineTTL = 30 * time.Second
	// JobLease is how long a node may hold a job before it is requeued.
	JobLease = 2 * time.Minute
	// MaxAttempts is how many leases a job gets before it is marked timed out.
	MaxAttempts = 3
	// PairingCodeTTL is how long a pairing code stays valid.
	PairingCodeTTL = 10 * time.Minute
	// NodeTokenPrefix marks node tokens so they are easy to recognize in leaks.
	NodeTokenPrefix = "avn_"
)

type (
	Job       = store.Job
	JobResult = store.Result
)

type JobRequest struct {
	Target string           `json:"target"`
	Action string           `json:"action"`
	Agent  *store.AgentSpec `json:"agent,omitempty"`
}

// NodeStatus is a node as shown to the admin, with its online state.
type NodeStatus struct {
	store.Node
	Online      bool               `json:"online"`
	LocalModels []LocalModelReport `json:"local_models,omitempty"`
	SharedDir   string             `json:"shared_dir,omitempty"`
}

type PairingCodeRequest struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type PairingCodeResponse struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PairRequest struct {
	Code string `json:"code"`
}

// PairResponse carries the node token. It is shown exactly once.
type PairResponse struct {
	NodeID string           `json:"node_id"`
	Name   string           `json:"name"`
	Kind   scheduler.Target `json:"kind"`
	Token  string           `json:"token"`
}

type Server struct {
	adminToken string
	store      store.Store
	// engine is the chat engine in use; Settings can swap it while running.
	engine         atomic.Pointer[engineBox]
	models         modelState
	logins         loginState
	ui             http.Handler
	now            func() time.Time
	quotaBook      quotaBook
	agyJobs        agyBusy
	toolBook       toolBook
	localModels    localModelBook
	localLLM       localLLMHub
	missionBridges missionBridges
	compacting     sync.Map // conversation id -> compacting now
	personSeen     sync.Map // person id -> last time last_seen was written
	hub            jobHub
	channel        channelState
	router         routerState
	routines       routineState
	goals          goalState
	beats          heartbeatRun
	night          nightState
	nightMu        sync.Mutex
	wa             WhatsApp
	triggers       triggerState
	browsers       browserHub
	turns          turnState
	waits          jobWaits
	approvals      approvalWaits
	// approvalTTL is how long a locked action waits for the owner.
	approvalTTL time.Duration
	loc         *time.Location
	// delegateModels is the model each delegated CLI gets ("" = its default).
	delegateModels map[string]string
	// researchModel is the Codex model of pesquisar (DefaultResearchModel).
	researchModel string
	// web is the agents' search and page reader; workspace holds their
	// folders (agents/<id>), "" when this server has none.
	web       *web.Client
	workspace string
	// connectors are the Plugins; the vault seals their credentials.
	mcpMu      sync.Mutex
	usage      usageCache
	connectors *connect.Registry
	vault      *vault.Vault
	oauth      oauthState
	// bridges are the keys of the delegated runs that reach the Plugins.
	bridges bridges
}

// SetDelegateModels picks the model of each delegated CLI, by engine.
func (s *Server) SetDelegateModels(claude, codex string) error {
	if !validModel(claude) || !validModel(codex) {
		return errors.New("delegate models must be plain names like haiku")
	}
	s.delegateModels = map[string]string{"claude": claude, "codex": codex}
	return nil
}

// SetResearchModel picks the Codex model of the pesquisar tool.
func (s *Server) SetResearchModel(model string) error {
	if !validModel(model) {
		return errors.New("research model must be a plain name like gpt-6.1-sol")
	}
	s.researchModel = model
	return nil
}

func NewServer(adminToken string, st store.Store) (*Server, error) {
	if len(adminToken) < 32 {
		return nil, errors.New("AVORA_ADMIN_TOKEN must have at least 32 characters")
	}
	if st == nil {
		return nil, errors.New("a store is required")
	}
	return &Server{adminToken: adminToken, store: st, now: time.Now}, nil
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func hashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// normalizeCode accepts codes typed with lowercase letters, dashes or spaces.
func normalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(code)))
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "avora-server"})
	})
	mux.HandleFunc("GET /v1/capabilities", s.admin(s.capabilities))
	mux.HandleFunc("GET /v1/nodes", s.admin(s.nodes))
	mux.HandleFunc("GET /v1/quotas", s.admin(s.quotas))
	mux.HandleFunc("POST /v1/nodes/pairing-codes", s.admin(s.createPairingCode))
	mux.HandleFunc("POST /v1/nodes/{id}/revoke", s.admin(s.revokeNode))
	mux.HandleFunc("GET /v1/events", s.admin(s.events))
	mux.HandleFunc("POST /v1/jobs", s.admin(s.submitJob))
	mux.HandleFunc("GET /v1/jobs", s.admin(s.listJobs))
	mux.HandleFunc("GET /v1/jobs/{id}", s.admin(s.getJob))
	mux.HandleFunc("POST /v1/jobs/{id}/cancel", s.admin(s.cancelJob))
	mux.HandleFunc("POST /v1/runtime/pair", s.pair)
	mux.HandleFunc("POST /v1/runtime/heartbeat", s.node(s.heartbeat))
	mux.HandleFunc("POST /v1/runtime/claim", s.node(s.claim))
	mux.HandleFunc("POST /v1/runtime/jobs/{id}/complete", s.node(s.completeJob))
	mux.HandleFunc("POST /v1/runtime/jobs/{id}/steps", s.node(s.agentStep))
	mux.HandleFunc("POST /v1/runtime/jobs/{id}/files", s.node(s.jobFile))
	s.registerChat(mux)
	s.registerChannels(mux)
	s.registerLive(mux)
	s.registerMemories(mux)
	s.registerConnectors(mux)
	s.registerModels(mux)
	s.registerProjects(mux)
	s.registerSkills(mux)
	s.registerDocs(mux)
	s.registerMissions(mux)
	s.registerGoals(mux)
	s.registerHeartbeat(mux)
	s.registerNight(mux)
	s.registerWhatsApp(mux)
	mux.HandleFunc("GET /v1/mentions", s.admin(s.listMentions))
	s.registerRuns(mux)
	s.registerBrowser(mux)
	s.registerLocalLLM(mux)
	s.registerContext(mux)
	s.registerNodeTools(mux)
	s.registerAccounts(mux)
	s.registerPeople(mux)
	s.registerWorkspaces(mux)
	s.registerHome(mux)
	s.registerNotices(mux)
	s.registerTemplates(mux)
	if s.ui != nil {
		mux.Handle("GET /", s.ui)
	}
	return mux
}

// SetUI serves the web app at every path the API does not use.
func (s *Server) SetUI(h http.Handler) { s.ui = h }

// Sweep requeues or times out jobs whose node stopped answering.
func (s *Server) Sweep(ctx context.Context) error {
	changed, err := s.store.ExpireLeases(ctx, s.now(), MaxAttempts)
	for _, job := range changed {
		log.Printf("job %s lease expired on %s: now %s", job.ID, job.DeviceID, job.Status)
		if job.Status.Terminal() {
			s.finishAgentReply(job, nil)
		}
	}
	return err
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}

func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		if secureEqual(tok, s.adminToken) {
			ctx := withAdmin(r.Context())
			if ws, err := s.store.GetWorkspace(ctx, store.DefaultWorkspaceID); err == nil && ws.OwnerID != "" {
				if p, err := s.store.GetPerson(ctx, ws.OwnerID); err == nil {
					ctx = withPerson(ctx, p)
				}
			}
			next(w, r.WithContext(ctx))
			return
		}
		if strings.HasPrefix(tok, SessionTokenPrefix) {
			now := s.now()
			p, err := s.store.PersonBySession(r.Context(), hashSecret(tok), now)
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if err != nil {
				storeError(w, err)
				return
			}
			if s.shouldTouchPerson(p.ID, p.LastSeen, now) {
				if err := s.store.TouchPerson(r.Context(), p.ID, now); err == nil {
					seen := now.UTC().Truncate(time.Microsecond)
					p.LastSeen = &seen
				}
			}
			next(w, r.WithContext(withPerson(r.Context(), p)))
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}

// node authenticates a runtime by the hash of its token; revoked nodes fail.
func (s *Server) node(next func(http.ResponseWriter, *http.Request, store.Node)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if !strings.HasPrefix(token, NodeTokenPrefix) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		node, err := s.store.NodeByToken(r.Context(), hashSecret(token))
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err != nil {
			storeError(w, err)
			return
		}
		next(w, r, node)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	return readJSONMax(w, r, out, 8192)
}

func readJSONMax(w http.ResponseWriter, r *http.Request, out any, max int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		http.Error(w, "unexpected content after JSON", http.StatusBadRequest)
		return false
	}
	return true
}

// storeError maps store errors to HTTP responses without leaking internals.
func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, store.ErrForbidden):
		http.Error(w, "job belongs to another node", http.StatusForbidden)
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "not in a valid state for this operation", http.StatusConflict)
	default:
		log.Printf("store error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) online(n store.Node) bool {
	return n.RevokedAt == nil && n.LastSeen != nil && s.now().Sub(*n.LastSeen) < OnlineTTL
}

func (s *Server) capabilities(w http.ResponseWriter, _ *http.Request) {
	eng := s.chatEngine()
	chat := map[string]any{"enabled": eng != nil}
	if eng != nil {
		chat["engine"], chat["provider"], chat["model"] = eng.Name(), eng.Provider(), eng.Model()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "early", "execution_enabled": true,
		"allowed_actions": []string{"ping", "diagnostics", store.ActionAgentRun}, "chat": chat,
		"agent":   map[string]any{"engines": AgentEngines, "modes": AgentModes, "max_minutes": MaxAgentMinutes},
		"targets": []string{"windows", "vps"}, "persistence": s.store.Durable(),
	})
}

func (s *Server) nodes(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	out := make([]NodeStatus, len(nodes))
	for i, n := range nodes {
		models, shared := s.localModels.info(n.ID)
		out[i] = NodeStatus{Node: n, Online: s.online(n), LocalModels: models, SharedDir: shared}
	}
	writeJSON(w, http.StatusOK, out)
}

func validKind(kind string) bool {
	return kind == string(scheduler.Windows) || kind == string(scheduler.VPS)
}

func (s *Server) createPairingCode(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	if !isAdminToken(r.Context()) {
		if _, _, ok := s.requireWorkspacePerm(w, r, wsID, "manage"); !ok {
			return
		}
	}
	var req PairingCodeRequest
	if !readJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 64 {
		http.Error(w, "name must have 1 to 64 characters", http.StatusBadRequest)
		return
	}
	if !validKind(req.Kind) {
		http.Error(w, "kind must be windows or vps", http.StatusBadRequest)
		return
	}
	raw, err := randomBytes(10) // 80 bits, valid for minutes and only once
	if err != nil {
		http.Error(w, "failed to create code", http.StatusInternalServerError)
		return
	}
	plain := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	now := s.now()
	code := store.PairingCode{
		Hash: hashSecret(plain), WorkspaceID: wsID, Name: req.Name, Kind: scheduler.Target(req.Kind),
		CreatedAt: now, ExpiresAt: now.Add(PairingCodeTTL),
	}
	if err := s.store.CreatePairingCode(r.Context(), code, actorFrom(r)); err != nil {
		storeError(w, err)
		return
	}
	display := plain[0:4] + "-" + plain[4:8] + "-" + plain[8:12] + "-" + plain[12:16]
	writeJSON(w, http.StatusCreated, PairingCodeResponse{
		Code: display, Name: req.Name, Kind: req.Kind, ExpiresAt: code.ExpiresAt.UTC().Truncate(time.Microsecond),
	})
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var req PairRequest
	if !readJSON(w, r, &req) {
		return
	}
	code := normalizeCode(req.Code)
	if len(code) != 16 {
		http.Error(w, "invalid or expired pairing code", http.StatusUnauthorized)
		return
	}
	idBytes, err := randomBytes(4)
	if err != nil {
		http.Error(w, "failed to pair", http.StatusInternalServerError)
		return
	}
	secret, err := randomBytes(32)
	if err != nil {
		http.Error(w, "failed to pair", http.StatusInternalServerError)
		return
	}
	token := NodeTokenPrefix + base64.RawURLEncoding.EncodeToString(secret)
	node, err := s.store.RedeemPairingCode(r.Context(), hashSecret(code), "node-"+hex.EncodeToString(idBytes), hashSecret(token), s.now())
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "invalid or expired pairing code", http.StatusUnauthorized)
		return
	}
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, PairResponse{NodeID: node.ID, Name: node.Name, Kind: node.Kind, Token: token})
}

func (s *Server) revokeNode(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	if !isAdminToken(r.Context()) {
		if _, _, ok := s.requireWorkspacePerm(w, r, wsID, "manage"); !ok {
			return
		}
	}
	cur, err := s.nodeByID(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if cur.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	node, err := s.store.RevokeNode(r.Context(), cur.ID, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, NodeStatus{Node: node})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := store.EventFilter{JobID: q.Get("job"), Latest: q.Get("order") == "latest"}
	if v := q.Get("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			http.Error(w, "invalid after", http.StatusBadRequest)
			return
		}
		filter.AfterID = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > store.MaxEventLimit {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		filter.Limit = n
	}
	events, err := s.store.Events(r.Context(), filter)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request, node store.Node) {
	var req HeartbeatRequest
	if r.ContentLength != 0 && !readJSONMax(w, r, &req, 32768) {
		return
	}
	if len(req.Running) > 32 {
		http.Error(w, "too many running jobs", http.StatusBadRequest)
		return
	}
	if !validQuotas(req.Quotas) {
		http.Error(w, "invalid quotas", http.StatusBadRequest)
		return
	}
	if !validTools(req.Tools) {
		http.Error(w, "invalid tools", http.StatusBadRequest)
		return
	}
	if !validLocalModels(req.LocalModels) || len(req.SharedDir) > 512 {
		http.Error(w, "invalid local models", http.StatusBadRequest)
		return
	}
	if err := s.store.RecordHeartbeat(r.Context(), node.ID, s.now(), OnlineTTL); err != nil {
		storeError(w, err)
		return
	}
	s.quotaBook.set(node.ID, req.Quotas)
	s.toolBook.set(node.ID, req.Tools)
	s.localModels.set(node.ID, req.LocalModels, req.SharedDir)
	stop, err := s.store.RenewLeases(r.Context(), node.ID, req.Running, s.now(), JobLease)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, HeartbeatResponse{Status: "online", NodeID: node.ID, Stop: stop})
}

// onlineNode picks the first online node of a kind, in pairing order.
func (s *Server) onlineNode(nodes []store.Node, kind scheduler.Target) string {
	for _, n := range nodes {
		if n.Kind == kind && s.online(n) {
			return n.ID
		}
	}
	return ""
}

func (s *Server) submitJob(w http.ResponseWriter, r *http.Request) {
	var req JobRequest
	if !readJSONMax(w, r, &req, 2*MaxAgentPrompt) {
		return
	}
	engine := scheduler.Native
	switch req.Action {
	case "ping", "diagnostics":
		if req.Agent != nil {
			http.Error(w, "agent spec only applies to agent.run", http.StatusBadRequest)
			return
		}
	case store.ActionAgentRun:
		if req.Agent != nil {
			// Conversation runs are created by the chat, never by hand.
			req.Agent.ConversationID, req.Agent.Resume = "", ""
		}
		if err := validateAgent(req.Agent); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		engine = scheduler.Claude
		if req.Agent.Engine == "codex" {
			engine = scheduler.Codex
		}
	default:
		http.Error(w, "action not allowlisted", http.StatusBadRequest)
		return
	}
	if req.Target != "" && req.Target != "auto" && !validKind(req.Target) {
		http.Error(w, "unsupported target", http.StatusBadRequest)
		return
	}
	nodes, err := s.store.ListNodes(r.Context())
	if err != nil {
		storeError(w, err)
		return
	}
	windows := s.onlineNode(nodes, scheduler.Windows)
	vps := s.onlineNode(nodes, scheduler.VPS)
	routing := scheduler.Request{Engine: engine}
	if req.Target != "" && req.Target != "auto" {
		routing.Preferred = scheduler.Target(req.Target)
	}
	decision := scheduler.Route(routing, scheduler.Availability{WindowsOnline: windows != "", VPSReady: vps != ""})
	if !decision.Allowed {
		http.Error(w, "requested node unavailable", http.StatusConflict)
		return
	}
	assigned := windows
	if decision.Target == scheduler.VPS {
		assigned = vps
	}
	id, err := randomBytes(12)
	if err != nil {
		http.Error(w, "failed to create job", http.StatusInternalServerError)
		return
	}
	job, err := s.store.CreateJob(r.Context(), Job{
		ID: hex.EncodeToString(id), Action: req.Action, Target: decision.Target,
		DeviceID: assigned, CreatedAt: s.now(), Agent: req.Agent,
	}, decision.Reason, store.ActorAdmin)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			http.Error(w, "limit must be between 1 and 200", http.StatusBadRequest)
			return
		}
		limit = n
	}
	jobs, err := s.store.ListJobs(r.Context(), limit)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.CancelJob(r.Context(), r.PathValue("id"), store.ActorAdmin, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	s.finishAgentReply(job, nil)
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) claim(w http.ResponseWriter, r *http.Request, node store.Node) {
	if !s.online(node) {
		http.Error(w, "heartbeat required", http.StatusConflict)
		return
	}
	var req ClaimRequest
	if r.ContentLength != 0 && !readJSON(w, r, &req) {
		return
	}
	job, err := s.store.ClaimJob(r.Context(), node.ID, s.now(), JobLease, req)
	if err != nil {
		storeError(w, err)
		return
	}
	if job == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) completeJob(w http.ResponseWriter, r *http.Request, node store.Node) {
	var result JobResult
	if !readJSONMax(w, r, &result, 2*MaxJobOutput) {
		return
	}
	if len(result.Output) > MaxJobOutput || len(result.Error) > 2048 || len(result.Stats) > 2048 {
		http.Error(w, "result too large", http.StatusBadRequest)
		return
	}
	job, err := s.store.CompleteJob(r.Context(), r.PathValue("id"), node.ID, result, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	s.finishAgentReply(job, &result)
	writeJSON(w, http.StatusOK, job)
}
