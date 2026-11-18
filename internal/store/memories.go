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

// MaxMemoryQueryWords bounds the words a memory search matches.
const MaxMemoryQueryWords = 6

// ValidMemoryKind reports whether k is one of the allowed memory kinds.
func ValidMemoryKind(k string) bool {
	switch k {
	case "decision", "fact", "preference", "lesson", "rule":
		return true
	}
	return false
}

// ValidMemoryScope reports whether s is one of the allowed memory scopes.
func ValidMemoryScope(s string) bool {
	return s == "agent" || s == "team"
}

func memoryWords(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	if len(words) > MaxMemoryQueryWords {
		words = words[:MaxMemoryQueryWords]
	}
	return words
}

func normalizeMemory(mem AgentMemory, now time.Time) AgentMemory {
	if !ValidMemoryScope(mem.Scope) {
		mem.Scope = "agent"
	}
	if !ValidMemoryKind(mem.Kind) {
		mem.Kind = "fact"
	}
	if mem.Occurrences < 1 {
		mem.Occurrences = 1
	}
	mem.CreatedAt, mem.UpdatedAt = ts(now), ts(now)
	if mem.ArchivedAt != nil {
		t := ts(*mem.ArchivedAt)
		mem.ArchivedAt = &t
	}
	return mem
}

func cloneMemory(mem AgentMemory) AgentMemory {
	mem.WorkspaceID = effectiveWorkspace(mem.WorkspaceID)
	if !ValidMemoryScope(mem.Scope) {
		mem.Scope = "agent"
	}
	if !ValidMemoryKind(mem.Kind) {
		mem.Kind = "fact"
	}
	if mem.Occurrences < 1 {
		mem.Occurrences = 1
	}
	if mem.ArchivedAt != nil {
		t := mem.ArchivedAt.UTC()
		mem.ArchivedAt = &t
	}
	return mem
}

func memoryEvent(m AgentMemory) map[string]any {
	return map[string]any{
		"memory_id": m.ID, "workspace_id": m.WorkspaceID, "agent_id": m.AgentID,
		"scope": m.Scope, "kind": m.Kind, "verified": m.Verified, "pinned": m.Pinned,
		"source": m.Source, "chars": len(m.Content),
	}
}

func matchMemoryWords(content string, words []string) bool {
	if len(words) == 0 {
		return true
	}
	low := strings.ToLower(content)
	for _, w := range words {
		if !strings.Contains(low, w) {
			return false
		}
	}
	return true
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) AddMemory(ctx context.Context, mem AgentMemory, actor string, now time.Time) (AgentMemory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mem = normalizeMemory(mem, now)
	if mem.Scope == "team" {
		if mem.AgentID != "" {
			ag, ok := m.agents[mem.AgentID]
			if !ok {
				return AgentMemory{}, ErrNotFound
			}
			if mem.WorkspaceID == "" {
				mem.WorkspaceID = ag.WorkspaceID
			}
		}
		if mem.WorkspaceID == "" {
			mem.WorkspaceID = WorkspaceFromContext(ctx)
		}
		mem.WorkspaceID = effectiveWorkspace(mem.WorkspaceID)
	} else {
		ag, ok := m.agents[mem.AgentID]
		if !ok {
			return AgentMemory{}, ErrNotFound
		}
		if mem.WorkspaceID == "" {
			mem.WorkspaceID = ag.WorkspaceID
		}
		mem.WorkspaceID = effectiveWorkspace(mem.WorkspaceID)
	}
	m.nextMemory++
	mem.ID = m.nextMemory
	m.memories = append(m.memories, cloneMemory(mem))
	m.emit(now, EventMemorySaved, actor, "", "", memoryEvent(mem))
	return cloneMemory(mem), nil
}

func (m *Memory) GetMemory(_ context.Context, id int64) (AgentMemory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mem := range m.memories {
		if mem.ID == id {
			return cloneMemory(mem), nil
		}
	}
	return AgentMemory{}, ErrNotFound
}

