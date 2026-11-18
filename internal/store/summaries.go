package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ConversationSummary stands in the model's context for the messages of a
// conversation up to Upto (see 020_conversation_summaries).
type ConversationSummary struct {
	ConversationID string    `json:"conversation_id"`
	Summary        string    `json:"summary"`
	Upto           int64     `json:"upto"`
	Count          int       `json:"count"`
	UpdatedAt      time.Time `json:"updated_at"`
}

const EventConversationCompacted = "conversation.compacted"

func (m *Memory) GetSummary(_ context.Context, convID string) (ConversationSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.summaries[convID]
	if !ok {
		return ConversationSummary{}, ErrNotFound
	}
	return s, nil
}

func (m *Memory) SaveSummary(_ context.Context, s ConversationSummary, actor string, now time.Time) (ConversationSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.summaries == nil {
		m.summaries = map[string]ConversationSummary{}
	}
	s.UpdatedAt = ts(now)
	m.summaries[s.ConversationID] = s
	m.emit(now, EventConversationCompacted, actor, "", "", map[string]any{"conversation_id": s.ConversationID, "upto": s.Upto, "count": s.Count})
	return s, nil
}

func (p *Postgres) GetSummary(ctx context.Context, convID string) (ConversationSummary, error) {
	var s ConversationSummary
	err := p.pool.QueryRow(ctx, `SELECT conversation_id, summary, upto, count, updated_at FROM conversation_summaries
		WHERE conversation_id = $1`, convID).Scan(&s.ConversationID, &s.Summary, &s.Upto, &s.Count, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ConversationSummary{}, ErrNotFound
	}
	s.UpdatedAt = s.UpdatedAt.UTC()
	return s, err
}

func (p *Postgres) SaveSummary(ctx context.Context, s ConversationSummary, actor string, now time.Time) (ConversationSummary, error) {
	s.UpdatedAt = ts(now)
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_summaries (conversation_id, summary, upto, count, updated_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (conversation_id) DO UPDATE SET summary = EXCLUDED.summary, upto = EXCLUDED.upto,
				count = EXCLUDED.count, updated_at = EXCLUDED.updated_at`,
			s.ConversationID, s.Summary, s.Upto, s.Count, s.UpdatedAt); err != nil {
			return err
		}
		return emit(ctx, tx, now, EventConversationCompacted, actor, "", "", map[string]any{"conversation_id": s.ConversationID, "upto": s.Upto, "count": s.Count})
	})
	return s, err
}
