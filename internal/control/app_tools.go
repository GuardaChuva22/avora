package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Apps on demand (D43): every connected app's tools in every round cost
// thousands of tokens and make the model pick worse (with 100+ tools it went
// to the browser for the owner's own Instagram). The system prompt lists the
// apps in one line each; an app's tools join the turn when the owner marks
// it with @, names it in the message, it was used in the last messages, or
// the agent calls ativar_app. Only the native engine resolves tools each
// round, so only it loads on demand; the others get every app.

// eagerAppTools: below this many app tools in all, load them all.
const eagerAppTools = 16

// recentAppMessages is how far back an app's use keeps it loaded.
const recentAppMessages = 8

const activateAppTool = "ativar_app"

// appAliases are the words that name an app in a message besides its name.
var appAliases = map[string][]string{
	"google":        {"gmail", "email", "e-mail", "emails", "e-mails", "drive", "agenda", "calendario", "reuniao", "reunioes", "planilha", "planilhas", "sheets", "google tasks", "contato", "contatos", "telefone de", "youtube", "canal"},
	"instagram":     {"insta", "ig", "direct", "directs"},
	"clickup-token": {"clickup", "click up"},
	"clickup":       {"click up"},
	"spotify":       {"musica", "musicas", "playlist"},
	"ifood":         {"comida", "delivery", "lanche"},
	"nfse":          {"nota fiscal", "notas fiscais", "nfs-e", "nfse"},
	"pluggy":        {"banco", "saldo", "extrato", "gastos", "fatura"},
	"pix":           {"cobranca", "cobrancas", "cobrar", "copia e cola", "qr code", "pagou", "pagaram"},
	"elevenlabs":    {"audio", "voz", "narracao", "transcrever"},
	"produtos":      {"google shopping", "shopping", "comprar", "preco", "precos", "mais barato", "produto", "produtos", "oferta", "ofertas", "comparar", "vale a pena", "review", "avaliacoes"},
	"mercadolivre":  {"mercado livre", "mercadolivre", "meli", "comprar", "preco", "precos", "mais barato", "produto", "produtos", "oferta", "ofertas"},
	"shopee":        {"comprar", "preco", "precos", "mais barato", "produto", "produtos", "oferta", "ofertas"},
	"aliexpress":    {"ali express", "comprar", "preco", "precos", "mais barato", "produto", "produtos", "oferta", "ofertas"},
	"apify":         {"tiktok", "raspar", "coletar", "scraper", "robo", "robos", "mercado livre", "mercadolivre", "shopee", "aliexpress", "ali express", "amazon", "comprar", "preco", "precos", "mais barato", "produto", "produtos", "oferta", "ofertas", "promocao", "promocoes", "loja", "lojas"},
	"maps":          {"maps", "google maps", "mapa", "rota", "rotas", "trajeto", "como chego", "como chegar", "distancia", "distancias", "transito", "perto de", "perto daqui", "restaurante", "restaurantes", "lugares", "endereco", "onde fica", "quanto tempo leva"},
}

// appTurn is what the turn knows about apps: which are loaded (nil: all)
// and which failed, so the browser may stand in for them.
type appTurn struct {
	loaded map[string]bool
	failed map[string]bool
}

func (t *turn) appLoaded(kind string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.apps.loaded == nil || t.apps.loaded[kind]
}

func (t *turn) loadApp(kind string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.apps.loaded == nil || t.apps.loaded[kind] {
		return false
	}
	t.apps.loaded[kind] = true
	return true
}

func (t *turn) appFailed(kind string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.apps.failed == nil {
		t.apps.failed = map[string]bool{}
	}
	t.apps.failed[kind] = true
}

func (t *turn) appHasFailed(kind string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.apps.failed[kind]
}

func (t *turn) onDemand() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.apps.loaded != nil
}

// usableApp is one app the agent can use now.
type usableApp struct {
	kind, name, desc string
	words            []string // folded, how the owner may call it
	tools            int
}

func (s *Server) usableApps(ctx context.Context, agentID string) []usableApp {
	if s.connectors == nil {
		return nil
	}
	conns, err := s.store.ListConnections(ctx)
	if err != nil {
		return nil
	}
	_, by := accountsOf(conns)
	out := []usableApp{}
	for _, con := range s.connectors.All() {
		info := con.Info()
		if len(usableOnly(by[info.Kind], agentID)) == 0 {
			continue
		}
		name := info.Name
		if len(info.Apps) > 0 {
			name = strings.Join(info.Apps, ", ")
		}
		words := []string{foldText(info.Name), foldText(info.Kind)}
		for _, a := range info.Apps {
			words = append(words, foldText(a))
		}
		words = append(words, appAliases[info.Kind]...)
		n := 0
		for _, tl := range con.Tools() {
			if !isMCPTool(tl.Name) {
				n++
			}
		}
		out = append(out, usableApp{kind: info.Kind, name: name, desc: info.Description, words: words, tools: n})
	}
	return out
}

