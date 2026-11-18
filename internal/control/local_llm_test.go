package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestLocalLLMQueueClientStreamsAndListsModels(t *testing.T) {
	s, _ := makeServer(t)
	pc := pairNode(t, s, "PC Local", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, HeartbeatRequest{
		LocalModels: []LocalModelReport{
			{Kind: "ollama", URL: "http://127.0.0.1:11434/v1", Models: []string{"llama3.2", "qwen2.5-coder"}},
		},
		SharedDir: `C:\Avora\work\shared`,
	})

	// GET /v1/nodes exposes the detected local models and shared directory.
	nodes := decode[[]NodeStatus](t, request(t, s, "GET", "/v1/nodes", adminTestToken, nil))
	if len(nodes) != 1 || len(nodes[0].LocalModels) != 1 || nodes[0].LocalModels[0].Kind != "ollama" ||
		nodes[0].SharedDir != `C:\Avora\work\shared` {
		t.Fatalf("unexpected node status: %+v", nodes)
	}

	// Fake runner loop backed by httptest: answers /v1/runtime/llm/next with /v1/runtime/llm/result chunks.
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	fakeOllama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			writeJSON(w, http.StatusOK, map[string]any{
				"data": []map[string]string{{"id": "qwen2.5-coder"}, {"id": "llama3.2"}},
			})
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			pieces := []string{
				`data: {"model":"llama3.2","choices":[{"delta":{"content":"Olá"}}]}`,
				`data: {"model":"llama3.2","choices":[{"delta":{"content":", modelo "}}]}`,
				`data: {"model":"llama3.2","choices":[{"delta":{"content":"local!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":5}}`,
				`data: [DONE]`,
			}
			for _, line := range pieces {
				_, _ = fmt.Fprintln(w, line)
				_, _ = fmt.Fprintln(w)
				if flusher != nil {
					flusher.Flush()
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeOllama.Close()

	stopRunner := make(chan struct{})
	var runnerWG sync.WaitGroup
	runnerWG.Add(1)
	go func() {
		defer runnerWG.Done()
		httpClient := &http.Client{Timeout: 5 * time.Second}
		for {
			select {
			case <-stopRunner:
				return
			default:
			}
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/runtime/llm/next", nil)
			req.Header.Set("Authorization", "Bearer "+pc.Token)
			resp, err := httpClient.Do(req)
			if err != nil {
				return
			}
			if resp.StatusCode == http.StatusNoContent {
				resp.Body.Close()
				continue
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return
			}
			var call LocalLLMCall
			_ = json.NewDecoder(resp.Body).Decode(&call)
			resp.Body.Close()

			postPiece := func(res LocalLLMResult) {
				b, _ := json.Marshal(res)
				pr, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/runtime/llm/result", bytes.NewReader(b))
				pr.Header.Set("Authorization", "Bearer "+pc.Token)
				pr.Header.Set("Content-Type", "application/json")
				if r2, err := httpClient.Do(pr); err == nil {
					r2.Body.Close()
				}
			}

			if call.Kind == "models" {
				r2, err := httpClient.Get(fakeOllama.URL + "/v1/models")
				if err != nil {
					postPiece(LocalLLMResult{ID: call.ID, Seq: 1, Error: err.Error(), Done: true})
					continue
				}
				var buf bytes.Buffer
				_, _ = buf.ReadFrom(r2.Body)
				r2.Body.Close()
				postPiece(LocalLLMResult{ID: call.ID, Seq: 1, Status: r2.StatusCode, Chunk: buf.String(), Done: true})
				continue
			}

			r2, err := httpClient.Post(fakeOllama.URL+"/v1/chat/completions", "application/json", bytes.NewReader(call.Body))
			if err != nil {
				postPiece(LocalLLMResult{ID: call.ID, Seq: 1, Error: err.Error(), Done: true})
				continue
			}
			var raw bytes.Buffer
			_, _ = raw.ReadFrom(r2.Body)
			r2.Body.Close()
			seq := 0
			for _, line := range strings.Split(raw.String(), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "data:") {
					seq++
					postPiece(LocalLLMResult{ID: call.ID, Seq: seq, Status: http.StatusOK, Chunk: line + "\n\n", Done: false})
				}
			}
			postPiece(LocalLLMResult{ID: call.ID, Seq: seq + 1, Status: http.StatusOK, Done: true})
		}
	}()
	defer func() {
		close(stopRunner)
	}()

	// Connect button endpoint creates the provider with node_id.
	w := request(t, s, "POST", "/v1/nodes/"+pc.NodeID+"/providers", adminTestToken, ConnectLocalModelRequest{
		Kind:    "ollama",
		BaseURL: "http://127.0.0.1:11434/v1",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("connect local model: %d %s", w.Code, w.Body.String())
	}
	pv := decode[ProviderView](t, w)
	if pv.NodeID != pc.NodeID || pv.Kind != "ollama" || !reflect.DeepEqual(pv.Models, []string{"llama3.2", "qwen2.5-coder"}) {
		t.Fatalf("connected provider: %+v", pv)
	}

	// Direct LocalLLMClient test: Models + streamed Chat deltas.
	// Deliberately point BaseURL at an unreachable port on the server so direct calls would fail.
	prov := pv.Provider
	prov.BaseURL = "http://127.0.0.1:1/v1"
	client := s.NewLocalLLMClient(prov)

	models, err := client.Models(context.Background())
	if err != nil || !reflect.DeepEqual(models, []string{"llama3.2", "qwen2.5-coder"}) {
		t.Fatalf("Models via queue: %v %v", models, err)
	}

	var deltas []string
	chatResp, err := client.Chat(context.Background(), llm.Request{
		Model:    "llama3.2",
		Messages: []llm.Message{{Role: "user", Content: "oi"}},
	}, func(d llm.Delta) {
		if d.Content != "" {
			deltas = append(deltas, d.Content)
		}
	})
	if err != nil {
		t.Fatalf("Chat via queue: %v", err)
	}
	if chatResp.Content != "Olá, modelo local!" || len(deltas) != 3 {
		t.Fatalf("unexpected streamed response: content=%q deltas=%v", chatResp.Content, deltas)
	}
}

func TestLocalProviderWhoCanUseRule(t *testing.T) {
	s, clk := makeServer(t)
	ctx := context.Background()

	// Create people Ana, Bruno and Carla, and their workspaces.
	ana, _, err := s.store.CreatePerson(ctx, store.Person{
		ID: "p-ana", Email: "ana@exemplo.com", Name: "Ana", PasswordHash: "x",
	}, false, clk.now())
	if err != nil {
		t.Fatal(err)
	}
	bruno, _, err := s.store.CreatePerson(ctx, store.Person{
		ID: "p-bruno", Email: "bruno@exemplo.com", Name: "Bruno", PasswordHash: "x",
	}, false, clk.now())
	if err != nil {
		t.Fatal(err)
	}
	carla, _, err := s.store.CreatePerson(ctx, store.Person{
		ID: "p-carla", Email: "carla@exemplo.com", Name: "Carla", PasswordHash: "x",
	}, false, clk.now())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.store.CreateWorkspace(ctx, store.Workspace{
		ID: "ws-ana", Name: "Espaço da Ana", Mode: store.WorkspaceTeam, OwnerID: ana.ID,
	}, clk.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.CreateWorkspace(ctx, store.Workspace{
		ID: "ws-bruno", Name: "Espaço do Bruno", Mode: store.WorkspaceTeam, OwnerID: bruno.ID,
	}, clk.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.CreateWorkspace(ctx, store.Workspace{
		ID: "ws-carla", Name: "Espaço da Carla", Mode: store.WorkspaceTeam, OwnerID: carla.ID,
	}, clk.now()); err != nil {
		t.Fatal(err)
	}

	// Pair Ana's computer inside ws-ana and connect her local Ollama (shared_with = []).
	codeW := requestWS(t, s, "POST", "/v1/nodes/pairing-codes", adminTestToken, "ws-ana", PairingCodeRequest{
		Name: "Mac da Ana", Kind: "windows",
	})
	if codeW.Code != http.StatusCreated {
		t.Fatalf("pair code ws-ana: %d %s", codeW.Code, codeW.Body.String())
	}
	code := decode[PairingCodeResponse](t, codeW)
	pairW := request(t, s, "POST", "/v1/runtime/pair", "", PairRequest{Code: code.Code})
	anaNode := decode[PairResponse](t, pairW)

	request(t, s, "POST", "/v1/runtime/heartbeat", anaNode.Token, HeartbeatRequest{
		LocalModels: []LocalModelReport{{Kind: "ollama", URL: "http://127.0.0.1:11434/v1", Models: []string{"llama3.2"}}},
	})
	connW := requestWS(t, s, "POST", "/v1/nodes/"+anaNode.NodeID+"/providers", adminTestToken, "ws-ana", ConnectLocalModelRequest{
		Kind: "ollama",
	})
	if connW.Code != http.StatusCreated {
		t.Fatalf("connect ana ollama: %d %s", connW.Code, connW.Body.String())
	}
	anaProv := decode[ProviderView](t, connW).Provider

	// Runner responder for Ana's local Ollama.
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		hc := &http.Client{Timeout: 5 * time.Second}
		for {
			select {
			case <-done:
				return
			default:
			}
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/runtime/llm/next", nil)
			req.Header.Set("Authorization", "Bearer "+anaNode.Token)
			resp, err := hc.Do(req)
			if err != nil {
				return
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				continue
			}
			var call LocalLLMCall
			_ = json.NewDecoder(resp.Body).Decode(&call)
			resp.Body.Close()
			chunk := `data: {"model":"llama3.2","choices":[{"delta":{"content":"Resposta pelo PC da Ana."},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
			b, _ := json.Marshal(LocalLLMResult{ID: call.ID, Seq: 1, Status: http.StatusOK, Chunk: chunk, Done: true})
			pr, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/runtime/llm/result", bytes.NewReader(b))
			pr.Header.Set("Authorization", "Bearer "+anaNode.Token)
			pr.Header.Set("Content-Type", "application/json")
			if r2, err := hc.Do(pr); err == nil {
				r2.Body.Close()
			}
		}
	}()

	wantRefusal := "Este modelo é do computador de Ana e não foi dividido com este espaço"

	// 1. Trying to assign Ana's provider to an agent in ws-bruno via HTTP API is refused with the exact phrase.
	forbidW := requestWS(t, s, "POST", "/v1/agents", adminTestToken, "ws-bruno", AgentRequest{
		Name:  "Beto",
		Role:  "Assistente",
		Brain: &store.AgentBrain{Provider: anaProv.ID, Model: "llama3.2"},
	})
	if forbidW.Code != http.StatusForbidden || strings.TrimSpace(forbidW.Body.String()) != wantRefusal {
		t.Fatalf("expected 403 %q, got %d %q", wantRefusal, forbidW.Code, forbidW.Body.String())
	}

	// 2. Even if an agent in ws-bruno already has Brain.Provider = anaProv.ID in the store, running a turn refuses with the exact phrase.
	if _, err := s.store.SaveAgent(ctx, store.Agent{
		ID:          "beto",
		WorkspaceID: "ws-bruno",
		Name:        "Beto",
		Role:        "Assistente",
		Engine:      "native",
		Brain:       store.AgentBrain{Provider: anaProv.ID, Model: "llama3.2"},
	}, store.ActorAdmin, clk.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.SaveAgent(ctx, store.Agent{
		ID:          "lia-ana",
		WorkspaceID: "ws-ana",
		Name:        "Lia",
		Role:        "Assistente",
		Engine:      "native",
		Brain:       store.AgentBrain{Provider: anaProv.ID, Model: "llama3.2"},
	}, store.ActorAdmin, clk.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.SaveAgent(ctx, store.Agent{
		ID:          "clara-carla",
		WorkspaceID: "ws-carla",
		Name:        "Clara",
		Role:        "Assistente",
		Engine:      "native",
		Brain:       store.AgentBrain{Provider: anaProv.ID, Model: "llama3.2"},
	}, store.ActorAdmin, clk.now()); err != nil {
		t.Fatal(err)
	}

	// Same workspace (ws-ana): allowed!
	convAna := decode[store.Conversation](t, requestWS(t, s, "POST", "/v1/conversations", adminTestToken, "ws-ana", ConversationRequest{AgentID: "lia-ana"}))
	okAna := requestWS(t, s, "POST", "/v1/conversations/"+convAna.ID+"/messages", adminTestToken, "ws-ana", MessageRequest{Content: "oi"})
	if okAna.Code != http.StatusOK || !strings.Contains(okAna.Body.String(), "Resposta pelo PC da Ana.") {
		t.Fatalf("same-workspace turn failed: %d %s", okAna.Code, okAna.Body.String())
	}

	// Unshared workspace (ws-bruno): refused with exact message!
	convBruno := decode[store.Conversation](t, requestWS(t, s, "POST", "/v1/conversations", adminTestToken, "ws-bruno", ConversationRequest{AgentID: "beto"}))
	denyBruno := requestWS(t, s, "POST", "/v1/conversations/"+convBruno.ID+"/messages", adminTestToken, "ws-bruno", MessageRequest{Content: "oi"})
	if denyBruno.Code != http.StatusBadGateway || strings.TrimSpace(denyBruno.Body.String()) != wantRefusal {
		t.Fatalf("expected turn refusal %q, got %d %q", wantRefusal, denyBruno.Code, denyBruno.Body.String())
	}

	// 3. Ana shares the provider with workspace "ws-bruno" and person "p-carla".
	shareW := requestWS(t, s, "PUT", "/v1/nodes/providers/"+anaProv.ID+"/share", adminTestToken, "ws-ana", ShareProviderRequest{
		SharedWith: []string{"ws-bruno", carla.ID},
	})
	if shareW.Code != http.StatusOK {
		t.Fatalf("share provider: %d %s", shareW.Code, shareW.Body.String())
	}

	allowBruno := requestWS(t, s, "POST", "/v1/conversations/"+convBruno.ID+"/messages", adminTestToken, "ws-bruno", MessageRequest{Content: "agora dividido"})
	if allowBruno.Code != http.StatusOK || !strings.Contains(allowBruno.Body.String(), "Resposta pelo PC da Ana.") {
		t.Fatalf("shared workspace turn failed: %d %s", allowBruno.Code, allowBruno.Body.String())
	}

	convCarla := decode[store.Conversation](t, requestWS(t, s, "POST", "/v1/conversations", adminTestToken, "ws-carla", ConversationRequest{AgentID: "clara-carla"}))
	allowCarla := requestWS(t, s, "POST", "/v1/conversations/"+convCarla.ID+"/messages", adminTestToken, "ws-carla", MessageRequest{Content: "por pessoa"})
	if allowCarla.Code != http.StatusOK || !strings.Contains(allowCarla.Body.String(), "Resposta pelo PC da Ana.") {
		t.Fatalf("shared person turn failed: %d %s", allowCarla.Code, allowCarla.Body.String())
	}
}
