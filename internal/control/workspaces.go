package control

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

const (
	defaultInviteDays = 7
	maxInviteDays     = 365
)

var errWorkspaceForbidden = errors.New("not an active member of this workspace")

// can reports whether member m holds permission perm in its workspace.
func can(m store.Member, perm string) bool {
	if m.Status != store.MemberActive {
		return false
	}
	switch perm {
	case "owner":
		return m.Role == store.RoleOwner
	case "manage":
		return m.Role == store.RoleOwner || m.Role == store.RoleAdmin
	case "member", "read", "write":
		return m.Role == store.RoleOwner || m.Role == store.RoleAdmin || m.Role == store.RoleMember
	default:
		return m.Role == store.RoleOwner
	}
}

// workspaceFrom resolves X-Avora-Workspace (default "pessoal") and checks that
// the caller is an active member; the admin token always passes.
func (s *Server) workspaceFrom(r *http.Request) (string, error) {
	wsID := strings.TrimSpace(r.Header.Get("X-Avora-Workspace"))
	if wsID == "" {
		wsID = store.DefaultWorkspaceID
	}
	if isAdminToken(r.Context()) {
		return wsID, nil
	}
	p := personFrom(r.Context())
	if p == nil {
		return "", errWorkspaceForbidden
	}
	m, err := s.store.GetMember(r.Context(), wsID, p.ID)
	if errors.Is(err, store.ErrNotFound) {
		return "", errWorkspaceForbidden
	}
	if err != nil {
		return "", err
	}
	if !can(m, "member") {
		return "", errWorkspaceForbidden
	}
	return wsID, nil
}

func (s *Server) requireWorkspace(w http.ResponseWriter, r *http.Request) (string, bool) {
	wsID, err := s.workspaceFrom(r)
	if errors.Is(err, errWorkspaceForbidden) {
		http.Error(w, "sem acesso a este espaço", http.StatusForbidden)
		return "", false
	}
	if err != nil {
		storeError(w, err)
		return "", false
	}
	return wsID, true
}

func (s *Server) withMemberOnline(m store.Member) store.Member {
	m.Online = m.Presence != store.PresenceInvisible && m.LastSeen != nil && s.now().Sub(*m.LastSeen) < MemberOnlineTTL
	return m
}

func (s *Server) requireWorkspacePerm(w http.ResponseWriter, r *http.Request, wsID, perm string) (store.Workspace, store.Member, bool) {
	ctx := r.Context()
	ws, err := s.store.GetWorkspace(ctx, wsID)
	if err != nil {
		storeError(w, err)
		return store.Workspace{}, store.Member{}, false
	}
	if isAdminToken(ctx) {
		return ws, store.Member{WorkspaceID: wsID, Role: store.RoleOwner, Status: store.MemberActive}, true
	}
	p := personFrom(ctx)
	if p == nil {
		http.Error(w, "sem acesso a este espaço", http.StatusForbidden)
		return store.Workspace{}, store.Member{}, false
	}
	m, err := s.store.GetMember(ctx, wsID, p.ID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "você não faz parte deste espaço", http.StatusForbidden)
		return store.Workspace{}, store.Member{}, false
	}
	if err != nil {
		storeError(w, err)
		return store.Workspace{}, store.Member{}, false
	}
	if !can(m, perm) {
		http.Error(w, "sem permissão neste espaço", http.StatusForbidden)
		return store.Workspace{}, store.Member{}, false
	}
	return ws, m, true
}

func validWorkspaceMode(mode string) bool {
	return mode == store.WorkspacePersonal || mode == store.WorkspaceTeam
}

func validMemberRole(role string) bool {
	return role == store.RoleOwner || role == store.RoleAdmin || role == store.RoleMember
}

// ── API ─────────────────────────────────────────────────────────

type CreateWorkspaceRequest struct {
	Name         string `json:"name"`
	Icon         string `json:"icon"`
	Mode         string `json:"mode"`
	ApproveJoins *bool  `json:"approve_joins"`
}

