package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
)

// toolBook keeps the tools each node reported in its last heartbeat. It
// lives in memory: a restarted server learns them within seconds.
type toolBook struct {
	mu     sync.Mutex
	byNode map[string][]ToolInfo
}

func (b *toolBook) set(nodeID string, tools []ToolInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byNode == nil {
		b.byNode = map[string][]ToolInfo{}
	}
	b.byNode[nodeID] = tools
}

func (b *toolBook) get(nodeID string) []ToolInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byNode[nodeID]
}

func (b *toolBook) has(nodeID, tool string) bool {
	for _, t := range b.get(nodeID) {
		if t.ID == tool {
			return true
		}
	}
	return false
}

// toolNodes are the online nodes with each tool.
func (s *Server) toolNodes(ctx context.Context) map[string][]store.Node {
	out := map[string][]store.Node{}
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return out
	}
	for _, n := range nodes {
		if !s.online(n) {
			continue
		}
		for _, t := range s.toolBook.get(n.ID) {
			out[t.ID] = append(out[t.ID], n)
		}
	}
	return out
}

func nodePlace(n store.Node) string {
	if n.Kind == scheduler.VPS {
		return "VPS"
	}
	return "PC"
}

// nodeToolsToolFor offers ferramenta for a specific agent, including tools
// cached from its home computer even while that computer is offline.
func (s *Server) nodeToolsToolFor(ctx context.Context, a store.Agent) *llm.Tool {
	if tl := s.nodeToolsTool(ctx); tl != nil {
		return tl
	}
	if strings.TrimSpace(a.HomeNodeID) == "" {
		return nil
	}
	n, err := s.nodeByID(ctx, strings.TrimSpace(a.HomeNodeID))
	if err != nil || n.RevokedAt != nil {
		return nil
	}
	cached := s.toolBook.get(n.ID)
	if len(cached) == 0 {
		return nil
	}
	have := map[string][]store.Node{}
	for _, ti := range cached {
		have[ti.ID] = []store.Node{n}
	}
	return buildNodeToolsTool(have)
}

// nodeToolsTool is the ferramenta tool, listing only what some online node
// has; nil when none has anything.
func (s *Server) nodeToolsTool(ctx context.Context) *llm.Tool {
	have := s.toolNodes(ctx)
	if len(have) == 0 {
		return nil
	}
	return buildNodeToolsTool(have)
}

func buildNodeToolsTool(have map[string][]store.Node) *llm.Tool {
	var b strings.Builder
	b.WriteString("Roda um programa instalado no PC do dono ou na VPS, por ações fixas (sem shell). " +
		"Arquivos são caminhos relativos à pasta onde roda: a pasta principal do seu projeto, se houver, ou a sua pasta de trabalho no nó. " +
		"Um arquivo de saída não pode existir ainda. O resultado diz os arquivos novos. " +
		"Downloads (yt-dlp) vão pelo PC quando ele está online: YouTube e outros sites bloqueiam o endereço da VPS. Disponível agora (ferramenta.acao(parâmetros) — onde):")
	ids := []string{}
	for _, t := range NodeToolCatalog {
		nodes := have[t.ID]
		if len(nodes) == 0 {
			continue
		}
		ids = append(ids, t.ID)
		places := map[string]bool{}
		for _, n := range nodes {
			places[nodePlace(n)] = true
		}
		where := []string{}
		for p := range places {
			where = append(where, p)
		}
		sort.Strings(where)
		for _, a := range t.Actions {
			params := []string{}
			for _, p := range a.Params {
				name := p.Name
				if p.Optional {
					name += "?"
				}
				params = append(params, name)
			}
			fmt.Fprintf(&b, "\n- %s.%s(%s): %s — %s", t.ID, a.Name, strings.Join(params, ", "), a.Desc, strings.Join(where, ", "))
		}
	}
	enum, _ := json.Marshal(ids)
	tl := tool("ferramenta", b.String(), `{"type": "object", "properties": {
	"ferramenta": {"type": "string", "enum": `+string(enum)+`},
	"acao": {"type": "string", "description": "A ação, ex.: baixar_video."},
	"args": {"type": "object", "description": "Os parâmetros da ação, ex.: {\"url\": \"https://...\", \"saida\": \"video.mp4\"}. Os com ? são opcionais."},
	"no": {"type": "string", "enum": ["pc", "vps"], "description": "Opcional: onde rodar. Vazio = na pasta do projeto, ou na VPS, ou no PC."},
	"projeto": {"type": "string", "description": "Opcional, para universais: rodar na pasta de um projeto."},
	"pasta": {"type": "string", "description": "Opcional: outra pasta do projeto (o nome dela)."}
}, "required": ["ferramenta", "acao"]}`)
	return &tl
}

