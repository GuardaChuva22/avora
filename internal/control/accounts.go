package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
)

// More Claude and Codex subscriptions: each extra account is a provider of
// kind claude or codex that lives on one node, in its own config folder
// there. The node runs the CLI's own login (`claude auth login`, `codex
// login --device-auth`) and relays its link and code to the app; for
// Claude the owner pastes the code back. Avora never sees the tokens: they
// stay in that folder, written by the official binary.
//
// The account each node was set up with by hand stays the "principal"; an
// agent picks another with "conta", and when the principal hits its limit
// the delegation moves to an extra account on its own.

const loginMinutes = 15

type LoginView struct {
	State     string `json:"state"` // starting, waiting, done, error
	URL       string `json:"url,omitempty"`
	Code      string `json:"code,omitempty"`       // device code to type (Codex)
	NeedsCode bool   `json:"needs_code,omitempty"` // the owner pastes a code (Claude)
	Error     string `json:"error,omitempty"`
}

type loginRun struct {
	LoginView
	job   string
	input string
}

type loginState struct {
	mu   sync.Mutex
	runs map[string]*loginRun // by provider id
}

func (l *loginState) get(id string) (LoginView, string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.runs[id]
	if !ok {
		return LoginView{}, "", false
	}
	return r.LoginView, r.job, true
}

func (l *loginState) update(id string, f func(r *loginRun)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if r, ok := l.runs[id]; ok {
		f(r)
	}
}

// takeInput hands the pasted code to the node running job, once.
func (l *loginState) takeInput(job string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.runs {
		if r.job == job {
			in := r.input
			r.input = ""
			return in, true
		}
	}
	return "", false
}

type AccountRequest struct {
	Code string `json:"code"`
}

func (s *Server) registerAccounts(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/models/providers/{id}/login", s.admin(s.restartLogin))
	mux.HandleFunc("GET /v1/models/providers/{id}/login", s.admin(s.loginStatus))
	mux.HandleFunc("POST /v1/models/providers/{id}/login/code", s.admin(s.loginCode))
	mux.HandleFunc("GET /v1/runtime/jobs/{id}/input", s.node(s.jobInput))
}

// addAccount creates an extra Claude or Codex account and starts its login
// on a node (the VPS when none is named: it is on all day).
func (s *Server) addAccount(w http.ResponseWriter, r *http.Request, kind ProviderKind, req ProviderRequest) {
	ctx := r.Context()
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	node := ""
	for _, n := range nodes {
		if n.ID == req.NodeID && s.online(n) {
			node = n.ID
		}
	}
	if req.NodeID == "" && kind.Kind == "antigravity" {
		node = s.onlineNode(nodes, scheduler.Windows)
	} else if req.NodeID == "" {
		node = s.onlineNode(nodes, scheduler.VPS)
		if node == "" {
			node = s.onlineNode(nodes, scheduler.Windows)
		}
	}
	if node == "" {
		http.Error(w, "o nó precisa estar online para fazer o login", http.StatusConflict)
		return
	}
	name := req.Name
	if name == "" {
		name = kind.Name
	}
	if len([]rune(name)) > 60 {
		http.Error(w, "nome longo demais", http.StatusBadRequest)
		return
	}
	p := store.Provider{ID: kind.Kind + "." + accountSuffix(), WorkspaceID: store.WorkspaceFromContext(ctx), Kind: kind.Kind, Name: name, NodeID: node, Status: "login"}
	saved, err := s.store.SaveProvider(ctx, p, store.ActorAdmin, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	if err := s.startLogin(ctx, saved); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, ProviderView{Provider: saved})
}

// startLogin runs the CLI login of an account on its node and follows it.
func (s *Server) startLogin(ctx context.Context, p store.Provider) error {
	node, err := s.nodeByID(ctx, p.NodeID)
	if err != nil || !s.online(node) {
		return errors.New("o nó dessa conta está offline")
	}
	jobID := newID()
	ended := s.waits.add(jobID)
	events, unsubscribe := s.hub.subscribe(jobID)
	s.logins.mu.Lock()
	if s.logins.runs == nil {
		s.logins.runs = map[string]*loginRun{}
	}
	s.logins.runs[p.ID] = &loginRun{LoginView: LoginView{State: "starting"}, job: jobID}
	s.logins.mu.Unlock()
	spec := &store.AgentSpec{Engine: p.Kind, Account: p.ID, Mode: "login", MaxMinutes: loginMinutes}
	if p.Kind == "antigravity" && isAgyPCAccount(p) {
		spec.Mode = store.AgyCheckMode
	}
	if _, err := s.store.CreateJob(ctx, Job{ID: jobID, Action: store.ActionAgentLogin, Target: node.Kind, DeviceID: node.ID,
		CreatedAt: s.now(), Agent: spec}, "login de "+p.Name, store.ActorAdmin); err != nil {
		s.waits.drop(jobID)
		unsubscribe()
		return errors.New("não consegui criar a tarefa de login")
	}
	go func() {
		defer s.waits.drop(jobID)
		defer unsubscribe()
		timeout := time.NewTimer((loginMinutes + 2) * time.Minute)
		defer timeout.Stop()
		for {
			select {
			case ev := <-events:
				st, ok := ev.data.(AgentStep)
				if !ok || ev.kind != "step" {
					continue
				}
				s.logins.update(p.ID, func(r *loginRun) {
					r.State = "waiting"
					switch st.Tool {
					case "url":
						r.URL = st.Summary
					case "code":
						r.Code = st.Summary
					case "paste":
						r.NeedsCode = true
					}
				})
			case end := <-ended:
				s.loginEnded(p.ID, end)
				return
			case <-timeout.C:
				if job, err := s.store.CancelJob(context.Background(), jobID, store.ActorSystem, s.now()); err == nil {
					s.finishAgentReply(job, nil)
				}
			}
		}
	}()
	return nil
}