// appOfTool is the app a tool belongs to ("" for the built-in ones).
func (s *Server) appOfTool(name string) string {
	if s.connectors == nil {
		return ""
	}
	if i := strings.LastIndex(name, " · "); i >= 0 {
		name = name[i+len(" · "):]
	}
	if con, _, ok := s.connectors.Tool(name); ok {
		return con.Info().Kind
	}
	return ""
}

func namesApp(text string, words []string) bool {
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		re := regexp.MustCompile(`(^|[^\pL\pN])@?` + regexp.QuoteMeta(w) + `($|[^\pL\pN])`)
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// startApps picks the apps the turn starts with. Only the native engine
// calls it; without it the turn has every app.
func (s *Server) startApps(ctx context.Context, t *turn, history []store.ChatMessage) {
	apps := s.usableApps(ctx, t.agent.ID)
	total := 0
	for _, a := range apps {
		total += a.tools
	}
	if total <= eagerAppTools {
		return
	}
	loaded := map[string]bool{}
	if msg, ok := lastOwnerMessage(history); ok {
		text := foldText(msg.Content)
		for _, a := range apps {
			if namesApp(text, a.words) {
				loaded[a.kind] = true
			}
		}
	}
	from := max(0, len(history)-recentAppMessages)
	for _, m := range history[from:] {
		if len(m.Steps) == 0 {
			continue
		}
		var steps []AgentStep
		if json.Unmarshal(m.Steps, &steps) != nil {
			continue
		}
		for _, st := range steps {
			if kind := s.appOfTool(st.Tool); kind != "" {
				loaded[kind] = true
			}
		}
	}
	t.mu.Lock()
	t.apps.loaded = loaded
	t.mu.Unlock()
}

// activateTool offers the apps not loaded yet.
func (s *Server) activateTool(ctx context.Context, t *turn) []llm.Tool {
	if !t.onDemand() {
		return nil
	}
	kinds, lines := []string{}, []string{}
	for _, a := range s.usableApps(ctx, t.agent.ID) {
		if t.appLoaded(a.kind) {
			continue
		}
		kinds = append(kinds, a.kind)
		lines = append(lines, a.kind+" = "+a.name)
	}
	if len(kinds) == 0 {
		return nil
	}
	params, _ := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{
		"app": map[string]any{"type": "string", "enum": kinds, "description": "O app: " + strings.Join(lines, "; ") + "."},
	}, "required": []string{"app"}})
	return []llm.Tool{tool(activateAppTool, "Carrega as ferramentas de um app conectado do dono (a lista de apps está nas suas instruções). Chame antes de usar um app cujas ferramentas você ainda não tem; elas aparecem na próxima rodada. Para os apps do dono, use sempre isto em vez do navegador.", string(params))}
}

func (s *Server) toolActivateApp(ctx context.Context, t *turn, raw string) (string, error) {
	var a struct {
		App string `json:"app"`
	}
	_ = json.Unmarshal([]byte(raw), &a)
	kind := strings.TrimSpace(a.App)
	var app *usableApp
	for _, u := range s.usableApps(ctx, t.agent.ID) {
		if u.kind == kind || foldText(u.name) == foldText(kind) || slices.Contains(u.words, foldText(kind)) {
			app = &u
			break
		}
	}
	if app == nil {
		return "", fmt.Errorf("o app %q não está conectado para você; se precisar dele, chame pedir_conexao", a.App)
	}
	t.loadApp(app.kind)
	con, _ := s.connectors.Get(app.kind)
	names := []string{}
	for _, tl := range con.Tools() {
		if !isMCPTool(tl.Name) {
			names = append(names, tl.Name)
		}
	}
	s.step(t, AgentStep{Tool: activateAppTool, Summary: "carregou " + app.name})
	return fmt.Sprintf("Pronto: as ferramentas do %s já estão com você (%s). Use-as agora.", app.name, strings.Join(names, ", ")), nil
}

// ── The browser guard ───────────────────────────────────────────

