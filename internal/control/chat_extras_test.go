package control

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/engine"
	"github.com/GuardaChuva22/avora/internal/store"
)

func rawRequest(s *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+adminTestToken)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestReplyToAMessage(t *testing.T) {
	e := &fakeEngine{}
	s := chatServer(t, e)
	c := agentConversation(t, s, "atlas")
	first := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "primeira"}))
	r := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken,
		MessageRequest{Content: "explica melhor", ReplyTo: first.Assistant.ID}))
	if r.User.ReplyTo != first.Assistant.ID {
		t.Fatalf("stored: %+v", r.User)
	}
	// The engine gets the history with reply_to; the native engine quotes it.
	last := e.lastSeen[len(e.lastSeen)-1]
	if last.ReplyTo != first.Assistant.ID {
		t.Fatalf("engine saw: %+v", last)
	}
	if q := engine.Quote(first.Assistant); !strings.Contains(q, "«eco: primeira»") || !strings.HasPrefix(q, "[Respondendo à sua mensagem") {
		t.Fatalf("quote: %q", q)
	}
	other := agentConversation(t, s, "atlas")
	if w := request(t, s, "POST", "/v1/conversations/"+other.ID+"/messages", adminTestToken,
		MessageRequest{Content: "x", ReplyTo: first.Assistant.ID}); w.Code != 400 {
		t.Fatalf("reply across conversations: %d", w.Code)
	}
}

func TestReplyInChannelCallsThatAgent(t *testing.T) {
	s := chatServer(t, &fakeEngine{})
	q, err := s.store.AppendMessage(t.Context(), store.ChatMessage{ConversationID: TeamChannelID, Role: "assistant", AgentID: "cora",
		Content: "Sobram R$ 1.400.", CreatedAt: s.now()}, "agent:cora")
	if err != nil {
		t.Fatal(err)
	}
	w := request(t, s, "POST", "/v1/conversations/time/messages", adminTestToken, MessageRequest{Content: "e se eu cortar o streaming?", ReplyTo: q.ID})
	got := decode[map[string]any](t, w)
	if called, _ := got["called"].([]any); len(called) != 1 || called[0] != "cora" {
		t.Fatalf("called: %v", got["called"])
	}
	team, _ := s.store.ListAgents(t.Context())
	msgs, _ := s.store.Messages(t.Context(), TeamChannelID)
	if p := channelPrompt("time", team[0], team, msgs, ownerName); !strings.Contains(p, "(respondendo a Cora: «Sobram R$ 1.400.»): e se eu cortar o streaming?") {
		t.Fatalf("prompt: %s", p)
	}
}

func TestArchiveAndDeleteConversations(t *testing.T) {
	s := chatServer(t, &fakeEngine{})
	keep, gone := agentConversation(t, s, "atlas"), agentConversation(t, s, "atlas")
	request(t, s, "POST", "/v1/conversations/"+gone.ID+"/messages", adminTestToken, MessageRequest{Content: "oi"})

	if w := request(t, s, "PATCH", "/v1/conversations/"+keep.ID, adminTestToken, map[string]bool{"archived": true}); w.Code != 200 {
		t.Fatalf("archive: %d", w.Code)
	}
	list := decode[[]store.Conversation](t, request(t, s, "GET", "/v1/conversations?agent=atlas", adminTestToken, nil))
	archived := decode[[]store.Conversation](t, request(t, s, "GET", "/v1/conversations?agent=atlas&archived=1", adminTestToken, nil))
	if len(list) != 1 || list[0].ID != gone.ID || len(archived) != 1 || archived[0].ID != keep.ID {
		t.Fatalf("lists: %+v / %+v", list, archived)
	}
	request(t, s, "PATCH", "/v1/conversations/"+keep.ID, adminTestToken, map[string]bool{"archived": false})
	if list = decode[[]store.Conversation](t, request(t, s, "GET", "/v1/conversations?agent=atlas", adminTestToken, nil)); len(list) != 2 {
		t.Fatalf("after unarchive: %+v", list)
	}

	if w := request(t, s, "DELETE", "/v1/conversations/"+gone.ID, adminTestToken, nil); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := request(t, s, "GET", "/v1/conversations/"+gone.ID+"/messages", adminTestToken, nil); w.Code != 404 {
		t.Fatalf("messages after delete: %d", w.Code)
	}
	for _, m := range []string{"DELETE", "PATCH"} {
		if w := request(t, s, m, "/v1/conversations/time", adminTestToken, map[string]bool{"archived": true}); w.Code != 400 {
			t.Fatalf("%s #time: %d", m, w.Code)
		}
	}
	// While an agent answers, the conversation stays.
	_ = s.store.StartConversationJob(t.Context(), keep.ID, "turn-x")
	if w := request(t, s, "DELETE", "/v1/conversations/"+keep.ID, adminTestToken, nil); w.Code != 409 {
		t.Fatalf("delete while busy: %d", w.Code)
	}
}

func TestAgentAvatar(t *testing.T) {
	s := chatServer(t, &fakeEngine{})
	webp := append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), bytes.Repeat([]byte{1}, 20)...)
	if w := rawRequest(s, "PUT", "/v1/agents/atlas/avatar", []byte("GIF89a....")); w.Code != 400 {
		t.Fatalf("gif: %d", w.Code)
	}
	if w := rawRequest(s, "PUT", "/v1/agents/atlas/avatar", append(webp, make([]byte, MaxAvatarBytes)...)); w.Code != 413 {
		t.Fatalf("too big: %d", w.Code)
	}
	if w := rawRequest(s, "PUT", "/v1/agents/ninguem/avatar", webp); w.Code != 404 {
		t.Fatalf("unknown agent: %d", w.Code)
	}
	if w := rawRequest(s, "PUT", "/v1/agents/atlas/avatar", webp); w.Code != 204 {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	w := rawRequest(s, "GET", "/v1/agents/atlas/avatar", nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/webp" || !bytes.Equal(w.Body.Bytes(), webp) {
		t.Fatalf("get: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	team := decode[[]AgentStatus](t, request(t, s, "GET", "/v1/agents", adminTestToken, nil))
	for _, a := range team {
		if (a.ID == "atlas") != (a.AvatarAt != nil) {
			t.Fatalf("avatar_at: %s %v", a.ID, a.AvatarAt)
		}
	}
	if w := rawRequest(s, "GET", "/v1/agents/atlas/avatar", nil); w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("nosniff missing")
	}
	if w := rawRequest(s, "DELETE", "/v1/agents/atlas/avatar", nil); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := rawRequest(s, "GET", "/v1/agents/atlas/avatar", nil); w.Code != 404 {
		t.Fatalf("after delete: %d", w.Code)
	}
	if w := httptestNoAuth(s, "/v1/agents/atlas/avatar"); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
}

func httptestNoAuth(s *Server, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}
