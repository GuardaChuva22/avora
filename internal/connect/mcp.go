package connect

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/mcp"
)

// MCPApp is an app reached through its official MCP server: the owner
// clicks Conectar, consents in the browser and the server's tools become
// the agents' tools. The risk of each tool comes from the server's hints
// and the tool's name; anything that sends, deletes or pays waits for the
// owner.
type MCPApp struct {
	Kind        string
	Name        string
	Description string
	URL         string
	Category    string
	Custom      bool
	// Help and HelpURL say how to get the token of a token app.
	Help    string
	HelpURL string
	// Auth is how the server is reached (mcp.AuthOAuth for the catalog).
	Auth mcp.Auth
	// Scope, when set, is what the login asks for instead of every scope
	// the server lists.
	Scope string
	// HTTP is for tests; nil means mcp.SafeClient.
	HTTP *http.Client
	// Local runs a stdio server through Avora's sandbox.
	Local MCPLocal

	mu     sync.RWMutex
	tools  []mcp.Tool
	server string
	built  []Tool
	all    []Tool
	byName map[string]mcp.Tool
	// clients keeps one MCP session per connected account.
	clients map[string]*mcp.Client
}

type MCPLocal interface {
	List(context.Context) (mcp.ServerInfo, []mcp.Tool, error)
	Call(context.Context, string, json.RawMessage) (mcp.CallResult, error)
}

// directTools is how many tools an app may give the model one by one;
// bigger apps come as a list plus a "use" tool, so twenty apps do not
// flood every turn with hundreds of schemas.
const directTools = 20

func (m *MCPApp) slug() string {
	s := strings.TrimPrefix(m.Kind, "mcp-")
	return strings.ReplaceAll(s, "-", "_")
}

func (m *MCPApp) auth() mcp.Auth {
	if m.Auth == "" {
		return mcp.AuthOAuth
	}
	return m.Auth
}

func (m *MCPApp) Info() Info {
	info := Info{Kind: m.Kind, Name: m.Name, Description: m.Description, Ready: true, Category: m.Category,
		Auth: string(m.auth()), URL: m.URL, Custom: m.Custom}
	switch m.auth() {
	case mcp.AuthOAuth:
		info.Help = "Clique em Conectar: o navegador abre o login do " + m.Name + ". Autorize e volte ao Avora."
	case mcp.AuthToken:
		info.Help, info.HelpURL = "Este servidor pede uma chave de API. Gere no "+m.Name+" e cole aqui.", m.HelpURL
		if m.Help != "" {
			info.Help = m.Help
		}
	default:
		info.Help = "Servidor aberto, sem login."
		if m.Local != nil {
			info.Help = "Servidor executado no sandbox da VPS."
		}
	}
	return info
}

// MCPState is what the server keeps about an MCP app between restarts.
type MCPState struct {
	URL    string     `json:"url,omitempty"`
	Name   string     `json:"name,omitempty"`
	Auth   mcp.Auth   `json:"auth,omitempty"`
	Server string     `json:"server,omitempty"`
	Tools  []mcp.Tool `json:"tools"`
}

func (m *MCPApp) State() json.RawMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, _ := json.Marshal(MCPState{URL: m.URL, Name: m.Name, Auth: m.auth(), Server: m.server, Tools: m.tools})
	return b
}

// Restore loads the tools seen at the last check.
func (m *MCPApp) Restore(raw []byte) {
	var st MCPState
	if json.Unmarshal(raw, &st) != nil {
		return
	}
	m.setTools(st.Server, st.Tools)
}

// MCPFromState rebuilds an app the owner added by address.
func MCPFromState(kind string, raw []byte) (*MCPApp, error) {
	var st MCPState
	if err := json.Unmarshal(raw, &st); err != nil || st.URL == "" {
		return nil, errors.New("estado do app MCP inválido")
	}
	m := &MCPApp{Kind: kind, Name: orStr(st.Name, kind), URL: st.URL, Auth: st.Auth, Custom: true, Category: "Seus apps",
		Description: "Servidor MCP em " + hostOf(st.URL)}
	m.Restore(raw)
	return m, nil
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Host
	}
	return raw
}

