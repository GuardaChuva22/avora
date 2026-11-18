package store

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Skill is written know-how in the SKILL.md format of Agent Skills: the
// name and description go into every prompt that can see it, and the body
// (the instructions) is loaded with skill_ler when a task matches.
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
	// ProjectID limits the skill to a project's agents (and the universal
	// ones); empty = every agent.
	ProjectID string    `json:"project_id"`
	Author    string    `json:"author"` // "admin" or "agent:<id>"
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const (
	EventSkillSaved   = "skill.saved"
	EventSkillDeleted = "skill.deleted"
)

func skillEvent(s Skill) map[string]any {
	return map[string]any{"id": s.ID, "name": s.Name, "project_id": s.ProjectID}
}

func cleanSkill(s Skill, now time.Time) Skill {
	s.UpdatedAt = ts(now)
	if s.CreatedAt.IsZero() {
		s.CreatedAt = ts(now)
	}
	s.CreatedAt = ts(s.CreatedAt)
	return s
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) ListSkills(context.Context) ([]Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Skill, 0, len(m.skills))
	for _, s := range m.skills {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Memory) GetSkill(_ context.Context, id string) (Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.skills[id]
	if !ok {
		return Skill{}, ErrNotFound
	}
	return s, nil
}

func (m *Memory) SaveSkill(_ context.Context, s Skill, actor string, now time.Time) (Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.skills == nil {
		m.skills = map[string]Skill{}
	}
	if old, ok := m.skills[s.ID]; ok {
		s.CreatedAt = old.CreatedAt
	}
	s = cleanSkill(s, now)
	m.skills[s.ID] = s
	m.emit(now, EventSkillSaved, actor, "", "", skillEvent(s))
	return s, nil
}

func (m *Memory) DeleteSkill(_ context.Context, id, actor string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.skills[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.skills, id)
	m.emit(now, EventSkillDeleted, actor, "", "", skillEvent(s))
	return nil
}

// ── Postgres ────────────────────────────────────────────────────

const skillColumns = `id, name, description, body, project_id, author, created_at, updated_at`

func scanSkill(row pgx.Row) (Skill, error) {
	var s Skill
	err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Body, &s.ProjectID, &s.Author, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Skill{}, ErrNotFound
	}
	s.CreatedAt, s.UpdatedAt = s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	return s, err
}

func (p *Postgres) ListSkills(ctx context.Context) ([]Skill, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+skillColumns+` FROM skills ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Skill{}
	for rows.Next() {
		s, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) GetSkill(ctx context.Context, id string) (Skill, error) {
	return scanSkill(p.pool.QueryRow(ctx, `SELECT `+skillColumns+` FROM skills WHERE id = $1`, id))
}

func (p *Postgres) SaveSkill(ctx context.Context, s Skill, actor string, now time.Time) (Skill, error) {
	s = cleanSkill(s, now)
	var out Skill
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanSkill(tx.QueryRow(ctx, `INSERT INTO skills (`+skillColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description,
				body = EXCLUDED.body, project_id = EXCLUDED.project_id, author = EXCLUDED.author,
				updated_at = EXCLUDED.updated_at
			RETURNING `+skillColumns,
			s.ID, s.Name, s.Description, s.Body, s.ProjectID, s.Author, s.CreatedAt, s.UpdatedAt))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventSkillSaved, actor, "", "", skillEvent(out))
	})
	return out, err
}

func (p *Postgres) DeleteSkill(ctx context.Context, id, actor string, now time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		s, err := scanSkill(tx.QueryRow(ctx, `DELETE FROM skills WHERE id = $1 RETURNING `+skillColumns, id))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventSkillDeleted, actor, "", "", skillEvent(s))
	})
}
