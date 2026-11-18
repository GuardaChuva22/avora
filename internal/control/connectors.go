package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
	"github.com/GuardaChuva22/avora/internal/vault"
)

// Plugins (Fase B): the owner connects an app once, on the Plugins page; the
// agents then get that app's tools. The credential is sealed by the vault
// and opened only inside a call, so it never reaches the model, the event
// log or any API answer. Reading and writing in the owner's own space run
// freely; sending, posting or deleting goes through the approval gate.

const (
	connectorTimeout = 2 * time.Minute
	oauthTTL         = 10 * time.Minute
	maxOAuthPending  = 8
	maxCredential    = 64 << 10
	maxFileField     = 48 << 10 // a certificate file, in base64
)

// SetConnectors enables the Plugins with their vault.
func (s *Server) SetConnectors(reg *connect.Registry, v *vault.Vault) {
	s.connectors, s.vault = reg, v
}

// credPurpose binds a sealed credential to its connection: one account's
// secret cannot be opened as another's.
func credPurpose(id string) string { return "connection:" + id }

func (s *Server) sealCred(id string, c connect.Cred) ([]byte, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return s.vault.Seal(b, credPurpose(id))
}

func (s *Server) openCred(conn store.Connection) (connect.Cred, error) {
	var c connect.Cred
	b, err := s.vault.Open(conn.Secret, credPurpose(conn.ID))
	if err != nil {
		return c, errors.New("a credencial não abre com a chave do cofre atual; reconecte em Plugins")
	}
	return c, json.Unmarshal(b, &c)
}

// connectorCall finds a connector tool and parses its arguments.
func (s *Server) connectorCall(name, raw string) (connect.Tool, connect.Args, bool) {
	if s.connectors == nil {
		return connect.Tool{}, nil, false
	}
	_, t, ok := s.connectors.Tool(name)
	if !ok {
		return connect.Tool{}, nil, false
	}
	args, err := connect.ParseArgs(raw)
	if err != nil {
		args = connect.Args{}
	}
	return t, args, true
}

func usableBy(c store.Connection, agentID string) bool {
	return c.Status != "reconnect" && c.Status != "pending" && (len(c.Agents) == 0 || slices.Contains(c.Agents, agentID))
}

// accountsOf groups connections by app, keeping the store's order (oldest
// first, so an app's first account is its default).
func accountsOf(conns []store.Connection) (kinds []string, by map[string][]store.Connection) {
	by = map[string][]store.Connection{}
	for _, c := range conns {
		if _, seen := by[c.Kind]; !seen {
			kinds = append(kinds, c.Kind)
		}
		by[c.Kind] = append(by[c.Kind], c)
	}
	// An app that runs on another's accounts has those accounts.
	backed := make([]string, 0, len(connect.Backed))
	for kind := range connect.Backed {
		backed = append(backed, kind)
	}
	slices.Sort(backed)
	for _, kind := range backed {
		kinds = slices.DeleteFunc(kinds, func(k string) bool { return k == kind })
		delete(by, kind)
		if via := by[connect.Backed[kind]]; len(via) > 0 {
			by[kind] = via
			kinds = append(kinds, kind)
		}
	}
	return kinds, by
}

// accountLabels name each account of one app the way an agent picks it:
// the account itself, numbered when two read the same.
func accountLabels(conns []store.Connection) []string {
	count := map[string]int{}
	for _, c := range conns {
		count[strings.ToLower(c.Account)]++
	}
	out := make([]string, len(conns))
	for i, c := range conns {
		out[i] = c.Account
		if c.Account == "" || count[strings.ToLower(c.Account)] > 1 {
			out[i] = strings.TrimSpace(fmt.Sprintf("%s #%d", c.Account, i+1))
		}
	}
	return out
}

func usableOnly(conns []store.Connection, agentID string) []store.Connection {
	out := []store.Connection{}
	for _, c := range conns {
		if usableBy(c, agentID) {
			out = append(out, c)
		}
	}
	return out
}

