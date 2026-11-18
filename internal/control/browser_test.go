package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/browser"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// fakeBrowserNode answers the server's browser calls like a runtime would.
func fakeBrowserNode(ctx context.Context, s *Server, token string, got *[]browser.Command, mu *sync.Mutex) {
	page := browser.Result{OK: true, URL: "https://www.facebook.com/marketplace/item/1", Title: "Casa 4 quartos",
		Text:     "Casa 4 quartos, R$ 2.100\n[1] link «Vendedor: Ana» [2] botão «Enviar mensagem» [3] caixa de texto «Escreva uma mensagem»",
		Elements: []browser.Element{{Kind: "link", Label: "Vendedor: Ana"}, {Kind: "botão", Label: "Enviar mensagem"}, {Kind: "caixa de texto", Label: "Escreva uma mensagem"}},
		Shot:     []byte("\xff\xd8\xff fake jpeg")}
	for ctx.Err() == nil {
		r := httptest.NewRequest("POST", "/v1/runtime/browser/next", nil).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			continue
		}
		var call BrowserCall
		_ = json.Unmarshal(w.Body.Bytes(), &call)
		mu.Lock()
		*got = append(*got, call.Command)
		mu.Unlock()
		b, _ := json.Marshal(BrowserAnswer{ID: call.ID, Result: page})
		r = httptest.NewRequest("POST", "/v1/runtime/browser/result", bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer "+token)
		s.Handler().ServeHTTP(httptest.NewRecorder(), r)
	}
}

func fakeAppBrowser(ctx context.Context, s *Server, agentID string, page browser.Result, got *[]BrowserCall, mu *sync.Mutex) {
	for ctx.Err() == nil {
		body, _ := json.Marshal(map[string]string{"agent": agentID})
		r := httptest.NewRequest("POST", "/v1/browser/app/next", bytes.NewReader(body)).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+adminTestToken)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			continue
		}
		var call BrowserCall
		_ = json.Unmarshal(w.Body.Bytes(), &call)
		mu.Lock()
		*got = append(*got, call)
		mu.Unlock()
		b, _ := json.Marshal(BrowserAnswer{ID: call.ID, Agent: agentID, Result: page})
		r = httptest.NewRequest("POST", "/v1/browser/app/result", bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer "+adminTestToken)
		s.Handler().ServeHTTP(httptest.NewRecorder(), r)
	}
}

