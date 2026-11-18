package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	personColumns    = `id, email, name, avatar, about, presence, status_text, password_hash, last_seen, last_home_at, created_at`
	workspaceColumns = `id, name, icon, mode, owner_id, approve_joins, created_at`
	inviteColumns    = `id, workspace_id, code_hash, role, approval, expires_at, uses, created_by, created_at`
	memberSelect     = `SELECT wm.workspace_id, wm.person_id, p.name, p.email, p.avatar, wm.role, wm.status,
		p.presence, p.status_text, p.last_seen, wm.joined_at
		FROM workspace_members wm JOIN people p ON p.id = wm.person_id`
)

func scanPerson(row pgx.Row) (Person, error) {
	var p Person
	err := row.Scan(&p.ID, &p.Email, &p.Name, &p.Avatar, &p.About, &p.Presence, &p.StatusText, &p.PasswordHash, &p.LastSeen, &p.LastHomeAt, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Person{}, ErrNotFound
	}
	if err != nil {
		return Person{}, err
	}
	p.CreatedAt = p.CreatedAt.UTC()
	if p.LastSeen != nil {
		t := p.LastSeen.UTC()
		p.LastSeen = &t
	}
	if p.LastHomeAt != nil {
		t := p.LastHomeAt.UTC()
		p.LastHomeAt = &t
	}
	return p, nil
}

func scanWorkspace(row pgx.Row) (Workspace, error) {
	var w Workspace
	err := row.Scan(&w.ID, &w.Name, &w.Icon, &w.Mode, &w.OwnerID, &w.ApproveJoins, &w.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	w.CreatedAt = w.CreatedAt.UTC()
	return w, nil
}

func scanMember(row pgx.Row) (Member, error) {
	var m Member
	err := row.Scan(&m.WorkspaceID, &m.PersonID, &m.Name, &m.Email, &m.Avatar, &m.Role, &m.Status,
		&m.Presence, &m.StatusText, &m.LastSeen, &m.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, err
	}
	m.JoinedAt = m.JoinedAt.UTC()
	if m.LastSeen != nil {
		t := m.LastSeen.UTC()
		m.LastSeen = &t
	}
	return m, nil
}

func scanInvite(row pgx.Row) (Invite, error) {
	var inv Invite
	err := row.Scan(&inv.ID, &inv.WorkspaceID, &inv.CodeHash, &inv.Role, &inv.Approval,
		&inv.ExpiresAt, &inv.Uses, &inv.CreatedBy, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invite{}, ErrNotFound
	}
	if err != nil {
		return Invite{}, err
	}
	inv.CreatedAt, inv.ExpiresAt = inv.CreatedAt.UTC(), inv.ExpiresAt.UTC()
	return inv, nil
}

func mapPgErr(err error) error {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		switch pgErr.SQLState() {
		case "23505":
			return ErrConflict
		case "23503":
			return ErrNotFound
		}
	}
	return err
}

