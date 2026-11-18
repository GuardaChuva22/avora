package control

import (
	"context"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// summarizer is a tool engine that also summarizes (Reply).
type summarizer struct {
	*toolEngine
	inputs []string
}

func (e *summarizer) Reply(_ context.Context, system string, history []store.ChatMessage, _ func(llm.Delta)) (llm.Response, error) {
	e.inputs = append(e.inputs, history[len(history)-1].Content)
	return llm.Response{Content: "- O dono quer organizar as fotos de 2019."}, nil
}

func TestLongConversationsAreCompacted(t *testing.T) {
	e := &summarizer{toolEngine: &toolEngine{calls: map[string][]llm.ToolCall{}, answers: map[string]string{"Cora": "ok"}}}
	s := chatServer(t, e)
	c := agentConversation(t, s, "cora")

	// Short: nothing to compact.
	v := decode[ContextView](t, request(t, s, "GET", "/v1/conversations/"+c.ID+"/context", adminTestToken, nil))
	if v.Window != ContextWindow || v.Used <= 0 || len(v.Parts) != 4 || v.CanCompact {
		t.Fatalf("short: %+v", v)
	}
	if w := request(t, s, "POST", "/v1/conversations/"+c.ID+"/compact", adminTestToken, nil); w.Code != 409 {
		t.Fatalf("short compact: %d", w.Code)
	}

	// 30 long messages pass the limit.
	big := strings.Repeat("fotos de 2019 na pasta Viagens; ", 300)
	ids := []int64{}
	for i := 0; i < 30; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		m, err := s.store.AppendMessage(t.Context(), store.ChatMessage{ConversationID: c.ID, Role: role, Content: big, CreatedAt: s.now()}, store.ActorAdmin)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}
	if v, _ := s.contextOf(t.Context(), c); v.Used < CompactAtTokens || !v.CanCompact {
		t.Fatalf("long: %+v", v)
	}
	if err := s.compact(t.Context(), c.ID, false); err != nil {
		t.Fatal(err)
	}
	sum, err := s.store.GetSummary(t.Context(), c.ID)
	if err != nil || sum.Upto != ids[len(ids)-compactKeep-1] || sum.Count != 1 || !strings.Contains(sum.Summary, "2019") {
		t.Fatalf("summary: %+v %v", sum, err)
	}
	if len(e.inputs) != 1 || !strings.HasPrefix(e.inputs[0], "Mensagens:") || !strings.Contains(e.inputs[0], "Dono: fotos") {
		t.Fatalf("input: %d", len(e.inputs))
	}
	v, _ = s.contextOf(t.Context(), c)
	if v.Messages != compactKeep || v.Hidden != 30-compactKeep || v.Summary == nil || v.Used >= CompactAtTokens || v.Parts[2].Tokens == 0 {
		t.Fatalf("after: %+v", v)
	}
	// The next turn sees only the newest messages plus the summary.
	history, _ := s.store.Messages(t.Context(), c.ID)
	if kept := compacted(history, sum); len(kept) != compactKeep || kept[0].ID != ids[len(ids)-compactKeep] {
		t.Fatalf("kept: %d", len(kept))
	}
}
