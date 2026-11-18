package control

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/mcp"
	"github.com/GuardaChuva22/avora/internal/store"
)

// MCP apps the owner adds by address, beyond the catalog. Any server that
// speaks MCP over HTTP works: with one-click login, with a pasted key or
// open.

// AddMCPRequest identifies a remote server or a GitHub stdio repository.
type AddMCPRequest struct {
	URL        string `json:"url"`
	Name       string `json:"name"`
	Repository string `json:"repository,omitempty"`
	Entry      string `json:"entry,omitempty"`
	Revision   string `json:"revision,omitempty"`
}

// mcpHTTP is the client MCP apps use; tests swap it for one that reaches
// local fakes.
var mcpHTTP *http.Client

var slugChars = regexp.MustCompile(`[^a-z0-9]+`)

// mcpSlug names an app after its host: mcp.foo.com and foo.com/mcp are "foo".
func mcpSlug(u *url.URL) string {
	skip := map[string]bool{"mcp": true, "www": true, "api": true, "app": true, "ai": true, "server": true}
	labels := strings.Split(strings.ToLower(u.Hostname()), ".")
	if len(labels) > 1 {
		labels = labels[:len(labels)-1] // the TLD
	}
	for _, l := range labels {
		if l = slugChars.ReplaceAllString(l, ""); l != "" && !skip[l] {
			return l
		}
	}
	return "app"
}

func (s *Server) addMCP(w http.ResponseWriter, r *http.Request) {
	if s.connectors == nil || s.vault == nil {
		http.Error(w, "plugins are not configured on this server", http.StatusServiceUnavailable)
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute))
	var req AddMCPRequest
	if !readJSON(w, r, &req) {
		return
	}
	var con connect.Connector
	var created bool
	var err error
	if req.Repository != "" || req.Entry != "" || req.Revision != "" {
		if strings.TrimSpace(req.URL) != "" {
			http.Error(w, "informe o endpoint MCP remoto ou o repositório GitHub", http.StatusBadRequest)
			return
		}
		cfg, label, draftErr := localMCPDraft(installMCPTool, mcpToolArgs{
			Nome: req.Name, Repositorio: req.Repository, Entrada: req.Entry, Revisao: req.Revision,
		})
		if draftErr != nil {
			http.Error(w, draftErr.Error(), http.StatusBadRequest)
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(localMCPInstallTimeout + time.Second))
		con, created, err = s.installLocalMCP(r.Context(), nil, store.ActorAdmin, cfg, label, "")
	} else {
		con, created, err = s.installRemoteMCP(r.Context(), req, store.ActorAdmin)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, s.connectorView(r.Context(), con))
}

func remoteMCPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("use o endereço HTTPS do servidor MCP (ex.: https://mcp.exemplo.com/mcp)")
	}
	if (strings.EqualFold(u.Hostname(), "www.npmjs.com") || strings.EqualFold(u.Hostname(), "npmjs.com")) && strings.HasPrefix(u.Path, "/package/") {
		return nil, errors.New("este endereço é uma página de pacote npm; use o endpoint HTTPS do MCP ou o repositório GitHub do projeto")
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u, nil
}

func (s *Server) connectorAt(addr string) connect.Connector {
	for _, con := range s.connectors.All() {
		if _, local := con.(*localMCPApp); local {
			continue
		}
		if endpoint := con.Info().URL; endpoint != "" && strings.TrimRight(endpoint, "/") == strings.TrimRight(addr, "/") {
			return con
		}
	}
	return nil
}

// installRemoteMCP is shared by Plugins and the chat installer. Save the
// connection (or its pending registration) before adding it to the catalog.
func (s *Server) installRemoteMCP(ctx context.Context, req AddMCPRequest, actor string) (connect.Connector, bool, error) {
	if s.connectors == nil {
		return nil, false, errors.New("plugins não estão configurados neste servidor")
	}
	u, err := remoteMCPURL(req.URL)
	if err != nil {
		return nil, false, err
	}
	addr := u.String()
	if con := s.connectorAt(addr); con != nil {
		info := con.Info()
		if !info.Custom && info.Auth == string(mcp.AuthNone) && len(s.accounts(ctx, info.Kind)) == 0 {
			if _, err := s.saveCredentialAs(ctx, con, connect.Cred{}, "", actor); err != nil {
				return nil, false, err
			}
		}
		return con, false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	auth, _, err := mcp.Discover(ctx, mcpHTTP, addr)
	if err != nil {
		return nil, false, err
	}
	slug := mcpSlug(u)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.ToUpper(slug[:1]) + slug[1:]
	}
	app := &connect.MCPApp{Kind: "mcp-" + slug, Name: truncate(name, 60), URL: addr, Auth: auth, Custom: true, HTTP: mcpHTTP,
		Category: "Seus apps", Description: "Servidor MCP em " + u.Host}
	var cred connect.Cred
	var account string
	if auth == mcp.AuthNone {
		account, err = s.checkCredential(ctx, app, &cred)
		if err != nil {
			return nil, false, err
		}
	}

	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if con := s.connectorAt(addr); con != nil {
		return con, false, nil
	}

	kind := app.Kind
	for n := 2; ; n++ {
		if _, taken := s.connectors.Get(kind); !taken {
			break
		}
		kind = fmt.Sprintf("mcp-%s-%d", slug, n)
	}
	if kind != app.Kind {
		app.Kind = kind
		// Rediscover the namespaced tool names under the final slug.
		app.Restore(app.State())
	}
	if auth == mcp.AuthNone {
		if _, err := s.saveCheckedCredential(ctx, app, cred, "", actor, account); err != nil {
			return nil, false, err
		}
	} else {
		pending := store.Connection{Kind: kind, Status: "pending", Secret: []byte{}, Meta: app.State()}
		if _, err := s.store.SaveConnection(ctx, pending, actor, s.now()); err != nil {
			return nil, false, err
		}
	}
	s.connectors.Add(app)
	return app, true, nil
}

// RestoreConnectors brings back what the connections remember: an MCP
// app's tools, and the apps the owner added by address.
func (s *Server) RestoreConnectors(ctx context.Context) error {
	if s.connectors == nil {
		return nil
	}
	conns, err := s.store.ListConnections(ctx)
	if err != nil {
		return err
	}
	for _, c := range conns {
		if len(c.Meta) == 0 || string(c.Meta) == "{}" {
			continue
		}
		if con, ok := s.connectors.Get(c.Kind); ok {
			if st, ok := con.(Stateful); ok {
				st.Restore(c.Meta)
			}
			continue
		}
		if s.restoreLocalMCP(c) {
			continue
		}
		if strings.HasPrefix(c.Kind, "mcp-") {
			app, err := connect.MCPFromState(c.Kind, c.Meta)
			if err != nil {
				log.Printf("plugin %s: %v", c.Kind, err)
				continue
			}
			app.HTTP = mcpHTTP
			s.connectors.Add(app)
		}
	}
	return nil
}