func TestBrowserToolDrivesTheNodeAndGatesSending(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	s := chatServer(t, e)
	pc := pairNode(t, s, "meu-pc", "windows")
	c := agentConversation(t, s, "cora")
	// No browser yet: the tool is not offered.
	for _, tl := range s.toolsFor(&turn{agent: store.Agent{ID: "cora"}}) {
		if tl.Function.Name == "navegador" {
			t.Fatal("navegador offered with no browser")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var got []browser.Command
	go fakeBrowserNode(ctx, s, pc.Token, &got, &mu)
	for i := 0; i < 200 && s.browserNodeID(ctx) == ""; i++ {
		time.Sleep(5 * time.Millisecond)
	}

	setCalls(e, "Cora", call("navegador", map[string]any{"acao": "abrir", "url": "facebook.com/marketplace"}),
		call("navegador", map[string]any{"acao": "print"}))
	resp := <-sendAsync(t, s, c.ID, "acha casas no marketplace")
	if !strings.Contains(resp.Assistant.Content, "[2] botão «Enviar mensagem»") || !strings.Contains(resp.Assistant.Content, "](avora:file/") {
		t.Fatalf("answer: %q", resp.Assistant.Content)
	}
	id := resp.Assistant.Content[strings.Index(resp.Assistant.Content, "avora:file/")+len("avora:file/"):]
	id = id[:strings.IndexAny(id, ")")]
	if w := request(t, s, "GET", "/v1/files/"+id, adminTestToken, nil); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("file: %d", w.Code)
	}
	views := decode[[]BrowserView](t, request(t, s, "GET", "/v1/browser", adminTestToken, nil))
	if len(views) != 1 || !views[0].Ready || views[0].AgentID != "cora" || !views[0].Active || len(views[0].Log) != 2 {
		t.Fatalf("views: %+v", views)
	}
	if w := request(t, s, "GET", "/v1/browser/"+pc.NodeID+"/screen", adminTestToken, nil); w.Code != 200 {
		t.Fatalf("screen: %d", w.Code)
	}

	// Clicking «Enviar mensagem» waits for the owner.
	setCalls(e, "Cora", call("navegador", map[string]any{"acao": "clicar", "ref": 2}))
	answer := sendAsync(t, s, c.ID, "manda mensagem")
	ap := waitApproval(t, s)
	if ap.Action != "navegador.enviar" || !strings.Contains(ap.Summary, "«Enviar mensagem» em facebook.com") {
		t.Fatalf("approval: %+v", ap)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	<-answer
	mu.Lock()
	n := len(got)
	last := got[n-1]
	mu.Unlock()
	if n != 3 || last.Action != "clicar" || last.Ref != 2 || got[0].URL != "facebook.com/marketplace" {
		t.Fatalf("commands: %+v", got)
	}

	// The owner takes over: the agent waits.
	request(t, s, "POST", "/v1/browser/"+pc.NodeID+"/control", adminTestToken, map[string]bool{"owner": true})
	setCalls(e, "Cora", call("navegador", map[string]any{"acao": "ler"}))
	resp = <-sendAsync(t, s, c.ID, "lê")
	if !strings.Contains(resp.Assistant.Content, "o dono assumiu o navegador") {
		t.Fatalf("takeover: %q", resp.Assistant.Content)
	}
}

func TestBrowserPerAgentQueuesAndTakeover(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}}
	s := chatServer(t, e)
	coraConv := agentConversation(t, s, "cora")
	atlasConv := agentConversation(t, s, "atlas")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var coraCalls, atlasCalls []BrowserCall
	coraPage := browser.Result{OK: true, URL: "https://www.mercadolivre.com.br/p/1", Title: "Cadeira", Text: "Cadeira R$ 500", Shot: []byte("shot-cora")}
	atlasPage := browser.Result{OK: true, URL: "https://github.com/org/repo", Title: "Repo", Text: "Código do repo", Shot: []byte("shot-atlas")}

	go fakeAppBrowser(ctx, s, "cora", coraPage, &coraCalls, &mu)
	go fakeAppBrowser(ctx, s, "atlas", atlasPage, &atlasCalls, &mu)
	for i := 0; i < 200 && (s.browserNodeIDFor(ctx, "cora") == "" || s.browserNodeIDFor(ctx, "atlas") == ""); i++ {
		time.Sleep(5 * time.Millisecond)
	}

	setCalls(e, "Cora", call("navegador", map[string]any{"acao": "abrir", "url": "mercadolivre.com.br"}))
	setCalls(e, "Atlas", call("navegador", map[string]any{"acao": "abrir", "url": "github.com/org/repo"}))
	coraResp := <-sendAsync(t, s, coraConv.ID, "procura cadeira")
	atlasResp := <-sendAsync(t, s, atlasConv.ID, "abre o repo")
	if !strings.Contains(coraResp.Assistant.Content, "Cadeira R$ 500") || !strings.Contains(atlasResp.Assistant.Content, "Código do repo") {
		t.Fatalf("answers: cora=%q atlas=%q", coraResp.Assistant.Content, atlasResp.Assistant.Content)
	}

	mu.Lock()
	if len(coraCalls) != 1 || coraCalls[0].Agent != "cora" || len(atlasCalls) != 1 || atlasCalls[0].Agent != "atlas" {
		mu.Unlock()
		t.Fatalf("calls not routed by agent id: cora=%+v atlas=%+v", coraCalls, atlasCalls)
	}
	mu.Unlock()

	// Each agent sees its own URL, log and screen.
	coraViews := decode[[]BrowserView](t, request(t, s, "GET", "/v1/browser?agent=cora", adminTestToken, nil))
	atlasViews := decode[[]BrowserView](t, request(t, s, "GET", "/v1/browser?agent=atlas", adminTestToken, nil))
	if len(coraViews) != 1 || coraViews[0].URL != coraPage.URL || len(atlasViews) != 1 || atlasViews[0].URL != atlasPage.URL {
		t.Fatalf("isolated views: cora=%+v atlas=%+v", coraViews, atlasViews)
	}
	if w := request(t, s, "GET", "/v1/browser/app/screen?agent=cora", adminTestToken, nil); w.Code != 200 || w.Body.String() != "shot-cora" {
		t.Fatalf("cora screen: %d %q", w.Code, w.Body.String())
	}
	if w := request(t, s, "GET", "/v1/browser/app/screen?agent=atlas", adminTestToken, nil); w.Code != 200 || w.Body.String() != "shot-atlas" {
		t.Fatalf("atlas screen: %d %q", w.Code, w.Body.String())
	}

	// Taking over Cora's browser blocks only Cora; Atlas keeps working.
	request(t, s, "POST", "/v1/browser/app/control", adminTestToken, map[string]any{"agent": "cora", "take": true})
	coraViews = decode[[]BrowserView](t, request(t, s, "GET", "/v1/browser?agent=cora", adminTestToken, nil))
	atlasViews = decode[[]BrowserView](t, request(t, s, "GET", "/v1/browser?agent=atlas", adminTestToken, nil))
	if !coraViews[0].Owner || atlasViews[0].Owner {
		t.Fatalf("owner isolation: cora=%+v atlas=%+v", coraViews, atlasViews)
	}

	setCalls(e, "Cora", call("navegador", map[string]any{"acao": "ler"}))
	coraResp = <-sendAsync(t, s, coraConv.ID, "lê")
	if !strings.Contains(coraResp.Assistant.Content, "a pessoa está usando o navegador") {
		t.Fatalf("cora takeover message: %q", coraResp.Assistant.Content)
	}
	setCalls(e, "Atlas", call("navegador", map[string]any{"acao": "ler"}))
	atlasResp = <-sendAsync(t, s, atlasConv.ID, "lê")
	if !strings.Contains(atlasResp.Assistant.Content, "Código do repo") {
		t.Fatalf("atlas should not be blocked: %q", atlasResp.Assistant.Content)
	}

	// A queued agent command waits while taken, then runs once handed back.
	queuedDone := make(chan error, 1)
	go func() {
		_, err := s.browse(ctx, AppBrowser, "cora", "Cora", browser.Command{Action: "ler"}, false)
		queuedDone <- err
	}()
	select {
	case err := <-queuedDone:
		t.Fatalf("queued command ran while taken: %v", err)
	case <-time.After(60 * time.Millisecond):
	}
	request(t, s, "POST", "/v1/browser/app/control", adminTestToken, map[string]any{"agent": "cora", "take": false})
	select {
	case err := <-queuedDone:
		if err != nil {
			t.Fatalf("queued command failed after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued command did not resume after release")
	}
}