func (s *Server) loginEnded(id string, end jobEnd) {
	ctx := context.Background()
	p, err := s.store.GetProvider(ctx, id)
	if err != nil {
		return // removed meanwhile
	}
	r := end.result
	if r != nil && r.Success {
		var who struct {
			Account string `json:"account"`
		}
		_ = json.Unmarshal([]byte(r.Output), &who)
		p.Status, p.LastError = "ok", ""
		if who.Account != "" {
			p.Account = truncate(who.Account, 120)
		}
		// A pool account is known by its Google e-mail (D61).
		if p.Kind == "antigravity" && !isAgyPCAccount(p) && (p.Name == "" || p.Name == "Antigravity") && p.Account != "" {
			p.Name = truncate(p.Account, 60)
		}
	} else {
		p.Status = "error"
		p.LastError = "o login não terminou"
		if r != nil && r.Error != "" {
			p.LastError = truncate(r.Error, 200)
		}
	}
	if _, err := s.store.SaveProvider(ctx, p, store.ActorAdmin, s.now()); err != nil {
		log.Printf("account %s: saving the login: %v", id, err)
	}
	s.logins.update(id, func(run *loginRun) {
		run.State, run.Error, run.input = "done", "", ""
		if p.Status != "ok" {
			run.State, run.Error = "error", p.LastError
		}
	})
	if p.Status == "ok" {
		s.repick(ctx, true)
	}
}

func (s *Server) loginProvider(w http.ResponseWriter, r *http.Request) (store.Provider, bool) {
	p, err := s.store.GetProvider(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return p, false
	}
	if k, _ := providerKind(p.Kind); !k.Login {
		http.Error(w, "esse provedor não tem login", http.StatusBadRequest)
		return p, false
	}
	return p, true
}

func (s *Server) restartLogin(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loginProvider(w, r)
	if !ok {
		return
	}
	if v, _, running := s.logins.get(p.ID); running && (v.State == "starting" || v.State == "waiting") {
		writeJSON(w, http.StatusOK, v)
		return
	}
	if err := s.startLogin(r.Context(), p); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	v, _, _ := s.logins.get(p.ID)
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) loginStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loginProvider(w, r)
	if !ok {
		return
	}
	v, _, found := s.logins.get(p.ID)
	if !found {
		v = LoginView{State: "done"}
		if p.Status != "ok" {
			v = LoginView{State: "error", Error: p.LastError}
		}
	}
	writeJSON(w, http.StatusOK, v)
}

// loginCode takes the code the owner copied from the login page.
func (s *Server) loginCode(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loginProvider(w, r)
	if !ok {
		return
	}
	var req AccountRequest
	if !readJSON(w, r, &req) {
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" || len(code) > 512 || strings.ContainsAny(code, "\r\n") {
		http.Error(w, "cole o código da página de login", http.StatusBadRequest)
		return
	}
	set := false
	s.logins.update(p.ID, func(run *loginRun) {
		if run.State == "waiting" {
			run.input, set = code, true
		}
	})
	if !set {
		http.Error(w, "esse login não está esperando um código", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// jobInput gives a node running a login the code the owner pasted.
func (s *Server) jobInput(w http.ResponseWriter, r *http.Request, node store.Node) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if job.DeviceID != node.ID || job.Action != store.ActionAgentLogin {
		storeError(w, store.ErrForbidden)
		return
	}
	in, _ := s.logins.takeInput(job.ID)
	if in == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"input": in})
}

// accountFor picks the subscription account a delegation runs on: the one
// the agent named, or an extra one when the node's own hit its limit.
// It returns the account and the node it lives on ("" = the principal).
func (s *Server) accountFor(ctx context.Context, engine, want, node string) (store.Provider, error) {
	if engine == "antigravity" && strings.TrimSpace(want) == "" {
		// The pool: the freest account (D61), or the PC's own agy.
		if p, _, ok := s.agyRoute(ctx); ok {
			return p, nil
		}
		return store.Provider{}, nil
	}
	list, err := s.store.ListProviders(ctx)
	if err != nil {
		return store.Provider{}, err
	}
	nodes, _ := s.store.ListNodes(ctx)
	online := map[string]bool{}
	for _, n := range nodes {
		online[n.ID] = s.online(n)
	}
	var usable []store.Provider
	for _, p := range list {
		if p.Kind == engine && p.Status == "ok" && online[p.NodeID] {
			usable = append(usable, p)
		}
	}
	want = strings.ToLower(strings.TrimSpace(want))
	if want != "" && want != "principal" {
		for _, p := range usable {
			if strings.ToLower(p.Name) == want || strings.ToLower(p.Account) == want || p.ID == want ||
				(p.Account != "" && strings.Contains(strings.ToLower(p.Account), want)) {
				return p, nil
			}
		}
		names := []string{"principal"}
		for _, p := range usable {
			names = append(names, p.Name)
		}
		return store.Provider{}, errors.New("não há a conta " + want + " online; as contas são: " + strings.Join(names, ", "))
	}
	if want == "" && s.quotaLimited(node, engine) && len(usable) > 0 {
		for _, p := range usable {
			if !s.quotaLimited(p.NodeID, engine, p.ID) {
				return p, nil
			}
		}
		return usable[0], nil
	}
	return store.Provider{}, nil
}

// nodeByID finds a node (revoked ones too).
func (s *Server) nodeByID(ctx context.Context, id string) (store.Node, error) {
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return store.Node{}, err
	}
	for _, n := range nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return store.Node{}, store.ErrNotFound
}

// accountSuffix names an extra account's folder: six hex digits.
func accountSuffix() string {
	b, err := randomBytes(3)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
