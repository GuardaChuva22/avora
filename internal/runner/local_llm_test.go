package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestRunnerLocalLLMQueueAndDetection(t *testing.T) {
	// Fake local Ollama server (OpenAI /v1/chat/completions streaming + /api/tags fallback).
	fakeOllama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			http.NotFound(w, r) // exercise Ollama /api/tags fallback too
		case "/api/tags":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2:latest"},{"name":"deepseek-r1:8b"}]}`))
		case "/v1/chat/completions":
			var body struct {
				Model  string `json:"model"`
				Stream bool   `json:"stream"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !body.Stream || body.Model != "llama3.2:latest" {
				http.Error(w, "bad chat body", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			chunks := []string{
				`data: {"model":"llama3.2:latest","choices":[{"delta":{"reasoning_content":"pensando... "}}]}`,
				`data: {"model":"llama3.2:latest","choices":[{"delta":{"content":"Oi do "}}]}`,
				`data: {"model":"llama3.2:latest","choices":[{"delta":{"content":"Ollama local!"}}]}`,
				`data: {"model":"llama3.2:latest","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":4}}`,
				`data: [DONE]`,
			}
			for _, c := range chunks {
				_, _ = fmt.Fprintln(w, c)
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

	// Fake local LM Studio server (/v1/models).
	fakeLMStudio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen2.5-7b-instruct"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeLMStudio.Close()

	core, err := control.NewServer(testAdmin, store.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(core.Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var code control.PairingCodeResponse
	adminCall(t, server.URL, "POST", "/v1/nodes/pairing-codes", control.PairingCodeRequest{Name: "PC Gamer", Kind: "windows"}, &code)
	state, err := LoadOrPair(ctx, filepath.Join(t.TempDir(), "node.json"), server.URL, code.Code)
	if err != nil {
		t.Fatal(err)
	}

	client, err := NewClient(server.URL, state.Token)
	if err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(t.TempDir(), "work")
	client.Agents = AgentConfig{Workspace: workDir}
	client.LocalProbes = []LocalProbeTarget{
		{Kind: "ollama", BaseURL: fakeOllama.URL + "/v1"},
		{Kind: "lmstudio", BaseURL: fakeLMStudio.URL + "/v1"},
	}

	// 1. Heartbeat detects both Ollama and LM Studio and reports them + shared_dir.
	if _, err := client.Heartbeat(ctx, nil); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	var nodes []control.NodeStatus
	adminCall(t, server.URL, "GET", "/v1/nodes", nil, &nodes)
	if len(nodes) != 1 || len(nodes[0].LocalModels) != 2 {
		t.Fatalf("expected 2 detected local model servers, got: %+v", nodes)
	}
	if nodes[0].LocalModels[0].Kind != "ollama" || !reflect.DeepEqual(nodes[0].LocalModels[0].Models, []string{"deepseek-r1:8b", "llama3.2:latest"}) {
		t.Fatalf("unexpected ollama report: %+v", nodes[0].LocalModels[0])
	}
	if nodes[0].LocalModels[1].Kind != "lmstudio" || !reflect.DeepEqual(nodes[0].LocalModels[1].Models, []string{"qwen2.5-7b-instruct"}) {
		t.Fatalf("unexpected lmstudio report: %+v", nodes[0].LocalModels[1])
	}
	if nodes[0].SharedDir != filepath.Join(workDir, "shared") {
		t.Fatalf("unexpected shared_dir: %q", nodes[0].SharedDir)
	}

	// 2. Runner llmLoop proxies /models and streaming /chat/completions over the queue.
	loopCtx, stopLoop := context.WithCancel(ctx)
	doneLoop := make(chan struct{})
	go func() {
		defer close(doneLoop)
		client.llmLoop(loopCtx)
	}()
	defer func() {
		stopLoop()
		<-doneLoop
		client.wg.Wait()
	}()

	llmClient := core.NewLocalLLMClient(store.Provider{
		ID:      "ollama." + state.NodeID,
		Kind:    "ollama",
		NodeID:  state.NodeID,
		BaseURL: fakeOllama.URL + "/v1",
	})

	models, err := llmClient.Models(ctx)
	if err != nil || !reflect.DeepEqual(models, []string{"deepseek-r1:8b", "llama3.2:latest"}) {
		t.Fatalf("Models over runner queue: %v %v", models, err)
	}

	var contentPieces []string
	var reasoningPieces []string
	resp, err := llmClient.Chat(ctx, llm.Request{
		Model:    "llama3.2:latest",
		Messages: []llm.Message{{Role: "user", Content: "olá"}},
	}, func(d llm.Delta) {
		if d.Content != "" {
			contentPieces = append(contentPieces, d.Content)
		}
		if d.Reasoning != "" {
			reasoningPieces = append(reasoningPieces, d.Reasoning)
		}
	})
	if err != nil {
		t.Fatalf("Chat over runner queue: %v", err)
	}
	if resp.Content != "Oi do Ollama local!" || resp.Reasoning != "pensando... " {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(contentPieces) != 2 || len(reasoningPieces) != 1 {
		t.Fatalf("expected streamed pieces, got content=%v reasoning=%v", contentPieces, reasoningPieces)
	}
}

func TestResolveLocalLLMURLRestrictsToLoopback(t *testing.T) {
	u, err := resolveLocalLLMURL("ollama", "")
	if err != nil || u != "http://127.0.0.1:11434/v1" {
		t.Fatalf("default ollama URL: %q %v", u, err)
	}
	u, err = resolveLocalLLMURL("lmstudio", "http://localhost:1234")
	if err != nil || u != "http://127.0.0.1:1234/v1" {
		t.Fatalf("localhost rewrite: %q %v", u, err)
	}
	for _, bad := range []string{
		"http://192.168.1.20:11434/v1",
		"http://10.0.0.5:1234/v1",
		"https://api.openai.com/v1",
		"ftp://127.0.0.1:11434/v1",
	} {
		if got, err := resolveLocalLLMURL("openai", bad); err == nil || !strings.Contains(err.Error(), "loopback") && !strings.Contains(err.Error(), "http") {
			t.Fatalf("expected non-loopback URL %q to be rejected, got %q (%v)", bad, got, err)
		}
	}
}
