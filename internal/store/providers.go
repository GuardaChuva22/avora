package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// ModelMeta holds probed capabilities and optional pricing (USD per 1M tokens)
// for a single model of a provider.
type ModelMeta struct {
	Tools       *bool   `json:"tools,omitempty"`
	InputPrice  float64 `json:"input_price,omitempty"`
	OutputPrice float64 `json:"output_price,omitempty"`
	Context     int     `json:"context,omitempty"`
}

func cloneModelMeta(in map[string]ModelMeta) map[string]ModelMeta {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ModelMeta, len(in))
	for k, v := range in {
		if v.Tools != nil {
			b := *v.Tools
			v.Tools = &b
		}
		out[k] = v
	}
	return out
}

// Provider is a source of models the owner configured: an API, a local
// llama.cpp server or a subscription CLI account on a node. The API key is
// sealed by the vault and never serialized.
type Provider struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Kind        string `json:"kind"` // a ProviderKinds kind (control/models.go)
	Name        string `json:"name"`
	BaseURL     string `json:"base_url,omitempty"`
	// APIFormat selects the wire protocol for custom endpoints: "" / "openai"
	// or "anthropic".
	APIFormat string `json:"api_format,omitempty"`
	// Account names who is logged in (a CLI subscription's e-mail).
	Account string `json:"account,omitempty"`
	// NodeID is where a CLI account or local model server lives.
	NodeID     string               `json:"node_id,omitempty"`
	SharedWith []string             `json:"shared_with"`
	Status     string               `json:"status"` // ok, error, login (waiting for the owner)
	LastError  string               `json:"last_error,omitempty"`
	Models     []string             `json:"models"`
	ModelMeta  map[string]ModelMeta `json:"model_meta,omitempty"`
	Secret     []byte               `json:"-"`
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
}

const (
	EventProviderSaved   = "provider.saved"
	EventProviderDeleted = "provider.deleted"
	EventSettingSaved    = "setting.saved"
)

func providerEvent(p Provider) map[string]any {
	return map[string]any{"id": p.ID, "kind": p.Kind, "name": p.Name, "status": p.Status}
}

