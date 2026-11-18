package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/jackc/pgx/v5"
)

// Mission statuses. Running: a run is on a node. Review: a run ended and the
// agent is (being) woken to decide. Waiting: the agent asked the owner.
const (
	MissionRunning  = "running"
	MissionReview   = "review"
	MissionWaiting  = "waiting"
	MissionDone     = "done"
	MissionCanceled = "canceled"
	MissionFailed   = "failed"
)

// Checklist step statuses.
const (
	StepTodo    = "pendente"
	StepDoing   = "fazendo"
	StepDone    = "feito"
	StepDropped = "pulado"
)

const EventMissionSaved = "mission.saved"

// MissionStep is one item of a mission's checklist.
type MissionStep struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

// Mission is long work an agent runs in the background (see 019_missions).
type Mission struct {
	ID             string        `json:"id"`
	AgentID        string        `json:"agent_id"`
	ConversationID string        `json:"conversation_id"`
	Title          string        `json:"title"`
	Objective      string        `json:"objective"`
	Plan           []MissionStep `json:"plan"`
	Status         string        `json:"status"`
	// Spec is the run template (engine, mode, folder, account); each run
	// copies it with its own prompt. Runs stay on NodeID, where the CLI
	// session lives.
	Spec        AgentSpec        `json:"-"`
	Target      scheduler.Target `json:"target"`
	NodeID      string           `json:"node_id"`
	Brief       string           `json:"-"` // the first run's prompt, for a run that cannot resume
	Session     string           `json:"-"`
	JobID       string           `json:"job_id,omitempty"`
	Runs        int              `json:"runs"`
	LastResult  string           `json:"last_result,omitempty"`
	Summary     string           `json:"summary,omitempty"`
	WakePending bool             `json:"-"`
	Engine      string           `json:"engine"` // Spec.Engine, for the app
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	FinishedAt  *time.Time       `json:"finished_at,omitempty"`
}

// Active reports whether the mission is not over.
func (m Mission) Active() bool {
	return m.Status == MissionRunning || m.Status == MissionReview || m.Status == MissionWaiting
}

// MissionFilter narrows ListMissions; empty fields match all.
type MissionFilter struct {
	AgentID        string
	ConversationID string
	ActiveOnly     bool
}

func (f MissionFilter) match(m Mission) bool {
	return (f.AgentID == "" || m.AgentID == f.AgentID) && (f.ConversationID == "" || m.ConversationID == f.ConversationID) &&
		(!f.ActiveOnly || m.Active())
}

func missionEvent(m Mission) map[string]any {
	return map[string]any{"id": m.ID, "agent_id": m.AgentID, "conversation_id": m.ConversationID, "status": m.Status, "runs": m.Runs}
}