func (p *Postgres) CountPeople(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM people`).Scan(&n)
	return n, err
}

func (p *Postgres) CreatePerson(ctx context.Context, person Person, onlyFirst bool, now time.Time) (Person, bool, error) {
	person = cleanPerson(person, now)
	if person.LastSeen == nil {
		seen := ts(now)
		person.LastSeen = &seen
	}
	var out Person
	var first bool
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE people IN EXCLUSIVE MODE`); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM people`).Scan(&count); err != nil {
			return err
		}
		first = count == 0
		if onlyFirst && !first {
			return ErrForbidden
		}
		var err error
		out, err = scanPerson(tx.QueryRow(ctx, `INSERT INTO people (`+personColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING `+personColumns,
			person.ID, person.Email, person.Name, person.Avatar, person.About, person.Presence,
			person.StatusText, person.PasswordHash, person.LastSeen, person.LastHomeAt, person.CreatedAt))
		if err != nil {
			return mapPgErr(err)
		}
		if first {
			if _, err := tx.Exec(ctx, `INSERT INTO workspaces (id, name, icon, mode, owner_id, approve_joins, created_at)
				VALUES ($1, 'Pessoal', '', 'personal', $2, false, $3)
				ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.owner_id`,
				DefaultWorkspaceID, out.ID, ts(now)); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO workspace_members (workspace_id, person_id, role, status, joined_at)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (workspace_id, person_id) DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status`,
				DefaultWorkspaceID, out.ID, RoleOwner, MemberActive, ts(now)); err != nil {
				return err
			}
		}
		return emit(ctx, tx, now, EventPersonCreated, PersonActor(out.ID), "", "", personEvent(out))
	})
	return out, first, err
}

func (p *Postgres) GetPerson(ctx context.Context, id string) (Person, error) {
	return scanPerson(p.pool.QueryRow(ctx, `SELECT `+personColumns+` FROM people WHERE id = $1`, id))
}

func (p *Postgres) PersonByEmail(ctx context.Context, email string) (Person, error) {
	return scanPerson(p.pool.QueryRow(ctx, `SELECT `+personColumns+` FROM people WHERE email = $1`, cleanEmail(email)))
}

func (p *Postgres) UpdatePerson(ctx context.Context, person Person) (Person, error) {
	var out Person
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		cur, err := scanPerson(tx.QueryRow(ctx, `SELECT `+personColumns+` FROM people WHERE id = $1 FOR UPDATE`, person.ID))
		if err != nil {
			return err
		}
		email := cleanEmail(person.Email)
		if email == "" {
			email = cur.Email
		}
		pw := person.PasswordHash
		if pw == "" {
			pw = cur.PasswordHash
		}
		presence := person.Presence
		if presence == "" {
			presence = cur.Presence
		}
		lastHome := person.LastHomeAt
		if lastHome != nil {
			t := ts(*lastHome)
			lastHome = &t
		} else {
			lastHome = cur.LastHomeAt
		}
		profileChanged := email != cur.Email || person.Name != cur.Name || person.Avatar != cur.Avatar ||
			person.About != cur.About || presence != cur.Presence || person.StatusText != cur.StatusText ||
			pw != cur.PasswordHash
		out, err = scanPerson(tx.QueryRow(ctx, `UPDATE people
			SET email = $2, name = $3, avatar = $4, about = $5, presence = $6, status_text = $7, password_hash = $8, last_home_at = $9
			WHERE id = $1 RETURNING `+personColumns,
			person.ID, email, person.Name, person.Avatar, person.About, presence, person.StatusText, pw, lastHome))
		if err != nil {
			return mapPgErr(err)
		}
		if !profileChanged {
			return nil
		}
		now := time.Now()
		if out.LastSeen != nil {
			now = *out.LastSeen
		}
		return emit(ctx, tx, now, EventPersonUpdated, PersonActor(out.ID), "", "", personEvent(out))
	})
	return out, err
}

func (p *Postgres) TouchPerson(ctx context.Context, id string, now time.Time) error {
	tag, err := p.pool.Exec(ctx, `UPDATE people SET last_seen = $2 WHERE id = $1`, id, ts(now))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) CreateSession(ctx context.Context, s Session) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO sessions (token_hash, person_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4)`, s.TokenHash, s.PersonID, ts(s.ExpiresAt), ts(s.CreatedAt))
	return mapPgErr(err)
}

func (p *Postgres) PersonBySession(ctx context.Context, tokenHash []byte, now time.Time) (Person, error) {
	return scanPerson(p.pool.QueryRow(ctx, `SELECT p.id, p.email, p.name, p.avatar, p.about, p.presence, p.status_text,
		p.password_hash, p.last_seen, p.last_home_at, p.created_at
		FROM sessions s JOIN people p ON p.id = s.person_id
		WHERE s.token_hash = $1 AND s.expires_at > $2`, tokenHash, ts(now)))
}