func (m *Memory) ListMemories(_ context.Context, agentID, query string, limit int) ([]AgentMemory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agents[agentID]; !ok {
		return []AgentMemory{}, nil
	}
	words := memoryWords(query)
	out := []AgentMemory{}
	for i := len(m.memories) - 1; i >= 0; i-- {
		mem := cloneMemory(m.memories[i])
		if mem.Scope != "agent" || mem.AgentID != agentID || mem.ArchivedAt != nil {
			continue
		}
		if matchMemoryWords(mem.Content, words) {
			out = append(out, mem)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID > out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) SearchMemories(ctx context.Context, f MemoryFilter) ([]AgentMemory, error) {
	ws := f.WorkspaceID
	if ws == "" {
		ws = WorkspaceFromContext(ctx)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	words := memoryWords(f.Query)
	out := []AgentMemory{}
	for _, raw := range m.memories {
		mem := cloneMemory(raw)
		if f.ArchivedOnly {
			if mem.ArchivedAt == nil {
				continue
			}
		} else if !f.IncludeArchived && mem.ArchivedAt != nil {
			continue
		}
		if f.AgentOrTeam {
			agentMatch := mem.Scope == "agent" && mem.AgentID == f.AgentID
			teamMatch := mem.Scope == "team" && (ws == "" || mem.WorkspaceID == effectiveWorkspace(ws))
			if !agentMatch && !teamMatch {
				continue
			}
		} else {
			if ws != "" && mem.WorkspaceID != effectiveWorkspace(ws) {
				continue
			}
			if f.AgentID != "" && mem.AgentID != f.AgentID {
				continue
			}
			if f.Scope != "" && mem.Scope != f.Scope {
				continue
			}
		}
		if f.Kind != "" && mem.Kind != f.Kind {
			continue
		}
		if f.Verified != nil && mem.Verified != *f.Verified {
			continue
		}
		if f.Pinned != nil && mem.Pinned != *f.Pinned {
			continue
		}
		if !matchMemoryWords(mem.Content, words) {
			continue
		}
		out = append(out, mem)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID > out[j].ID
	})
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) UpdateMemory(ctx context.Context, id int64, content, actor string, now time.Time) (AgentMemory, error) {
	return m.PatchMemory(ctx, id, MemoryPatch{Content: &content}, actor, now)
}

func (m *Memory) PatchMemory(_ context.Context, id int64, patch MemoryPatch, actor string, now time.Time) (AgentMemory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.memories {
		if m.memories[i].ID != id {
			continue
		}
		cur := cloneMemory(m.memories[i])
		if patch.Content != nil {
			cur.Content = *patch.Content
		}
		if patch.Kind != nil && ValidMemoryKind(*patch.Kind) {
			cur.Kind = *patch.Kind
		}
		if patch.Scope != nil && ValidMemoryScope(*patch.Scope) {
			cur.Scope = *patch.Scope
			if cur.Scope == "team" {
				cur.AgentID = ""
			}
		}
		if patch.Source != nil {
			cur.Source = *patch.Source
		}
		if patch.Verified != nil {
			cur.Verified = *patch.Verified
		}
		if patch.Pinned != nil {
			cur.Pinned = *patch.Pinned
		}
		if patch.Archived != nil {
			if *patch.Archived {
				t := ts(now)
				cur.ArchivedAt = &t
			} else {
				cur.ArchivedAt = nil
			}
		}
		if patch.Occurrences != nil && *patch.Occurrences >= 1 {
			cur.Occurrences = *patch.Occurrences
		}
		cur.UpdatedAt = ts(now)
		m.memories[i] = cur
		m.emit(now, EventMemorySaved, actor, "", "", memoryEvent(cur))
		return cloneMemory(cur), nil
	}
	return AgentMemory{}, ErrNotFound
}

func (m *Memory) DeleteMemory(_ context.Context, id int64, actor string, now time.Time) (AgentMemory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, mem := range m.memories {
		if mem.ID == id {
			cp := cloneMemory(mem)
			m.memories = append(m.memories[:i], m.memories[i+1:]...)
			m.emit(now, EventMemoryDeleted, actor, "", "", memoryEvent(cp))
			return cp, nil
		}
	}
	return AgentMemory{}, ErrNotFound
}

// ── Postgres ────────────────────────────────────────────────────

const memoryColumns = `id, workspace_id, agent_id, scope, kind, content, source, verified, pinned, archived_at, occurrences, created_at, updated_at`

func scanMemory(row pgx.Row) (AgentMemory, error) {
	var mem AgentMemory
	err := row.Scan(&mem.ID, &mem.WorkspaceID, &mem.AgentID, &mem.Scope, &mem.Kind, &mem.Content, &mem.Source,
		&mem.Verified, &mem.Pinned, &mem.ArchivedAt, &mem.Occurrences, &mem.CreatedAt, &mem.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentMemory{}, ErrNotFound
	}
	if err != nil {
		return AgentMemory{}, err
	}
	mem.CreatedAt, mem.UpdatedAt = mem.CreatedAt.UTC(), mem.UpdatedAt.UTC()
	return cloneMemory(mem), nil
}

func (p *Postgres) AddMemory(ctx context.Context, mem AgentMemory, actor string, now time.Time) (AgentMemory, error) {
	mem = normalizeMemory(mem, now)
	var out AgentMemory
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		if mem.Scope == "team" {
			if mem.AgentID != "" {
				var agWS string
				if err := tx.QueryRow(ctx, `SELECT workspace_id FROM agents WHERE id = $1`, mem.AgentID).Scan(&agWS); errors.Is(err, pgx.ErrNoRows) {
					return ErrNotFound
				} else if err != nil {
					return err
				}
				if mem.WorkspaceID == "" {
					mem.WorkspaceID = agWS
				}
			}
			if mem.WorkspaceID == "" {
				mem.WorkspaceID = WorkspaceFromContext(ctx)
			}
			mem.WorkspaceID = effectiveWorkspace(mem.WorkspaceID)
		} else {
			var agWS string
			if err := tx.QueryRow(ctx, `SELECT workspace_id FROM agents WHERE id = $1`, mem.AgentID).Scan(&agWS); errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
			if mem.WorkspaceID == "" {
				mem.WorkspaceID = agWS
			}
			mem.WorkspaceID = effectiveWorkspace(mem.WorkspaceID)
		}
		var err error
		out, err = scanMemory(tx.QueryRow(ctx, `INSERT INTO agent_memories
			(workspace_id, agent_id, scope, kind, content, source, verified, pinned, archived_at, occurrences, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
			RETURNING `+memoryColumns,
			mem.WorkspaceID, mem.AgentID, mem.Scope, mem.Kind, mem.Content, mem.Source,
			mem.Verified, mem.Pinned, mem.ArchivedAt, mem.Occurrences, ts(now)))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventMemorySaved, actor, "", "", memoryEvent(out))
	})
	return out, err
}

