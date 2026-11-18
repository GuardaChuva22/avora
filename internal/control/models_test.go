package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/vault"
)

// fakeOpenAI is a provider in the OpenAI format: a model list and a
// streaming chat that answers with the model's name.
type fakeOpenAI struct {
	mu    sync.Mutex
	key   string // "" accepts any request
	auths []string
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.auths = append(f.auths, r.Header.Get("Authorization"))
	f.mu.Unlock()
	if f.key != "" && r.Header.Get("Authorization") != "Bearer "+f.key {
		http.Error(w, "bad key", http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/v1/models":
		fmt.Fprint(w, `{"data":[{"id":"zeta-8b"},{"id":"alpha-70b"}]}`)
	case "/v1/chat/completions":
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		chunk, _ := json.Marshal(map[string]any{"model": req.Model, "choices": []any{map[string]any{"delta": map[string]any{"content": "Oi de " + req.Model}}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	default:
		http.NotFound(w, r)
	}
}

func modelsServer(t *testing.T) *Server {
	t.Helper()
	s := chatServer(t, nil)
	s.SetConnectors(connect.NewRegistry(), vault.Random())
	return s
}

func listModelsOf(t *testing.T, s *Server) (MainModel, []ProviderView) {
	t.Helper()
	w := request(t, s, "GET", "/v1/models", adminTestToken, nil)
	var out struct {
		Main      MainModel      `json:"main"`
		Providers []ProviderView `json:"providers"`
		Kinds     []ProviderKind `json:"kinds"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Kinds) < 5 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	return out.Main, out.Providers
}

func TestModelsProvidersAndMainModel(t *testing.T) {
	cloud := &fakeOpenAI{key: "sk-cloud-SECRET"}
	api := httptest.NewServer(cloud)
	defer api.Close()
	local := &fakeOpenAI{}
	llama := httptest.NewServer(local)
	defer llama.Close()
	s := modelsServer(t)

	// Without a main model the chat says where to pick one.
	if main, list := listModelsOf(t, s); main.Provider != "" || len(list) != 0 {
		t.Fatalf("empty: %+v %+v", main, list)
	}

	// A wrong key is saved, but marked broken.
	w := request(t, s, "POST", "/v1/models/providers", adminTestToken, ProviderRequest{Kind: "openai", BaseURL: api.URL + "/v1/", APIKey: "sk-wrong"})
	var bad ProviderView
	_ = json.Unmarshal(w.Body.Bytes(), &bad)
	if w.Code != 200 || bad.Status != "error" || bad.LastError != "a chave foi recusada" || strings.Contains(w.Body.String(), "sk-wrong") {
		t.Fatalf("wrong key: %d %s", w.Code, w.Body)
	}
	// The right key fixes it: the models come in, sorted.
	w = request(t, s, "PUT", "/v1/models/providers/"+bad.ID, adminTestToken, ProviderRequest{Name: "OpenRouter", APIKey: "sk-cloud-SECRET"})
	var good ProviderView
	_ = json.Unmarshal(w.Body.Bytes(), &good)
	if good.Status != "ok" || good.Name != "OpenRouter" || len(good.Models) != 2 || good.Models[0] != "alpha-70b" || !good.HasKey || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatalf("fixed: %s", w.Body)
	}

	// A llama.cpp server on the owner's computer needs no key.
	w = request(t, s, "POST", "/v1/models/providers", adminTestToken, ProviderRequest{Kind: "local", BaseURL: llama.URL + "/v1"})
	var loc ProviderView
	_ = json.Unmarshal(w.Body.Bytes(), &loc)
	if loc.Status != "ok" || loc.HasKey || loc.Name != "llama.cpp" {
		t.Fatalf("local: %s", w.Body)
	}
	// Two accounts of the same kind live side by side.
	w = request(t, s, "POST", "/v1/models/providers", adminTestToken, ProviderRequest{Kind: "local", Name: "Notebook", BaseURL: llama.URL + "/v1"})
	var loc2 ProviderView
	_ = json.Unmarshal(w.Body.Bytes(), &loc2)
	if loc2.ID == loc.ID || !strings.HasPrefix(loc2.ID, "local.") {
		t.Fatalf("second local: %s", w.Body)
	}

	// Refused: plain http to the internet, a cloud API without a key, a
	// model the provider does not have.
	for _, req := range []ProviderRequest{
		{Kind: "local", BaseURL: "http://example.com/v1"},
		{Kind: "openrouter"},
	} {
		if w := request(t, s, "POST", "/v1/models/providers", adminTestToken, req); w.Code != 400 {
			t.Fatalf("%+v: %d %s", req, w.Code, w.Body)
		}
	}
	if w := request(t, s, "PUT", "/v1/models/main", adminTestToken, MainModel{Provider: good.ID, Model: "gpt-9"}); w.Code != 400 {
		t.Fatalf("unknown model: %d", w.Code)
	}

	// The main model answers the chat at once, through its own provider.
	if w := request(t, s, "PUT", "/v1/models/main", adminTestToken, MainModel{Provider: good.ID, Model: "alpha-70b"}); w.Code != 200 {
		t.Fatalf("main: %d %s", w.Code, w.Body)
	}
	if main, _ := listModelsOf(t, s); main.Provider != good.ID || main.Model != "alpha-70b" {
		t.Fatalf("main after set: %+v", main)
	}
	answer := chatOnce(t, s)
	if answer != "Oi de alpha-70b" {
		t.Fatalf("answer: %q", answer)
	}
	// Switching to the local model needs no restart, and sends no key.
	request(t, s, "PUT", "/v1/models/main", adminTestToken, MainModel{Provider: loc.ID, Model: "zeta-8b"})
	if answer := chatOnce(t, s); answer != "Oi de zeta-8b" {
		t.Fatalf("local answer: %q", answer)
	}
	for _, a := range local.auths {
		if a != "" {
			t.Fatalf("key sent to llama.cpp: %q", a)
		}
	}

	// A restart brings the main model back from the store.
	s2 := modelsServer(t)
	s2.store = s.store
	s2.vault = s.vault
	if err := s2.RestoreModels(t.Context()); err != nil {
		t.Fatal(err)
	}
	if main, _ := listModelsOf(t, s2); main.Provider != loc.ID || main.Model != "zeta-8b" {
		t.Fatalf("restored: %+v", main)
	}

	// Removing the pinned model's provider unpins it: the next AI connected
	// answers by itself.
	if w := request(t, s, "DELETE", "/v1/models/providers/"+loc.ID, adminTestToken, nil); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if main, list := listModelsOf(t, s); main.Provider == "" || main.Provider == loc.ID || len(list) != 2 || s.chatEngine() == nil {
		t.Fatalf("after delete: %+v %d", main, len(list))
	}
	events := request(t, s, "GET", "/v1/events", adminTestToken, nil).Body.String()
	if strings.Contains(events, "SECRET") || !strings.Contains(events, "provider.saved") {
		t.Fatalf("events: %s", events)
	}
}

func TestModelsBuiltinProvider(t *testing.T) {
	api := httptest.NewServer(&fakeOpenAI{key: "nv-SECRET"})
	defer api.Close()
	s := modelsServer(t)
	p, err := llm.NewProvider("nvidia", api.URL+"/v1", "nv-SECRET", 0)
	if err != nil {
		t.Fatal(err)
	}
	s.SetBuiltinModel(p, "zeta-8b")
	main, list := listModelsOf(t, s)
	if main.Provider != builtinProviderID || len(list) != 1 || !list[0].Builtin {
		t.Fatalf("builtin: %+v %+v", main, list)
	}
	w := request(t, s, "POST", "/v1/models/providers/"+builtinProviderID+"/refresh", adminTestToken, nil)
	if !strings.Contains(w.Body.String(), "alpha-70b") || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatalf("refresh: %s", w.Body)
	}
	if w := request(t, s, "DELETE", "/v1/models/providers/"+builtinProviderID, adminTestToken, nil); w.Code != 400 {
		t.Fatalf("delete builtin: %d", w.Code)
	}
	if w := request(t, s, "PUT", "/v1/models/main", adminTestToken, MainModel{Provider: builtinProviderID, Model: "alpha-70b"}); w.Code != 200 {
		t.Fatalf("switch builtin model: %d %s", w.Code, w.Body)
	}
	if answer := chatOnce(t, s); answer != "Oi de alpha-70b" {
		t.Fatalf("answer: %q", answer)
	}
}

func chatOnce(t *testing.T, s *Server) string {
	t.Helper()
	c := newConversation(t, s)
	r := send(t, s, c.ID, "oi")
	return r.Assistant.Content + r.Assistant.Error
}
