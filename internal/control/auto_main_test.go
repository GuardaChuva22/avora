package control

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestChatModel(t *testing.T) {
	cases := []struct {
		models []string
		want   string
	}{
		{nil, ""},
		{[]string{"text-embedding-3", "whisper-1"}, ""},
		{[]string{"zeta-8b", "alpha-70b"}, "zeta-8b"},
		{[]string{"llama-3-70b", "text-embedding-3", "gpt-9", "tts-1"}, "gpt-9"},
		{[]string{"openai/gpt-9", "anthropic/claude-x"}, "anthropic/claude-x"},
	}
	for _, c := range cases {
		if got := chatModel(c.models); got != c.want {
			t.Errorf("chatModel(%v) = %q, want %q", c.models, got, c.want)
		}
	}
}

// Connecting an AI is enough: nobody picks a main model.
func TestAutomaticModel(t *testing.T) {
	api := httptest.NewServer(&fakeOpenAI{})
	defer api.Close()
	s := modelsServer(t)
	if s.chatEngine() != nil {
		t.Fatal("an engine with nothing connected")
	}
	w := request(t, s, "POST", "/v1/models/providers", adminTestToken, ProviderRequest{Kind: "openai", BaseURL: api.URL + "/v1"})
	var p ProviderView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Status != "ok" {
		t.Fatalf("connect: %d %s", w.Code, w.Body)
	}
	if main, _ := listModelsOf(t, s); main.Provider != p.ID || main.Model != "alpha-70b" {
		t.Fatalf("automatic: %+v", main)
	}
	if answer := chatOnce(t, s); answer != "Oi de alpha-70b" {
		t.Fatalf("answer: %q", answer)
	}

	// A pin holds until it is cleared; then the automatic pick is back.
	if w := request(t, s, "PUT", "/v1/models/main", adminTestToken, MainModel{Provider: p.ID, Model: "zeta-8b"}); w.Code != 200 {
		t.Fatalf("pin: %d %s", w.Code, w.Body)
	}
	if w := request(t, s, "GET", "/v1/models", adminTestToken, nil); !json.Valid(w.Body.Bytes()) || !containsJSON(w.Body.Bytes(), "pinned", true) {
		t.Fatalf("pinned: %s", w.Body)
	}
	if w := request(t, s, "DELETE", "/v1/models/main", adminTestToken, nil); w.Code != 204 {
		t.Fatalf("unpin: %d", w.Code)
	}
	if main, _ := listModelsOf(t, s); main.Model != "alpha-70b" {
		t.Fatalf("after unpin: %+v", main)
	}

	// Removing the only AI leaves nothing answering.
	if w := request(t, s, "DELETE", "/v1/models/providers/"+p.ID, adminTestToken, nil); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if s.chatEngine() != nil {
		t.Fatal("still answering after the only AI went")
	}
}

func containsJSON(body []byte, key string, want any) bool {
	var m map[string]any
	return json.Unmarshal(body, &m) == nil && m[key] == want
}
