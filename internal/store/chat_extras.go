package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) ArchiveConversation(_ context.Context, id string, archived bool, actor string, now time.Time) (Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conversations[id]
	if !ok {
		return Conversation{}, ErrNotFound
	}
	if archived {
		at := ts(now)
		c.ArchivedAt = &at
	} else {
		c.ArchivedAt = nil
	}
	m.emit(now, EventConversationArchive, actor, "", "", map[string]any{"conversation_id": id, "archived": archived})
	return *c, nil
}

func (m *Memory) DeleteConversation(_ context.Context, id, actor string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conversations[id]
	if !ok {
		return ErrNotFound
	}
	if c.JobID != "" {
		return ErrConflict
	}
	delete(m.conversations, id)
	kept, n := make([]ChatMessage, 0, len(m.messages)), 0
	for _, msg := range m.messages {
		if msg.ConversationID == id {
			n++
			continue
		}
		kept = append(kept, msg)
	}
	m.messages = kept
	m.emit(now, EventConversationDeleted, actor, "", "", map[string]any{"conversation_id": id, "agent_id": c.AgentID, "messages": n})
	return nil
}

func (m *Memory) SetAvatar(_ context.Context, agentID string, data []byte, contentType, actor string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agents[agentID]; !ok {
		return ErrNotFound
	}
	if m.avatars == nil {
		m.avatars = map[string]Avatar{}
	}
	m.avatars[agentID] = Avatar{AgentID: agentID, Data: append([]byte(nil), data...), ContentType: contentType, UpdatedAt: ts(now)}
	m.emit(now, EventAvatarSaved, actor, "", "", map[string]any{"agent_id": agentID, "bytes": len(data), "content_type": contentType})
	return nil
}

func (m *Memory) GetAvatar(_ context.Context, agentID string) (Avatar, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.avatars[agentID]
	if !ok {
		return Avatar{}, ErrNotFound
	}
	if _, ok := m.agents[agentID]; !ok {
		return Avatar{}, ErrNotFound
	}
	return a, nil
}

func (m *Memory) DeleteAvatar(_ context.Context, agentID, actor string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.avatars[agentID]; !ok {
		return ErrNotFound
	}
	delete(m.avatars, agentID)
	m.emit(now, EventAvatarDeleted, actor, "", "", map[string]any{"agent_id": agentID})
	return nil
}

func (m *Memory) AvatarTimes(_ context.Context) (map[string]time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]time.Time{}
	for id, a := range m.avatars {
		if _, ok := m.agents[id]; ok {
			out[id] = a.UpdatedAt
		}
	}
	return out, nil
}

// ── Postgres ────────────────────────────────────────────────────

func (p *Postgres) ArchiveConversation(ctx context.Context, id string, archived bool, actor string, now time.Time) (Conversation, error) {
	var c Conversation
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var at *time.Time
		if archived {
			t := ts(now)
			at = &t
		}
		var err error
		c, err = scanConversation(tx.QueryRow(ctx, `UPDATE conversations SET archived_at = $2 WHERE id = $1 RETURNING `+conversationColumns, id, at))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventConversationArchive, actor, "", "", map[string]any{"conversation_id": id, "archived": archived})
	})
	return c, err
}

func (p *Postgres) DeleteConversation(ctx context.Context, id, actor string, now time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		var agentID, jobID string
		err := tx.QueryRow(ctx, `SELECT agent_id, job_id FROM conversations WHERE id = $1 FOR UPDATE`, id).Scan(&agentID, &jobID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if jobID != "" {
			return ErrConflict
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id = $1`, id).Scan(&n); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM conversations WHERE id = $1`, id); err != nil {
			return err
		}
		return emit(ctx, tx, now, EventConversationDeleted, actor, "", "", map[string]any{"conversation_id": id, "agent_id": agentID, "messages": n})
	})
}

func (p *Postgres) SetAvatar(ctx context.Context, agentID string, data []byte, contentType, actor string, now time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agents WHERE id = $1)`, agentID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_avatars (agent_id, data, content_type, updated_at) VALUES ($1, $2, $3, $4)
			ON CONFLICT (agent_id) DO UPDATE SET data = EXCLUDED.data, content_type = EXCLUDED.content_type, updated_at = EXCLUDED.updated_at`,
			agentID, data, contentType, ts(now)); err != nil {
			return err
		}
		return emit(ctx, tx, now, EventAvatarSaved, actor, "", "", map[string]any{"agent_id": agentID, "bytes": len(data), "content_type": contentType})
	})
}

func (p *Postgres) GetAvatar(ctx context.Context, agentID string) (Avatar, error) {
	a := Avatar{AgentID: agentID}
	err := p.pool.QueryRow(ctx, `SELECT data, content_type, updated_at FROM agent_avatars WHERE agent_id = $1`, agentID).
		Scan(&a.Data, &a.ContentType, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Avatar{}, ErrNotFound
	}
	a.UpdatedAt = a.UpdatedAt.UTC()
	return a, err
}

func (p *Postgres) DeleteAvatar(ctx context.Context, agentID, actor string, now time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM agent_avatars WHERE agent_id = $1`, agentID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return emit(ctx, tx, now, EventAvatarDeleted, actor, "", "", map[string]any{"agent_id": agentID})
	})
}

func (p *Postgres) AvatarTimes(ctx context.Context) (map[string]time.Time, error) {
	rows, err := p.pool.Query(ctx, `SELECT agent_id, updated_at FROM agent_avatars`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at.UTC()
	}
	return out, rows.Err()
}
