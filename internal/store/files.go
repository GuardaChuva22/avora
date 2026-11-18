package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// File is a file in the chat: a browser print, an image an agent shows, an
// attachment (see 022_files). The id is random and is the only way to it.
type File struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	AgentID        string    `json:"agent_id"`
	Name           string    `json:"name"`
	Mime           string    `json:"mime"`
	Size           int       `json:"size"`
	Data           []byte    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
}

// MaxFileBytes bounds one stored file.
const MaxFileBytes = 15 << 20

func (m *Memory) SaveFile(_ context.Context, f File) (File, error) {
	if len(f.Data) > MaxFileBytes {
		return File{}, errors.New("file too large")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		m.files = map[string]File{}
	}
	f.Size, f.CreatedAt = len(f.Data), ts(f.CreatedAt)
	m.files[f.ID] = f
	return f, nil
}

func (m *Memory) GetFile(_ context.Context, id string) (File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[id]
	if !ok {
		return File{}, ErrNotFound
	}
	return f, nil
}

func (p *Postgres) SaveFile(ctx context.Context, f File) (File, error) {
	if len(f.Data) > MaxFileBytes {
		return File{}, errors.New("file too large")
	}
	f.Size, f.CreatedAt = len(f.Data), ts(f.CreatedAt)
	_, err := p.pool.Exec(ctx, `INSERT INTO files (id, conversation_id, agent_id, name, mime, data, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, f.ID, f.ConversationID, f.AgentID, f.Name, f.Mime, f.Data, f.CreatedAt)
	return f, err
}

func (p *Postgres) GetFile(ctx context.Context, id string) (File, error) {
	var f File
	err := p.pool.QueryRow(ctx, `SELECT id, conversation_id, agent_id, name, mime, data, created_at FROM files WHERE id = $1`, id).
		Scan(&f.ID, &f.ConversationID, &f.AgentID, &f.Name, &f.Mime, &f.Data, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, ErrNotFound
	}
	f.Size, f.CreatedAt = len(f.Data), f.CreatedAt.UTC()
	return f, err
}