// toolRoute picks the node and folder of a tool call.
func (s *Server) toolRoute(ctx context.Context, t *turn, a toolArgs) (store.Node, *store.AgentSpec, string, error) {
	homeNode, hasHome, herr := s.resolveHomeNode(ctx, t.agent)
	if herr != nil {
		return store.Node{}, nil, "", herr
	}
	if hasHome {
		if s.online(homeNode) && len(s.toolBook.get(homeNode.ID)) > 0 && !s.toolBook.has(homeNode.ID, a.Ferramenta) {
			return store.Node{}, nil, "", fmt.Errorf("%s não está instalado em %s", a.Ferramenta, homeNode.Name)
		}
		spec := &store.AgentSpec{Engine: "tool"}
		applyAgentHome(spec, t.agent)
		return homeNode, spec, "sua casa em " + homeNode.Name + " (" + nodePlace(homeNode) + ")", nil
	}

	have := s.toolNodes(ctx)[a.Ferramenta]
	if len(have) == 0 {
		info := ""
		for _, nt := range NodeToolCatalog {
			if nt.ID == a.Ferramenta {
				info = " Para instalar no PC: " + nt.Install["windows"] + "."
			}
		}
		return store.Node{}, nil, "", fmt.Errorf("nenhum nó online tem %s agora.%s", a.Ferramenta, info)
	}
	want := map[string]scheduler.Target{"pc": scheduler.Windows, "vps": scheduler.VPS}[strings.ToLower(strings.TrimSpace(a.No))]
	spec := &store.AgentSpec{Engine: "tool", Workdir: "agents/" + t.agent.ID}
	applyAgentIdentity(spec, t.agent)
	project, folder, err := s.projectFolder(ctx, t.agent, a.Projeto, a.Pasta)
	if err != nil {
		return store.Node{}, nil, "", err
	}
	if folder != nil {
		n, nerr := s.nodeByID(ctx, folder.Node)
		switch {
		case nerr != nil:
		case want != "" && n.Kind != want:
		case !s.online(n):
			if a.Projeto != "" || a.Pasta != "" {
				return store.Node{}, nil, "", fmt.Errorf("a pasta do projeto fica em %s, que está offline agora", n.Name)
			}
		case !s.toolBook.has(n.ID, a.Ferramenta):
			if a.Projeto != "" || a.Pasta != "" {
				return store.Node{}, nil, "", fmt.Errorf("%s não está instalado em %s, onde fica a pasta do projeto", a.Ferramenta, n.Name)
			}
		default:
			spec.Workdir = ""
			if n.Kind == scheduler.Windows {
				spec.Folder = folder.Path
			} else {
				spec.Workdir = folder.Path
			}
			return n, spec, "pasta do projeto " + project.Name + " (" + nodePlace(n) + ")", nil
		}
	}
	// Downloads go through the PC first: sites like YouTube block
	// datacenter addresses (the VPS gets "confirm you're not a bot").
	// Everything else prefers the VPS, which is always on.
	first := scheduler.VPS
	if _, action, _ := FindTool(a.Ferramenta, a.Acao); action.Net {
		first = scheduler.Windows
	}
	sort.SliceStable(have, func(i, j int) bool { return have[i].Kind == first && have[j].Kind != first })
	for _, n := range have {
		if want == "" || n.Kind == want {
			return n, spec, "sua pasta agents/" + t.agent.ID + " (" + nodePlace(n) + ")", nil
		}
	}
	return store.Node{}, nil, "", fmt.Errorf("%s não está instalado no %s", a.Ferramenta, strings.ToUpper(a.No))
}