var nonName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// toolName is the name the model sees: the app's slug, then the tool's own
// name, at most 64 characters.
func (m *MCPApp) toolName(orig string) string {
	s := strings.Trim(nonName.ReplaceAllString(orig, "_"), "_")
	slug := m.slug()
	if !strings.HasPrefix(strings.ToLower(s), slug+"_") && !strings.HasPrefix(strings.ToLower(s), slug+"-") {
		s = slug + "_" + s
	}
	if len(s) > 64 {
		sum := sha256.Sum256([]byte(orig))
		s = s[:55] + "_" + hex.EncodeToString(sum[:])[:8]
	}
	return s
}

var (
	spendWords  = []string{"pay", "payment", "payout", "refund", "charge", "invoice", "purchase", "buy", "order", "transfer", "subscription", "checkout", "capture", "pagar", "pagamento", "reembolso", "cobranca", "cobrar", "comprar"}
	deleteWords = []string{"delete", "remove", "archive", "trash", "destroy", "purge", "revoke", "cancel", "unpublish", "apagar", "excluir", "remover", "arquivar"}
	sendWords   = []string{"send", "post", "publish", "reply", "comment", "message", "invite", "share", "email", "mail", "notify", "forward", "tweet", "dm", "react", "broadcast", "enviar", "publicar", "responder", "comentar", "convidar", "compartilhar"}
	readWords   = []string{"get", "list", "search", "find", "fetch", "read", "view", "query", "describe", "retrieve", "lookup", "show", "count", "check", "download", "export", "whoami", "filter", "ler", "buscar", "listar", "ver", "consultar"}
	writeWords  = []string{"create", "add", "update", "set", "edit", "move", "upload", "insert", "make", "start", "stop", "assign", "attach", "rename", "duplicate", "copy", "merge", "generate", "run", "execute", "apply", "track", "save", "write", "patch", "put", "mark", "complete", "close", "enable", "disable", "schedule", "book", "link", "tag", "convert", "import", "criar", "atualizar", "editar", "mover"}
	camel       = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

func words(name string) []string {
	name = camel.ReplaceAllString(name, "${1}_${2}")
	return strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') })
}

func isWord(w string, set []string) bool {
	for _, s := range set {
		if w == s || w == s+"s" || w == s+"es" || w == s+"d" || w == s+"ed" {
			return true
		}
	}
	return false
}

func hasAny(ws, set []string) bool {
	for _, w := range ws {
		if isWord(w, set) {
			return true
		}
	}
	return false
}

// MCPRisk classifies a server tool. The server's read-only and destructive
// hints win; otherwise the first verb in the name decides, and a create or
// update that sends, pays or deletes something on the way asks first.
func MCPRisk(t mcp.Tool) Risk {
	a := t.Annotations
	if a != nil && a.ReadOnlyHint != nil && *a.ReadOnlyHint {
		return Read
	}
	if a != nil && a.DestructiveHint != nil && *a.DestructiveHint {
		return Delete
	}
	ws := words(t.Name)
	verb := ""
	for _, w := range ws {
		if isWord(w, readWords) || isWord(w, writeWords) || isWord(w, sendWords) || isWord(w, deleteWords) || isWord(w, spendWords) {
			verb = w
			break
		}
	}
	switch {
	case isWord(verb, readWords):
		return Read
	case hasAny(ws, spendWords):
		return Spend
	case hasAny(ws, deleteWords):
		return Delete
	case hasAny(ws, sendWords):
		return Send
	}
	return Write
}

func shortDesc(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i > 0 && i < n {
		s = s[:i+1]
	}
	return cut(s, n)
}

// schema returns a tool's input schema, or an empty object schema.
func schema(t mcp.Tool) string {
	if len(t.InputSchema) == 0 || string(t.InputSchema) == "null" {
		return `{"type":"object","properties":{}}`
	}
	var m map[string]any
	if json.Unmarshal(t.InputSchema, &m) != nil {
		return `{"type":"object","properties":{}}`
	}
	if m["type"] == nil {
		m["type"] = "object"
	}
	if m["properties"] == nil {
		m["properties"] = map[string]any{}
	}
	delete(m, "$schema")
	b, _ := json.Marshal(m)
	return string(b)
}

func compactArgs(a Args, n int) string {
	if len(a) == 0 {
		return "sem argumentos"
	}
	b, _ := json.Marshal(a)
	return cut(string(b), n)
}

func (m *MCPApp) setTools(server string, tools []mcp.Tool) {
	sort.SliceStable(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	byName := map[string]mcp.Tool{}
	direct := []Tool{}
	for _, t := range tools {
		name := m.toolName(t.Name)
		if _, dup := byName[name]; dup {
			continue
		}
		byName[name] = t
		label := orStr(t.Title, orStr(func() string {
			if t.Annotations != nil {
				return t.Annotations.Title
			}
			return ""
		}(), t.Name))
		app := m.Name
		direct = append(direct, Tool{Name: name, Risk: MCPRisk(t), Params: schema(t),
			Description: "[" + app + "] " + shortDesc(t.Description, 900),
			Summary:     func(a Args) string { return app + " · " + label + ": " + compactArgs(a, 600) }})
	}
	built := direct
	if len(direct) > directTools {
		built = m.metaTools(direct)
	}
	m.mu.Lock()
	m.server = server
	m.tools, m.byName, m.built, m.all = tools, byName, built, direct
	m.mu.Unlock()
}

// metaTools stand in for a big app: one lists the tools and their
// arguments, the other runs one of them with the same approval it would
// have on its own.
func (m *MCPApp) metaTools(direct []Tool) []Tool {
	slug, app := m.slug(), m.Name
	index := map[string]Tool{}
	lines := []string{}
	for _, t := range direct {
		index[t.Name] = t
		mark := ""
		if t.Risk.Locked() {
			mark = " 🔒"
		}
		lines = append(lines, t.Name+mark+": "+shortDesc(strings.TrimPrefix(t.Description, "["+app+"] "), 90))
	}
	inner := func(a Args) (Tool, bool) {
		t, ok := index[a.Str("ferramenta")]
		return t, ok
	}
	innerArgs := func(a Args) Args {
		if v, ok := a["argumentos"].(map[string]any); ok {
			return Args(v)
		}
		return Args{}
	}
	return []Tool{
		{Name: slug + "_ferramentas", Risk: Read,
			Description: fmt.Sprintf("Mostra os argumentos de ferramentas do %s, para usar com %s_usar. Ferramentas (🔒 = pede aprovação):\n%s", app, slug, strings.Join(lines, "\n")),
			Params:      `{"type":"object","properties":{"nomes":{"type":"array","items":{"type":"string"},"description":"Nomes das ferramentas."},"busca":{"type":"string","description":"Ou uma palavra para achar ferramentas."}}}`,
			Summary:     func(a Args) string { return "ferramentas do " + app + quoted(strings.Join(a.List("nomes"), ", ")) }},
		{Name: slug + "_usar", Risk: Write,
			RiskOf: func(a Args) Risk {
				if t, ok := inner(a); ok {
					return t.Risk
				}
				return Write
			},
			Description: fmt.Sprintf("Executa uma ferramenta do %s. Veja os argumentos antes com %s_ferramentas.", app, slug),
			Params:      `{"type":"object","properties":{"ferramenta":{"type":"string"},"argumentos":{"type":"object"}},"required":["ferramenta"]}`,
			Summary: func(a Args) string {
				if t, ok := inner(a); ok {
					return t.SummaryFor(innerArgs(a))
				}
				return app + " · " + a.Str("ferramenta")
			}},
	}
}

// Tools are what the model gets.
func (m *MCPApp) Tools() []Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.built
}

// AllTools are every tool of the app, for the Plugins page.
func (m *MCPApp) AllTools() []Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.all
}

