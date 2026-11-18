package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const agentColumns = `id, name, role, persona, engine, model, mode, color, position, responsibilities, version, created_at, updated_at, project_id, style, brain, approvals, archived_at, workspace_id, sector, reports_to, pinned, autonomy, apps, home_node_id, home_dir`

func scanAgent(row pgx.Row) (Agent, error) {
	var a Agent
	var resp, style, brain, autonomy, apps []byte
	err := row.Scan(&a.ID, &a.Name, &a.Role, &a.Persona, &a.Engine, &a.Model, &a.Mode, &a.Color,
		&a.Position, &resp, &a.Version, &a.CreatedAt, &a.UpdatedAt, &a.ProjectID, &style, &brain, &a.Approvals, &a.ArchivedAt, &a.WorkspaceID,
		&a.Sector, &a.ReportsTo, &a.Pinned, &autonomy, &apps, &a.HomeNodeID, &a.HomeDir)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	if err == nil && len(resp) > 0 {
		err = json.Unmarshal(resp, &a.Responsibilities)
	}
	if err == nil && len(brain) > 0 {
		err = json.Unmarshal(brain, &a.Brain)
	}
	if err == nil && len(style) > 0 {
		err = json.Unmarshal(style, &a.Style)
	}
	if err == nil && len(autonomy) > 0 {
		err = json.Unmarshal(autonomy, &a.Autonomy)
	}
	if err == nil && len(apps) > 0 {
		err = json.Unmarshal(apps, &a.Apps)
	}
	if a.Responsibilities == nil {
		a.Responsibilities = []Responsibility{}
	}
	a.Autonomy = NormalizeAutonomy(a.Autonomy)
	if len(a.Apps) == 0 {
		a.Apps = nil
	}
	a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
	if a.ArchivedAt != nil {
		t := a.ArchivedAt.UTC()
		a.ArchivedAt = &t
	}
	return a, err
}

func (p *Postgres) ListAgents(ctx context.Context) ([]Agent, error) {
	if ws := WorkspaceFromContext(ctx); ws != "" {
		return p.queryAgents(ctx, `SELECT `+agentColumns+` FROM agents WHERE archived_at IS NULL AND workspace_id = $1 ORDER BY position, id`, ws)
	}
	return p.queryAgents(ctx, `SELECT `+agentColumns+` FROM agents WHERE archived_at IS NULL ORDER BY position, id`)
}

func (p *Postgres) ListArchivedAgents(ctx context.Context) ([]Agent, error) {
	if ws := WorkspaceFromContext(ctx); ws != "" {
		return p.queryAgents(ctx, `SELECT `+agentColumns+` FROM agents WHERE archived_at IS NOT NULL AND workspace_id = $1 ORDER BY archived_at DESC, id`, ws)
	}
	return p.queryAgents(ctx, `SELECT `+agentColumns+` FROM agents WHERE archived_at IS NOT NULL ORDER BY archived_at DESC, id`)
}