func (s *Server) toolNodeTool(ctx context.Context, t *turn, a toolArgs) (string, error) {
	call := ToolCall{Tool: strings.TrimSpace(a.Ferramenta), Action: strings.TrimSpace(a.Acao), Args: a.Args}
	if call.Args == nil {
		call.Args = map[string]any{}
	}
	nt, action, found := FindTool(call.Tool, call.Action)
	if nt.ID == "" {
		return "", fmt.Errorf("não conheço a ferramenta %q", call.Tool)
	}
	if !found {
		names := []string{}
		for _, x := range nt.Actions {
			names = append(names, x.Name)
		}
		return "", fmt.Errorf("%s não tem a ação %q (as ações: %s)", call.Tool, call.Action, strings.Join(names, ", "))
	}
	if _, _, _, err := call.Argv(); err != nil {
		return "", err
	}
	node, spec, where, err := s.toolRoute(ctx, t, a)
	if err != nil {
		return "", err
	}
	spec.Prompt = EncodeToolCall(call)
	spec.MaxMinutes = max(action.Minutes, 1)
	spec.Mode = "offline"
	if action.Net {
		spec.Mode = "net"
	}
	label := call.Tool + "." + call.Action
	if u, ok := call.Clean["url"]; ok {
		label += " · " + truncate(u, 80)
	} else if in, ok := call.Clean["entrada"]; ok {
		label += " · " + in
	}
	s.step(t, AgentStep{Tool: "ferramenta", Summary: label + " · " + where})
	end, err := s.runNodeJob(ctx, t, spec, node.Kind, node.ID, "ferramenta "+label, "")
	if err != nil {
		return "", err
	}
	r := end.result
	switch {
	case r != nil && r.Success:
		return truncate("Rodou em "+where+".\n"+r.Output, maxToolResult), nil
	case r != nil:
		out := "Falhou (" + where + "): " + orDefault(r.Error, "a ferramenta falhou")
		if o := strings.TrimSpace(r.Output); o != "" {
			out += "\nSaída:\n" + o
		}
		return truncate(out, maxToolResult), nil
	case end.job.Status == store.StatusTimedOut:
		return "O nó parou de responder e a ferramenta foi encerrada.", nil
	}
	return "A ferramenta foi parada antes de terminar.", nil
}

// ── API: what each node has, for the app ─────────────────────────

type nodeToolStatus struct {
	NodeTool
	Nodes []nodeToolHave `json:"nodes"`
}

type nodeToolHave struct {
	NodeID  string `json:"node_id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Online  bool   `json:"online"`
	Version string `json:"version,omitempty"`
	Has     bool   `json:"has"`
}

func (s *Server) registerNodeTools(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/tools", s.admin(s.listNodeTools))
}

func (s *Server) listNodeTools(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListNodes(r.Context())
	if err != nil {
		storeError(w, err)
		return
	}
	out := []nodeToolStatus{}
	for _, t := range NodeToolCatalog {
		st := nodeToolStatus{NodeTool: t, Nodes: []nodeToolHave{}}
		for _, n := range nodes {
			if n.RevokedAt != nil || (n.Kind != scheduler.VPS && n.Kind != scheduler.Windows) {
				continue
			}
			h := nodeToolHave{NodeID: n.ID, Name: n.Name, Kind: string(n.Kind), Online: s.online(n)}
			for _, ti := range s.toolBook.get(n.ID) {
				if ti.ID == t.ID {
					h.Has, h.Version = true, ti.Version
				}
			}
			st.Nodes = append(st.Nodes, h)
		}
		out = append(out, st)
	}
	writeJSON(w, http.StatusOK, out)
}
