package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// ApprovalStatuses are the states an approval goes through.
var ApprovalStatuses = []string{"pending", "approved", "denied", "expired"}

func approvalEvent(a Approval) map[string]any {
	return map[string]any{"approval_id": a.ID, "agent_id": a.AgentID, "action": a.Action, "status": a.Status,
		"always": a.Always, "conversation_id": a.ConversationID, "turn_id": a.TurnID}
}

func grantEvent(g Grant) map[string]any {
	return map[string]any{"grant_id": g.ID, "agent_id": g.AgentID, "action": g.Action}
}

func newGrantID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func copyApproval(a Approval) Approval {
	if a.DecidedAt != nil {
		t := ts(*a.DecidedAt)
		a.DecidedAt = &t
	}
	return a
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) CreateApproval(_ context.Context, a Approval, actor string) (Approval, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.approvals == nil {
		m.approvals = map[string]*Approval{}
	}
	if _, ok := m.approvals[a.ID]; ok {
		return Approval{}, ErrConflict
	}
	a.Status, a.Always, a.DecidedAt, a.DecidedBy = "pending", false, nil, ""
	a.CreatedAt, a.ExpiresAt = ts(a.CreatedAt), ts(a.ExpiresAt)
	stored := a
	m.approvals[a.ID] = &stored
	m.emit(a.CreatedAt, EventApprovalRequested, actor, "", "", approvalEvent(a))
	return copyApproval(a), nil
}

func (m *Memory) GetApproval(_ context.Context, id string) (Approval, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.approvals[id]
	if !ok {
		return Approval{}, ErrNotFound
	}
	return copyApproval(*a), nil
}