func (p *Postgres) queryAgents(ctx context.Context, sql string, args ...any) ([]Agent, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *Postgres) GetAgent(ctx context.Context, id string) (Agent, error) {
	return scanAgent(p.pool.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents WHERE id = $1`, id))
}

func (p *Postgres) SaveAgent(ctx context.Context, a Agent, actor string, now time.Time) (Agent, error) {
	var saved Agent
	a.Autonomy = NormalizeAutonomy(a.Autonomy)
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		saved, err = scanAgent(tx.QueryRow(ctx, `INSERT INTO agents
			(id, name, role, persona, engine, model, mode, color, position, responsibilities, version, created_at, updated_at, project_id, style, brain, approvals, workspace_id, sector, reports_to, pinned, autonomy, apps, home_node_id, home_dir)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $11, 1, $10, $10, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, role = EXCLUDED.role, persona = EXCLUDED.persona,
				engine = EXCLUDED.engine, model = EXCLUDED.model, mode = EXCLUDED.mode, color = EXCLUDED.color,
				position = EXCLUDED.position, responsibilities = EXCLUDED.responsibilities, style = EXCLUDED.style, brain = EXCLUDED.brain, approvals = EXCLUDED.approvals,
				sector = EXCLUDED.sector, reports_to = EXCLUDED.reports_to, pinned = EXCLUDED.pinned, autonomy = EXCLUDED.autonomy, apps = EXCLUDED.apps, home_node_id = EXCLUDED.home_node_id, home_dir = EXCLUDED.home_dir,
				version = agents.version + 1, updated_at = EXCLUDED.updated_at
			RETURNING `+agentColumns,
			a.ID, a.Name, a.Role, a.Persona, a.Engine, a.Model, a.Mode, a.Color, a.Position, ts(now),
			respJSON(a.Responsibilities), a.ProjectID, styleJSON(a.Style), brainJSON(a.Brain), a.Approvals,
			effectiveWorkspace(a.WorkspaceID), a.Sector, a.ReportsTo, a.Pinned, autonomyJSON(a.Autonomy), appsJSON(a.Apps), a.HomeNodeID, a.HomeDir))
		if err != nil {
			return err
		}
		snapshot, err := json.Marshal(saved)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_versions (agent_id, version, snapshot, created_at) VALUES ($1, $2, $3, $4)`,
			saved.ID, saved.Version, snapshot, ts(now)); err != nil {
			return err
		}
		return emit(ctx, tx, now, EventAgentSaved, actor, "", "",
			map[string]any{"agent_id": saved.ID, "version": saved.Version, "engine": saved.Engine})
	})
	return saved, err
}

func autonomyJSON(a AgentAutonomy) []byte {
	b, _ := json.Marshal(NormalizeAutonomy(a))
	return b
}

func appsJSON(apps []string) []byte {
	if apps == nil {
		apps = []string{}
	}
	b, _ := json.Marshal(apps)
	return b
}

func brainJSON(b AgentBrain) []byte {
	out, _ := json.Marshal(b)
	return out
}

func styleJSON(st AgentStyle) []byte {
	b, _ := json.Marshal(st)
	return b
}

func respJSON(r []Responsibility) []byte {
	if r == nil {
		r = []Responsibility{}
	}
	b, _ := json.Marshal(r)
	return b
}

func (p *Postgres) AgentVersions(ctx context.Context, id string) ([]AgentVersion, error) {
	if _, err := p.GetAgent(ctx, id); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT version, snapshot, created_at FROM agent_versions
		WHERE agent_id = $1 ORDER BY version DESC LIMIT 50`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentVersion{}
	for rows.Next() {
		v := AgentVersion{AgentID: id}
		var snapshot []byte
		if err := rows.Scan(&v.Version, &snapshot, &v.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(snapshot, &v.Agent); err != nil {
			return nil, err
		}
		v.Agent.Autonomy = NormalizeAutonomy(v.Agent.Autonomy)
		v.CreatedAt = v.CreatedAt.UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

func (p *Postgres) StartConversationJob(ctx context.Context, convID, jobID string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE conversations SET job_id = $2 WHERE id = $1 AND job_id = ''`, convID, jobID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := p.GetConversation(ctx, convID); err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}

func (p *Postgres) FinishConversationJob(ctx context.Context, convID, jobID string, answer ChatMessage, sessionID, sessionNode, actor string) (ChatMessage, error) {
	var stored ChatMessage
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE conversations SET job_id = '',
				session_id = CASE WHEN $3 <> '' THEN $3 ELSE session_id END,
				session_node = CASE WHEN $3 <> '' THEN $4 ELSE session_node END
			WHERE id = $1 AND job_id = $2 AND $2 <> ''`, convID, jobID, sessionID, sessionNode)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		answer.ConversationID, answer.JobID = convID, jobID
		stored, err = appendMessageTx(ctx, tx, answer, actor)
		return err
	})
	return stored, err
}