// withAccount adds the "conta" argument to a tool's schema when the agent
// has more than one account of its app.
func withAccount(params, app string, labels []string) string {
	schema := map[string]any{}
	if strings.TrimSpace(params) != "" && json.Unmarshal([]byte(params), &schema) != nil {
		return params
	}
	if schema["type"] == nil {
		schema["type"] = "object"
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	props["conta"] = map[string]any{"type": "string", "enum": labels,
		"description": fmt.Sprintf("Qual conta do %s usar. Sem este argumento: %s (a principal).", app, labels[0])}
	schema["properties"] = props
	b, err := json.Marshal(schema)
	if err != nil {
		return params
	}
	return string(b)
}

// connectorTools are the tools of the apps connected for this agent (only
// the loaded ones when loaded is set). With several accounts of one app,
// each tool takes a "conta" argument.
func (s *Server) connectorTools(ctx context.Context, agentID string, loaded func(kind string) bool) []llm.Tool {
	if s.connectors == nil {
		return nil
	}
	conns, err := s.store.ListConnections(ctx)
	if err != nil {
		return nil
	}
	kinds, by := accountsOf(conns)
	out := []llm.Tool{}
	for _, kind := range kinds {
		con, ok := s.connectors.Get(kind)
		usable := usableOnly(by[kind], agentID)
		if !ok || len(usable) == 0 || (loaded != nil && !loaded(kind)) {
			continue
		}
		labels := accountLabels(usable)
		for _, t := range con.Tools() {
			if isMCPTool(t.Name) {
				continue
			}
			params := t.Params
			if len(usable) > 1 {
				params = withAccount(params, con.Info().Name, labels)
			}
			out = append(out, tool(t.Name, t.Description, params))
		}
	}
	return out
}

// connectionsBlock tells the agent which apps it has and which the owner
// could connect.
func (s *Server) connectionsBlock(ctx context.Context, agentID string) string {
	if s.connectors == nil {
		return ""
	}
	conns, _ := s.store.ListConnections(ctx)
	_, by := accountsOf(conns)
	have, missing, broken, pending := []string{}, []string{}, []string{}, []string{}
	several := false
	for _, con := range s.connectors.All() {
		info := con.Info()
		apps := strings.Join(info.Apps, ", ")
		if apps == "" {
			apps = info.Name
		}
		all := by[info.Kind]
		if len(all) == 0 {
			missing = append(missing, apps)
			continue
		}
		for _, c := range all {
			switch c.Status {
			case "pending":
				pending = append(pending, apps)
			case "reconnect":
				broken = append(broken, fmt.Sprintf("%s (%s)", apps, c.Account))
			}
		}
		usable := usableOnly(all, agentID)
		switch len(usable) {
		case 0:
		case 1:
			have = append(have, fmt.Sprintf("%s (%s)", apps, usable[0].Account))
		default:
			several = true
			labels := accountLabels(usable)
			have = append(have, fmt.Sprintf("%s (contas: %s; a principal é %s)", apps, strings.Join(labels, ", "), labels[0]))
		}
	}
	out := "\n\nApps do dono:"
	if len(have) > 0 {
		out += " conectados para você: " + strings.Join(have, "; ") + "." +
			" Use as ferramentas desses apps direto (para eles, nunca o navegador): ler, buscar, criar rascunhos, tarefas e documentos é livre; enviar, comentar, convidar, apagar ou pagar pede a aprovação do dono sozinho (não pergunte antes no chat, chame a ferramenta e o pedido de aprovação aparece para ele)." +
			" Se as ferramentas de um desses apps não estiverem na sua lista, chame ativar_app com o app e elas aparecem; nunca abra o app no navegador por isso." +
				" Apps grandes vêm como <app>_ferramentas (lista e argumentos) e <app>_usar (executa): veja os argumentos antes do primeiro uso."
		if several {
			out += " Quando um app tem mais de uma conta, escolha com o argumento conta (sem ele vale a principal); se o pedido não deixar claro qual conta, use a principal e diga qual usou, ou consulte as contas uma a uma quando o pedido for sobre todas."
		}
	} else {
		out += " nenhum conectado para você."
	}
	if s.vault != nil {
		if s.workspace != "" {
			out += " Quando o dono pedir para instalar ou criar um MCP, use instalar_mcp ou criar_mcp."
		} else {
			out += " Quando o dono pedir para instalar um MCP por endereço HTTPS, use instalar_mcp."
		}
	}
	if len(broken) > 0 {
		out += " Precisam ser reconectados pelo dono em Plugins: " + strings.Join(broken, "; ") + "."
	}
	if len(pending) > 0 {
		out += " Já aparecem em Plugins e aguardam conexão do dono: " + strings.Join(pending, ", ") + "."
	}
	if len(missing) > 0 {
		out += " O dono pode conectar com um clique em Plugins no app: " + strings.Join(missing, ", ") + ". Se um pedido precisar de um deles, chame pedir_conexao: o dono conecta ali mesmo na conversa."
	}
	return out
}

// agentFiles saves what a connector produces in the agent's folder.
type agentFiles struct {
	s       *Server
	agentID string
}

func (f agentFiles) Save(name string, data []byte) (string, error) {
	return f.s.saveBytes(f.agentID, name, data)
}

// runConnector runs a connector tool for the agent. ok is false for other tools.
func (s *Server) runConnector(ctx context.Context, t *turn, name, raw string) (out string, err error, ok bool) {
	if s.connectors == nil {
		return "", nil, false
	}
	con, tl, found := s.connectors.Tool(name)
	if !found {
		return "", nil, false
	}
	info := con.Info()
	args, err := connect.ParseArgs(raw)
	if err != nil {
		return "", err, true
	}
	want := args.Str("conta")
	delete(args, "conta")
	if r, isRouted := con.(connect.Routed); isRouted && want == "" {
		want = r.RouteOf(name, args)
	}
	conn, several, err := s.pickAccount(ctx, info, t.agent.ID, want)
	if err != nil {
		return "", err, true
	}
	tries := []store.Connection{conn}
	if _, pooled := con.(connect.Pooled); pooled && want == "" {
		tries = s.poolOrder(ctx, con, t.agent.ID)
	}
	for i, c := range tries {
		conn = c
		out, err = s.useConnection(ctx, con, conn, func(ctx context.Context, env *connect.Env) (string, error) {
			env.Files = agentFiles{s, t.agent.ID}
			env.Incoming = s.incomingFor(t.agent.ID)
			return con.Call(ctx, env, name, args)
		}, t.agent.ID, name)
		if !connect.IsCredit(err) {
			break
		}
		s.usage.spent(conn.ID, s.now())
		if i == len(tries)-1 && len(tries) > 1 {
			err = fmt.Errorf("todas as %d contas do %s estão sem crédito este mês", len(tries), info.Name)
		}
	}
	if err != nil {
		t.appFailed(info.Kind) // the browser may stand in for it now
	}
	if err == nil {
		summary := tl.SummaryFor(args)
		if several {
			summary += " · " + conn.Account
		}
		s.step(t, AgentStep{Tool: name, Summary: summary})
	}
	return out, err, true
}

// pickAccount chooses the account a call runs as: the one named in "conta"
// (by its label, or the account itself), else the app's first usable one.
// several says whether the agent had a choice.
func (s *Server) pickAccount(ctx context.Context, info connect.Info, agentID, want string) (store.Connection, bool, error) {
	conns, err := s.store.ListConnections(ctx)
	if err != nil {
		return store.Connection{}, false, err
	}
	_, by := accountsOf(conns)
	all := by[info.Kind]
	if len(all) == 0 {
		return store.Connection{}, false, fmt.Errorf("o %s não está conectado; peça ao dono para conectar em Plugins", info.Name)
	}
	usable := usableOnly(all, agentID)
	if want == "" {
		if len(usable) > 0 {
			return usable[0], len(usable) > 1, nil
		}
		if all[0].Status == "reconnect" {
			return store.Connection{}, false, fmt.Errorf("o %s precisa ser reconectado pelo dono em Plugins", info.Name)
		}
		if all[0].Status == "pending" {
			return store.Connection{}, false, fmt.Errorf("o %s aguarda conexão do dono em Plugins", info.Name)
		}
		return store.Connection{}, false, fmt.Errorf("o dono não liberou o %s para você", info.Name)
	}
	labels := accountLabels(all)
	match := -1
	for i, c := range all {
		if strings.EqualFold(labels[i], want) || strings.EqualFold(c.Account, want) || strings.EqualFold(c.ID, want) {
			match = i
			break
		}
	}
	if match < 0 {
		for i, c := range all {
			if c.Account != "" && strings.Contains(strings.ToLower(c.Account), strings.ToLower(want)) {
				match = i
				break
			}
		}
	}
	if match < 0 {
		return store.Connection{}, false, fmt.Errorf("o %s não tem a conta %q; as contas são: %s", info.Name, want, strings.Join(accountLabels(usable), ", "))
	}
	c := all[match]
	if !usableBy(c, agentID) {
		if c.Status == "reconnect" {
			return store.Connection{}, false, fmt.Errorf("a conta %s do %s precisa ser reconectada pelo dono em Plugins", c.Account, info.Name)
		}
		if c.Status == "pending" {
			return store.Connection{}, false, fmt.Errorf("o %s aguarda conexão do dono em Plugins", info.Name)
		}
		return store.Connection{}, false, fmt.Errorf("o dono não liberou a conta %s do %s para você", c.Account, info.Name)
	}
	return c, len(usable) > 1, nil
}

// useConnection opens the credential, runs fn and stores what changed: a
// refreshed token, the status, and a connector.called event without content.
func (s *Server) useConnection(ctx context.Context, con connect.Connector, conn store.Connection,
	fn func(context.Context, *connect.Env) (string, error), agentID, toolName string) (string, error) {
	cred, err := s.openCred(conn)
	if err != nil {
		return "", err
	}
	env := &connect.Env{Conn: conn.ID, Cred: &cred, Loc: s.zone(), Now: s.now}
	callCtx, cancel := context.WithTimeout(ctx, connectorTimeout)
	defer cancel()
	started := time.Now()
	out, err := fn(callCtx, env)
	now := s.now()
	before := conn.Status
	switch {
	case connect.IsAuth(err):
		conn.Status, conn.LastError = "reconnect", err.Error()
	case err == nil:
		conn.Status, conn.LastError, conn.LastOK = "ok", "", &now
	}
	if toolName == "" && err == nil {
		// A check names the account and, for an MCP app, reads its tools again.
		if out != "" {
			conn.Account = truncate(out, 200)
		}
		if st, ok := con.(Stateful); ok {
			conn.Meta = st.State()
		}
	}
	saved := false
	if env.Changed || conn.Status != before || err == nil {
		beforeSecret := conn.Secret
		if env.Changed {
			if sealed, serr := s.sealCred(conn.ID, cred); serr == nil {
				conn.Secret = sealed
			}
		}
		var serr error
		saved, serr = s.saveConnectionResult(context.WithoutCancel(ctx), con, conn, beforeSecret, toolName == "" && err == nil, now)
		if serr != nil {
			log.Printf("connection %s: %v", conn.ID, serr)
		}
	}
	if saved && conn.Status == "reconnect" && before != "reconnect" {
		s.announceReconnect(ctx, con.Info(), conn.Account, err)
	}
	if toolName != "" {
		payload := map[string]any{"kind": conn.Kind, "connection": conn.ID, "tool": toolName, "agent_id": agentID, "ok": err == nil,
			"duration_ms": time.Since(started).Milliseconds()}
		if err != nil {
			payload["error"] = truncate(err.Error(), 200)
		}
		b, _ := json.Marshal(payload)
		_ = s.store.RecordEvent(context.WithoutCancel(ctx), store.Event{At: now, Type: store.EventConnectorCalled, Actor: "agent:" + agentID, Payload: b})
	}
	if err != nil && ctx.Err() == nil && callCtx.Err() != nil {
		err = fmt.Errorf("o %s não respondeu em %s", con.Info().Name, connectorTimeout)
	}
	return out, err
}

// A plugin may be removed or reconnected while its call is running.
// Keep the current credential and permissions when an older call returns.
func (s *Server) saveConnectionResult(ctx context.Context, con connect.Connector, conn store.Connection, beforeSecret []byte, checked bool, now time.Time) (bool, error) {
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if current, ok := s.connectors.Get(conn.Kind); !ok || current != con {
		return false, nil
	}
	current, err := s.store.GetConnection(ctx, conn.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !bytes.Equal(beforeSecret, current.Secret) {
		return false, nil
	}
	conn.Agents = current.Agents
	if !checked {
		conn.Account, conn.Meta = current.Account, current.Meta
	}
	_, err = s.store.SaveConnection(ctx, conn, "", now)
	return err == nil, err
}

// announceReconnect tells the owner in #time that an app lost its access.
func (s *Server) announceReconnect(ctx context.Context, info connect.Info, account string, err error) {
	name := info.Name
	if account != "" {
		name += " (" + account + ")"
	}
	_, _ = s.store.AppendMessage(context.WithoutCancel(ctx), store.ChatMessage{
		ConversationID: TeamChannelID, Role: "system", CreatedAt: s.now(),
		Content: fmt.Sprintf("🔌 %s perdeu o acesso e precisa ser reconectado em Plugins. (%v)", name, err),
	}, store.ActorSystem)
}

func (s *Server) actionLabel(action string) string {
	if l, ok := ActionLabels[action]; ok {
		return l
	}
	if s.connectors != nil {
		if _, t, ok := s.connectors.Tool(action); ok {
			return t.Description
		}
	}
	return action
}

// ── API ─────────────────────────────────────────────────────────

// ConnectorView is one app of the Plugins page with its connected accounts
// (Connection is the first one, for older apps).
type ConnectorView struct {
	connect.Info
	Tools       []ConnectorToolView `json:"tools"`
	Connections []store.Connection  `json:"connections"`
	Connection  *store.Connection   `json:"connection"`
	// Usage is what each account has left (credits...), by connection id,
	// for apps that report it.
	Usage map[string]ConnectionUsage `json:"usage,omitempty"`
}

type ConnectorToolView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Risk        string `json:"risk"`
	Locked      bool   `json:"locked"`
}

// ConnectRequest carries a token typed on the Plugins page. Connection, when
// set, is the account being reconnected; otherwise the token adds an account
// (or refreshes the one it belongs to).
type ConnectRequest struct {
	Token string `json:"token"`
	// Fields are the values of an app with a form (connect.Info.Fields).
	Fields     map[string]string `json:"fields,omitempty"`
	Connection string            `json:"connection,omitempty"`
}

type ConnectorAgentsRequest struct {
	Agents []string `json:"agents"`
}

type OAuthStartRequest struct {
	RedirectURI string `json:"redirect_uri"`
	// Connection is the account being reconnected; empty adds one.
	Connection string `json:"connection,omitempty"`
}

type OAuthStartResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

type OAuthFinishRequest struct {
	State string `json:"state"`
	Code  string `json:"code"`
}

// OAuth is a connector the owner connects by consenting in the browser.
// BeginAuth may talk to the app (an MCP server signs Avora up); what it
// returns in extra comes back to FinishAuth.
type OAuth interface {
	BeginAuth(ctx context.Context, redirect, state, verifier string) (url string, extra map[string]string, err error)
	FinishAuth(ctx context.Context, code, redirect, verifier string, extra map[string]string, now time.Time) (connect.Cred, error)
}

// Stateful connectors keep something besides the credential between
// restarts (an MCP app's tools).
type Stateful interface {
	State() json.RawMessage
	Restore(raw []byte)
}

// fullList is a connector whose model-facing tools stand in for more.
type fullList interface{ AllTools() []connect.Tool }

type oauthPending struct {
	con                            connect.Connector
	connection, redirect, verifier string
	extra                          map[string]string
	expires                        time.Time
}

type oauthState struct {
	mu      sync.Mutex
	pending map[string]oauthPending
}

func (o *oauthState) add(state string, p oauthPending, now time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.pending == nil {
		o.pending = map[string]oauthPending{}
	}
	for k, v := range o.pending {
		if now.After(v.expires) {
			delete(o.pending, k)
		}
	}
	if len(o.pending) >= maxOAuthPending {
		return errors.New("too many logins in progress; try again in a few minutes")
	}
	o.pending[state] = p
	return nil
}

func (o *oauthState) take(state string, now time.Time) (oauthPending, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	p, ok := o.pending[state]
	delete(o.pending, state)
	return p, ok && now.Before(p.expires)
}

func (s *Server) registerConnectors(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/connectors", s.admin(s.listConnectors))
	mux.HandleFunc("PUT /v1/connectors/{kind}", s.admin(s.connectToken))
	mux.HandleFunc("DELETE /v1/connectors/{kind}", s.admin(s.disconnect))
	mux.HandleFunc("POST /v1/connectors/{kind}/test", s.admin(s.testConnector))
	mux.HandleFunc("PUT /v1/connectors/{kind}/agents", s.admin(s.connectorAgents))
	mux.HandleFunc("POST /v1/connectors/{kind}/oauth/start", s.admin(s.oauthStart))
	mux.HandleFunc("POST /v1/connectors/{kind}/oauth/finish", s.admin(s.oauthFinish))
	mux.HandleFunc("POST /v1/connectors/mcp", s.admin(s.addMCP))
	// The bridge authenticates with the run's own key, not the admin token.
	mux.HandleFunc("POST /v1/bridge/mcp", s.bridgeMCP)
}

func (s *Server) connectorOf(w http.ResponseWriter, r *http.Request) (connect.Connector, bool) {
	if s.connectors == nil {
		http.Error(w, "plugins are not configured on this server", http.StatusServiceUnavailable)
		return nil, false
	}
	// Checking a credential talks to the app; outlast the server's short write timeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute))
	con, ok := s.connectors.Get(r.PathValue("kind"))
	if !ok {
		http.Error(w, "unknown connector", http.StatusNotFound)
		return nil, false
	}
	return con, true
}

// connectorView is one app as the page shows it, its usage read fresh.
func (s *Server) connectorView(ctx context.Context, con connect.Connector) ConnectorView {
	return s.buildConnectorView(ctx, con, s.accounts(ctx, con.Info().Kind), true)
}

// buildConnectorView shows con with its accounts (the default first).
// waitUsage false takes usage from the cache and refreshes it behind.
func (s *Server) buildConnectorView(ctx context.Context, con connect.Connector, accounts []store.Connection, waitUsage bool) ConnectorView {
	v := ConnectorView{Info: con.Info(), Tools: []ConnectorToolView{}, Connections: []store.Connection{}}
	tools := con.Tools()
	if f, ok := con.(fullList); ok {
		tools = f.AllTools()
	}
	for _, t := range tools {
		v.Tools = append(v.Tools, ConnectorToolView{Name: t.Name, Description: t.Description, Risk: string(t.Risk), Locked: t.Risk.Locked()})
	}
	for _, c := range accounts {
		if c.Status != "pending" {
			v.Connections = append(v.Connections, c)
		}
	}
	if len(v.Connections) > 0 {
		v.Connection = &v.Connections[0]
	}
	v.Usage = s.connectionsUsage(ctx, con, v.Connections, waitUsage)
	return v
}

// accounts are the connections of one app, the default first.
func (s *Server) accounts(ctx context.Context, kind string) []store.Connection {
	out := []store.Connection{}
	conns, err := s.store.ListConnections(ctx)
	if err != nil {
		return out
	}
	if via := connect.Backed[kind]; via != "" {
		kind = via
	}
	for _, c := range conns {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// connectionOf is the account a request acts on: ?id=, or the app's first.
func (s *Server) connectionOf(w http.ResponseWriter, r *http.Request, kind string) (store.Connection, bool) {
	id := r.URL.Query().Get("id")
	if id == "" {
		for _, c := range s.accounts(r.Context(), kind) {
			if c.Status != "pending" {
				return c, true
			}
		}
		http.Error(w, "not connected", http.StatusNotFound)
		return store.Connection{}, false
	}
	c, err := s.store.GetConnection(r.Context(), id)
	if err == nil && (c.Kind != kind && c.Kind != connect.Backed[kind] || c.Status == "pending") {
		err = store.ErrNotFound
	}
	if err != nil {
		storeError(w, err)
		return store.Connection{}, false
	}
	return c, true
}

// sameAccount says whether a check's answer names the person: an MCP app
// answers with its server's name, so two of its accounts read the same.
func sameAccount(con connect.Connector, a, b string) bool {
	if _, isMCP := con.(*connect.MCPApp); isMCP || con.Info().Auth == "none" {
		return false
	}
	return a != "" && strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func (s *Server) listConnectors(w http.ResponseWriter, r *http.Request) {
	if s.connectors == nil {
		writeJSON(w, http.StatusOK, []ConnectorView{})
		return
	}
	// One query for every app's accounts (it was one per app), and usage
	// from the cache: the page must not wait on an app's API.
	byKind := map[string][]store.Connection{}
	if conns, err := s.store.ListConnections(r.Context()); err == nil {
		_, byKind = accountsOf(conns)
	}
	out := []ConnectorView{}
	for _, con := range s.connectors.All() {
		out = append(out, s.buildConnectorView(r.Context(), con, byKind[con.Info().Kind], false))
	}
	writeJSON(w, http.StatusOK, out)
}

// saveCredential proves a credential works and keeps it. target is the
// account being reconnected; empty adds an account, or refreshes the one
// the credential turns out to belong to (the same Gmail logged in twice).
func (s *Server) saveCredential(ctx context.Context, con connect.Connector, cred connect.Cred, target string) (store.Connection, error) {
	return s.saveCredentialAs(ctx, con, cred, target, store.ActorAdmin)
}

func (s *Server) saveCredentialAs(ctx context.Context, con connect.Connector, cred connect.Cred, target, actor string) (store.Connection, error) {
	account, err := s.checkCredential(ctx, con, &cred)
	if err != nil {
		return store.Connection{}, err
	}
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if current, ok := s.connectors.Get(con.Info().Kind); !ok || current != con {
		return store.Connection{}, errors.New("o plugin foi removido durante a conexão")
	}
	return s.saveCheckedCredential(ctx, con, cred, target, actor, account)
}

func (s *Server) checkCredential(ctx context.Context, con connect.Connector, cred *connect.Cred) (string, error) {
	info := con.Info()
	env := &connect.Env{Cred: cred, Loc: s.zone(), Now: s.now}
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	account, err := con.Check(cctx, env)
	if err != nil {
		return "", fmt.Errorf("o %s recusou: %v", info.Name, err)
	}
	return account, nil
}

func (s *Server) saveCheckedCredential(ctx context.Context, con connect.Connector, cred connect.Cred, target, actor, account string) (store.Connection, error) {
	if err := ctx.Err(); err != nil {
		return store.Connection{}, err
	}
	info := con.Info()
	account = truncate(account, 200)
	conns, err := s.store.ListConnections(ctx)
	if err != nil {
		return store.Connection{}, err
	}
	existing := slices.DeleteFunc(conns, func(c store.Connection) bool { return c.Kind != info.Kind })
	var old *store.Connection
	for i, c := range existing {
		switch {
		case target != "" && c.ID == target:
			old = &existing[i]
		case target == "" && (c.Status == "pending" || sameAccount(con, c.Account, account) || info.Auth == "none"):
			old = &existing[i]
		}
		if old != nil {
			break
		}
	}
	if target != "" {
		if old == nil {
			return store.Connection{}, errors.New("essa conta não está mais conectada; use Adicionar conta")
		}
		if _, isMCP := con.(*connect.MCPApp); !isMCP && old.Account != "" && !sameAccount(con, old.Account, account) {
			return store.Connection{}, fmt.Errorf("você entrou como %s, mas esta conexão é de %s; entre com %s, ou use Adicionar conta para conectar as duas", account, old.Account, old.Account)
		}
	}
	id := info.Kind
	if old != nil {
		id = old.ID
	} else {
		for _, c := range existing {
			if c.ID == id {
				id = info.Kind + "." + strings.ToLower(randomString(6))
				break
			}
		}
	}
	sealed, err := s.sealCred(id, cred)
	if err != nil {
		return store.Connection{}, err
	}
	now := s.now()
	conn := store.Connection{ID: id, Kind: info.Kind, Account: account, Status: "ok", LastOK: &now, Secret: sealed}
	if st, ok := con.(Stateful); ok {
		conn.Meta = st.State()
	}
	if old != nil {
		conn.Agents = old.Agents
	}
	s.usage.drop(id)
	return s.store.SaveConnection(ctx, conn, actor, now)
}

// viaOnly refuses changing an app that runs on another's accounts.
func viaOnly(w http.ResponseWriter, con connect.Connector) bool {
	if via := con.Info().Via; via != "" {
		http.Error(w, "o "+con.Info().Name+" usa as contas do "+via+": conecte, desconecte ou libere por lá", http.StatusBadRequest)
		return true
	}
	return false
}

func (s *Server) connectToken(w http.ResponseWriter, r *http.Request) {
	con, ok := s.connectorOf(w, r)
	if !ok || viaOnly(w, con) {
		return
	}
	if con.Info().Auth == "none" {
		// An open server: nothing to type, just check it and keep it.
		if _, err := s.saveCredential(r.Context(), con, connect.Cred{}, ""); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, s.connectorView(r.Context(), con))
		return
	}
	if con.Info().Auth != "token" {
		http.Error(w, "this connector connects through its login (oauth/start)", http.StatusBadRequest)
		return
	}
	var req ConnectRequest
	if !readJSONMax(w, r, &req, maxCredential) {
		return
	}
	if fields := con.Info().Fields; len(fields) > 0 {
		extra := map[string]string{}
		for _, f := range fields {
			v := strings.TrimSpace(req.Fields[f.Key])
			limit := 4096
			if f.File {
				limit = maxFileField
			}
			if v == "" && f.Optional {
				continue
			}
			if v == "" || len(v) > limit {
				http.Error(w, "preencha "+f.Label, http.StatusBadRequest)
				return
			}
			extra[f.Key] = v
		}
		if _, err := s.saveCredential(r.Context(), con, connect.Cred{Extra: extra}, req.Connection); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, s.connectorView(r.Context(), con))
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" || strings.ContainsAny(token, " \r\n\t") {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}
	if _, err := s.saveCredential(r.Context(), con, connect.Cred{Token: token}, req.Connection); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, s.connectorView(r.Context(), con))
}

func (s *Server) testConnector(w http.ResponseWriter, r *http.Request) {
	con, ok := s.connectorOf(w, r)
	if !ok {
		return
	}
	conn, ok := s.connectionOf(w, r, con.Info().Kind)
	if !ok {
		return
	}
	_, err := s.useConnection(r.Context(), con, conn, func(ctx context.Context, env *connect.Env) (string, error) {
		return con.Check(ctx, env)
	}, "", "")
	if err != nil && !connect.IsAuth(err) {
		conn.Status, conn.LastError = "error", truncate(err.Error(), 300)
		_, _ = s.saveConnectionResult(r.Context(), con, conn, conn.Secret, false, s.now())
	}
	writeJSON(w, http.StatusOK, s.connectorView(r.Context(), con))
}

func (s *Server) disconnect(w http.ResponseWriter, r *http.Request) {
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	con, ok := s.connectorOf(w, r)
	if !ok || viaOnly(w, con) {
		return
	}
	kind := con.Info().Kind
	conns, err := s.store.ListConnections(r.Context())
	if err != nil {
		storeError(w, err)
		return
	}
	_, by := accountsOf(conns)
	list := by[kind]
	id := r.URL.Query().Get("id")
	if id == "" {
		id = kind
		if len(list) > 0 {
			id = list[0].ID
		}
	}
	if c, err := s.store.GetConnection(r.Context(), id); err == nil && c.Kind != kind {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.usage.drop(id)
	err = s.store.DeleteConnection(r.Context(), id, store.ActorAdmin, s.now())
	if err != nil && !(errors.Is(err, store.ErrNotFound) && con.Info().Custom) {
		storeError(w, err)
		return
	}
	// An app added by address leaves the catalog with its last account.
	noAccountsLeft := len(list) == 0 || len(list) == 1 && list[0].ID == id
	if con.Info().Custom && noAccountsLeft {
		s.connectors.Remove(kind)
		s.removeLocalMCP(con)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) connectorAgents(w http.ResponseWriter, r *http.Request) {
	con, ok := s.connectorOf(w, r)
	if !ok || viaOnly(w, con) {
		return
	}
	var req ConnectorAgentsRequest
	if !readJSON(w, r, &req) {
		return
	}
	agents := []string{}
	for _, id := range req.Agents {
		if _, err := s.store.GetAgent(r.Context(), id); err != nil {
			http.Error(w, "unknown agent "+id, http.StatusBadRequest)
			return
		}
		if !slices.Contains(agents, id) {
			agents = append(agents, id)
		}
	}
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if current, ok := s.connectors.Get(con.Info().Kind); !ok || current != con {
		http.Error(w, "unknown connector", http.StatusNotFound)
		return
	}
	conn, ok := s.connectionOf(w, r, con.Info().Kind)
	if !ok {
		return
	}
	conn.Agents = agents
	if _, err := s.store.SaveConnection(r.Context(), conn, store.ActorAdmin, s.now()); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.connectorView(r.Context(), con))
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// loopbackRedirect accepts only the desktop app's own listener.
func loopbackRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Port() == "" {
		return false
	}
	return u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"
}

func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	con, ok := s.connectorOf(w, r)
	if !ok {
		return
	}
	oa, isOAuth := con.(OAuth)
	if !isOAuth {
		http.Error(w, "this connector takes a token", http.StatusBadRequest)
		return
	}
	if !con.Info().Ready {
		http.Error(w, con.Info().Help, http.StatusServiceUnavailable)
		return
	}
	var req OAuthStartRequest
	if !readJSON(w, r, &req) {
		return
	}
	if !loopbackRedirect(req.RedirectURI) {
		http.Error(w, "redirect_uri must be the app's loopback address (http://127.0.0.1:<port>/...)", http.StatusBadRequest)
		return
	}
	if req.Connection != "" {
		if c, err := s.store.GetConnection(r.Context(), req.Connection); err != nil || c.Kind != con.Info().Kind {
			http.Error(w, "unknown connection", http.StatusNotFound)
			return
		}
	}
	state, verifier := randomString(24), randomString(48)
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	authURL, extra, err := oa.BeginAuth(ctx, req.RedirectURI, state, verifier)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if !strings.HasPrefix(authURL, "https://") {
		http.Error(w, "o login do app não usa https", http.StatusBadGateway)
		return
	}
	if err := s.oauth.add(state, oauthPending{con: con, connection: req.Connection, redirect: req.RedirectURI, verifier: verifier, extra: extra, expires: s.now().Add(oauthTTL)}, s.now()); err != nil {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	writeJSON(w, http.StatusOK, OAuthStartResponse{URL: authURL, State: state})
}

func (s *Server) oauthFinish(w http.ResponseWriter, r *http.Request) {
	con, ok := s.connectorOf(w, r)
	if !ok {
		return
	}
	oa, isOAuth := con.(OAuth)
	if !isOAuth {
		http.Error(w, "this connector takes a token", http.StatusBadRequest)
		return
	}
	var req OAuthFinishRequest
	if !readJSON(w, r, &req) {
		return
	}
	p, ok := s.oauth.take(req.State, s.now())
	if !ok || p.con != con || strings.TrimSpace(req.Code) == "" {
		http.Error(w, "login expired or unknown; start again", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cred, err := oa.FinishAuth(ctx, req.Code, p.redirect, p.verifier, p.extra, s.now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.saveCredential(r.Context(), con, cred, p.connection); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, s.connectorView(r.Context(), con))
}

// incomingFor reads the credits into the owner's banks through the Pluggy
// accounts the agent may use, for confirming Pix charges; nil without one.
func (s *Server) incomingFor(agentID string) func(context.Context, time.Time) ([]connect.Incoming, error) {
	if s.connectors == nil {
		return nil
	}
	con, ok := s.connectors.Get("pluggy")
	if !ok {
		return nil
	}
	pl, ok := con.(*connect.Pluggy)
	if !ok {
		return nil
	}
	return func(ctx context.Context, since time.Time) ([]connect.Incoming, error) {
		conns, err := s.store.ListConnections(ctx)
		if err != nil {
			return nil, err
		}
		_, by := accountsOf(conns)
		usable := usableOnly(by["pluggy"], agentID)
		if len(usable) == 0 {
			return nil, errors.New("o Pluggy não está conectado para este funcionário; o dono conecta em Plugins (com o banco da chave Pix)")
		}
		all := []connect.Incoming{}
		for _, conn := range usable {
			var got []connect.Incoming
			_, err := s.useConnection(ctx, con, conn, func(ctx context.Context, env *connect.Env) (string, error) {
				var err error
				got, err = pl.Incoming(ctx, env, since)
				return "", err
			}, agentID, "pluggy_transacoes")
			if err != nil {
				return nil, err
			}
			all = append(all, got...)
		}
		return all, nil
	}
}