func (p *Postgres) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (p *Postgres) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+workspaceColumns+` FROM workspaces
		ORDER BY (id <> $1), created_at, id`, DefaultWorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (p *Postgres) ListPersonWorkspaces(ctx context.Context, personID string) ([]PersonWorkspace, error) {
	rows, err := p.pool.Query(ctx, `SELECT w.id, w.name, w.icon, w.mode, w.owner_id, w.approve_joins, w.created_at,
		wm.role, wm.status,
		(SELECT COUNT(*) FROM workspace_members m2 WHERE m2.workspace_id = w.id AND m2.status = 'active')
		FROM workspace_members wm
		JOIN workspaces w ON w.id = wm.workspace_id
		WHERE wm.person_id = $1
		ORDER BY (w.id <> $2), wm.joined_at, w.id`, personID, DefaultWorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PersonWorkspace{}
	for rows.Next() {
		var pw PersonWorkspace
		if err := rows.Scan(&pw.ID, &pw.Name, &pw.Icon, &pw.Mode, &pw.OwnerID, &pw.ApproveJoins, &pw.CreatedAt,
			&pw.Role, &pw.Status, &pw.Members); err != nil {
			return nil, err
		}
		pw.CreatedAt = pw.CreatedAt.UTC()
		out = append(out, pw)
	}
	return out, rows.Err()
}

func (p *Postgres) GetWorkspace(ctx context.Context, id string) (Workspace, error) {
	return scanWorkspace(p.pool.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE id = $1`, id))
}

func (p *Postgres) CreateWorkspace(ctx context.Context, w Workspace, now time.Time) (Workspace, error) {
	w = cleanWorkspace(w, now)
	var out Workspace
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		if w.OwnerID != "" {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM people WHERE id = $1)`, w.OwnerID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrNotFound
			}
		}
		var err error
		out, err = scanWorkspace(tx.QueryRow(ctx, `INSERT INTO workspaces (`+workspaceColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (id) DO NOTHING RETURNING `+workspaceColumns,
			w.ID, w.Name, w.Icon, w.Mode, w.OwnerID, w.ApproveJoins, w.CreatedAt))
		if errors.Is(err, ErrNotFound) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if w.OwnerID != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO workspace_members (workspace_id, person_id, role, status, joined_at)
				VALUES ($1, $2, $3, $4, $5)`, out.ID, out.OwnerID, RoleOwner, MemberActive, ts(now)); err != nil {
				return mapPgErr(err)
			}
		}
		actor := ActorAdmin
		if out.OwnerID != "" {
			actor = PersonActor(out.OwnerID)
		}
		return emit(ctx, tx, now, EventWorkspaceCreated, actor, "", "", workspaceEvent(out))
	})
	return out, err
}

func (p *Postgres) UpdateWorkspace(ctx context.Context, w Workspace) (Workspace, error) {
	var out Workspace
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanWorkspace(tx.QueryRow(ctx, `UPDATE workspaces
			SET name = $2, icon = $3, mode = CASE WHEN $4 = '' THEN mode ELSE $4 END, approve_joins = $5
			WHERE id = $1 RETURNING `+workspaceColumns,
			w.ID, w.Name, w.Icon, w.Mode, w.ApproveJoins))
		if err != nil {
			return err
		}
		return emit(ctx, tx, time.Now(), EventWorkspaceUpdated, ActorAdmin, "", "", workspaceEvent(out))
	})
	return out, err
}

func (p *Postgres) DeleteWorkspace(ctx context.Context, id string) error {
	if id == DefaultWorkspaceID {
		return ErrForbidden
	}
	return p.inTx(ctx, func(tx pgx.Tx) error {
		ws, err := scanWorkspace(tx.QueryRow(ctx, `DELETE FROM workspaces WHERE id = $1 RETURNING `+workspaceColumns, id))
		if err != nil {
			return err
		}
		return emit(ctx, tx, time.Now(), EventWorkspaceDeleted, ActorAdmin, "", "", workspaceEvent(ws))
	})
}

func (p *Postgres) ListMembers(ctx context.Context, workspaceID string) ([]Member, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspaces WHERE id = $1)`, workspaceID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := p.pool.Query(ctx, memberSelect+` WHERE wm.workspace_id = $1 ORDER BY wm.joined_at, wm.person_id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (p *Postgres) GetMember(ctx context.Context, workspaceID, personID string) (Member, error) {
	return scanMember(p.pool.QueryRow(ctx, memberSelect+` WHERE wm.workspace_id = $1 AND wm.person_id = $2`, workspaceID, personID))
}

func (p *Postgres) SetMemberRole(ctx context.Context, workspaceID, personID, role string) (Member, error) {
	var out Member
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE workspace_members SET role = $3 WHERE workspace_id = $1 AND person_id = $2`,
			workspaceID, personID, role)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if role == RoleOwner {
			if _, err := tx.Exec(ctx, `UPDATE workspaces SET owner_id = $2 WHERE id = $1`, workspaceID, personID); err != nil {
				return err
			}
		}
		out, err = scanMember(tx.QueryRow(ctx, memberSelect+` WHERE wm.workspace_id = $1 AND wm.person_id = $2`, workspaceID, personID))
		if err != nil {
			return err
		}
		return emit(ctx, tx, time.Now(), EventMemberUpdated, ActorAdmin, "", "",
			map[string]any{"workspace_id": workspaceID, "person_id": personID, "role": out.Role, "status": out.Status})
	})
	return out, err
}

func (p *Postgres) ApproveMember(ctx context.Context, workspaceID, personID string) (Member, error) {
	var out Member
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE workspace_members SET status = $3 WHERE workspace_id = $1 AND person_id = $2`,
			workspaceID, personID, MemberActive)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		out, err = scanMember(tx.QueryRow(ctx, memberSelect+` WHERE wm.workspace_id = $1 AND wm.person_id = $2`, workspaceID, personID))
		if err != nil {
			return err
		}
		return emit(ctx, tx, time.Now(), EventMemberUpdated, ActorAdmin, "", "",
			map[string]any{"workspace_id": workspaceID, "person_id": personID, "role": out.Role, "status": out.Status})
	})
	return out, err
}