// appSites are the pages of an app the connector already covers: the
// browser does not go there while the app is connected for the agent.
// A nil path list means the whole site.
var appSites = map[string][]struct {
	host  string
	paths []string
}{
	"instagram":     {{"instagram.com", []string{"/direct", "/accounts", "/insights"}}},
	"google":        {{"mail.google.com", nil}, {"gmail.com", nil}, {"drive.google.com", nil}, {"calendar.google.com", nil}, {"docs.google.com", []string{"/spreadsheets", "/document"}}, {"contacts.google.com", nil}, {"tasks.google.com", nil}, {"studio.youtube.com", nil}},
	"clickup-token": {{"app.clickup.com", nil}},
	"clickup":       {{"app.clickup.com", nil}},
	"spotify":       {{"open.spotify.com", nil}, {"accounts.spotify.com", nil}},
	"tinder":        {{"tinder.com", nil}},
	"mercadolivre":  {{"lista.mercadolivre.com.br", nil}},
	"shopee":        {{"shopee.com.br", []string{"/search"}}},
	"aliexpress":    {{"aliexpress.com", []string{"/w"}}, {"pt.aliexpress.com", []string{"/w"}}},
	"maps":          {{"google.com", []string{"/maps"}}, {"maps.google.com", nil}, {"maps.app.goo.gl", nil}},
	"ifood":         {{"ifood.com.br", nil}},
	"discord":       {{"discord.com", []string{"/channels"}}},
	"notion":        {{"notion.so", nil}, {"notion.com", nil}},
	"linear":        {{"linear.app", nil}},
	"asana":         {{"app.asana.com", nil}},
	"todoist":       {{"app.todoist.com", nil}},
	"github":        {{"github.com", []string{"/notifications", "/pulls", "/issues"}}},
}

func siteMatches(u *url.URL, host string, paths []string) bool {
	h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if h != host && !strings.HasSuffix(h, "."+host) {
		return false
	}
	if paths == nil {
		return true
	}
	p := strings.ToLower(u.Path)
	for _, want := range paths {
		if p == want || strings.HasPrefix(p, want+"/") {
			return true
		}
	}
	return false
}

// ownInstagram is the path of the owner's own profile (instagram.com/<user>).
func ownInstagram(account string) string {
	acc := strings.ToLower(strings.TrimSpace(account))
	if i := strings.Index(acc, "@"); i >= 0 {
		acc = acc[i+1:]
	}
	if i := strings.IndexAny(acc, " ·,("); i >= 0 {
		acc = acc[:i]
	}
	if acc == "" {
		return ""
	}
	return "/" + acc
}

// browserForApp refuses to open, in the browser, a page of an app connected
// for the agent, and says which tools to use instead. The owner asking for
// the browser with @Navegador, or the app's tools failing in this turn,
// lets it through.
func (s *Server) browserForApp(ctx context.Context, t *turn, raw string) string {
	a := parseBrowserArgs(raw)
	if a.Acao != "abrir" || strings.TrimSpace(a.URL) == "" || t.askedBrowser {
		return ""
	}
	addr := strings.TrimSpace(a.URL)
	if !strings.Contains(addr, "://") {
		addr = "https://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return ""
	}
	if s.whatsappReady() && siteMatches(u, "web.whatsapp.com", nil) {
		return "Recusado: o WhatsApp do dono está conectado; use whatsapp_conversas, whatsapp_ler e whatsapp_enviar em vez do navegador."
	}
	if s.connectors == nil {
		return ""
	}
	conns, err := s.store.ListConnections(ctx)
	if err != nil {
		return ""
	}
	_, by := accountsOf(conns)
	kinds := make([]string, 0, len(appSites))
	for k := range appSites {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		usable := usableOnly(by[kind], t.agent.ID)
		if len(usable) == 0 || t.appHasFailed(kind) || !appSiteHit(u, kind, usable) {
			continue
		}
		con, ok := s.connectors.Get(kind)
		if !ok {
			continue
		}
		names := []string{}
		for _, tl := range con.Tools() {
			if !isMCPTool(tl.Name) {
				names = append(names, tl.Name)
			}
		}
		t.loadApp(kind)
		return fmt.Sprintf("Recusado: o %s do dono está conectado e essa página é coberta pelo conector. Use as ferramentas dele (%s), que já estão com você, em vez do navegador. Só volte ao navegador se elas falharem ou não fizerem o que o pedido precisa (ex.: o perfil de outra pessoa).",
			con.Info().Name, strings.Join(names, ", "))
	}
	return ""
}

// appSiteHit: u is a page of the app that its connector covers. For
// Instagram that is also the owner's own profile.
func appSiteHit(u *url.URL, kind string, usable []store.Connection) bool {
	for _, site := range appSites[kind] {
		paths := site.paths
		if kind == "instagram" {
			paths = slices.Clone(paths)
			for _, c := range usable {
				if own := ownInstagram(c.Account); own != "" {
					paths = append(paths, own)
				}
			}
		}
		if siteMatches(u, site.host, paths) {
			return true
		}
	}
	return false
}

// ownerAskedBrowser: the owner marked @Navegador in the message.
func ownerAskedBrowser(history []store.ChatMessage) bool {
	msg, ok := lastOwnerMessage(history)
	return ok && len(mentioned(msg.Content, builtinMentions[:1])) > 0
}
