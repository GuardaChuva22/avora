package store

import (
	"bytes"
	"context"
	"sort"
	"time"
)

const (
	DefaultWorkspaceID = "pessoal"

	WorkspacePersonal = "personal"
	WorkspaceTeam     = "team"

	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"

	MemberActive  = "active"
	MemberPending = "pending"
)

type workspaceCtxKey struct{}

// WithWorkspace scopes list queries on ctx to a single workspace.
func WithWorkspace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, workspaceCtxKey{}, id)
}

// WorkspaceFromContext returns the workspace bound to ctx, or "" when unscoped.
func WorkspaceFromContext(ctx context.Context) string {
	id, _ := ctx.Value(workspaceCtxKey{}).(string)
	return id
}

func effectiveWorkspace(id string) string {
	if id == "" {
		return DefaultWorkspaceID
	}
	return id
}

// Workspace is an isolated space of agents, channels, projects and docs.
type Workspace struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Icon         string    `json:"icon"`
	Mode         string    `json:"mode"`
	OwnerID      string    `json:"owner_id"`
	ApproveJoins bool      `json:"approve_joins"`
	CreatedAt    time.Time `json:"created_at"`
}

// Member is a person's membership in a workspace, joined with their profile.
type Member struct {
	WorkspaceID string     `json:"-"`
	PersonID    string     `json:"person_id"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	Avatar      string     `json:"avatar"`
	Role        string     `json:"role"`
	Status      string     `json:"status"`
	Online      bool       `json:"online"`
	Presence    string     `json:"presence"`
	StatusText  string     `json:"status_text"`
	LastSeen    *time.Time `json:"-"`
	JoinedAt    time.Time  `json:"joined_at"`
}

// PersonWorkspace is a workspace as seen by one member, with active member count.
type PersonWorkspace struct {
	Workspace
	Role    string `json:"role"`
	Status  string `json:"status"`
	Members int    `json:"members"`
}

// Invite is a link that lets people join a workspace; only CodeHash is stored.
type Invite struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	CodeHash    []byte    `json:"-"`
	Role        string    `json:"role"`
	Approval    bool      `json:"approval"`
	ExpiresAt   time.Time `json:"expires_at"`
	Uses        int       `json:"uses"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	Code        string    `json:"code,omitempty"`
}

type workspaceMemberRow struct {
	Role     string
	Status   string
	JoinedAt time.Time
}

func cleanWorkspace(w Workspace, now time.Time) Workspace {
	if w.Mode == "" {
		w.Mode = WorkspaceTeam
	}
	if w.CreatedAt.IsZero() {
		w.CreatedAt = ts(now)
	} else {
		w.CreatedAt = ts(w.CreatedAt)
	}
	return w
}

func workspaceEvent(w Workspace) map[string]any {
	return map[string]any{"id": w.ID, "name": w.Name, "mode": w.Mode, "owner_id": w.OwnerID}
}

// ── Memory ──────────────────────────────────────────────────────

var seedWorkspaceTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func (m *Memory) ensureWorkspacesLocked(now time.Time) {
	if m.workspaces == nil {
		m.workspaces = map[string]*Workspace{}
	}
	if m.members == nil {
		m.members = map[string]map[string]workspaceMemberRow{}
	}
	if _, ok := m.workspaces[DefaultWorkspaceID]; !ok {
		created := seedWorkspaceTime
		if !now.IsZero() {
			created = ts(now)
		}
		m.workspaces[DefaultWorkspaceID] = &Workspace{
			ID:        DefaultWorkspaceID,
			Name:      "Pessoal",
			Icon:      "",
			Mode:      WorkspacePersonal,
			OwnerID:   "",
			CreatedAt: created,
		}
	}
}

func (m *Memory) setMemberLocked(wsID, personID string, row workspaceMemberRow) {
	if m.members[wsID] == nil {
		m.members[wsID] = map[string]workspaceMemberRow{}
	}
	m.members[wsID][personID] = row
}

