package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Project groups agents: its instructions (the AGENTS.md of the main
// folder) and its folders. Agents in a project see only its agents and
// the universal ones.
type Project struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	Name         string `json:"name"`
	Color        string `json:"color"`
	Instructions string `json:"instructions"`
	// Folders: the first is the main one (where AGENTS.md lives); the
	// others are extra workspaces.
	Folders   []ProjectFolder `json:"folders"`
	Position  int             `json:"position"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ProjectFolder is a folder on a node: an absolute path on the PC, or a
// path inside the VPS workspace.
type ProjectFolder struct {
	Node  string `json:"node"`
	Path  string `json:"path"`
	Label string `json:"label,omitempty"`
}

const (
	EventProjectSaved   = "project.saved"
	EventProjectDeleted = "project.deleted"
	EventAgentMoved     = "agent.moved"
)

func projectEvent(p Project) map[string]any {
	return map[string]any{"id": p.ID, "name": p.Name, "folders": len(p.Folders)}
}

func cleanProject(p Project, now time.Time) Project {
	p.WorkspaceID = effectiveWorkspace(p.WorkspaceID)
	if p.Folders == nil {
		p.Folders = []ProjectFolder{}
	}
	p.UpdatedAt = ts(now)
	if p.CreatedAt.IsZero() {
		p.CreatedAt = ts(now)
	}
	p.CreatedAt = ts(p.CreatedAt)
	return p
}

func sortProjects(out []Project) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) ListProjects(ctx context.Context) ([]Project, error) {
	ws := WorkspaceFromContext(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Project, 0, len(m.projects))
	for _, p := range m.projects {
		p.WorkspaceID = effectiveWorkspace(p.WorkspaceID)
		if ws != "" && p.WorkspaceID != ws {
			continue
		}
		p.Folders = append([]ProjectFolder{}, p.Folders...)
		out = append(out, p)
	}
	sortProjects(out)
	return out, nil
}

func (m *Memory) GetProject(_ context.Context, id string) (Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return Project{}, ErrNotFound
	}
	p.WorkspaceID = effectiveWorkspace(p.WorkspaceID)
	p.Folders = append([]ProjectFolder{}, p.Folders...)
	return p, nil
}

func (m *Memory) SaveProject(_ context.Context, p Project, actor string, now time.Time) (Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.projects == nil {
		m.projects = map[string]Project{}
	}
	if old, ok := m.projects[p.ID]; ok {
		p.CreatedAt = old.CreatedAt
		p.WorkspaceID = effectiveWorkspace(old.WorkspaceID)
	}
	p = cleanProject(p, now)
	p.Folders = append([]ProjectFolder{}, p.Folders...)
	m.projects[p.ID] = p
	m.emit(now, EventProjectSaved, actor, "", "", projectEvent(p))
	return p, nil
}

// DeleteProject removes a project; its agents become universal.
func (m *Memory) DeleteProject(_ context.Context, id, actor string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.projects, id)
	for _, a := range m.agents {
		if a.ProjectID == id {
			a.ProjectID = ""
		}
	}
	for cid, c := range m.channels {
		if c.ProjectID == id {
			c.ProjectID = ""
			m.channels[cid] = c
		}
	}
	m.emit(now, EventProjectDeleted, actor, "", "", projectEvent(p))
	return nil
}

// MoveAgent puts an agent in a project ("" = universal). It is not a new
// version of the agent.
func (m *Memory) MoveAgent(_ context.Context, agentID, projectID, actor string, now time.Time) (Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[agentID]
	if !ok {
		return Agent{}, ErrNotFound
	}
	if _, ok := m.projects[projectID]; projectID != "" && !ok {
		return Agent{}, ErrNotFound
	}
	a.ProjectID = projectID
	m.emit(now, EventAgentMoved, actor, "", "", map[string]any{"agent_id": agentID, "project_id": projectID})
	return *a, nil
}

// ── Postgres ────────────────────────────────────────────────────

const projectColumns = `id, name, color, instructions, folders, position, created_at, updated_at, workspace_id`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	var folders []byte
	err := row.Scan(&p.ID, &p.Name, &p.Color, &p.Instructions, &folders, &p.Position, &p.CreatedAt, &p.UpdatedAt, &p.WorkspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(folders, &p.Folders)
	}
	if p.Folders == nil {
		p.Folders = []ProjectFolder{}
	}
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	return p, err
}

func (p *Postgres) ListProjects(ctx context.Context) ([]Project, error) {
	ws := WorkspaceFromContext(ctx)
	rows, err := p.pool.Query(ctx, `SELECT `+projectColumns+` FROM projects WHERE ($1 = '' OR workspace_id = $1) ORDER BY position, created_at`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		pr, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

func (p *Postgres) GetProject(ctx context.Context, id string) (Project, error) {
	return scanProject(p.pool.QueryRow(ctx, `SELECT `+projectColumns+` FROM projects WHERE id = $1`, id))
}

func (p *Postgres) SaveProject(ctx context.Context, pr Project, actor string, now time.Time) (Project, error) {
	pr = cleanProject(pr, now)
	folders, _ := json.Marshal(pr.Folders)
	var out Project
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanProject(tx.QueryRow(ctx, `INSERT INTO projects (`+projectColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, color = EXCLUDED.color,
				instructions = EXCLUDED.instructions, folders = EXCLUDED.folders, position = EXCLUDED.position,
				updated_at = EXCLUDED.updated_at
			RETURNING `+projectColumns,
			pr.ID, pr.Name, pr.Color, pr.Instructions, string(folders), pr.Position, pr.CreatedAt, pr.UpdatedAt, pr.WorkspaceID))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventProjectSaved, actor, "", "", projectEvent(out))
	})
	return out, err
}

func (p *Postgres) DeleteProject(ctx context.Context, id, actor string, now time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		pr, err := scanProject(tx.QueryRow(ctx, `DELETE FROM projects WHERE id = $1 RETURNING `+projectColumns, id))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE agents SET project_id = '' WHERE project_id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE channels SET project_id = '' WHERE project_id = $1`, id); err != nil {
			return err
		}
		return emit(ctx, tx, now, EventProjectDeleted, actor, "", "", projectEvent(pr))
	})
}

func (p *Postgres) MoveAgent(ctx context.Context, agentID, projectID, actor string, now time.Time) (Agent, error) {
	var out Agent
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		if projectID != "" {
			var one int
			if err := tx.QueryRow(ctx, `SELECT 1 FROM projects WHERE id = $1`, projectID).Scan(&one); errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
		}
		var err error
		out, err = scanAgent(tx.QueryRow(ctx, `UPDATE agents SET project_id = $2 WHERE id = $1 RETURNING `+agentColumns, agentID, projectID))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventAgentMoved, actor, "", "", map[string]any{"agent_id": agentID, "project_id": projectID})
	})
	return out, err
}