// mcpClient is the session of the call's account. A credential still being
// checked gets a session of its own that nobody keeps.
func (m *MCPApp) mcpClient(env *Env) *mcp.Client {
	if env == nil || env.Conn == "" {
		return &mcp.Client{URL: m.URL, HTTP: m.HTTP}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.clients == nil {
		m.clients = map[string]*mcp.Client{}
	}
	c, ok := m.clients[env.Conn]
	if !ok {
		c = &mcp.Client{URL: m.URL, HTTP: m.HTTP}
		m.clients[env.Conn] = c
	}
	return c
}

// resetSessions makes every account open a new session on its next call.
func (m *MCPApp) resetSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.clients {
		c.Reset()
	}
}

// token returns a valid access token, refreshing it when it is about to end.
func (m *MCPApp) token(ctx context.Context, env *Env, force bool) (string, error) {
	c := env.Cred
	if c == nil || m.auth() == mcp.AuthNone {
		return "", nil
	}
	if !force && c.Token != "" && (c.Expiry.IsZero() || env.now().Add(time.Minute).Before(c.Expiry)) {
		return c.Token, nil
	}
	if c.Refresh == "" || c.Extra["token_endpoint"] == "" {
		if force || c.Token == "" {
			return "", errReconnect
		}
		return c.Token, nil
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c.Refresh}}
	if r := c.Extra["resource"]; r != "" {
		form.Set("resource", r)
	}
	t, err := mcp.Grant(ctx, m.HTTP, c.Extra["token_endpoint"], form,
		mcp.Registration{ClientID: c.Extra["client_id"], ClientSecret: c.Extra["client_secret"]}, env.now())
	var g *mcp.GrantError
	if errors.As(err, &g) {
		return "", &AuthError{Msg: "o " + m.Name + " não renovou o acesso do Avora; reconecte em Plugins"}
	}
	if err != nil {
		return "", err
	}
	c.Token, c.Expiry = t.Access, t.Expiry
	if t.Refresh != "" {
		c.Refresh = t.Refresh
	}
	env.Changed = true
	m.mcpClient(env).Reset()
	return c.Token, nil
}

