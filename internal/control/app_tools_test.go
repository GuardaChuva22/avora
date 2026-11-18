package control

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/store"
	"github.com/GuardaChuva22/avora/internal/vault"
)

func appsServer(t *testing.T, cons ...connect.Connector) *Server {
	t.Helper()
	s := chatServer(t, nil)
	s.SetConnectors(connect.NewRegistry(cons...), vault.Random())
	for _, c := range cons {
		account := "conta"
		if c.Info().Kind == "instagram" {
			account = "@gui.dono"
		}
		if _, err := s.store.SaveConnection(t.Context(), store.Connection{Kind: c.Info().Kind, Account: account, Status: "ok"}, "", s.now()); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func toolNames(s *Server, tr *turn) []string {
	out := []string{}
	for _, tl := range s.toolsFor(tr) {
		out = append(out, tl.Function.Name)
	}
	return out
}

func TestAppsLoadOnDemand(t *testing.T) {
	s := appsServer(t, &connect.Instagram{}, &connect.Spotify{}, &connect.Tinder{})
	tr := &turn{agent: store.Agent{ID: "cora"}}
	s.startApps(t.Context(), tr, []store.ChatMessage{{Role: "user", Content: "me resume as DMs do insta"}})
	names := toolNames(s, tr)
	if !slices.Contains(names, "instagram_conversas") || slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, "spotify_") || strings.HasPrefix(n, "tinder_") }) {
		t.Fatalf("named app only: %v", names)
	}
	if !slices.Contains(names, activateAppTool) {
		t.Fatalf("no %s: %v", activateAppTool, names)
	}
	out, err := s.toolActivateApp(t.Context(), tr, `{"app":"spotify"}`)
	if err != nil || !strings.Contains(out, "spotify_") {
		t.Fatalf("activate: %q %v", out, err)
	}
	if names := toolNames(s, tr); !slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, "spotify_") }) {
		t.Fatalf("spotify not loaded: %v", names)
	}
	if _, err := s.toolActivateApp(t.Context(), tr, `{"app":"notion"}`); err == nil {
		t.Fatal("an app not connected cannot be activated")
	}

	// An app used in the last messages stays loaded.
	tinder := ""
	for _, tl := range (&connect.Tinder{}).Tools() {
		tinder = tl.Name
		break
	}
	steps, _ := json.Marshal([]AgentStep{{Tool: tinder, Summary: "leu os matches"}})
	tr = &turn{agent: store.Agent{ID: "cora"}}
	s.startApps(t.Context(), tr, []store.ChatMessage{
		{Role: "user", Content: "vê meus matches"}, {Role: "assistant", Content: "pronto", Steps: steps},
		{Role: "user", Content: "e agora?"}})
	if !tr.appLoaded("tinder") || tr.appLoaded("instagram") || tr.appLoaded("spotify") {
		t.Fatalf("loaded: %v", tr.apps.loaded)
	}

	// Few app tools: everything goes, no ativar_app.
	s = appsServer(t, &connect.Instagram{})
	tr = &turn{agent: store.Agent{ID: "cora"}}
	s.startApps(t.Context(), tr, []store.ChatMessage{{Role: "user", Content: "oi"}})
	if names := toolNames(s, tr); !slices.Contains(names, "instagram_perfil") || slices.Contains(names, activateAppTool) {
		t.Fatalf("eager: %v", names)
	}
}

func TestBrowserRefusesConnectedApps(t *testing.T) {
	s := appsServer(t, &connect.Instagram{}, &connect.Spotify{})
	tr := &turn{agent: store.Agent{ID: "cora"}}
	open := func(u string) string { return s.browserForApp(t.Context(), tr, `{"acao":"abrir","url":"`+u+`"}`) }
	for _, u := range []string{"https://www.instagram.com/direct/inbox/", "instagram.com/gui.dono", "https://open.spotify.com/playlist/1"} {
		if r := open(u); !strings.HasPrefix(r, "Recusado") {
			t.Fatalf("%s: %q", u, r)
		}
	}
	if r := open("https://www.instagram.com/direct/inbox/"); !strings.Contains(r, "instagram_conversas") {
		t.Fatalf("names the tools: %q", r)
	}
	for _, u := range []string{"https://www.instagram.com/outra.pessoa/", "https://www.facebook.com/marketplace", "https://tinder.com"} {
		if r := open(u); r != "" {
			t.Fatalf("%s refused: %q", u, r)
		}
	}
	if r := s.browserForApp(t.Context(), tr, `{"acao":"ler"}`); r != "" {
		t.Fatalf("reading is not opening: %q", r)
	}
	// The connector failed in this turn: the browser may stand in.
	tr.appFailed("instagram")
	if r := open("https://www.instagram.com/direct/inbox/"); r != "" {
		t.Fatalf("after failure: %q", r)
	}
	// The owner asked for the browser with @Navegador.
	tr = &turn{agent: store.Agent{ID: "cora"}, askedBrowser: ownerAskedBrowser([]store.ChatMessage{{Role: "user", Content: "abre no @navegador o meu spotify"}})}
	if r := s.browserForApp(t.Context(), tr, `{"acao":"abrir","url":"https://open.spotify.com"}`); r != "" {
		t.Fatalf("@Navegador: %q", r)
	}
}

func TestAntigravityRunsOnThePCOnly(t *testing.T) {
	s := chatServer(t, nil)
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)
	if _, err := s.routeEngine(t.Context(), "antigravity"); err == nil {
		t.Fatal("Antigravity must not run on the VPS")
	}
	pc := pairNode(t, s, "meu-pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	r, err := s.routeEngine(t.Context(), "antigravity")
	if err != nil || r.node != pc.NodeID {
		t.Fatalf("route: %+v %v", r, err)
	}
	if !slices.Contains(toolNames(s, &turn{agent: store.Agent{ID: "cora"}}), "antigravity") {
		t.Fatal("no antigravity tool")
	}
	if action, locked := policyOf("antigravity", toolArgs{PodeEditar: true}); action != "antigravity.edit" || !locked {
		t.Fatalf("policy: %s %v", action, locked)
	}
}