type PatchWorkspaceRequest struct {
	Name         *string `json:"name"`
	Icon         *string `json:"icon"`
	Mode         *string `json:"mode"`
	ApproveJoins *bool   `json:"approve_joins"`
}

type MemberRoleRequest struct {
	Role string `json:"role"`
}

type InviteRequest struct {
	Role     string `json:"role"`
	Days     *int   `json:"days"`
	Approval *bool  `json:"approval"`
}

func (s *Server) registerWorkspaces(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/workspaces", s.admin(s.listWorkspaces))
	mux.HandleFunc("POST /v1/workspaces", s.admin(s.createWorkspace))
	mux.HandleFunc("PATCH /v1/workspaces/{id}", s.admin(s.patchWorkspace))
	mux.HandleFunc("DELETE /v1/workspaces/{id}", s.admin(s.deleteWorkspace))
	mux.HandleFunc("GET /v1/workspaces/{id}/members", s.admin(s.listMembers))
	mux.HandleFunc("PATCH /v1/workspaces/{id}/members/{person}", s.admin(s.patchMemberRole))
	mux.HandleFunc("POST /v1/workspaces/{id}/members/{person}/approve", s.admin(s.approveMember))
	mux.HandleFunc("DELETE /v1/workspaces/{id}/members/{person}", s.admin(s.removeMember))
	mux.HandleFunc("GET /v1/workspaces/{id}/invites", s.admin(s.listInvites))
	mux.HandleFunc("POST /v1/workspaces/{id}/invites", s.admin(s.createInvite))
	mux.HandleFunc("DELETE /v1/workspaces/{id}/invites/{invite}", s.admin(s.deleteInvite))
	mux.HandleFunc("GET /v1/invites/{code}", s.getInvitePublic)
	mux.HandleFunc("POST /v1/invites/{code}/accept", s.admin(s.acceptInvite))
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := personFrom(ctx)
	if isAdminToken(ctx) {
		all, err := s.store.ListWorkspaces(ctx)
		if err != nil {
			storeError(w, err)
			return
		}
		ownByID := map[string]store.PersonWorkspace{}
		if p != nil {
			if own, err := s.store.ListPersonWorkspaces(ctx, p.ID); err == nil {
				for _, w := range own {
					ownByID[w.ID] = w
				}
			}
		}
		out := make([]store.PersonWorkspace, 0, len(all))
		for _, ws := range all {
			pw := store.PersonWorkspace{Workspace: ws, Role: store.RoleOwner, Status: store.MemberActive}
			if m, ok := ownByID[ws.ID]; ok {
				pw.Role, pw.Status, pw.Members = m.Role, m.Status, m.Members
			} else if members, err := s.store.ListMembers(ctx, ws.ID); err == nil {
				for _, mem := range members {
					if mem.Status == store.MemberActive {
						pw.Members++
					}
				}
			}
			out = append(out, pw)
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	list, err := s.store.ListPersonWorkspaces(ctx, p.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var req CreateWorkspaceRequest
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	icon := strings.TrimSpace(req.Icon)
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = store.WorkspaceTeam
	}
	switch {
	case name == "" || utf8.RuneCountInString(name) > 60:
		http.Error(w, "o nome do espaço tem de 1 a 60 caracteres", http.StatusBadRequest)
		return
	case utf8.RuneCountInString(icon) > 16:
		http.Error(w, "o ícone é longo demais", http.StatusBadRequest)
		return
	case !validWorkspaceMode(mode):
		http.Error(w, "o modo precisa ser personal ou team", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	base := slug(name)
	if len(base) < 2 || base == store.DefaultWorkspaceID {
		base = "espaco"
	}
	id := base
	for i := 2; ; i++ {
		if _, err := s.store.GetWorkspace(ctx, id); errors.Is(err, store.ErrNotFound) {
			break
		} else if err != nil {
			storeError(w, err)
			return
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	ownerID := ""
	if p := personFrom(ctx); p != nil {
		ownerID = p.ID
	}
	approve := req.ApproveJoins != nil && *req.ApproveJoins
	saved, err := s.store.CreateWorkspace(ctx, store.Workspace{
		ID:           id,
		Name:         name,
		Icon:         icon,
		Mode:         mode,
		OwnerID:      ownerID,
		ApproveJoins: approve,
		CreatedAt:    s.now(),
	}, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) patchWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	ws, _, ok := s.requireWorkspacePerm(w, r, wsID, "manage")
	if !ok {
		return
	}
	var req PatchWorkspaceRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || utf8.RuneCountInString(name) > 60 {
			http.Error(w, "o nome do espaço tem de 1 a 60 caracteres", http.StatusBadRequest)
			return
		}
		ws.Name = name
	}
	if req.Icon != nil {
		icon := strings.TrimSpace(*req.Icon)
		if utf8.RuneCountInString(icon) > 16 {
			http.Error(w, "o ícone é longo demais", http.StatusBadRequest)
			return
		}
		ws.Icon = icon
	}
	if req.Mode != nil {
		mode := strings.TrimSpace(*req.Mode)
		if !validWorkspaceMode(mode) {
			http.Error(w, "o modo precisa ser personal ou team", http.StatusBadRequest)
			return
		}
		ws.Mode = mode
	}
	if req.ApproveJoins != nil {
		ws.ApproveJoins = *req.ApproveJoins
	}
	saved, err := s.store.UpdateWorkspace(r.Context(), ws)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	if wsID == store.DefaultWorkspaceID {
		http.Error(w, "o espaço pessoal não pode ser apagado", http.StatusForbidden)
		return
	}
	if _, _, ok := s.requireWorkspacePerm(w, r, wsID, "owner"); !ok {
		return
	}
	if err := s.store.DeleteWorkspace(r.Context(), wsID); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	if _, _, ok := s.requireWorkspacePerm(w, r, wsID, "member"); !ok {
		return
	}
	members, err := s.store.ListMembers(r.Context(), wsID)
	if err != nil {
		storeError(w, err)
		return
	}
	for i := range members {
		members[i] = s.withMemberOnline(members[i])
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) patchMemberRole(w http.ResponseWriter, r *http.Request) {
	wsID, targetID := r.PathValue("id"), r.PathValue("person")
	ws, actor, ok := s.requireWorkspacePerm(w, r, wsID, "manage")
	if !ok {
		return
	}
	var req MemberRoleRequest
	if !readJSON(w, r, &req) {
		return
	}
	role := strings.TrimSpace(req.Role)
	if !validMemberRole(role) {
		http.Error(w, "o cargo precisa ser owner, admin ou member", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	target, err := s.store.GetMember(ctx, wsID, targetID)
	if err != nil {
		storeError(w, err)
		return
	}
	if (target.Role == store.RoleOwner || role == store.RoleOwner) && actor.Role != store.RoleOwner {
		http.Error(w, "só o dono pode alterar o cargo de dono", http.StatusForbidden)
		return
	}
	if target.Role == store.RoleOwner && role != store.RoleOwner && ws.OwnerID == target.PersonID {
		http.Error(w, "transfira a propriedade antes de deixar de ser dono", http.StatusBadRequest)
		return
	}
	updated, err := s.store.SetMemberRole(ctx, wsID, targetID, role)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.withMemberOnline(updated))
}

func (s *Server) approveMember(w http.ResponseWriter, r *http.Request) {
	wsID, targetID := r.PathValue("id"), r.PathValue("person")
	if _, _, ok := s.requireWorkspacePerm(w, r, wsID, "manage"); !ok {
		return
	}
	updated, err := s.store.ApproveMember(r.Context(), wsID, targetID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.withMemberOnline(updated))
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsID, targetID := r.PathValue("id"), r.PathValue("person")
	if _, err := s.store.GetWorkspace(ctx, wsID); err != nil {
		storeError(w, err)
		return
	}
	target, err := s.store.GetMember(ctx, wsID, targetID)
	if err != nil {
		storeError(w, err)
		return
	}
	p := personFrom(ctx)
	self := !isAdminToken(ctx) && p != nil && p.ID == targetID
	switch {
	case self:
		if target.Role == store.RoleOwner {
			http.Error(w, "o dono não pode sair do espaço", http.StatusBadRequest)
			return
		}
	case isAdminToken(ctx):
		if target.Role == store.RoleOwner {
			http.Error(w, "o dono não pode ser removido", http.StatusBadRequest)
			return
		}
	default:
		if p == nil {
			http.Error(w, "sem acesso a este espaço", http.StatusForbidden)
			return
		}
		actor, err := s.store.GetMember(ctx, wsID, p.ID)
		if err != nil || !can(actor, "manage") {
			http.Error(w, "sem permissão neste espaço", http.StatusForbidden)
			return
		}
		if target.Role == store.RoleOwner {
			http.Error(w, "o dono não pode ser removido", http.StatusForbidden)
			return
		}
	}
	if err := s.store.RemoveMember(ctx, wsID, targetID); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	if _, _, ok := s.requireWorkspacePerm(w, r, wsID, "manage"); !ok {
		return
	}
	list, err := s.store.ListInvites(r.Context(), wsID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	ws, _, ok := s.requireWorkspacePerm(w, r, wsID, "manage")
	if !ok {
		return
	}
	var req InviteRequest
	if r.ContentLength != 0 && !readJSON(w, r, &req) {
		return
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = store.RoleMember
	}
	if role != store.RoleMember && role != store.RoleAdmin {
		http.Error(w, "o cargo do convite precisa ser member ou admin", http.StatusBadRequest)
		return
	}
	days := defaultInviteDays
	if req.Days != nil && *req.Days != 0 {
		days = *req.Days
	}
	if days < 1 || days > maxInviteDays {
		http.Error(w, "a validade precisa ser de 1 a 365 dias", http.StatusBadRequest)
		return
	}
	approval := ws.ApproveJoins
	if req.Approval != nil {
		approval = *req.Approval
	}
	idBytes, err := randomBytes(8)
	if err != nil {
		http.Error(w, "falha ao criar convite", http.StatusInternalServerError)
		return
	}
	rawCode, err := randomBytes(16)
	if err != nil {
		http.Error(w, "falha ao criar convite", http.StatusInternalServerError)
		return
	}
	code := base64.RawURLEncoding.EncodeToString(rawCode)
	createdBy := store.ActorAdmin
	if p := personFrom(r.Context()); p != nil {
		createdBy = p.ID
	}
	now := s.now()
	saved, err := s.store.CreateInvite(r.Context(), store.Invite{
		ID:          hex.EncodeToString(idBytes),
		WorkspaceID: wsID,
		CodeHash:    hashSecret(code),
		Role:        role,
		Approval:    approval,
		ExpiresAt:   now.AddDate(0, 0, days),
		CreatedBy:   createdBy,
		CreatedAt:   now,
	})
	if err != nil {
		storeError(w, err)
		return
	}
	saved.Code = code
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) deleteInvite(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	if _, _, ok := s.requireWorkspacePerm(w, r, wsID, "manage"); !ok {
		return
	}
	if err := s.store.DeleteInvite(r.Context(), wsID, r.PathValue("invite")); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getInvitePublic(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.PathValue("code"))
	if code == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	inv, err := s.store.InviteByCode(ctx, hashSecret(code), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	ws, err := s.store.GetWorkspace(ctx, inv.WorkspaceID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace":  map[string]string{"name": ws.Name, "icon": ws.Icon},
		"role":       inv.Role,
		"expires_at": inv.ExpiresAt,
		"approval":   inv.Approval,
	})
}

func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	p := personFrom(r.Context())
	if p == nil {
		http.Error(w, "entre com uma conta para aceitar o convite", http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(r.PathValue("code"))
	if code == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	mem, err := s.store.AcceptInvite(r.Context(), hashSecret(code), p.ID, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"workspace_id": mem.WorkspaceID,
		"status":       mem.Status,
	})
}