// with runs fn with a token, refreshing and retrying once on a 401.
func (m *MCPApp) with(ctx context.Context, env *Env, fn func(token string) error) error {
	tok, err := m.token(ctx, env, false)
	if err != nil {
		return err
	}
	err = fn(tok)
	if !mcp.IsUnauthorized(err) {
		return friendly(m.Name, err)
	}
	if tok, err = m.token(ctx, env, true); err != nil {
		return err
	}
	if err = fn(tok); mcp.IsUnauthorized(err) {
		return errReconnect
	}
	return friendly(m.Name, err)
}

func friendly(app string, err error) error {
	if err == nil {
		return nil
	}
	var h *mcp.HTTPError
	if errors.As(err, &h) {
		return &APIError{Status: h.Status, Body: cut(h.Body, 300)}
	}
	return fmt.Errorf("%s: %v", app, err)
}

func (m *MCPApp) Check(ctx context.Context, env *Env) (string, error) {
	var tools []mcp.Tool
	var info mcp.ServerInfo
	var err error
	if m.Local != nil {
		info, tools, err = m.Local.List(ctx)
	} else {
		client := m.mcpClient(env)
		err = m.with(ctx, env, func(tok string) error {
			var err error
			if info, err = client.Initialize(ctx, tok); err != nil {
				return err
			}
			tools, err = client.ListTools(ctx, tok)
			return err
		})
	}
	if err != nil {
		return "", err
	}
	return m.LoadTools(info, tools), nil
}

// LoadTools records the tools discovered during a successful connection.
func (m *MCPApp) LoadTools(info mcp.ServerInfo, tools []mcp.Tool) string {
	server := orStr(info.Title, info.Name)
	m.setTools(server, tools)
	return fmt.Sprintf("%s · %d ferramentas", orStr(server, m.Name), len(tools))
}

func (m *MCPApp) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	slug := m.slug()
	switch tool {
	case slug + "_ferramentas":
		return m.describe(a), nil
	case slug + "_usar":
		tool = a.Str("ferramenta")
		if v, ok := a["argumentos"].(map[string]any); ok {
			a = Args(v)
		} else {
			a = Args{}
		}
	}
	m.mu.RLock()
	t, ok := m.byName[tool]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("o %s não tem a ferramenta %s; veja a lista em %s_ferramentas", m.Name, tool, slug)
	}
	args, _ := json.Marshal(a)
	var res mcp.CallResult
	var err error
	if m.Local != nil {
		res, err = m.Local.Call(ctx, t.Name, args)
	} else {
		err = m.with(ctx, env, func(tok string) error {
			var err error
			res, err = m.mcpClient(env).CallTool(ctx, tok, t.Name, args)
			return err
		})
	}
	if err != nil {
		return "", err
	}
	text := m.resultText(env, res)
	if res.IsError {
		return "", fmt.Errorf("o %s respondeu com erro: %s", m.Name, cut(text, 1500))
	}
	return text, nil
}

