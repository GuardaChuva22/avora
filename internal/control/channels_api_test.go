package control

import (
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

func settleIn(t *testing.T, s *Server, ch string) ChannelStatus {
	t.Helper()
	for i := 0; i < 400; i++ {
		st := decode[ChannelStatus](t, request(t, s, "GET", "/v1/channels/"+ch+"/status", adminTestToken, nil))
		if len(st.Working) == 0 && !st.Routing && len(st.Next) == 0 {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the channel never settled")
	return ChannelStatus{}
}

func TestChannels(t *testing.T) {
	engine := &teamEngine{lines: map[string]string{
		"router": `{"agentes": ["vega", "cora"], "motivo": "pergunta de finanças"}`,
		"Cora":   "Sobram R$ 800.",
		"Atlas":  "Eu olho o código.",
		"Vega":   "Oi!",
	}}
	s := chatServer(t, engine)

	// #time is the first channel, with everyone.
	list := decode[[]ChannelView](t, request(t, s, "GET", "/v1/channels", adminTestToken, nil))
	if len(list) != 1 || list[0].ID != TeamChannelID || !list[0].Everyone || len(list[0].Agents) != len(teamTemplates) {
		t.Fatalf("channels: %+v", list)
	}
	if w := request(t, s, "PATCH", "/v1/channels/time", adminTestToken, map[string]any{"archived": true}); w.Code != 400 {
		t.Fatalf("archived #time: %d", w.Code)
	}

	// A channel with a name, a purpose and two agents; Atlas only answers @.
	if w := request(t, s, "POST", "/v1/channels", adminTestToken, map[string]any{"name": "Finanças"}); w.Code != 400 {
		t.Fatalf("a channel with nobody: %d", w.Code)
	}
	w := request(t, s, "POST", "/v1/channels", adminTestToken, map[string]any{
		"name": "# Finanças da Casa", "purpose": "contas do mês e investimentos",
		"members": []store.ChannelMember{{AgentID: "cora"}, {AgentID: "atlas", MentionOnly: true}},
	})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	ch := decode[store.Channel](t, w)
	if ch.Name != "finanças-da-casa" || !strings.HasPrefix(ch.ID, "canal-") {
		t.Fatalf("channel: %+v", ch)
	}
	if w := request(t, s, "POST", "/v1/channels", adminTestToken, map[string]any{"name": "finanças da casa", "members": []store.ChannelMember{{AgentID: "cora"}}}); w.Code != 400 {
		t.Fatalf("same name twice: %d", w.Code)
	}
	// Not a conversation of the list, and not archivable as one.
	convs := decode[[]store.Conversation](t, request(t, s, "GET", "/v1/conversations", adminTestToken, nil))
	for _, c := range convs {
		if c.ID == ch.ID {
			t.Fatal("the channel shows among the conversations")
		}
	}

	// Cora is the only one who answers on her own: no router call, and she
	// reads the purpose and who is in the channel.
	post := func(content string) {
		w := request(t, s, "POST", "/v1/conversations/"+ch.ID+"/messages", adminTestToken, MessageRequest{Content: content})
		if w.Code != 202 {
			t.Fatalf("send: %d %s", w.Code, w.Body)
		}
	}
	post("quanto sobra no mês?")
	settleIn(t, s, ch.ID)
	if engine.prompts["router"] != "" {
		t.Fatalf("the router was asked: %q", engine.prompts["router"])
	}
	sys := engine.systems["Cora"]
	if !strings.Contains(sys, "#finanças-da-casa") || !strings.Contains(sys, "Propósito do canal: contas do mês") || !strings.Contains(sys, "No canal com você: Atlas") || strings.Contains(sys, "Vega") && strings.Contains(sys, "No canal com você: Atlas, Vega") {
		t.Fatalf("Cora's system: %q", sys)
	}
	if !strings.Contains(engine.prompts["Cora"], "Canal #finanças-da-casa") {
		t.Fatalf("Cora's prompt: %q", engine.prompts["Cora"])
	}
	msgs := decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/"+ch.ID+"/messages", adminTestToken, nil))
	if len(msgs) != 2 || msgs[1].AgentID != "cora" || msgs[1].Content != "Sobram R$ 800." {
		t.Fatalf("messages: %+v", msgs)
	}
	// #time did not hear any of it.
	if len(channelMessages(t, s)) != 0 {
		t.Fatal("#time got the channel's messages")
	}

	// Atlas answers when named.
	post("@Atlas e o código?")
	settleIn(t, s, ch.ID)
	msgs = decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/"+ch.ID+"/messages", adminTestToken, nil))
	if last := msgs[len(msgs)-1]; last.AgentID != "atlas" {
		t.Fatalf("after @Atlas: %+v", last)
	}

	// Rename, move to a project, then archive: it leaves the list and takes
	// no more messages.
	if _, err := s.store.SaveProject(t.Context(), store.Project{ID: "casa", Name: "Casa"}, store.ActorAdmin, s.now()); err != nil {
		t.Fatal(err)
	}
	w = request(t, s, "PATCH", "/v1/channels/"+ch.ID, adminTestToken, map[string]any{"name": "contas", "project_id": "casa"})
	if got := decode[store.Channel](t, w); got.Name != "contas" || got.ProjectID != "casa" {
		t.Fatalf("patched: %+v", got)
	}
	if conv, _ := s.store.GetConversation(t.Context(), ch.ID); conv.Title != "#contas" {
		t.Fatalf("conversation title: %q", conv.Title)
	}
	request(t, s, "PATCH", "/v1/channels/"+ch.ID, adminTestToken, map[string]any{"archived": true})
	if list := decode[[]ChannelView](t, request(t, s, "GET", "/v1/channels", adminTestToken, nil)); len(list) != 1 {
		t.Fatalf("archived still listed: %+v", list)
	}
	if w := request(t, s, "POST", "/v1/conversations/"+ch.ID+"/messages", adminTestToken, MessageRequest{Content: "oi"}); w.Code != 409 {
		t.Fatalf("message to an archived channel: %d", w.Code)
	}
}