func (m *Memory) activeMemberCountLocked(wsID string) int {
	n := 0
	for _, r := range m.members[wsID] {
		if r.Status == MemberActive {
			n++
		}
	}
	return n
}

func (m *Memory) buildMemberLocked(wsID, personID string, row workspaceMemberRow) (Member, bool) {
	p, ok := m.people[personID]
	if !ok {
		return Member{}, false
	}
	var seen *time.Time
	if p.LastSeen != nil {
		t := ts(*p.LastSeen)
		seen = &t
	}
	return Member{
		WorkspaceID: wsID,
		PersonID:    p.ID,
		Name:        p.Name,
		Email:       p.Email,
		Avatar:      p.Avatar,
		Role:        row.Role,
		Status:      row.Status,
		Presence:    p.Presence,
		StatusText:  p.StatusText,
		LastSeen:    seen,
		JoinedAt:    row.JoinedAt,
	}, true
}

func (m *Memory) ListWorkspaces(_ context.Context) ([]Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(time.Time{})
	out := make([]Workspace, 0, len(m.workspaces))
	for _, w := range m.workspaces {
		out = append(out, *w)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].ID == DefaultWorkspaceID) != (out[j].ID == DefaultWorkspaceID) {
			return out[i].ID == DefaultWorkspaceID
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Memory) ListPersonWorkspaces(_ context.Context, personID string) ([]PersonWorkspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(time.Time{})
	type entry struct {
		pw     PersonWorkspace
		joined time.Time
	}
	entries := []entry{}
	for wsID, rows := range m.members {
		row, ok := rows[personID]
		if !ok {
			continue
		}
		ws, ok := m.workspaces[wsID]
		if !ok {
			continue
		}
		entries = append(entries, entry{
			pw: PersonWorkspace{
				Workspace: *ws,
				Role:      row.Role,
				Status:    row.Status,
				Members:   m.activeMemberCountLocked(wsID),
			},
			joined: row.JoinedAt,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if (entries[i].pw.ID == DefaultWorkspaceID) != (entries[j].pw.ID == DefaultWorkspaceID) {
			return entries[i].pw.ID == DefaultWorkspaceID
		}
		if !entries[i].joined.Equal(entries[j].joined) {
			return entries[i].joined.Before(entries[j].joined)
		}
		return entries[i].pw.ID < entries[j].pw.ID
	})
	out := make([]PersonWorkspace, len(entries))
	for i, e := range entries {
		out[i] = e.pw
	}
	return out, nil
}

func (m *Memory) GetWorkspace(_ context.Context, id string) (Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(time.Time{})
	w, ok := m.workspaces[id]
	if !ok {
		return Workspace{}, ErrNotFound
	}
	return *w, nil
}

func (m *Memory) CreateWorkspace(_ context.Context, w Workspace, now time.Time) (Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(now)
	if _, exists := m.workspaces[w.ID]; exists {
		return Workspace{}, ErrConflict
	}
	if w.OwnerID != "" {
		if _, ok := m.people[w.OwnerID]; !ok {
			return Workspace{}, ErrNotFound
		}
	}
	w = cleanWorkspace(w, now)
	stored := w
	m.workspaces[w.ID] = &stored
	if w.OwnerID != "" {
		m.setMemberLocked(w.ID, w.OwnerID, workspaceMemberRow{
			Role: RoleOwner, Status: MemberActive, JoinedAt: ts(now),
		})
	}
	actor := ActorAdmin
	if w.OwnerID != "" {
		actor = PersonActor(w.OwnerID)
	}
	m.emit(now, EventWorkspaceCreated, actor, "", "", workspaceEvent(stored))
	return stored, nil
}

func (m *Memory) UpdateWorkspace(_ context.Context, w Workspace) (Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(time.Time{})
	cur, ok := m.workspaces[w.ID]
	if !ok {
		return Workspace{}, ErrNotFound
	}
	w.CreatedAt = cur.CreatedAt
	if w.OwnerID == "" {
		w.OwnerID = cur.OwnerID
	}
	if w.Mode == "" {
		w.Mode = cur.Mode
	}
	stored := w
	m.workspaces[w.ID] = &stored
	m.emit(time.Now(), EventWorkspaceUpdated, ActorAdmin, "", "", workspaceEvent(stored))
	return stored, nil
}

func (m *Memory) DeleteWorkspace(_ context.Context, id string) error {
	if id == DefaultWorkspaceID {
		return ErrForbidden
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(time.Time{})
	ws, ok := m.workspaces[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.workspaces, id)
	delete(m.members, id)
	for k, inv := range m.invites {
		if inv.WorkspaceID == id {
			delete(m.invites, k)
		}
	}
	m.emit(time.Now(), EventWorkspaceDeleted, ActorAdmin, "", "", workspaceEvent(*ws))
	return nil
}

func (m *Memory) ListMembers(_ context.Context, workspaceID string) ([]Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(time.Time{})
	if _, ok := m.workspaces[workspaceID]; !ok {
		return nil, ErrNotFound
	}
	out := []Member{}
	for personID, row := range m.members[workspaceID] {
		if mem, ok := m.buildMemberLocked(workspaceID, personID, row); ok {
			out = append(out, mem)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].JoinedAt.Equal(out[j].JoinedAt) {
			return out[i].JoinedAt.Before(out[j].JoinedAt)
		}
		return out[i].PersonID < out[j].PersonID
	})
	return out, nil
}

func (m *Memory) GetMember(_ context.Context, workspaceID, personID string) (Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(time.Time{})
	if _, ok := m.workspaces[workspaceID]; !ok {
		return Member{}, ErrNotFound
	}
	row, ok := m.members[workspaceID][personID]
	if !ok {
		return Member{}, ErrNotFound
	}
	mem, ok := m.buildMemberLocked(workspaceID, personID, row)
	if !ok {
		return Member{}, ErrNotFound
	}
	return mem, nil
}

func (m *Memory) SetMemberRole(_ context.Context, workspaceID, personID, role string) (Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(time.Time{})
	ws, ok := m.workspaces[workspaceID]
	if !ok {
		return Member{}, ErrNotFound
	}
	row, ok := m.members[workspaceID][personID]
	if !ok {
		return Member{}, ErrNotFound
	}
	row.Role = role
	m.members[workspaceID][personID] = row
	if role == RoleOwner {
		ws.OwnerID = personID
	}
	mem, ok := m.buildMemberLocked(workspaceID, personID, row)
	if !ok {
		return Member{}, ErrNotFound
	}
	m.emit(time.Now(), EventMemberUpdated, ActorAdmin, "", "",
		map[string]any{"workspace_id": workspaceID, "person_id": personID, "role": role, "status": row.Status})
	return mem, nil
}

func (m *Memory) ApproveMember(_ context.Context, workspaceID, personID string) (Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(time.Time{})
	if _, ok := m.workspaces[workspaceID]; !ok {
		return Member{}, ErrNotFound
	}
	row, ok := m.members[workspaceID][personID]
	if !ok {
		return Member{}, ErrNotFound
	}
	row.Status = MemberActive
	m.members[workspaceID][personID] = row
	mem, ok := m.buildMemberLocked(workspaceID, personID, row)
	if !ok {
		return Member{}, ErrNotFound
	}
	m.emit(time.Now(), EventMemberUpdated, ActorAdmin, "", "",
		map[string]any{"workspace_id": workspaceID, "person_id": personID, "role": row.Role, "status": row.Status})
	return mem, nil
}

func (m *Memory) RemoveMember(_ context.Context, workspaceID, personID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(time.Time{})
	if _, ok := m.workspaces[workspaceID]; !ok {
		return ErrNotFound
	}
	if _, ok := m.members[workspaceID][personID]; !ok {
		return ErrNotFound
	}
	delete(m.members[workspaceID], personID)
	m.emit(time.Now(), EventMemberRemoved, ActorAdmin, "", "",
		map[string]any{"workspace_id": workspaceID, "person_id": personID})
	return nil
}

func copyInvite(inv Invite) Invite {
	inv.CodeHash = bytes.Clone(inv.CodeHash)
	inv.Code = ""
	return inv
}

func (m *Memory) CreateInvite(_ context.Context, inv Invite) (Invite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(inv.CreatedAt)
	if _, ok := m.workspaces[inv.WorkspaceID]; !ok {
		return Invite{}, ErrNotFound
	}
	if m.invites == nil {
		m.invites = map[string]*Invite{}
	}
	if _, exists := m.invites[inv.ID]; exists {
		return Invite{}, ErrConflict
	}
	for _, other := range m.invites {
		if bytes.Equal(other.CodeHash, inv.CodeHash) {
			return Invite{}, ErrConflict
		}
	}
	inv.CreatedAt, inv.ExpiresAt = ts(inv.CreatedAt), ts(inv.ExpiresAt)
	stored := copyInvite(inv)
	m.invites[inv.ID] = &stored
	m.emit(inv.CreatedAt, EventInviteCreated, inv.CreatedBy, "", "",
		map[string]any{"id": inv.ID, "workspace_id": inv.WorkspaceID, "role": inv.Role, "approval": inv.Approval})
	return copyInvite(stored), nil
}

func (m *Memory) ListInvites(_ context.Context, workspaceID string) ([]Invite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(time.Time{})
	if _, ok := m.workspaces[workspaceID]; !ok {
		return nil, ErrNotFound
	}
	out := []Invite{}
	for _, inv := range m.invites {
		if inv.WorkspaceID == workspaceID {
			out = append(out, copyInvite(*inv))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Memory) DeleteInvite(_ context.Context, workspaceID, inviteID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv, ok := m.invites[inviteID]
	if !ok || inv.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	delete(m.invites, inviteID)
	m.emit(time.Now(), EventInviteDeleted, ActorAdmin, "", "",
		map[string]any{"id": inviteID, "workspace_id": workspaceID})
	return nil
}

func (m *Memory) InviteByCode(_ context.Context, codeHash []byte, now time.Time) (Invite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, inv := range m.invites {
		if bytes.Equal(inv.CodeHash, codeHash) && ts(now).Before(inv.ExpiresAt) {
			return copyInvite(*inv), nil
		}
	}
	return Invite{}, ErrNotFound
}

func (m *Memory) AcceptInvite(_ context.Context, codeHash []byte, personID string, now time.Time) (Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspacesLocked(now)
	if _, ok := m.people[personID]; !ok {
		return Member{}, ErrNotFound
	}
	var found *Invite
	for _, inv := range m.invites {
		if bytes.Equal(inv.CodeHash, codeHash) && ts(now).Before(inv.ExpiresAt) {
			found = inv
			break
		}
	}
	if found == nil {
		return Member{}, ErrNotFound
	}
	if _, ok := m.workspaces[found.WorkspaceID]; !ok {
		return Member{}, ErrNotFound
	}
	if existing, ok := m.members[found.WorkspaceID][personID]; ok {
		mem, _ := m.buildMemberLocked(found.WorkspaceID, personID, existing)
		return mem, nil
	}
	status := MemberActive
	if found.Approval {
		status = MemberPending
	}
	row := workspaceMemberRow{Role: found.Role, Status: status, JoinedAt: ts(now)}
	m.setMemberLocked(found.WorkspaceID, personID, row)
	found.Uses++
	mem, _ := m.buildMemberLocked(found.WorkspaceID, personID, row)
	m.emit(now, EventInviteAccepted, PersonActor(personID), "", "",
		map[string]any{"invite_id": found.ID, "workspace_id": found.WorkspaceID, "person_id": personID, "status": status})
	return mem, nil
}
