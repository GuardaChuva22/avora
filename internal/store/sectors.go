package store

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

func cleanSector(s Sector, now time.Time) Sector {
	s.WorkspaceID = effectiveWorkspace(s.WorkspaceID)
	s.UpdatedAt = ts(now)
	if s.CreatedAt.IsZero() {
		s.CreatedAt = ts(now)
	} else {
		s.CreatedAt = ts(s.CreatedAt)
	}
	return s
}

func sortSectors(out []Sector) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
}

func sectorEvent(s Sector) map[string]any {
	return map[string]any{
		"id": s.ID, "workspace_id": s.WorkspaceID, "name": s.Name,
		"lead_agent_id": s.LeadAgentID, "position": s.Position,
	}
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) ListSectors(ctx context.Context) ([]Sector, error) {
	ws := WorkspaceFromContext(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Sector, 0, len(m.sectors))
	for _, s := range m.sectors {
		cp := *s
		cp.WorkspaceID = effectiveWorkspace(cp.WorkspaceID)
		if ws != "" && cp.WorkspaceID != ws {
			continue
		}
		out = append(out, cp)
	}
	sortSectors(out)
	return out, nil
}

func (m *Memory) GetSector(_ context.Context, id string) (Sector, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sectors[id]
	if !ok {
		return Sector{}, ErrNotFound
	}
	cp := *s
	cp.WorkspaceID = effectiveWorkspace(cp.WorkspaceID)
	return cp, nil
}

func (m *Memory) SaveSector(_ context.Context, s Sector, actor string, now time.Time) (Sector, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sectors == nil {
		m.sectors = map[string]*Sector{}
	}
	if prev, ok := m.sectors[s.ID]; ok {
		s.CreatedAt = prev.CreatedAt
		s.WorkspaceID = effectiveWorkspace(prev.WorkspaceID)
	}
	s = cleanSector(s, now)
	stored := s
	m.sectors[s.ID] = &stored
	m.emit(now, EventSectorSaved, actor, "", "", sectorEvent(s))
	return s, nil
}

func (m *Memory) DeleteSector(_ context.Context, id, actor string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sectors[id]
	if !ok {
		return ErrNotFound
	}
	cp := *s
	delete(m.sectors, id)
	for _, a := range m.agents {
		if a.Sector == id {
			a.Sector = ""
		}
	}
	m.emit(now, EventSectorDeleted, actor, "", "", sectorEvent(cp))
	return nil
}

func (m *Memory) ReorderSectors(_ context.Context, workspaceID string, ids []string, actor string, now time.Time) ([]Sector, error) {
	ws := effectiveWorkspace(workspaceID)
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	pos := 0
	for _, id := range ids {
		s, ok := m.sectors[id]
		if !ok || effectiveWorkspace(s.WorkspaceID) != ws || seen[id] {
			continue
		}
		seen[id] = true
		s.Position = pos
		s.UpdatedAt = ts(now)
		pos++
	}
	rest := []*Sector{}
	for _, s := range m.sectors {
		if effectiveWorkspace(s.WorkspaceID) == ws && !seen[s.ID] {
			rest = append(rest, s)
		}
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].Position != rest[j].Position {
			return rest[i].Position < rest[j].Position
		}
		return rest[i].ID < rest[j].ID
	})
	for _, s := range rest {
		s.Position = pos
		pos++
	}
	out := []Sector{}
	for _, s := range m.sectors {
		cp := *s
		cp.WorkspaceID = effectiveWorkspace(cp.WorkspaceID)
		if cp.WorkspaceID == ws {
			out = append(out, cp)
		}
	}
	sortSectors(out)
	m.emit(now, EventSectorSaved, actor, "", "", map[string]any{"workspace_id": ws, "reordered": len(ids)})
	return out, nil
}

// ── Postgres ────────────────────────────────────────────────────

const sectorColumns = `id, workspace_id, name, color, icon, lead_agent_id, position, created_at, updated_at`

func scanSector(row pgx.Row) (Sector, error) {
	var s Sector
	err := row.Scan(&s.ID, &s.WorkspaceID, &s.Name, &s.Color, &s.Icon, &s.LeadAgentID, &s.Position, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Sector{}, ErrNotFound
	}
	if err != nil {
		return Sector{}, err
	}
	s.WorkspaceID = effectiveWorkspace(s.WorkspaceID)
	s.CreatedAt, s.UpdatedAt = s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	return s, nil
}

func (p *Postgres) ListSectors(ctx context.Context) ([]Sector, error) {
	ws := WorkspaceFromContext(ctx)
	rows, err := p.pool.Query(ctx, `SELECT `+sectorColumns+` FROM sectors
		WHERE ($1 = '' OR workspace_id = $1)
		ORDER BY position, created_at, id`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sector{}
	for rows.Next() {
		s, err := scanSector(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) GetSector(ctx context.Context, id string) (Sector, error) {
	return scanSector(p.pool.QueryRow(ctx, `SELECT `+sectorColumns+` FROM sectors WHERE id = $1`, id))
}

func (p *Postgres) SaveSector(ctx context.Context, s Sector, actor string, now time.Time) (Sector, error) {
	s = cleanSector(s, now)
	var out Sector
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanSector(tx.QueryRow(ctx, `INSERT INTO sectors (`+sectorColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, color = EXCLUDED.color,
				icon = EXCLUDED.icon, lead_agent_id = EXCLUDED.lead_agent_id,
				position = EXCLUDED.position, updated_at = EXCLUDED.updated_at
			RETURNING `+sectorColumns,
			s.ID, s.WorkspaceID, s.Name, s.Color, s.Icon, s.LeadAgentID, s.Position, s.CreatedAt, s.UpdatedAt))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventSectorSaved, actor, "", "", sectorEvent(out))
	})
	return out, err
}

func (p *Postgres) DeleteSector(ctx context.Context, id, actor string, now time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		s, err := scanSector(tx.QueryRow(ctx, `DELETE FROM sectors WHERE id = $1 RETURNING `+sectorColumns, id))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE agents SET sector = '' WHERE sector = $1`, id); err != nil {
			return err
		}
		return emit(ctx, tx, now, EventSectorDeleted, actor, "", "", sectorEvent(s))
	})
}

func (p *Postgres) ReorderSectors(ctx context.Context, workspaceID string, ids []string, actor string, now time.Time) ([]Sector, error) {
	ws := effectiveWorkspace(workspaceID)
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		seen := map[string]bool{}
		pos := 0
		for _, id := range ids {
			if seen[id] {
				continue
			}
			tag, err := tx.Exec(ctx, `UPDATE sectors SET position = $3, updated_at = $4 WHERE id = $1 AND workspace_id = $2`,
				id, ws, pos, ts(now))
			if err != nil {
				return err
			}
			if tag.RowsAffected() > 0 {
				seen[id] = true
				pos++
			}
		}
		rows, err := tx.Query(ctx, `SELECT id FROM sectors WHERE workspace_id = $1 ORDER BY position, created_at, id`, ws)
		if err != nil {
			return err
		}
		var rest []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if !seen[id] {
				rest = append(rest, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range rest {
			if _, err := tx.Exec(ctx, `UPDATE sectors SET position = $2 WHERE id = $1`, id, pos); err != nil {
				return err
			}
			pos++
		}
		return emit(ctx, tx, now, EventSectorSaved, actor, "", "", map[string]any{"workspace_id": ws, "reordered": len(ids)})
	})
	if err != nil {
		return nil, err
	}
	return p.ListSectors(WithWorkspace(ctx, ws))
}