func cleanMission(m Mission, now time.Time) Mission {
	m.UpdatedAt = ts(now)
	if m.CreatedAt.IsZero() {
		m.CreatedAt = ts(now)
	}
	m.CreatedAt = ts(m.CreatedAt)
	if m.Plan == nil {
		m.Plan = []MissionStep{}
	}
	m.Engine = m.Spec.Engine
	return m
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) ListMissions(_ context.Context, f MissionFilter) ([]Mission, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Mission{}
	for _, x := range m.missions {
		if f.match(x) {
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) GetMission(_ context.Context, id string) (Mission, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	x, ok := m.missions[id]
	if !ok {
		return Mission{}, ErrNotFound
	}
	return x, nil
}

func (m *Memory) MissionByJob(_ context.Context, jobID string) (Mission, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.missions {
		if jobID != "" && x.JobID == jobID {
			return x, nil
		}
	}
	return Mission{}, ErrNotFound
}

func (m *Memory) SaveMission(_ context.Context, x Mission, actor string, now time.Time) (Mission, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.missions == nil {
		m.missions = map[string]Mission{}
	}
	if old, ok := m.missions[x.ID]; ok {
		x.CreatedAt = old.CreatedAt
	}
	x = cleanMission(x, now)
	m.missions[x.ID] = x
	m.emit(now, EventMissionSaved, actor, "", "", missionEvent(x))
	return x, nil
}

// ── Postgres ────────────────────────────────────────────────────

const missionColumns = `id, agent_id, conversation_id, title, objective, plan, status, spec, target, node_id, brief,
	session, job_id, runs, last_result, summary, wake_pending, created_at, updated_at, finished_at`

func scanMission(row pgx.Row) (Mission, error) {
	var m Mission
	var plan, spec []byte
	err := row.Scan(&m.ID, &m.AgentID, &m.ConversationID, &m.Title, &m.Objective, &plan, &m.Status, &spec, &m.Target,
		&m.NodeID, &m.Brief, &m.Session, &m.JobID, &m.Runs, &m.LastResult, &m.Summary, &m.WakePending,
		&m.CreatedAt, &m.UpdatedAt, &m.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Mission{}, ErrNotFound
	}
	if err != nil {
		return Mission{}, err
	}
	_ = json.Unmarshal(plan, &m.Plan)
	_ = json.Unmarshal(spec, &m.Spec)
	if m.Plan == nil {
		m.Plan = []MissionStep{}
	}
	m.Engine = m.Spec.Engine
	m.CreatedAt, m.UpdatedAt = m.CreatedAt.UTC(), m.UpdatedAt.UTC()
	if m.FinishedAt != nil {
		t := m.FinishedAt.UTC()
		m.FinishedAt = &t
	}
	return m, nil
}

func (p *Postgres) ListMissions(ctx context.Context, f MissionFilter) ([]Mission, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+missionColumns+` FROM missions
		WHERE ($1 = '' OR agent_id = $1) AND ($2 = '' OR conversation_id = $2)
			AND (NOT $3 OR status IN ('running', 'review', 'waiting'))
		ORDER BY created_at, id`, f.AgentID, f.ConversationID, f.ActiveOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mission{}
	for rows.Next() {
		m, err := scanMission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (p *Postgres) GetMission(ctx context.Context, id string) (Mission, error) {
	return scanMission(p.pool.QueryRow(ctx, `SELECT `+missionColumns+` FROM missions WHERE id = $1`, id))
}

func (p *Postgres) MissionByJob(ctx context.Context, jobID string) (Mission, error) {
	if jobID == "" {
		return Mission{}, ErrNotFound
	}
	return scanMission(p.pool.QueryRow(ctx, `SELECT `+missionColumns+` FROM missions WHERE job_id = $1 LIMIT 1`, jobID))
}

func (p *Postgres) SaveMission(ctx context.Context, m Mission, actor string, now time.Time) (Mission, error) {
	m = cleanMission(m, now)
	plan, _ := json.Marshal(m.Plan)
	spec, _ := json.Marshal(m.Spec)
	var out Mission
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanMission(tx.QueryRow(ctx, `INSERT INTO missions (`+missionColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
			ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, objective = EXCLUDED.objective, plan = EXCLUDED.plan,
				status = EXCLUDED.status, spec = EXCLUDED.spec, target = EXCLUDED.target, node_id = EXCLUDED.node_id,
				brief = EXCLUDED.brief, session = EXCLUDED.session, job_id = EXCLUDED.job_id, runs = EXCLUDED.runs,
				last_result = EXCLUDED.last_result, summary = EXCLUDED.summary, wake_pending = EXCLUDED.wake_pending,
				updated_at = EXCLUDED.updated_at, finished_at = EXCLUDED.finished_at
			RETURNING `+missionColumns,
			m.ID, m.AgentID, m.ConversationID, m.Title, m.Objective, plan, m.Status, spec, m.Target, m.NodeID, m.Brief,
			m.Session, m.JobID, m.Runs, m.LastResult, m.Summary, m.WakePending, m.CreatedAt, m.UpdatedAt, m.FinishedAt))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventMissionSaved, actor, "", "", missionEvent(out))
	})
	return out, err
}
