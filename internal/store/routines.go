package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

func routineEvent(r Routine) map[string]any {
	return map[string]any{"routine_id": r.ID, "agent_id": r.AgentID, "enabled": r.Enabled, "deliver": r.Deliver}
}

func copyRoutine(r Routine) Routine {
	r.Schedule.Days = append([]int(nil), r.Schedule.Days...)
	if r.Trigger != nil {
		tr := *r.Trigger
		tr.Seen = append([]string(nil), tr.Seen...)
		if tr.Args != nil {
			args := map[string]any{}
			for k, v := range tr.Args {
				args[k] = v
			}
			tr.Args = args
		}
		r.Trigger = &tr
	}
	if r.NextRun != nil {
		t := ts(*r.NextRun)
		r.NextRun = &t
	}
	if r.LastRun != nil {
		t := ts(*r.LastRun)
		r.LastRun = &t
	}
	return r
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) ListRoutines(_ context.Context, agentID string) ([]Routine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Routine{}
	for _, r := range m.routines {
		if agentID == "" || r.AgentID == agentID {
			out = append(out, copyRoutine(*r))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Memory) GetRoutine(_ context.Context, id string) (Routine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.routines[id]
	if !ok {
		return Routine{}, ErrNotFound
	}
	return copyRoutine(*r), nil
}

func (m *Memory) SaveRoutine(_ context.Context, r Routine, actor string, now time.Time) (Routine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agents[r.AgentID]; !ok {
		return Routine{}, ErrNotFound
	}
	if m.routines == nil {
		m.routines = map[string]*Routine{}
	}
	now = ts(now)
	r = copyRoutine(r)
	if prev, ok := m.routines[r.ID]; ok {
		r.CreatedAt = prev.CreatedAt
	} else {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	stored := r
	m.routines[r.ID] = &stored
	if actor != "" {
		m.emit(now, EventRoutineSaved, actor, "", "", routineEvent(r))
	}
	return copyRoutine(stored), nil
}

func (m *Memory) DeleteRoutine(_ context.Context, id, actor string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.routines[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.routines, id)
	m.emit(ts(now), EventRoutineDeleted, actor, "", "", routineEvent(*r))
	return nil
}

// ── Postgres ────────────────────────────────────────────────────

const routineColumns = `id, agent_id, responsibility_id, title, prompt, schedule, deliver, conversation_id, enabled,
	next_run, last_run, last_status, last_preview, failures, created_at, updated_at, skill, trigger`

func scanRoutine(row pgx.Row) (Routine, error) {
	var r Routine
	var sched, trigger []byte
	err := row.Scan(&r.ID, &r.AgentID, &r.ResponsibilityID, &r.Title, &r.Prompt, &sched, &r.Deliver, &r.ConversationID,
		&r.Enabled, &r.NextRun, &r.LastRun, &r.LastStatus, &r.LastPreview, &r.Failures, &r.CreatedAt, &r.UpdatedAt, &r.Skill, &trigger)
	if errors.Is(err, pgx.ErrNoRows) {
		return Routine{}, ErrNotFound
	}
	if err != nil {
		return Routine{}, err
	}
	if err := json.Unmarshal(sched, &r.Schedule); err != nil {
		return Routine{}, err
	}
	if len(trigger) > 0 && string(trigger) != "null" {
		r.Trigger = &Trigger{}
		if err := json.Unmarshal(trigger, r.Trigger); err != nil {
			return Routine{}, err
		}
	}
	r.CreatedAt, r.UpdatedAt = r.CreatedAt.UTC(), r.UpdatedAt.UTC()
	return copyRoutine(r), nil
}

func (p *Postgres) ListRoutines(ctx context.Context, agentID string) ([]Routine, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+routineColumns+` FROM routines
		WHERE $1 = '' OR agent_id = $1 ORDER BY created_at, id`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Routine{}
	for rows.Next() {
		r, err := scanRoutine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) GetRoutine(ctx context.Context, id string) (Routine, error) {
	return scanRoutine(p.pool.QueryRow(ctx, `SELECT `+routineColumns+` FROM routines WHERE id = $1`, id))
}

func (p *Postgres) SaveRoutine(ctx context.Context, r Routine, actor string, now time.Time) (Routine, error) {
	sched, err := json.Marshal(r.Schedule)
	if err != nil {
		return Routine{}, err
	}
	var trigger []byte
	if r.Trigger != nil {
		if trigger, err = json.Marshal(r.Trigger); err != nil {
			return Routine{}, err
		}
	}
	var saved Routine
	err = p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		saved, err = scanRoutine(tx.QueryRow(ctx, `INSERT INTO routines
			(id, agent_id, responsibility_id, title, prompt, schedule, deliver, conversation_id, enabled,
			 next_run, last_run, last_status, last_preview, failures, created_at, updated_at, skill, trigger)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15, $16, $17)
			ON CONFLICT (id) DO UPDATE SET agent_id = EXCLUDED.agent_id, responsibility_id = EXCLUDED.responsibility_id,
				title = EXCLUDED.title, prompt = EXCLUDED.prompt, schedule = EXCLUDED.schedule, deliver = EXCLUDED.deliver,
				conversation_id = EXCLUDED.conversation_id, enabled = EXCLUDED.enabled, next_run = EXCLUDED.next_run,
				last_run = EXCLUDED.last_run, last_status = EXCLUDED.last_status, last_preview = EXCLUDED.last_preview,
				failures = EXCLUDED.failures, updated_at = EXCLUDED.updated_at, skill = EXCLUDED.skill,
				trigger = EXCLUDED.trigger
			RETURNING `+routineColumns,
			r.ID, r.AgentID, r.ResponsibilityID, r.Title, r.Prompt, sched, r.Deliver, r.ConversationID, r.Enabled,
			tsPtr(r.NextRun), tsPtr(r.LastRun), r.LastStatus, r.LastPreview, r.Failures, ts(now), r.Skill, trigger))
		if err != nil {
			var pgErr interface{ SQLState() string }
			if errors.As(err, &pgErr) && pgErr.SQLState() == "23503" {
				return ErrNotFound // the agent does not exist
			}
			return err
		}
		if actor == "" {
			return nil
		}
		return emit(ctx, tx, now, EventRoutineSaved, actor, "", "", routineEvent(saved))
	})
	return saved, err
}

func (p *Postgres) DeleteRoutine(ctx context.Context, id, actor string, now time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		r, err := scanRoutine(tx.QueryRow(ctx, `DELETE FROM routines WHERE id = $1 RETURNING `+routineColumns, id))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventRoutineDeleted, actor, "", "", routineEvent(r))
	})
}

func tsPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := ts(*t)
	return &v
}