func (p *Postgres) RemoveMember(ctx context.Context, workspaceID, personID string) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id = $1 AND person_id = $2`, workspaceID, personID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return emit(ctx, tx, time.Now(), EventMemberRemoved, ActorAdmin, "", "",
			map[string]any{"workspace_id": workspaceID, "person_id": personID})
	})
}

func (p *Postgres) CreateInvite(ctx context.Context, inv Invite) (Invite, error) {
	var out Invite
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = scanInvite(tx.QueryRow(ctx, `INSERT INTO invites (`+inviteColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, 0, $7, $8)
			RETURNING `+inviteColumns,
			inv.ID, inv.WorkspaceID, inv.CodeHash, inv.Role, inv.Approval, ts(inv.ExpiresAt), inv.CreatedBy, ts(inv.CreatedAt)))
		if err != nil {
			return mapPgErr(err)
		}
		return emit(ctx, tx, inv.CreatedAt, EventInviteCreated, inv.CreatedBy, "", "",
			map[string]any{"id": out.ID, "workspace_id": out.WorkspaceID, "role": out.Role, "approval": out.Approval})
	})
	return out, err
}

func (p *Postgres) ListInvites(ctx context.Context, workspaceID string) ([]Invite, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspaces WHERE id = $1)`, workspaceID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := p.pool.Query(ctx, `SELECT `+inviteColumns+` FROM invites WHERE workspace_id = $1 ORDER BY created_at DESC, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteInvite(ctx context.Context, workspaceID, inviteID string) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM invites WHERE id = $1 AND workspace_id = $2`, inviteID, workspaceID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return emit(ctx, tx, time.Now(), EventInviteDeleted, ActorAdmin, "", "",
			map[string]any{"id": inviteID, "workspace_id": workspaceID})
	})
}

func (p *Postgres) InviteByCode(ctx context.Context, codeHash []byte, now time.Time) (Invite, error) {
	return scanInvite(p.pool.QueryRow(ctx, `SELECT `+inviteColumns+` FROM invites WHERE code_hash = $1 AND expires_at > $2`,
		codeHash, ts(now)))
}

func (p *Postgres) AcceptInvite(ctx context.Context, codeHash []byte, personID string, now time.Time) (Member, error) {
	var out Member
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		inv, err := scanInvite(tx.QueryRow(ctx, `SELECT `+inviteColumns+` FROM invites WHERE code_hash = $1 AND expires_at > $2 FOR UPDATE`,
			codeHash, ts(now)))
		if err != nil {
			return err
		}
		existing, err := scanMember(tx.QueryRow(ctx, memberSelect+` WHERE wm.workspace_id = $1 AND wm.person_id = $2`,
			inv.WorkspaceID, personID))
		if err == nil {
			out = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		status := MemberActive
		if inv.Approval {
			status = MemberPending
		}
		if _, err := tx.Exec(ctx, `INSERT INTO workspace_members (workspace_id, person_id, role, status, joined_at)
			VALUES ($1, $2, $3, $4, $5)`, inv.WorkspaceID, personID, inv.Role, status, ts(now)); err != nil {
			return mapPgErr(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE invites SET uses = uses + 1 WHERE id = $1`, inv.ID); err != nil {
			return err
		}
		out, err = scanMember(tx.QueryRow(ctx, memberSelect+` WHERE wm.workspace_id = $1 AND wm.person_id = $2`,
			inv.WorkspaceID, personID))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventInviteAccepted, PersonActor(personID), "", "",
			map[string]any{"invite_id": inv.ID, "workspace_id": inv.WorkspaceID, "person_id": personID, "status": status})
	})
	return out, err
}
