package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Goal statuses.
const (
	GoalActive   = "ativo"
	GoalPaused   = "pausado"
	GoalDone     = "concluido"
	GoalCanceled = "cancelado"
)

const EventGoalSaved = "goal.saved"

// GoalItem is one entry of a goal's funnel: a prospect, a listing, a lead.
type GoalItem struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Stage     string    `json:"stage"`
	Link      string    `json:"link,omitempty"`
	Contact   string    `json:"contact,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	Next      string    `json:"next,omitempty"` // the next action
	UpdatedAt time.Time `json:"updated_at"`
}

// GoalWatch is a page the server re-reads without the model; the agent is
// woken only when its text changed.
type GoalWatch struct {
	ID        string     `json:"id"`
	URL       string     `json:"url"`
	Why       string     `json:"why"`
	Every     int        `json:"every"` // minutes
	Hash      string     `json:"hash,omitempty"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	// A wait (aguardar_evento) watches a Plugin instead of a page: Event is
	// its kind (email, tarefa, instagram_dm...), Args the read tool's
	// arguments, Seen what was already there, Until the deadline.
	Event   string         `json:"event,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
	Seen    []string       `json:"seen,omitempty"`
	Learned bool           `json:"learned,omitempty"`
	Until   *time.Time     `json:"until,omitempty"`
	Keep    bool           `json:"keep,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// Goal is an outcome an agent pursues under a mandate (see 023_goals).
type Goal struct {
	ID             string      `json:"id"`
	AgentID        string      `json:"agent_id"`
	ConversationID string      `json:"conversation_id"`
	Title          string      `json:"title"`
	Outcome        string      `json:"outcome"`
	Deadline       *time.Time  `json:"deadline,omitempty"`
	Mandate        string      `json:"mandate"`
	Never          string      `json:"never"`
	Stages         []string    `json:"stages"`
	Strategy       string      `json:"strategy"`
	Lessons        []string    `json:"lessons"`
	Items          []GoalItem  `json:"items"`
	Watches        []GoalWatch `json:"watches"`
	Status         string      `json:"status"`
	NextWake       *time.Time  `json:"next_wake,omitempty"`
	WakeReason     string      `json:"wake_reason,omitempty"`
	LastReview     *time.Time  `json:"last_review,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

func cleanGoal(g Goal, now time.Time) Goal {
	g.UpdatedAt = ts(now)
	if g.CreatedAt.IsZero() {
		g.CreatedAt = ts(now)
	}
	g.CreatedAt = ts(g.CreatedAt)
	if g.Stages == nil {
		g.Stages = []string{}
	}
	if g.Lessons == nil {
		g.Lessons = []string{}
	}
	if g.Items == nil {
		g.Items = []GoalItem{}
	}
	if g.Watches == nil {
		g.Watches = []GoalWatch{}
	}
	return g
}

func goalEvent(g Goal) map[string]any {
	return map[string]any{"id": g.ID, "agent_id": g.AgentID, "status": g.Status, "items": len(g.Items)}
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) ListGoals(_ context.Context, agentID string) ([]Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Goal{}
	for _, g := range m.goals {
		if agentID == "" || g.AgentID == agentID {
			out = append(out, copyGoal(g))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) GetGoal(_ context.Context, id string) (Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.goals[id]
	if !ok {
		return Goal{}, ErrNotFound
	}
	return copyGoal(g), nil
}

func (m *Memory) SaveGoal(_ context.Context, g Goal, actor string, now time.Time) (Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.goals == nil {
		m.goals = map[string]Goal{}
	}
	if old, ok := m.goals[g.ID]; ok {
		g.CreatedAt = old.CreatedAt
	}
	g = cleanGoal(g, now)
	m.goals[g.ID] = copyGoal(g)
	m.emit(now, EventGoalSaved, actor, "", "", goalEvent(g))
	return g, nil
}

// copyGoal is a deep copy, so callers never share the stored slices.
func copyGoal(g Goal) Goal {
	b, _ := json.Marshal(g)
	var out Goal
	_ = json.Unmarshal(b, &out)
	return out
}

// ── Postgres ────────────────────────────────────────────────────

const goalColumns = `id, agent_id, conversation_id, title, outcome, deadline, mandate, never, stages, strategy,
	lessons, items, watches, status, next_wake, wake_reason, last_review, created_at, updated_at`

func scanGoal(row pgx.Row) (Goal, error) {
	var g Goal
	var stages, lessons, items, watches []byte
	err := row.Scan(&g.ID, &g.AgentID, &g.ConversationID, &g.Title, &g.Outcome, &g.Deadline, &g.Mandate, &g.Never,
		&stages, &g.Strategy, &lessons, &items, &watches, &g.Status, &g.NextWake, &g.WakeReason, &g.LastReview,
		&g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Goal{}, ErrNotFound
	}
	if err != nil {
		return Goal{}, err
	}
	_ = json.Unmarshal(stages, &g.Stages)
	_ = json.Unmarshal(lessons, &g.Lessons)
	_ = json.Unmarshal(items, &g.Items)
	_ = json.Unmarshal(watches, &g.Watches)
	for _, t := range []**time.Time{&g.Deadline, &g.NextWake, &g.LastReview} {
		if *t != nil {
			u := (*t).UTC()
			*t = &u
		}
	}
	g.CreatedAt, g.UpdatedAt = g.CreatedAt.UTC(), g.UpdatedAt.UTC()
	updated := g.UpdatedAt
	g = cleanGoal(g, updated)
	return g, nil
}

func (p *Postgres) ListGoals(ctx context.Context, agentID string) ([]Goal, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+goalColumns+` FROM goals WHERE ($1 = '' OR agent_id = $1) ORDER BY created_at, id`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Goal{}
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *Postgres) GetGoal(ctx context.Context, id string) (Goal, error) {
	return scanGoal(p.pool.QueryRow(ctx, `SELECT `+goalColumns+` FROM goals WHERE id = $1`, id))
}

func (p *Postgres) SaveGoal(ctx context.Context, g Goal, actor string, now time.Time) (Goal, error) {
	g = cleanGoal(g, now)
	stages, _ := json.Marshal(g.Stages)
	lessons, _ := json.Marshal(g.Lessons)
	items, _ := json.Marshal(g.Items)
	watches, _ := json.Marshal(g.Watches)
	var out Goal
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanGoal(tx.QueryRow(ctx, `INSERT INTO goals (`+goalColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
			ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, outcome = EXCLUDED.outcome, deadline = EXCLUDED.deadline,
				mandate = EXCLUDED.mandate, never = EXCLUDED.never, stages = EXCLUDED.stages, strategy = EXCLUDED.strategy,
				lessons = EXCLUDED.lessons, items = EXCLUDED.items, watches = EXCLUDED.watches, status = EXCLUDED.status,
				next_wake = EXCLUDED.next_wake, wake_reason = EXCLUDED.wake_reason, last_review = EXCLUDED.last_review,
				updated_at = EXCLUDED.updated_at
			RETURNING `+goalColumns,
			g.ID, g.AgentID, g.ConversationID, g.Title, g.Outcome, g.Deadline, g.Mandate, g.Never, stages, g.Strategy,
			lessons, items, watches, g.Status, g.NextWake, g.WakeReason, g.LastReview, g.CreatedAt, g.UpdatedAt))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventGoalSaved, actor, "", "", goalEvent(out))
	})
	return out, err
}
