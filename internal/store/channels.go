package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Channel is a group conversation of the owner with some agents (D54), like a
// Discord channel: a name, a purpose the agents read, the agents in it and,
// optionally, the project it belongs to. Its messages live in the
// conversation with the same id (engine "team").
type Channel struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Purpose string `json:"purpose"`
	// ProjectID is the project it sits under in the sidebar ("" = none).
	ProjectID string `json:"project_id"`
	// Everyone: every agent is a member, new ones too (#time).
	Everyone bool            `json:"everyone"`
	Members  []ChannelMember `json:"members"`
	Position int             `json:"position"`
	// ArchivedAt hides it from the sidebar; its messages stay.
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// ChannelMember is an agent in a channel. MentionOnly: the router never
// picks it; it answers only when someone writes @Nome.
type ChannelMember struct {
	AgentID     string `json:"agent_id"`
	MentionOnly bool   `json:"mention_only,omitempty"`
}

// Member says whether an agent is in the channel, and how.
func (c Channel) Member(agentID string) (ChannelMember, bool) {
	if c.Everyone {
		for _, m := range c.Members {
			if m.AgentID == agentID {
				return m, true
			}
		}
		return ChannelMember{AgentID: agentID}, true
	}
	for _, m := range c.Members {
		if m.AgentID == agentID {
			return m, true
		}
	}
	return ChannelMember{}, false
}

const (
	EventChannelSaved    = "channel.saved"
	EventChannelArchived = "channel.archived"
)

func channelEvent(c Channel) map[string]any {
	return map[string]any{"id": c.ID, "name": c.Name, "project_id": c.ProjectID, "members": len(c.Members), "everyone": c.Everyone}
}

func cleanChannel(c Channel, now time.Time) Channel {
	c.WorkspaceID = effectiveWorkspace(c.WorkspaceID)
	if c.Members == nil {
		c.Members = []ChannelMember{}
	}
	c.UpdatedAt = ts(now)
	if c.CreatedAt.IsZero() {
		c.CreatedAt = ts(now)
	}
	c.CreatedAt = ts(c.CreatedAt)
	return c
}

func sortChannels(out []Channel) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) ListChannels(ctx context.Context) ([]Channel, error) {
	ws := WorkspaceFromContext(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Channel, 0, len(m.channels))
	for _, c := range m.channels {
		c.WorkspaceID = effectiveWorkspace(c.WorkspaceID)
		if ws != "" && c.WorkspaceID != ws {
			continue
		}
		c.Members = append([]ChannelMember{}, c.Members...)
		out = append(out, c)
	}
	sortChannels(out)
	return out, nil
}

func (m *Memory) GetChannel(_ context.Context, id string) (Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.channels[id]
	if !ok {
		return Channel{}, ErrNotFound
	}
	c.WorkspaceID = effectiveWorkspace(c.WorkspaceID)
	c.Members = append([]ChannelMember{}, c.Members...)
	return c, nil
}

// SaveChannel creates or updates a channel; a new one gets its conversation.
func (m *Memory) SaveChannel(ctx context.Context, c Channel, actor string, now time.Time) (Channel, error) {
	c.WorkspaceID = effectiveWorkspace(c.WorkspaceID)
	// Its conversation first, so a new channel can be written to at once.
	m.mu.Lock()
	_, existed := m.channels[c.ID]
	m.mu.Unlock()
	if !existed {
		if _, err := m.CreateConversation(ctx, Conversation{ID: c.ID, WorkspaceID: c.WorkspaceID, Title: "#" + c.Name, Engine: "team", CreatedAt: now}, actor); err != nil && !errors.Is(err, ErrConflict) {
			return c, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.channels == nil {
		m.channels = map[string]Channel{}
	}
	if old, ok := m.channels[c.ID]; ok {
		c.CreatedAt = old.CreatedAt
		c.WorkspaceID = effectiveWorkspace(old.WorkspaceID)
	}
	c = cleanChannel(c, now)
	c.Members = append([]ChannelMember{}, c.Members...)
	m.channels[c.ID] = c
	if conv := m.conversations[c.ID]; conv != nil {
		conv.Title = "#" + c.Name
	}
	m.emit(now, EventChannelSaved, actor, "", "", channelEvent(c))
	return c, nil
}

// ── Postgres ────────────────────────────────────────────────────

const channelColumns = `id, name, purpose, project_id, everyone, members, position, archived_at, created_at, updated_at, workspace_id`

func scanChannel(row pgx.Row) (Channel, error) {
	var c Channel
	var members []byte
	err := row.Scan(&c.ID, &c.Name, &c.Purpose, &c.ProjectID, &c.Everyone, &members, &c.Position, &c.ArchivedAt, &c.CreatedAt, &c.UpdatedAt, &c.WorkspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Channel{}, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(members, &c.Members)
	}
	if c.Members == nil {
		c.Members = []ChannelMember{}
	}
	c.CreatedAt, c.UpdatedAt = c.CreatedAt.UTC(), c.UpdatedAt.UTC()
	if c.ArchivedAt != nil {
		t := c.ArchivedAt.UTC()
		c.ArchivedAt = &t
	}
	return c, err
}

func (p *Postgres) ListChannels(ctx context.Context) ([]Channel, error) {
	ws := WorkspaceFromContext(ctx)
	rows, err := p.pool.Query(ctx, `SELECT `+channelColumns+` FROM channels WHERE ($1 = '' OR workspace_id = $1) ORDER BY position, created_at`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *Postgres) GetChannel(ctx context.Context, id string) (Channel, error) {
	return scanChannel(p.pool.QueryRow(ctx, `SELECT `+channelColumns+` FROM channels WHERE id = $1`, id))
}

func (p *Postgres) SaveChannel(ctx context.Context, c Channel, actor string, now time.Time) (Channel, error) {
	c = cleanChannel(c, now)
	members, _ := json.Marshal(c.Members)
	var out Channel
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		// Its conversation first, so a new channel can be written to at once.
		if _, err := tx.Exec(ctx, `INSERT INTO conversations (id, agent_id, title, engine, model, created_at, updated_at, workspace_id)
			VALUES ($1, '', $2, 'team', '', $3, $3, $4) ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title`,
			c.ID, "#"+c.Name, c.CreatedAt, c.WorkspaceID); err != nil {
			return err
		}
		var err error
		out, err = scanChannel(tx.QueryRow(ctx, `INSERT INTO channels (`+channelColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, purpose = EXCLUDED.purpose,
				project_id = EXCLUDED.project_id, everyone = EXCLUDED.everyone, members = EXCLUDED.members,
				position = EXCLUDED.position, archived_at = EXCLUDED.archived_at, updated_at = EXCLUDED.updated_at
			RETURNING `+channelColumns,
			c.ID, c.Name, c.Purpose, c.ProjectID, c.Everyone, string(members), c.Position, c.ArchivedAt, c.CreatedAt, c.UpdatedAt, c.WorkspaceID))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventChannelSaved, actor, "", "", channelEvent(out))
	})
	return out, err
}