func cleanProvider(p Provider, now time.Time) Provider {
	p.WorkspaceID = effectiveWorkspace(p.WorkspaceID)
	if p.Models == nil {
		p.Models = []string{}
	}
	if p.SharedWith == nil {
		p.SharedWith = []string{}
	}
	if p.Secret == nil {
		p.Secret = []byte{}
	}
	p.UpdatedAt = ts(now)
	if p.CreatedAt.IsZero() {
		p.CreatedAt = ts(now)
	}
	p.CreatedAt = ts(p.CreatedAt)
	return p
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) ListProviders(ctx context.Context) ([]Provider, error) {
	ws := WorkspaceFromContext(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		p.WorkspaceID = effectiveWorkspace(p.WorkspaceID)
		if ws != "" && p.WorkspaceID != ws {
			continue
		}
		p.Models = append([]string{}, p.Models...)
		p.SharedWith = append([]string{}, p.SharedWith...)
		p.ModelMeta = cloneModelMeta(p.ModelMeta)
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Memory) GetProvider(_ context.Context, id string) (Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.providers[id]
	if !ok {
		return Provider{}, ErrNotFound
	}
	p.WorkspaceID = effectiveWorkspace(p.WorkspaceID)
	p.Models = append([]string{}, p.Models...)
	p.SharedWith = append([]string{}, p.SharedWith...)
	p.ModelMeta = cloneModelMeta(p.ModelMeta)
	return p, nil
}

func (m *Memory) SaveProvider(_ context.Context, p Provider, actor string, now time.Time) (Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.providers == nil {
		m.providers = map[string]Provider{}
	}
	if old, ok := m.providers[p.ID]; ok {
		p.CreatedAt = old.CreatedAt
		p.WorkspaceID = effectiveWorkspace(old.WorkspaceID)
	}
	p = cleanProvider(p, now)
	p.Models = append([]string{}, p.Models...)
	p.SharedWith = append([]string{}, p.SharedWith...)
	p.ModelMeta = cloneModelMeta(p.ModelMeta)
	p.Secret = append([]byte{}, p.Secret...)
	m.providers[p.ID] = p
	if actor != "" {
		m.emit(now, EventProviderSaved, actor, "", "", providerEvent(p))
	}
	return p, nil
}

func (m *Memory) DeleteProvider(_ context.Context, id, actor string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.providers[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.providers, id)
	m.emit(now, EventProviderDeleted, actor, "", "", providerEvent(p))
	return nil
}

func (m *Memory) GetSetting(_ context.Context, key string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.settings[key]
	if !ok {
		return nil, ErrNotFound
	}
	return append(json.RawMessage{}, v...), nil
}

func (m *Memory) SetSetting(_ context.Context, key string, value json.RawMessage, actor string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings == nil {
		m.settings = map[string]json.RawMessage{}
	}
	m.settings[key] = append(json.RawMessage{}, value...)
	if actor != "" {
		m.emit(now, EventSettingSaved, actor, "", "", map[string]any{"key": key})
	}
	return nil
}

// ── Postgres ────────────────────────────────────────────────────

const providerColumns = `id, kind, name, base_url, api_format, account, node_id, status, last_error, models, model_meta, secret, created_at, updated_at, workspace_id, shared_with`

func scanProvider(row pgx.Row) (Provider, error) {
	var p Provider
	var models, meta, shared []byte
	err := row.Scan(&p.ID, &p.Kind, &p.Name, &p.BaseURL, &p.APIFormat, &p.Account, &p.NodeID, &p.Status, &p.LastError, &models, &meta, &p.Secret, &p.CreatedAt, &p.UpdatedAt, &p.WorkspaceID, &shared)
	if errors.Is(err, pgx.ErrNoRows) {
		return Provider{}, ErrNotFound
	}
	if err == nil && len(models) > 0 {
		err = json.Unmarshal(models, &p.Models)
	}
	if err == nil && len(meta) > 0 {
		var m map[string]ModelMeta
		if err = json.Unmarshal(meta, &m); err == nil && len(m) > 0 {
			p.ModelMeta = m
		}
	}
	if err == nil && len(shared) > 0 {
		err = json.Unmarshal(shared, &p.SharedWith)
	}
	if p.Models == nil {
		p.Models = []string{}
	}
	if p.SharedWith == nil {
		p.SharedWith = []string{}
	}
	return p, err
}

func (p *Postgres) ListProviders(ctx context.Context) ([]Provider, error) {
	ws := WorkspaceFromContext(ctx)
	rows, err := p.pool.Query(ctx, `SELECT `+providerColumns+` FROM providers WHERE ($1 = '' OR workspace_id = $1) ORDER BY created_at, id`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Provider{}
	for rows.Next() {
		pr, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

func (p *Postgres) GetProvider(ctx context.Context, id string) (Provider, error) {
	return scanProvider(p.pool.QueryRow(ctx, `SELECT `+providerColumns+` FROM providers WHERE id = $1`, id))
}

func (p *Postgres) SaveProvider(ctx context.Context, pr Provider, actor string, now time.Time) (Provider, error) {
	pr = cleanProvider(pr, now)
	models, _ := json.Marshal(pr.Models)
	shared, _ := json.Marshal(pr.SharedWith)
	metaMap := pr.ModelMeta
	if metaMap == nil {
		metaMap = map[string]ModelMeta{}
	}
	meta, _ := json.Marshal(metaMap)
	var out Provider
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanProvider(tx.QueryRow(ctx, `INSERT INTO providers (`+providerColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			ON CONFLICT (id) DO UPDATE SET kind = EXCLUDED.kind, name = EXCLUDED.name, base_url = EXCLUDED.base_url,
				api_format = EXCLUDED.api_format, account = EXCLUDED.account, node_id = EXCLUDED.node_id, status = EXCLUDED.status,
				last_error = EXCLUDED.last_error, models = EXCLUDED.models, model_meta = EXCLUDED.model_meta, secret = EXCLUDED.secret,
				shared_with = EXCLUDED.shared_with, updated_at = EXCLUDED.updated_at
			RETURNING `+providerColumns,
			pr.ID, pr.Kind, pr.Name, pr.BaseURL, pr.APIFormat, pr.Account, pr.NodeID, pr.Status, pr.LastError, string(models), string(meta), pr.Secret, pr.CreatedAt, pr.UpdatedAt, pr.WorkspaceID, string(shared)))
		if err != nil || actor == "" {
			return err
		}
		return emit(ctx, tx, now, EventProviderSaved, actor, "", "", providerEvent(out))
	})
	return out, err
}

func (p *Postgres) DeleteProvider(ctx context.Context, id, actor string, now time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		pr, err := scanProvider(tx.QueryRow(ctx, `DELETE FROM providers WHERE id = $1 RETURNING `+providerColumns, id))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventProviderDeleted, actor, "", "", providerEvent(pr))
	})
}

func (p *Postgres) GetSetting(ctx context.Context, key string) (json.RawMessage, error) {
	var v []byte
	err := p.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

func (p *Postgres) SetSetting(ctx context.Context, key string, value json.RawMessage, actor string, now time.Time) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, $3)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`, key, string(value), ts(now)); err != nil {
			return err
		}
		if actor == "" {
			return nil
		}
		return emit(ctx, tx, now, EventSettingSaved, actor, "", "", map[string]any{"key": key})
	})
}
