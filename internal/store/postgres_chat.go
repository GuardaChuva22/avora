package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const conversationColumns = `id, agent_id, title, engine, model, session_id, session_node, job_id, archived_at, preview, last_role, created_at, updated_at, workspace_id`

func scanConversation(row pgx.Row) (Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.AgentID, &c.Title, &c.Engine, &c.Model, &c.SessionID, &c.SessionNode, &c.JobID,
		&c.ArchivedAt, &c.Preview, &c.LastRole, &c.CreatedAt, &c.UpdatedAt, &c.WorkspaceID)
	if c.ArchivedAt != nil {
		t := c.ArchivedAt.UTC()
		c.ArchivedAt = &t
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	c.CreatedAt, c.UpdatedAt = c.CreatedAt.UTC(), c.UpdatedAt.UTC()
	return c, err
}

const messageColumns = `id, conversation_id, role, content, model, prompt_tokens, completion_tokens, duration_ms, job_id, steps, error, agent_id, reply_to, created_at, reactions`

func scanMessage(row pgx.Row) (ChatMessage, error) {
	var m ChatMessage
	var steps, reactions []byte
	err := row.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.Model,
		&m.PromptTokens, &m.CompletionTokens, &m.DurationMS, &m.JobID, &steps, &m.Error, &m.AgentID, &m.ReplyTo, &m.CreatedAt, &reactions)
	if len(steps) > 0 {
		m.Steps = steps
	}
	if err == nil && len(reactions) > 2 {
		err = json.Unmarshal(reactions, &m.Reactions)
	}
	m.CreatedAt = m.CreatedAt.UTC()
	return m, err
}

func (p *Postgres) CreateConversation(ctx context.Context, c Conversation, actor string) (Conversation, error) {
	var created Conversation
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		created, err = scanConversation(tx.QueryRow(ctx, `INSERT INTO conversations (id, agent_id, title, engine, model, created_at, updated_at, workspace_id)
			VALUES ($1, $2, $3, $4, $5, $6, $6, $7) ON CONFLICT (id) DO NOTHING RETURNING `+conversationColumns,
			c.ID, c.AgentID, c.Title, c.Engine, c.Model, ts(c.CreatedAt), effectiveWorkspace(c.WorkspaceID)))
		if errors.Is(err, ErrNotFound) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		return emit(ctx, tx, c.CreatedAt, EventConversationCreated, actor, "", "",
			map[string]any{"conversation_id": c.ID, "agent_id": c.AgentID, "engine": c.Engine, "model": c.Model})
	})
	return created, err
}

func (p *Postgres) GetConversation(ctx context.Context, id string) (Conversation, error) {
	return scanConversation(p.pool.QueryRow(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE id = $1`, id))
}

func (p *Postgres) ListConversations(ctx context.Context, agentID string, limit int) ([]Conversation, error) {
	if limit <= 0 {
		limit = 100
	}
	ws := WorkspaceFromContext(ctx)
	rows, err := p.pool.Query(ctx, `SELECT `+conversationColumns+` FROM conversations
		WHERE ($2 = '' OR agent_id = $2) AND ($3 = '' OR workspace_id = $3) ORDER BY updated_at DESC, id LIMIT $1`, limit, agentID, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *Postgres) AppendMessage(ctx context.Context, m ChatMessage, actor string) (ChatMessage, error) {
	var stored ChatMessage
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		stored, err = appendMessageTx(ctx, tx, m, actor)
		return err
	})
	return stored, err
}

func appendMessageTx(ctx context.Context, tx pgx.Tx, m ChatMessage, actor string) (ChatMessage, error) {
	tag, err := tx.Exec(ctx, `UPDATE conversations SET updated_at = GREATEST(updated_at, $2), preview = $3, last_role = $4
		WHERE id = $1`, m.ConversationID, ts(m.CreatedAt), previewOf(m), m.Role)
	if err != nil {
		return ChatMessage{}, err
	}
	if tag.RowsAffected() == 0 {
		return ChatMessage{}, ErrNotFound
	}
	var steps []byte
	if len(m.Steps) > 0 {
		steps = m.Steps
	}
	stored, err := scanMessage(tx.QueryRow(ctx, `INSERT INTO messages
		(conversation_id, role, content, model, prompt_tokens, completion_tokens, duration_ms, job_id, steps, error, agent_id, reply_to, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING `+messageColumns,
		m.ConversationID, m.Role, m.Content, m.Model, m.PromptTokens, m.CompletionTokens, m.DurationMS,
		m.JobID, steps, m.Error, m.AgentID, m.ReplyTo, ts(m.CreatedAt)))
	if err != nil {
		return ChatMessage{}, err
	}
	return stored, emit(ctx, tx, m.CreatedAt, EventMessageCreated, actor, m.JobID, "", messagePayload(stored))
}

func (p *Postgres) React(ctx context.Context, conversationID string, messageID int64, r Reaction, actor string, now time.Time) (ChatMessage, error) {
	var out ChatMessage
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		m, err := scanMessage(tx.QueryRow(ctx, `SELECT `+messageColumns+` FROM messages WHERE id = $1 AND conversation_id = $2 FOR UPDATE`, messageID, conversationID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		list, _ := json.Marshal(withReaction(m.Reactions, r))
		if out, err = scanMessage(tx.QueryRow(ctx, `UPDATE messages SET reactions = $2 WHERE id = $1 RETURNING `+messageColumns, messageID, list)); err != nil {
			return err
		}
		return emit(ctx, tx, now, EventMessageReacted, actor, "", "",
			map[string]any{"message_id": messageID, "conversation_id": conversationID, "agent_id": r.AgentID, "emoji": r.Emoji})
	})
	return out, err
}

func (p *Postgres) Messages(ctx context.Context, conversationID string) ([]ChatMessage, error) {
	if _, err := p.GetConversation(ctx, conversationID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT `+messageColumns+` FROM messages WHERE conversation_id = $1 ORDER BY id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (p *Postgres) RecordEvent(ctx context.Context, e Event) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO events (at, type, actor, job_id, node_id, payload)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6)`,
		ts(e.At), e.Type, e.Actor, e.JobID, e.NodeID, []byte(nonEmpty(e.Payload)))
	return err
}