func (p *Postgres) GetMemory(ctx context.Context, id int64) (AgentMemory, error) {
	return scanMemory(p.pool.QueryRow(ctx, `SELECT `+memoryColumns+` FROM agent_memories WHERE id = $1`, id))
}

func (p *Postgres) ListMemories(ctx context.Context, agentID, query string, limit int) ([]AgentMemory, error) {
	args := []any{agentID}
	where := "scope = 'agent' AND agent_id = $1 AND archived_at IS NULL"
	for _, w := range memoryWords(query) {
		args = append(args, "%"+strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(w)+"%")
		where += fmt.Sprintf(" AND content ILIKE $%d", len(args))
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := p.pool.Query(ctx, `SELECT `+memoryColumns+` FROM agent_memories WHERE `+where+
		fmt.Sprintf(` ORDER BY updated_at DESC, id DESC LIMIT %d`, limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentMemory{}
	for rows.Next() {
		mem, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, mem)
	}
	return out, rows.Err()
}

func (p *Postgres) SearchMemories(ctx context.Context, f MemoryFilter) ([]AgentMemory, error) {
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
	if f.ArchivedOnly {
		conds = append(conds, "archived_at IS NOT NULL")
	} else if !f.IncludeArchived {
		conds = append(conds, "archived_at IS NULL")
	}
	if f.AgentOrTeam {
		pAgent := addArg(f.AgentID)
		if ws != "" {
			pWS := addArg(effectiveWorkspace(ws))
			conds = append(conds, fmt.Sprintf("((scope = 'agent' AND agent_id = %s) OR (scope = 'team' AND workspace_id = %s))", pAgent, pWS))
		} else {
			conds = append(conds, fmt.Sprintf("((scope = 'agent' AND agent_id = %s) OR scope = 'team')", pAgent))
		}
	} else {
		if ws != "" {
			conds = append(conds, "workspace_id = "+addArg(effectiveWorkspace(ws)))
		}
		if f.AgentID != "" {
			conds = append(conds, "agent_id = "+addArg(f.AgentID))
		}
		if f.Scope != "" {
			conds = append(conds, "scope = "+addArg(f.Scope))
		}
	}
	if f.Kind != "" {
		conds = append(conds, "kind = "+addArg(f.Kind))
	}
	if f.Verified != nil {
		conds = append(conds, "verified = "+addArg(*f.Verified))
	}
	if f.Pinned != nil {
		conds = append(conds, "pinned = "+addArg(*f.Pinned))
	}
	for _, w := range memoryWords(f.Query) {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(w) + "%"
		conds = append(conds, "content ILIKE "+addArg(like))
	}
	where := "TRUE"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := p.pool.Query(ctx, `SELECT `+memoryColumns+` FROM agent_memories WHERE `+where+
		fmt.Sprintf(` ORDER BY pinned DESC, updated_at DESC, id DESC LIMIT %d`, limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentMemory{}
	for rows.Next() {
		mem, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, mem)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateMemory(ctx context.Context, id int64, content, actor string, now time.Time) (AgentMemory, error) {
	return p.PatchMemory(ctx, id, MemoryPatch{Content: &content}, actor, now)
}

func (p *Postgres) PatchMemory(ctx context.Context, id int64, patch MemoryPatch, actor string, now time.Time) (AgentMemory, error) {
	var out AgentMemory
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		cur, err := scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM agent_memories WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if patch.Content != nil {
			cur.Content = *patch.Content
		}
		if patch.Kind != nil && ValidMemoryKind(*patch.Kind) {
			cur.Kind = *patch.Kind
		}
		if patch.Scope != nil && ValidMemoryScope(*patch.Scope) {
			cur.Scope = *patch.Scope
			if cur.Scope == "team" {
				cur.AgentID = ""
			}
		}
		if patch.Source != nil {
			cur.Source = *patch.Source
		}
		if patch.Verified != nil {
			cur.Verified = *patch.Verified
		}
		if patch.Pinned != nil {
			cur.Pinned = *patch.Pinned
		}
		if patch.Archived != nil {
			if *patch.Archived {
				t := ts(now)
				cur.ArchivedAt = &t
			} else {
				cur.ArchivedAt = nil
			}
		}
		if patch.Occurrences != nil && *patch.Occurrences >= 1 {
			cur.Occurrences = *patch.Occurrences
		}
		out, err = scanMemory(tx.QueryRow(ctx, `UPDATE agent_memories
			SET agent_id = $2, scope = $3, kind = $4, content = $5, source = $6,
				verified = $7, pinned = $8, archived_at = $9, occurrences = $10, updated_at = $11
			WHERE id = $1 RETURNING `+memoryColumns,
			id, cur.AgentID, cur.Scope, cur.Kind, cur.Content, cur.Source,
			cur.Verified, cur.Pinned, cur.ArchivedAt, cur.Occurrences, ts(now)))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventMemorySaved, actor, "", "", memoryEvent(out))
	})
	return out, err
}

func (p *Postgres) DeleteMemory(ctx context.Context, id int64, actor string, now time.Time) (AgentMemory, error) {
	var out AgentMemory
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanMemory(tx.QueryRow(ctx, `DELETE FROM agent_memories WHERE id = $1 RETURNING `+memoryColumns, id))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventMemoryDeleted, actor, "", "", memoryEvent(out))
	})
	return out, err
}
