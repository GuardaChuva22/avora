package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRepliesArchiveAndAvatars(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if _, err := s.SaveAgent(ctx, Agent{ID: "atlas", Name: "Atlas", Engine: "native", Mode: "read"}, ActorSystem, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateConversation(ctx, Conversation{ID: "c1", AgentID: "atlas", Engine: "native", CreatedAt: t0}, ActorAdmin); err != nil {
			t.Fatal(err)
		}
		first, _ := s.AppendMessage(ctx, ChatMessage{ConversationID: "c1", Role: "assistant", Content: "oi", CreatedAt: t0}, ActorAdmin)
		reply, err := s.AppendMessage(ctx, ChatMessage{ConversationID: "c1", Role: "user", Content: "isso", ReplyTo: first.ID, CreatedAt: t0}, ActorAdmin)
		if err != nil || reply.ReplyTo != first.ID {
			t.Fatalf("reply: %+v %v", reply, err)
		}
		if msgs, _ := s.Messages(ctx, "c1"); len(msgs) != 2 || msgs[1].ReplyTo != first.ID {
			t.Fatalf("messages: %+v", msgs)
		}

		c, err := s.ArchiveConversation(ctx, "c1", true, ActorAdmin, t0.Add(time.Minute))
		if err != nil || c.ArchivedAt == nil || !c.ArchivedAt.Equal(t0.Add(time.Minute)) {
			t.Fatalf("archive: %+v %v", c, err)
		}
		if c, _ = s.GetConversation(ctx, "c1"); c.ArchivedAt == nil {
			t.Fatal("archive not stored")
		}
		if c, _ = s.ArchiveConversation(ctx, "c1", false, ActorAdmin, t0); c.ArchivedAt != nil {
			t.Fatal("not unarchived")
		}

		if err := s.StartConversationJob(ctx, "c1", "turn-1"); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteConversation(ctx, "c1", ActorAdmin, t0); !errors.Is(err, ErrConflict) {
			t.Fatalf("delete while busy: %v", err)
		}
		if _, err := s.FinishConversationJob(ctx, "c1", "turn-1", ChatMessage{Role: "assistant", Content: "x", CreatedAt: t0}, "", "", ActorSystem); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteConversation(ctx, "c1", ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Messages(ctx, "c1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("messages after delete: %v", err)
		}

		if err := s.SetAvatar(ctx, "ghost", []byte("x"), "image/webp", ActorAdmin, t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("avatar of nobody: %v", err)
		}
		if err := s.SetAvatar(ctx, "atlas", []byte("one"), "image/webp", ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		if err := s.SetAvatar(ctx, "atlas", []byte("two"), "image/png", ActorAdmin, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		a, err := s.GetAvatar(ctx, "atlas")
		if err != nil || string(a.Data) != "two" || a.ContentType != "image/png" || !a.UpdatedAt.Equal(t0.Add(time.Hour)) {
			t.Fatalf("avatar: %+v %v", a, err)
		}
		if times, _ := s.AvatarTimes(ctx); len(times) != 1 || !times["atlas"].Equal(t0.Add(time.Hour)) {
			t.Fatalf("times: %+v", times)
		}
		if err := s.DeleteAvatar(ctx, "atlas", ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetAvatar(ctx, "atlas"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("after delete: %v", err)
		}
	})
}

func TestReactionsAndStyle(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		a, err := s.SaveAgent(ctx, Agent{ID: "cora", Name: "Cora", Engine: "native", Mode: "read", Style: AgentStyle{Voice: "seca", Talk: "pouco"}}, ActorSystem, t0)
		if err != nil || a.Style.Voice != "seca" {
			t.Fatalf("save: %+v %v", a.Style, err)
		}
		if a, _ = s.GetAgent(ctx, "cora"); a.Style.Talk != "pouco" {
			t.Fatalf("style not stored: %+v", a.Style)
		}
		if _, err := s.CreateConversation(ctx, Conversation{ID: "c1", AgentID: "cora", Engine: "native", CreatedAt: t0}, ActorAdmin); err != nil {
			t.Fatal(err)
		}
		m, _ := s.AppendMessage(ctx, ChatMessage{ConversationID: "c1", Role: "user", Content: "valeu", CreatedAt: t0}, ActorAdmin)
		if _, err := s.React(ctx, "c1", m.ID, Reaction{AgentID: "cora", Emoji: "👍"}, "agent:cora", t0); err != nil {
			t.Fatal(err)
		}
		// A new emoji from the same agent replaces its old one.
		got, err := s.React(ctx, "c1", m.ID, Reaction{AgentID: "cora", Emoji: "❤️"}, "agent:cora", t0)
		if err != nil || len(got.Reactions) != 1 || got.Reactions[0].Emoji != "❤️" {
			t.Fatalf("react: %+v %v", got.Reactions, err)
		}
		if msgs, _ := s.Messages(ctx, "c1"); len(msgs[0].Reactions) != 1 {
			t.Fatalf("not stored: %+v", msgs)
		}
		if _, err := s.React(ctx, "c1", 999999, Reaction{AgentID: "cora", Emoji: "👍"}, "agent:cora", t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing message: %v", err)
		}
	})
}