func (m *MCPApp) describe(a Args) string {
	names := a.List("nomes")
	q := strings.ToLower(a.Str("busca"))
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []string{}
	for name, t := range m.byName {
		if len(names) > 0 {
			found := false
			for _, n := range names {
				found = found || n == name || n == t.Name
			}
			if !found {
				continue
			}
		} else if q != "" && !strings.Contains(strings.ToLower(name+" "+t.Description), q) {
			continue
		}
		lock := ""
		if MCPRisk(t).Locked() {
			lock = " (pede aprovação)"
		}
		out = append(out, fmt.Sprintf("## %s%s\n%s\nArgumentos: %s", name, lock, shortDesc(t.Description, 600), schema(t)))
	}
	sort.Strings(out)
	if len(out) == 0 {
		return "Nenhuma ferramenta com esse nome; os nomes estão na descrição de " + m.slug() + "_ferramentas."
	}
	if len(out) > 8 {
		out = append(out[:8], fmt.Sprintf("(mais %d; peça pelos nomes)", len(out)-8))
	}
	return strings.Join(out, "\n\n")
}

// resultText turns a result into text; files (images, audio) are saved in
// the agent's folder.
func (m *MCPApp) resultText(env *Env, res mcp.CallResult) string {
	parts := []string{}
	for i, c := range res.Content {
		switch c.Type {
		case "text":
			parts = append(parts, c.Text)
		case "image", "audio":
			parts = append(parts, m.saveBlob(env, c.Data, c.MimeType, fmt.Sprintf("%s-%d", c.Type, i)))
		case "resource_link":
			parts = append(parts, fmt.Sprintf("[%s] %s", orStr(c.Name, "link"), c.URI))
		case "resource":
			if c.Resource == nil {
				continue
			}
			if c.Resource.Text != "" {
				parts = append(parts, c.Resource.Text)
			} else if c.Resource.Blob != "" {
				parts = append(parts, m.saveBlob(env, c.Resource.Blob, c.Resource.MimeType, fmt.Sprintf("arquivo-%d", i)))
			} else {
				parts = append(parts, c.Resource.URI)
			}
		}
	}
	if len(parts) == 0 && len(res.StructuredContent) > 0 {
		parts = append(parts, string(res.StructuredContent))
	}
	if len(parts) == 0 {
		return "Feito (sem conteúdo na resposta)."
	}
	return strings.Join(parts, "\n")
}

func (m *MCPApp) saveBlob(env *Env, data, mimeType, base string) string {
	if env.Files == nil {
		return "[" + mimeType + " não salvo]"
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "[" + mimeType + " inválido]"
	}
	ext := ".bin"
	if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
		ext = exts[len(exts)-1]
	}
	name := fmt.Sprintf("%s/%s-%s%s", m.slug(), base, env.now().Format("20060102-150405"), ext)
	path, err := env.Files.Save(name, b)
	if err != nil {
		return "[" + mimeType + " não salvo: " + err.Error() + "]"
	}
	return "Arquivo salvo em " + path
}

// BeginAuth discovers the login, signs Avora up for this redirect and
// returns where the owner consents, plus what FinishAuth needs.
func (m *MCPApp) BeginAuth(ctx context.Context, redirect, state, verifier string) (string, map[string]string, error) {
	auth, md, err := mcp.Discover(ctx, m.HTTP, m.URL)
	if err != nil {
		return "", nil, err
	}
	if auth != mcp.AuthOAuth {
		return "", nil, fmt.Errorf("o %s não oferece login de um clique para apps como o Avora", m.Name)
	}
	if m.Scope != "" {
		md.Scope = m.Scope
	}
	reg, err := mcp.Register(ctx, m.HTTP, md, redirect)
	if err != nil {
		return "", nil, err
	}
	extra := map[string]string{"client_id": reg.ClientID, "client_secret": reg.ClientSecret,
		"token_endpoint": md.TokenEndpoint, "resource": md.Resource}
	return mcp.AuthURL(md, reg.ClientID, redirect, state, PKCE(verifier)), extra, nil
}

func (m *MCPApp) FinishAuth(ctx context.Context, code, redirect, verifier string, extra map[string]string, now time.Time) (Cred, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier}}
	if r := extra["resource"]; r != "" {
		form.Set("resource", r)
	}
	t, err := mcp.Grant(ctx, m.HTTP, extra["token_endpoint"], form,
		mcp.Registration{ClientID: extra["client_id"], ClientSecret: extra["client_secret"]}, now)
	if err != nil {
		return Cred{}, err
	}
	m.resetSessions()
	keep := map[string]string{}
	for _, k := range []string{"client_id", "client_secret", "token_endpoint", "resource"} {
		if extra[k] != "" {
			keep[k] = extra[k]
		}
	}
	return Cred{Token: t.Access, Refresh: t.Refresh, Expiry: t.Expiry, Extra: keep}, nil
}
