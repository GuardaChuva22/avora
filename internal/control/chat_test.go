package control

import (
	"bufio"
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// fakeEngine echoes the last message, or fails with err.
type fakeEngine struct {
	err      error
	lastSeen []store.ChatMessage
	system   string
}

func (f *fakeEngine) Name() string     { return "native" }
func (f *fakeEngine) Model() string    { return "fake-model" }
func (f *fakeEngine) Provider() string { return "fake" }
func (f *fakeEngine) Reply(_ context.Context, system string, history []store.ChatMessage, onDelta func(llm.Delta)) (llm.Response, error) {
	f.system = system
	f.lastSeen = history
	if f.err != nil {
		return llm.Response{}, f.err
	}
	answer := "eco: " + history[len(history)-1].Content
	if onDelta != nil {
		onDelta(llm.Delta{Reasoning: "hmm"})
		onDelta(llm.Delta{Content: "eco: "})
		onDelta(llm.Delta{Content: history[len(history)-1].Content})
	}
	return llm.Response{Content: answer, Usage: llm.Usage{PromptTokens: 7, CompletionTokens: 3}}, nil
}

func chatServer(t *testing.T, e ChatEngine) *Server {
	t.Helper()
	s, _ := makeServer(t)
	if e != nil {
		s.SetChatEngine(e)
	}
	return s
}

func newConversation(t *testing.T, s *Server) store.Conversation {
	t.Helper()
	w := request(t, s, "POST", "/v1/conversations", adminTestToken, ConversationRequest{Title: "teste"})
	if w.Code != 201 {
		t.Fatalf("create conversation: %d %s", w.Code, w.Body.String())
	}
	return decode[store.Conversation](t, w)
}

func TestChatJSON(t *testing.T) {
	engine := &fakeEngine{}
	s := chatServer(t, engine)
	c := newConversation(t, s)
	if c.Engine != "native" || c.Model != "fake-model" {
		t.Fatalf("conversation: %+v", c)
	}
	resp := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "oi"}))
	if resp.User.Content != "oi" || resp.Assistant.Content != "eco: oi" || resp.Assistant.CompletionTokens != 3 || resp.Assistant.Model != "fake-model" {
		t.Fatalf("reply: %+v", resp)
	}
	request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "de novo"})
	if len(engine.lastSeen) != 3 {
		t.Fatalf("engine did not get the history: %+v", engine.lastSeen)
	}
	msgs := decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/"+c.ID+"/messages", adminTestToken, nil))
	if len(msgs) != 4 {
		t.Fatalf("messages: %+v", msgs)
	}
	list := decode[[]store.Conversation](t, request(t, s, "GET", "/v1/conversations", adminTestToken, nil))
	if len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("list: %+v", list)
	}
}

func TestChatStreaming(t *testing.T) {
	s := chatServer(t, &fakeEngine{})
	c := newConversation(t, s)
	r := httptest.NewRequest("POST", "/v1/conversations/"+c.ID+"/messages", strings.NewReader(`{"content":"oi"}`))
	r.Header.Set("Authorization", "Bearer "+adminTestToken)
	r.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type %q", w.Header().Get("Content-Type"))
	}
	var events []string
	scanner := bufio.NewScanner(w.Body)
	for scanner.Scan() {
		if name, ok := strings.CutPrefix(scanner.Text(), "event: "); ok {
			events = append(events, name)
		}
	}
	want := "user,reasoning,delta,delta,done"
	if got := strings.Join(events, ","); got != want {
		t.Fatalf("events %s, want %s", got, want)
	}
}

func TestChatFailureIsAudited(t *testing.T) {
	s := chatServer(t, &fakeEngine{err: &llm.APIError{Status: 429, Body: "slow down"}})
	c := newConversation(t, s)
	w := request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "oi"})
	if w.Code != 502 || !strings.Contains(w.Body.String(), "rate limit") {
		t.Fatalf("failure response: %d %s", w.Code, w.Body.String())
	}
	events := decode[[]store.Event](t, request(t, s, "GET", "/v1/events", adminTestToken, nil))
	var failed *store.Event
	for i := range events {
		if events[i].Type == store.EventLLMFailed {
			failed = &events[i]
		}
	}
	if failed == nil || !strings.Contains(string(failed.Payload), `"status":429`) {
		t.Fatalf("no llm.failed event: %+v", events)
	}
	// The user's message is kept and the failure shows in the conversation.
	msgs := decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/"+c.ID+"/messages", adminTestToken, nil))
	if len(msgs) != 2 || msgs[0].Role != "user" || !strings.Contains(msgs[1].Error, "rate limit") {
		t.Fatalf("messages after failure: %+v", msgs)
	}
}

func TestChatValidation(t *testing.T) {
	if got := request(t, chatServer(t, nil), "POST", "/v1/conversations", adminTestToken, ConversationRequest{}); got.Code != 503 {
		t.Fatalf("chat without engine: %d", got.Code)
	}
	s := chatServer(t, &fakeEngine{})
	c := newConversation(t, s)
	for _, content := range []string{"", "   ", strings.Repeat("x", maxMessageSize+1)} {
		if got := request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: content}); got.Code != 400 {
			t.Fatalf("content of %d bytes accepted: %d", len(content), got.Code)
		}
	}
	if got := request(t, s, "POST", "/v1/conversations/missing/messages", adminTestToken, MessageRequest{Content: "oi"}); got.Code != 404 {
		t.Fatalf("missing conversation: %d", got.Code)
	}
	if got := request(t, s, "GET", "/v1/conversations", "", nil); got.Code != 401 {
		t.Fatalf("unauthenticated list: %d", got.Code)
	}
}
