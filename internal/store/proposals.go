package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ValidProposalField reports whether f is an allowed proposal target field.
func ValidProposalField(f string) bool {
	return f == "instructions" || f == "role" || f == "apps"
}

// ValidProposalStatus reports whether st is an allowed proposal status.
func ValidProposalStatus(st string) bool {
	return st == "pending" || st == "applied" || st == "rejected" || st == "undone"
}

func cloneProposal(p Proposal) Proposal {
	p.WorkspaceID = effectiveWorkspace(p.WorkspaceID)
	if p.Status == "" {
		p.Status = "pending"
	}
	if p.DecidedAt != nil {
		t := p.DecidedAt.UTC()
		p.DecidedAt = &t
	}
	return p
}

func proposalEvent(p Proposal) map[string]any {
	return map[string]any{
		"id": p.ID, "workspace_id": p.WorkspaceID, "conversation_id": p.ConversationID,
		"from_agent": p.FromAgent, "target_agent": p.TargetAgent,
		"field": p.Field, "status": p.Status,
	}
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) CreateProposal(ctx context.Context, prop Proposal, actor string, now time.Time) (Proposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proposals == nil {
		m.proposals = map[string]*Proposal{}
	}
	if _, exists := m.proposals[prop.ID]; exists {
		return Proposal{}, ErrConflict
	}
	if prop.WorkspaceID == "" {
		if ag, ok := m.agents[prop.TargetAgent]; ok {
			prop.WorkspaceID = ag.WorkspaceID
		} else {
			prop.WorkspaceID = WorkspaceFromContext(ctx)
		}
	}
	prop.WorkspaceID = effectiveWorkspace(prop.WorkspaceID)
	if prop.Status == "" {
		prop.Status = "pending"
	}
	prop.CreatedAt = ts(now)
	stored := cloneProposal(prop)
	m.proposals[prop.ID] = &stored
	m.emit(now, EventProposalCreated, actor, "", "", proposalEvent(stored))
	return cloneProposal(stored), nil
}

func (m *Memory) GetProposal(_ context.Context, id string) (Proposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.proposals[id]
	if !ok {
		return Proposal{}, ErrNotFound
	}
	return cloneProposal(*p), nil
}

func (m *Memory) ListProposals(ctx context.Context, f ProposalFilter) ([]Proposal, error) {
	ws := f.WorkspaceID
	if ws == "" {
		ws = WorkspaceFromContext(ctx)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Proposal{}
	for _, p := range m.proposals {
		cp := cloneProposal(*p)
		if ws != "" && cp.WorkspaceID != effectiveWorkspace(ws) {
			continue
		}
		if f.ConversationID != "" && cp.ConversationID != f.ConversationID {
			continue
		}
		if f.TargetAgent != "" && cp.TargetAgent != f.TargetAgent {
			continue
		}
		if f.Status != "" && cp.Status != f.Status {
			continue
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) UpdateProposal(_ context.Context, prop Proposal, actor string, now time.Time) (Proposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.proposals[prop.ID]
	if !ok {
		return Proposal{}, ErrNotFound
	}
	prop.CreatedAt = cur.CreatedAt
	prop.WorkspaceID = effectiveWorkspace(cur.WorkspaceID)
	stored := cloneProposal(prop)
	m.proposals[prop.ID] = &stored
	m.emit(now, EventProposalUpdated, actor, "", "", proposalEvent(stored))
	return cloneProposal(stored), nil
}

// ── Postgres ────────────────────────────────────────────────────

const proposalColumns = `id, workspace_id, conversation_id, from_agent, target_agent, field, before_value, after_value, reason, status, pre_version, applied_version, created_at, decided_at`

func scanProposal(row pgx.Row) (Proposal, error) {
	var p Proposal
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.ConversationID, &p.FromAgent, &p.TargetAgent,
		&p.Field, &p.Before, &p.After, &p.Reason, &p.Status, &p.PreVersion, &p.AppliedVersion,
		&p.CreatedAt, &p.DecidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Proposal{}, ErrNotFound
	}
	if err != nil {
		return Proposal{}, err
	}
	p.CreatedAt = p.CreatedAt.UTC()
	return cloneProposal(p), nil
}

func (p *Postgres) CreateProposal(ctx context.Context, prop Proposal, actor string, now time.Time) (Proposal, error) {
	if prop.WorkspaceID == "" {
		if ag, err := p.GetAgent(ctx, prop.TargetAgent); err == nil {
			prop.WorkspaceID = ag.WorkspaceID
		} else {
			prop.WorkspaceID = WorkspaceFromContext(ctx)
		}
	}
	prop.WorkspaceID = effectiveWorkspace(prop.WorkspaceID)
	if prop.Status == "" {
		prop.Status = "pending"
	}
	prop.CreatedAt = ts(now)
	var out Proposal
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanProposal(tx.QueryRow(ctx, `INSERT INTO proposals (`+proposalColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			RETURNING `+proposalColumns,
			prop.ID, prop.WorkspaceID, prop.ConversationID, prop.FromAgent, prop.TargetAgent,
			prop.Field, prop.Before, prop.After, prop.Reason, prop.Status,
			prop.PreVersion, prop.AppliedVersion, prop.CreatedAt, prop.DecidedAt))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventProposalCreated, actor, "", "", proposalEvent(out))
	})
	return out, err
}

func (p *Postgres) GetProposal(ctx context.Context, id string) (Proposal, error) {
	return scanProposal(p.pool.QueryRow(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE id = $1`, id))
}

func (p *Postgres) ListProposals(ctx context.Context, f ProposalFilter) ([]Proposal, error) {
	ws := f.WorkspaceID
	if ws == "" {
		ws = WorkspaceFromContext(ctx)
	}
	var conds []string
	var args []any
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if ws != "" {
		conds = append(conds, "workspace_id = "+addArg(effectiveWorkspace(ws)))
	}
	if f.ConversationID != "" {
		conds = append(conds, "conversation_id = "+addArg(f.ConversationID))
	}
	if f.TargetAgent != "" {
		conds = append(conds, "target_agent = "+addArg(f.TargetAgent))
	}
	if f.Status != "" {
		conds = append(conds, "status = "+addArg(f.Status))
	}
	where := "TRUE"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := p.pool.Query(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE `+where+
		fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT %d`, limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Proposal{}
	for rows.Next() {
		prop, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, prop)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateProposal(ctx context.Context, prop Proposal, actor string, now time.Time) (Proposal, error) {
	var out Proposal
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanProposal(tx.QueryRow(ctx, `UPDATE proposals
			SET before_value = $2, after_value = $3, reason = $4, status = $5,
				pre_version = $6, applied_version = $7, decided_at = $8
			WHERE id = $1 RETURNING `+proposalColumns,
			prop.ID, prop.Before, prop.After, prop.Reason, prop.Status,
			prop.PreVersion, prop.AppliedVersion, prop.DecidedAt))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventProposalUpdated, actor, "", "", proposalEvent(out))
	})
	return out, err
}
