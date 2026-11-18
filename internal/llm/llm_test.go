package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func streamHandler(t *testing.T, chunks []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("bad request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["stream"] != true || body["model"] != "m" {
			t.Errorf("bad body: %v %v", body, err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
		}
	}
}

func newTestProvider(t *testing.T, url string) *Provider {
	t.Helper()
	p, err := NewProvider("test", url+"/v1", "key", 0)
	if err != nil {
		t.Fatal(err)
	}
	p.sleep = func(context.Context, time.Duration) error { return nil }
	return p
}

func TestChatStreamsContentReasoningAndUsage(t *testing.T) {
	srv := httptest.NewServer(streamHandler(t, []string{
		`{"model":"m","choices":[{"delta":{"reasoning_content":"pensando"}}]}`,
		`{"model":"m","choices":[{"delta":{"content":"Olá"}}]}`,
		`{"model":"m","choices":[{"delta":{"content":", mundo"},"finish_reason":"stop"}]}`,
		`{"model":"m","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":5}}`,
		`[DONE]`,
	}))
	defer srv.Close()
	var deltas []Delta
	resp, err := newTestProvider(t, srv.URL).Chat(context.Background(),
		Request{Model: "m", Messages: []Message{{Role: "user", Content: "oi"}}},
		func(d Delta) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Olá, mundo" || resp.Reasoning != "pensando" || resp.FinishReason != "stop" ||
		resp.Usage.PromptTokens != 12 || resp.Usage.CompletionTokens != 5 || resp.Model != "m" {
		t.Fatalf("response: %+v", resp)
	}
	if len(deltas) != 3 {
		t.Fatalf("deltas: %+v", deltas)
	}
}

func TestChatRetriesRateLimit(t *testing.T) {
	var calls atomic.Int32
	ok := streamHandler(t, []string{`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`, `[DONE]`})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		ok(w, r)
	}))
	defer srv.Close()
	resp, err := newTestProvider(t, srv.URL).Chat(context.Background(), Request{Model: "m"}, nil)
	if err != nil || resp.Content != "ok" || calls.Load() != 3 {
		t.Fatalf("resp=%+v err=%v calls=%d", resp, err, calls.Load())
	}
}

func TestChatDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := newTestProvider(t, srv.URL).Chat(context.Background(), Request{Model: "m"}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 401 || !strings.Contains(apiErr.Body, "bad key") || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestChatRejectsTruncatedStream(t *testing.T) {
	srv := httptest.NewServer(streamHandler(t, []string{`{"choices":[{"delta":{"content":"meio"}}]}`}))
	defer srv.Close()
	if _, err := newTestProvider(t, srv.URL).Chat(context.Background(), Request{Model: "m"}, nil); err == nil {
		t.Fatal("truncated stream accepted")
	}
}

func TestProviderValidation(t *testing.T) {
	if _, err := NewProvider("x", "http://example.org/v1", "key", 0); err == nil {
		t.Fatal("plain http to a remote host accepted")
	}
	if _, err := NewProvider("x", "https://example.org/v1", "", 0); err == nil {
		t.Fatal("empty key accepted")
	}
}

func TestLimiterSpacesRequests(t *testing.T) {
	l := newLimiter(60) // one per second
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	if err := l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The second call would have to wait a full second; a canceled context returns at once.
	if err := l.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("second call did not wait: %v", err)
	}
}

func TestChatStreamsToolCalls(t *testing.T) {
	srv := httptest.NewServer(streamHandler(t, []string{
		`{"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"criar_rotina","arguments":"{\"titulo\":"}}]}}]}`,
		`{"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}`,
		`{"model":"m","choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","function":{"name":"estado_do_avora","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	}))
	defer srv.Close()
	tools := []Tool{{Type: "function", Function: ToolFunction{Name: "criar_rotina", Parameters: json.RawMessage(`{"type":"object"}`)}}}
	resp, err := newTestProvider(t, srv.URL).Chat(context.Background(),
		Request{Model: "m", Messages: []Message{{Role: "user", Content: "oi"}}, Tools: tools}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 2 || resp.ToolCalls[0].ID != "c1" || resp.ToolCalls[0].Function.Name != "criar_rotina" ||
		resp.ToolCalls[0].Function.Arguments != `{"titulo":"x"}` || resp.ToolCalls[1].Function.Name != "estado_do_avora" {
		t.Fatalf("tool calls: %+v", resp.ToolCalls)
	}
}
