package store

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

func connectionEvent(c Connection) map[string]any {
	return map[string]any{"id": c.ID, "kind": c.Kind, "account": c.Account, "status": c.Status, "agents": c.Agents}
}

func cleanConnection(c Connection, now time.Time) Connection {
	if c.ID == "" {
		c.ID = c.Kind
	}
	if c.Agents == nil {
		c.Agents = []string{}
	}
	if len(c.Meta) == 0 {
		c.Meta = []byte("{}")
	}
	c.UpdatedAt = ts(now)
	if c.CreatedAt.IsZero() {
		c.CreatedAt = ts(now)
	}
	c.CreatedAt = ts(c.CreatedAt)
	if c.LastOK != nil {
		t := ts(*c.LastOK)
		c.LastOK = &t
	}
	return c
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) ListConnections(context.Context) ([]Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Connection, 0, len(m.connections))
	for _, c := range m.connections {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	return out, nil
}

func (m *Memory) GetConnection(_ context.Context, id string) (Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.connections[id]
	if !ok {
		return Connection{}, ErrNotFound
	}
	return c, nil
}

func (m *Memory) SaveConnection(_ context.Context, c Connection, actor string, now time.Time) (Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connections == nil {
		m.connections = map[string]Connection{}
	}
	if c.ID == "" {
		c.ID = c.Kind
	}
	if old, ok := m.connections[c.ID]; ok {
		c.CreatedAt = old.CreatedAt
	}
	c = cleanConnection(c, now)
	c.Secret = append([]byte{}, c.Secret...)
	c.Agents = append([]string{}, c.Agents...)
	c.Meta = append([]byte{}, c.Meta...)
	m.connections[c.ID] = c
	if actor != "" {
		m.emit(now, EventConnectionSaved, actor, "", "", connectionEvent(c))
	}
	return c, nil
}

func (m *Memory) DeleteConnection(_ context.Context, id, actor string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.connections[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.connections, id)
	m.emit(now, EventConnectionDeleted, actor, "", "", connectionEvent(c))
	return nil
}

// ── Postgres ────────────────────────────────────────────────────

const connectionColumns = `id, kind, account, status, last_error, last_ok, agents, secret, meta, created_at, updated_at`

func scanConnection(row pgx.Row) (Connection, error) {
	var c Connection
	err := row.Scan(&c.ID, &c.Kind, &c.Account, &c.Status, &c.LastError, &c.LastOK, &c.Agents, &c.Secret, &c.Meta, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return c, err
}

func (p *Postgres) ListConnections(ctx context.Context) ([]Connection, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+connectionColumns+` FROM connections ORDER BY kind, created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *Postgres) GetConnection(ctx context.Context, id string) (Connection, error) {
	return scanConnection(p.pool.QueryRow(ctx, `SELECT `+connectionColumns+` FROM connections WHERE id = $1`, id))
}

func (p *Postgres) SaveConnection(ctx context.Context, c Connection, actor string, now time.Time) (Connection, error) {
	c = cleanConnection(c, now)
	var out Connection
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanConnection(tx.QueryRow(ctx, `INSERT INTO connections (`+connectionColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (id) DO UPDATE SET account = EXCLUDED.account, status = EXCLUDED.status,
				last_error = EXCLUDED.last_error, last_ok = EXCLUDED.last_ok, agents = EXCLUDED.agents,
				secret = EXCLUDED.secret, meta = EXCLUDED.meta, updated_at = EXCLUDED.updated_at
			RETURNING `+connectionColumns,
			c.ID, c.Kind, c.Account, c.Status, c.LastError, c.LastOK, c.Agents, c.Secret, string(c.Meta), c.CreatedAt, c.UpdatedAt))
		if err != nil || actor == "" {
			return err
		}
		return emit(ctx, tx, now, EventConnectionSaved, actor, "", "", connectionEvent(out))
	})
	return out, err
}

func (p *Postgres) DeleteConnection(ctx context.Context, id, actor string, now time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		c, err := scanConnection(tx.QueryRow(ctx, `DELETE FROM connections WHERE id = $1 RETURNING `+connectionColumns, id))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventConnectionDeleted, actor, "", "", connectionEvent(c))
	})
}