func (m *Memory) ListApprovals(_ context.Context, status string, limit int) ([]Approval, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Approval{}
	for _, a := range m.approvals {
		if status == "" || a.Status == status {
			out = append(out, copyApproval(*a))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) DecideApproval(_ context.Context, id, status string, always bool, actor string, now time.Time) (Approval, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.approvals[id]
	if !ok {
		return Approval{}, ErrNotFound
	}
	if a.Status != "pending" {
		return Approval{}, ErrConflict
	}
	now = ts(now)
	a.Status, a.Always, a.DecidedAt, a.DecidedBy = status, always && status == "approved", &now, actor
	m.emit(now, EventApprovalDecided, actor, "", "", approvalEvent(*a))
	if a.Always {
		if m.grants == nil {
			m.grants = map[string]*Grant{}
		}
		exists := false
		for _, g := range m.grants {
			exists = exists || (g.AgentID == a.AgentID && g.Action == a.Action)
		}
		if _, agent := m.agents[a.AgentID]; agent && !exists {
			g := &Grant{ID: newGrantID(), AgentID: a.AgentID, Action: a.Action, CreatedAt: now, CreatedBy: actor}
			m.grants[g.ID] = g
			m.emit(now, EventGrantCreated, actor, "", "", grantEvent(*g))
		}
	}
	return copyApproval(*a), nil
}

func (m *Memory) ListGrants(_ context.Context, agentID string) ([]Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Grant{}
	for _, g := range m.grants {
		if agentID == "" || g.AgentID == agentID {
			out = append(out, *g)
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

func (m *Memory) DeleteGrant(_ context.Context, id, actor string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.grants[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.grants, id)
	m.emit(ts(now), EventGrantDeleted, actor, "", "", grantEvent(*g))
	return nil
}

// ── Postgres ────────────────────────────────────────────────────

const approvalColumns = `id, agent_id, conversation_id, turn_id, action, summary, status, always,
	created_at, expires_at, decided_at, decided_by`

func scanApproval(row pgx.Row) (Approval, error) {
	var a Approval
	err := row.Scan(&a.ID, &a.AgentID, &a.ConversationID, &a.TurnID, &a.Action, &a.Summary, &a.Status, &a.Always,
		&a.CreatedAt, &a.ExpiresAt, &a.DecidedAt, &a.DecidedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return Approval{}, ErrNotFound
	}
	if err != nil {
		return Approval{}, err
	}
	a.CreatedAt, a.ExpiresAt = a.CreatedAt.UTC(), a.ExpiresAt.UTC()
	if a.DecidedAt != nil {
		t := a.DecidedAt.UTC()
		a.DecidedAt = &t
	}
	return a, nil
}

func (p *Postgres) CreateApproval(ctx context.Context, a Approval, actor string) (Approval, error) {
	var saved Approval
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		saved, err = scanApproval(tx.QueryRow(ctx, `INSERT INTO approvals
			(id, agent_id, conversation_id, turn_id, action, summary, status, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, $8)
			RETURNING `+approvalColumns,
			a.ID, a.AgentID, a.ConversationID, a.TurnID, a.Action, a.Summary, ts(a.CreatedAt), ts(a.ExpiresAt)))
		if err != nil {
			var pgErr interface{ SQLState() string }
			if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
				return ErrConflict
			}
			return err
		}
		return emit(ctx, tx, saved.CreatedAt, EventApprovalRequested, actor, "", "", approvalEvent(saved))
	})
	return saved, err
}

func (p *Postgres) GetApproval(ctx context.Context, id string) (Approval, error) {
	return scanApproval(p.pool.QueryRow(ctx, `SELECT `+approvalColumns+` FROM approvals WHERE id = $1`, id))
}

func (p *Postgres) ListApprovals(ctx context.Context, status string, limit int) ([]Approval, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := p.pool.Query(ctx, `SELECT `+approvalColumns+` FROM approvals
		WHERE $1 = '' OR status = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Approval{}
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *Postgres) DecideApproval(ctx context.Context, id, status string, always bool, actor string, now time.Time) (Approval, error) {
	var saved Approval
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		saved, err = scanApproval(tx.QueryRow(ctx, `UPDATE approvals
			SET status = $2, always = $3, decided_at = $4, decided_by = $5
			WHERE id = $1 AND status = 'pending'
			RETURNING `+approvalColumns, id, status, always && status == "approved", ts(now), actor))
		if errors.Is(err, ErrNotFound) {
			if _, getErr := scanApproval(tx.QueryRow(ctx, `SELECT `+approvalColumns+` FROM approvals WHERE id = $1`, id)); getErr == nil {
				return ErrConflict
			}
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := emit(ctx, tx, now, EventApprovalDecided, actor, "", "", approvalEvent(saved)); err != nil {
			return err
		}
		if !saved.Always {
			return nil
		}
		g := Grant{ID: newGrantID(), AgentID: saved.AgentID, Action: saved.Action, CreatedAt: ts(now), CreatedBy: actor}
		tag, err := tx.Exec(ctx, `INSERT INTO grants (id, agent_id, action, created_at, created_by)
			SELECT $1, $2, $3, $4, $5 WHERE EXISTS (SELECT 1 FROM agents WHERE id = $2)
			ON CONFLICT (agent_id, action) DO NOTHING`, g.ID, g.AgentID, g.Action, g.CreatedAt, g.CreatedBy)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return emit(ctx, tx, now, EventGrantCreated, actor, "", "", grantEvent(g))
	})
	return saved, err
}

func (p *Postgres) ListGrants(ctx context.Context, agentID string) ([]Grant, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, agent_id, action, created_at, created_by FROM grants
		WHERE $1 = '' OR agent_id = $1 ORDER BY created_at, id`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.ID, &g.AgentID, &g.Action, &g.CreatedAt, &g.CreatedBy); err != nil {
			return nil, err
		}
		g.CreatedAt = g.CreatedAt.UTC()
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteGrant(ctx context.Context, id, actor string, now time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		var g Grant
		err := tx.QueryRow(ctx, `DELETE FROM grants WHERE id = $1 RETURNING id, agent_id, action, created_at, created_by`, id).
			Scan(&g.ID, &g.AgentID, &g.Action, &g.CreatedAt, &g.CreatedBy)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventGrantDeleted, actor, "", "", grantEvent(g))
	})
}
