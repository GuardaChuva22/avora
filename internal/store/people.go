package store

import (
	"bytes"
	"context"
	"strings"
	"time"
)

const (
	PresenceAuto      = "auto"
	PresenceAway      = "away"
	PresenceBusy      = "busy"
	PresenceInvisible = "invisible"

	SessionTTL = 30 * 24 * time.Hour
)

// Person is a human account on an Avora server. PasswordHash is never serialized.
type Person struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	Avatar       string     `json:"avatar"`
	About        string     `json:"about"`
	Presence     string     `json:"presence"`
	StatusText   string     `json:"status_text"`
	PasswordHash string     `json:"-"`
	LastSeen     *time.Time `json:"last_seen"`
	LastHomeAt   *time.Time `json:"last_home_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// Session authenticates one logged-in client; only the sha256 of the token is stored.
type Session struct {
	TokenHash []byte    `json:"-"`
	PersonID  string    `json:"person_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func cleanEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func copyPerson(p Person) Person {
	if p.LastSeen != nil {
		t := ts(*p.LastSeen)
		p.LastSeen = &t
	}
	if p.LastHomeAt != nil {
		t := ts(*p.LastHomeAt)
		p.LastHomeAt = &t
	}
	return p
}

func cleanPerson(p Person, now time.Time) Person {
	p.Email = cleanEmail(p.Email)
	if p.Presence == "" {
		p.Presence = PresenceAuto
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = ts(now)
	} else {
		p.CreatedAt = ts(p.CreatedAt)
	}
	if p.LastSeen != nil {
		t := ts(*p.LastSeen)
		p.LastSeen = &t
	}
	if p.LastHomeAt != nil {
		t := ts(*p.LastHomeAt)
		p.LastHomeAt = &t
	}
	return p
}

func personEvent(p Person) map[string]any {
	return map[string]any{"id": p.ID, "email": p.Email, "name": p.Name}
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) CountPeople(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.people), nil
}

func (m *Memory) CreatePerson(_ context.Context, p Person, onlyFirst bool, now time.Time) (Person, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(now)
	if m.people == nil {
		m.people = map[string]*Person{}
	}
	first := len(m.people) == 0
	if onlyFirst && !first {
		return Person{}, false, ErrForbidden
	}
	p = cleanPerson(p, now)
	if p.LastSeen == nil {
		seen := ts(now)
		p.LastSeen = &seen
	}
	if _, exists := m.people[p.ID]; exists {
		return Person{}, false, ErrConflict
	}
	for _, existing := range m.people {
		if existing.Email == p.Email {
			return Person{}, false, ErrConflict
		}
	}
	stored := copyPerson(p)
	m.people[p.ID] = &stored
	if first {
		ws := m.workspaces[DefaultWorkspaceID]
		ws.OwnerID = p.ID
		m.setMemberLocked(DefaultWorkspaceID, p.ID, workspaceMemberRow{
			Role: RoleOwner, Status: MemberActive, JoinedAt: ts(now),
		})
	}
	m.emit(now, EventPersonCreated, PersonActor(p.ID), "", "", personEvent(stored))
	return copyPerson(stored), first, nil
}

func (m *Memory) GetPerson(_ context.Context, id string) (Person, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.people[id]
	if !ok {
		return Person{}, ErrNotFound
	}
	return copyPerson(*p), nil
}

func (m *Memory) PersonByEmail(_ context.Context, email string) (Person, error) {
	want := cleanEmail(email)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.people {
		if p.Email == want {
			return copyPerson(*p), nil
		}
	}
	return Person{}, ErrNotFound
}

func (m *Memory) UpdatePerson(_ context.Context, p Person) (Person, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.people[p.ID]
	if !ok {
		return Person{}, ErrNotFound
	}
	p.Email = cleanEmail(p.Email)
	if p.Email == "" {
		p.Email = cur.Email
	}
	for _, other := range m.people {
		if other.ID != p.ID && other.Email == p.Email {
			return Person{}, ErrConflict
		}
	}
	p.CreatedAt = cur.CreatedAt
	if p.PasswordHash == "" {
		p.PasswordHash = cur.PasswordHash
	}
	if p.Presence == "" {
		p.Presence = cur.Presence
	}
	if p.LastSeen == nil {
		p.LastSeen = cur.LastSeen
	}
	if p.LastHomeAt == nil {
		p.LastHomeAt = cur.LastHomeAt
	}
	profileChanged := p.Email != cur.Email || p.Name != cur.Name || p.Avatar != cur.Avatar ||
		p.About != cur.About || p.Presence != cur.Presence || p.StatusText != cur.StatusText ||
		p.PasswordHash != cur.PasswordHash
	stored := copyPerson(p)
	m.people[p.ID] = &stored
	if profileChanged {
		now := time.Now()
		if stored.LastSeen != nil {
			now = *stored.LastSeen
		}
		m.emit(now, EventPersonUpdated, PersonActor(p.ID), "", "", personEvent(stored))
	}
	return copyPerson(stored), nil
}

func (m *Memory) TouchPerson(_ context.Context, id string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.people[id]
	if !ok {
		return ErrNotFound
	}
	seen := ts(now)
	p.LastSeen = &seen
	return nil
}

func (m *Memory) CreateSession(_ context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.people[s.PersonID]; !ok {
		return ErrNotFound
	}
	if m.sessions == nil {
		m.sessions = map[string]Session{}
	}
	key := string(s.TokenHash)
	if _, exists := m.sessions[key]; exists {
		return ErrConflict
	}
	s.TokenHash = bytes.Clone(s.TokenHash)
	s.CreatedAt, s.ExpiresAt = ts(s.CreatedAt), ts(s.ExpiresAt)
	m.sessions[key] = s
	return nil
}

func (m *Memory) PersonBySession(_ context.Context, tokenHash []byte, now time.Time) (Person, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[string(tokenHash)]
	if !ok || !ts(now).Before(s.ExpiresAt) {
		return Person{}, ErrNotFound
	}
	p, ok := m.people[s.PersonID]
	if !ok {
		return Person{}, ErrNotFound
	}
	return copyPerson(*p), nil
}

func (m *Memory) DeleteSession(_ context.Context, tokenHash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, string(tokenHash))
	return nil
}
