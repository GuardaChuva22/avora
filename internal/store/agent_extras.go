package store

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// EventAgentArchived records an agent archived or brought back.
const EventAgentArchived = "agent.archived"

// UsageRow is one day and model of an agent's answers.
type UsageRow struct {
	Day              string `json:"day"` // YYYY-MM-DD in the asked zone
	Model            string `json:"model"`
	Replies          int    `json:"replies"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) ListArchivedAgents(_ context.Context) ([]Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Agent{}
	for _, a := range m.agents {
		if a.ArchivedAt != nil {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ArchivedAt.After(*out[j].ArchivedAt) })
	return out, nil
}

func (m *Memory) ArchiveAgent(_ context.Context, id string, archived bool, actor string, now time.Time) (Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return Agent{}, ErrNotFound
	}
	a.ArchivedAt = nil
	if archived {
		t := ts(now)
		a.ArchivedAt = &t
	}
	m.emit(now, EventAgentArchived, actor, "", "", map[string]any{"agent_id": id, "archived": archived})
	return *a, nil
}

func (m *Memory) AgentUsage(_ context.Context, agentID string, since time.Time, zone *time.Location) ([]UsageRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	type key struct{ day, model string }
	sums := map[key]*UsageRow{}
	for _, msg := range m.messages {
		if msg.Role != "assistant" || msg.CreatedAt.Before(since) {
			continue
		}
		owner := msg.AgentID
		if owner == "" {
			if c, ok := m.conversations[msg.ConversationID]; ok {
				owner = c.AgentID
			}
		}
		if owner != agentID {
			continue
		}
		k := key{msg.CreatedAt.In(zone).Format("2006-01-02"), msg.Model}
		r := sums[k]
		if r == nil {
			r = &UsageRow{Day: k.day, Model: k.model}
			sums[k] = r
		}
		r.Replies++
		r.PromptTokens += msg.PromptTokens
		r.CompletionTokens += msg.CompletionTokens
	}
	out := make([]UsageRow, 0, len(sums))
	for _, r := range sums {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day < out[j].Day
		}
		return out[i].Model < out[j].Model
	})
	return out, nil
}

// ── Postgres ────────────────────────────────────────────────────

func (p *Postgres) ArchiveAgent(ctx context.Context, id string, archived bool, actor string, now time.Time) (Agent, error) {
	var out Agent
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var at *time.Time
		if archived {
			t := ts(now)
			at = &t
		}
		var err error
		out, err = scanAgent(tx.QueryRow(ctx, `UPDATE agents SET archived_at = $2 WHERE id = $1 RETURNING `+agentColumns, id, at))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventAgentArchived, actor, "", "", map[string]any{"agent_id": id, "archived": archived})
	})
	return out, err
}

// AgentUsage counts the agent's answers: its own conversations' and the
// ones it wrote in channels (agent_id on the message).
func (p *Postgres) AgentUsage(ctx context.Context, agentID string, since time.Time, zone *time.Location) ([]UsageRow, error) {
	rows, err := p.pool.Query(ctx, `SELECT to_char((m.created_at AT TIME ZONE $3)::date, 'YYYY-MM-DD'), m.model,
			count(*), coalesce(sum(m.prompt_tokens), 0), coalesce(sum(m.completion_tokens), 0)
		FROM messages m JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'assistant' AND m.created_at >= $2
			AND (m.agent_id = $1 OR (m.agent_id = '' AND c.agent_id = $1))
		GROUP BY 1, 2 ORDER BY 1, 2`, agentID, since, zone.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageRow{}
	for rows.Next() {
		var r UsageRow
		if err := rows.Scan(&r.Day, &r.Model, &r.Replies, &r.PromptTokens, &r.CompletionTokens); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
