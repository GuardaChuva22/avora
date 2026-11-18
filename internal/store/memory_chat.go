package store

import (
	"context"
	"sort"
	"time"
)

func (m *Memory) CreateConversation(_ context.Context, c Conversation, actor string) (Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conversations == nil {
		m.conversations = map[string]*Conversation{}
	}
	if _, exists := m.conversations[c.ID]; exists {
		return Conversation{}, ErrConflict
	}
	c.WorkspaceID = effectiveWorkspace(c.WorkspaceID)
	c.CreatedAt = ts(c.CreatedAt)
	c.UpdatedAt = c.CreatedAt
	stored := c
	m.conversations[c.ID] = &stored
	m.emit(c.CreatedAt, EventConversationCreated, actor, "", "",
		map[string]any{"conversation_id": c.ID, "agent_id": c.AgentID, "engine": c.Engine, "model": c.Model})
	return stored, nil
}

func (m *Memory) GetConversation(_ context.Context, id string) (Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conversations[id]
	if !ok {
		return Conversation{}, ErrNotFound
	}
	cp := *c
	cp.WorkspaceID = effectiveWorkspace(cp.WorkspaceID)
	return cp, nil
}

func (m *Memory) ListConversations(ctx context.Context, agentID string, limit int) ([]Conversation, error) {
	ws := WorkspaceFromContext(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Conversation, 0, len(m.conversations))
	for _, c := range m.conversations {
		cp := *c
		cp.WorkspaceID = effectiveWorkspace(cp.WorkspaceID)
		if (ws == "" || cp.WorkspaceID == ws) && (agentID == "" || cp.AgentID == agentID) {
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID < out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) AppendMessage(_ context.Context, msg ChatMessage, actor string) (ChatMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conversations[msg.ConversationID]
	if !ok {
		return ChatMessage{}, ErrNotFound
	}
	return m.appendLocked(c, msg, actor), nil
}

func (m *Memory) appendLocked(c *Conversation, msg ChatMessage, actor string) ChatMessage {
	m.nextMessage++
	msg.ID = m.nextMessage
	msg.CreatedAt = ts(msg.CreatedAt)
	m.messages = append(m.messages, msg)
	if msg.CreatedAt.After(c.UpdatedAt) {
		c.UpdatedAt = msg.CreatedAt
	}
	c.Preview, c.LastRole = previewOf(msg), msg.Role
	m.emit(msg.CreatedAt, EventMessageCreated, actor, msg.JobID, "", messagePayload(msg))
	return msg
}

func (m *Memory) React(_ context.Context, conversationID string, messageID int64, r Reaction, actor string, now time.Time) (ChatMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.messages {
		if m.messages[i].ID == messageID && m.messages[i].ConversationID == conversationID {
			m.messages[i].Reactions = withReaction(m.messages[i].Reactions, r)
			m.emit(ts(now), EventMessageReacted, actor, "", "",
				map[string]any{"message_id": messageID, "conversation_id": conversationID, "agent_id": r.AgentID, "emoji": r.Emoji})
			return m.messages[i], nil
		}
	}
	return ChatMessage{}, ErrNotFound
}

func (m *Memory) Messages(_ context.Context, conversationID string) ([]ChatMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.conversations[conversationID]; !ok {
		return nil, ErrNotFound
	}
	out := []ChatMessage{}
	for _, msg := range m.messages {
		if msg.ConversationID == conversationID {
			out = append(out, msg)
		}
	}
	return out, nil
}

func (m *Memory) RecordEvent(_ context.Context, e Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, Event{
		ID: int64(len(m.events) + 1), At: ts(e.At), Type: e.Type, Actor: e.Actor,
		JobID: e.JobID, NodeID: e.NodeID, Payload: nonEmpty(e.Payload),
	})
	return nil
}
