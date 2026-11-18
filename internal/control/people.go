package control

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
	"golang.org/x/crypto/argon2"
)

const (
	// SessionTokenPrefix marks human session tokens.
	SessionTokenPrefix = "avs_"
	// MemberOnlineTTL is how recently a person must have been seen to appear online.
	MemberOnlineTTL       = 2 * time.Minute
	lastSeenWriteInterval = time.Minute

	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
)

type (
	personCtxKey struct{}
	adminCtxKey  struct{}
)

func withPerson(ctx context.Context, p store.Person) context.Context {
	cp := p
	if cp.LastSeen != nil {
		t := *cp.LastSeen
		cp.LastSeen = &t
	}
	if cp.LastHomeAt != nil {
		t := *cp.LastHomeAt
		cp.LastHomeAt = &t
	}
	return context.WithValue(ctx, personCtxKey{}, &cp)
}

func withAdmin(ctx context.Context) context.Context {
	return context.WithValue(ctx, adminCtxKey{}, true)
}

// personFrom returns the authenticated person on ctx (for admin token, the owner
// of the first workspace when one exists; otherwise nil).
func personFrom(ctx context.Context) *store.Person {
	p, _ := ctx.Value(personCtxKey{}).(*store.Person)
	if p == nil {
		return nil
	}
	cp := *p
	if cp.LastSeen != nil {
		t := *cp.LastSeen
		cp.LastSeen = &t
	}
	if cp.LastHomeAt != nil {
		t := *cp.LastHomeAt
		cp.LastHomeAt = &t
	}
	return &cp
}

func isAdminToken(ctx context.Context) bool {
	ok, _ := ctx.Value(adminCtxKey{}).(bool)
	return ok
}

func actorFrom(r *http.Request) string {
	if isAdminToken(r.Context()) {
		return store.ActorAdmin
	}
	if p := personFrom(r.Context()); p != nil {
		return store.PersonActor(p.ID)
	}
	return store.ActorAdmin
}

func (s *Server) shouldTouchPerson(id string, lastSeen *time.Time, now time.Time) bool {
	if v, ok := s.personSeen.Load(id); ok {
		prev := v.(time.Time)
		if d := now.Sub(prev); d >= 0 && d < lastSeenWriteInterval {
			return false
		}
	} else if lastSeen != nil {
		if d := now.Sub(*lastSeen); d >= 0 && d < lastSeenWriteInterval {
			s.personSeen.Store(id, *lastSeen)
			return false
		}
	}
	s.personSeen.Store(id, now)
	return true
}

func hashPassword(password string) (string, error) {
	if utf8.RuneCountInString(password) < 8 {
		return "", errors.New("a senha precisa ter pelo menos 8 caracteres")
	}
	if len(password) > 256 {
		return "", errors.New("a senha é longa demais")
	}
	salt, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return "argon2id$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

func decodeBase64(s string) ([]byte, error) {
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, err1 := decodeBase64(parts[1])
	want, err2 := decodeBase64(parts[2])
	if err1 != nil || err2 != nil || len(salt) == 0 || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func validEmail(email string) bool {
	at := strings.IndexByte(email, '@')
	return len(email) >= 3 && len(email) <= 200 && at > 0 && at < len(email)-1 &&
		!strings.ContainsAny(email, " \t\r\n") && strings.IndexByte(email[at+1:], '@') < 0
}

func validPresence(p string) bool {
	switch p {
	case store.PresenceAuto, store.PresenceAway, store.PresenceBusy, store.PresenceInvisible:
		return true
	}
	return false
}

func (s *Server) newSessionToken(ctx context.Context, personID string) (string, error) {
	secret, err := randomBytes(32)
	if err != nil {
		return "", err
	}
	token := SessionTokenPrefix + base64.RawURLEncoding.EncodeToString(secret)
	now := s.now()
	if err := s.store.CreateSession(ctx, store.Session{
		TokenHash: hashSecret(token),
		PersonID:  personID,
		CreatedAt: now,
		ExpiresAt: now.Add(store.SessionTTL),
	}); err != nil {
		return "", err
	}
	return token, nil
}

// ── API ─────────────────────────────────────────────────────────

type SignupRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Invite   string `json:"invite"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token  string       `json:"token"`
	Person store.Person `json:"person"`
}

type MeWorkspace struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Icon   string `json:"icon"`
	Mode   string `json:"mode"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

type MeResponse struct {
	Person     *store.Person `json:"person"`
	Workspaces []MeWorkspace `json:"workspaces"`
}

type PatchMeRequest struct {
	Name        *string `json:"name"`
	Avatar      *string `json:"avatar"`
	About       *string `json:"about"`
	Presence    *string `json:"presence"`
	StatusText  *string `json:"status_text"`
	Password    *string `json:"password"`
	OldPassword *string `json:"old_password"`
}

func (s *Server) registerPeople(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/signup", s.signup)
	mux.HandleFunc("POST /v1/auth/login", s.login)
	mux.HandleFunc("POST /v1/auth/logout", s.admin(s.logout))
	mux.HandleFunc("GET /v1/me", s.admin(s.getMe))
	mux.HandleFunc("PATCH /v1/me", s.admin(s.patchMe))
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	var req SignupRequest
	if !readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	now := s.now()
	byAdmin := secureEqual(bearer(r), s.adminToken)
	invCode := strings.TrimSpace(req.Invite)
	hasInvite := false
	if invCode != "" {
		if _, err := s.store.InviteByCode(ctx, hashSecret(invCode), now); errors.Is(err, store.ErrNotFound) {
			http.Error(w, "convite inválido ou expirado", http.StatusForbidden)
			return
		} else if err != nil {
			storeError(w, err)
			return
		}
		hasInvite = true
	}
	if !byAdmin && !hasInvite {
		count, err := s.store.CountPeople(ctx)
		if err != nil {
			storeError(w, err)
			return
		}
		if count > 0 {
			http.Error(w, "este servidor pede um convite para criar conta", http.StatusForbidden)
			return
		}
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	name := strings.TrimSpace(req.Name)
	if !validEmail(email) {
		http.Error(w, "informe um e-mail válido", http.StatusBadRequest)
		return
	}
	if name == "" || utf8.RuneCountInString(name) > 80 {
		http.Error(w, "o nome tem de 1 a 80 caracteres", http.StatusBadRequest)
		return
	}
	pwHash, err := hashPassword(req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	idBytes, err := randomBytes(8)
	if err != nil {
		http.Error(w, "não foi possível criar a conta", http.StatusInternalServerError)
		return
	}
	person, _, err := s.store.CreatePerson(ctx, store.Person{
		ID:           hex.EncodeToString(idBytes),
		Email:        email,
		Name:         name,
		Presence:     store.PresenceAuto,
		PasswordHash: pwHash,
		CreatedAt:    now,
	}, !byAdmin && !hasInvite, now)
	switch {
	case errors.Is(err, store.ErrForbidden):
		http.Error(w, "este servidor pede um convite para criar conta", http.StatusForbidden)
		return
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "já existe uma conta com esse e-mail", http.StatusConflict)
		return
	case err != nil:
		storeError(w, err)
		return
	}
	s.personSeen.Store(person.ID, now)
	if hasInvite {
		if _, err := s.store.AcceptInvite(ctx, hashSecret(invCode), person.ID, now); err != nil && !errors.Is(err, store.ErrNotFound) {
			storeError(w, err)
			return
		}
	}
	token, err := s.newSessionToken(ctx, person.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, AuthResponse{Token: token, Person: person})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if !readJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || req.Password == "" {
		http.Error(w, "e-mail ou senha errados", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	person, err := s.store.PersonByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "e-mail ou senha errados", http.StatusUnauthorized)
		return
	}
	if err != nil {
		storeError(w, err)
		return
	}
	if !verifyPassword(req.Password, person.PasswordHash) {
		http.Error(w, "e-mail ou senha errados", http.StatusUnauthorized)
		return
	}
	now := s.now()
	if err := s.store.TouchPerson(ctx, person.ID, now); err == nil {
		seen := now.UTC().Truncate(time.Microsecond)
		person.LastSeen = &seen
	}
	s.personSeen.Store(person.ID, now)
	token, err := s.newSessionToken(ctx, person.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, AuthResponse{Token: token, Person: person})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if tok := bearer(r); strings.HasPrefix(tok, SessionTokenPrefix) {
		_ = s.store.DeleteSession(r.Context(), hashSecret(tok))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
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
		out := make([]MeWorkspace, 0, len(all))
		for _, ws := range all {
			role, status := store.RoleOwner, store.MemberActive
			if m, ok := ownByID[ws.ID]; ok {
				role, status = m.Role, m.Status
			}
			out = append(out, MeWorkspace{
				ID: ws.ID, Name: ws.Name, Icon: ws.Icon, Mode: ws.Mode, Role: role, Status: status,
			})
		}
		writeJSON(w, http.StatusOK, MeResponse{Person: p, Workspaces: out})
		return
	}
	list, err := s.store.ListPersonWorkspaces(ctx, p.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	out := make([]MeWorkspace, len(list))
	for i, ws := range list {
		out[i] = MeWorkspace{
			ID: ws.ID, Name: ws.Name, Icon: ws.Icon, Mode: ws.Mode, Role: ws.Role, Status: ws.Status,
		}
	}
	writeJSON(w, http.StatusOK, MeResponse{Person: p, Workspaces: out})
}

func (s *Server) patchMe(w http.ResponseWriter, r *http.Request) {
	var req PatchMeRequest
	if !readJSONMax(w, r, &req, 512*1024) {
		return
	}
	p := personFrom(r.Context())
	if p == nil {
		if isAdminToken(r.Context()) {
			s.patchOperatorProfile(w, r, req)
			return
		}
		http.Error(w, "nenhuma pessoa autenticada", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	cur, err := s.store.GetPerson(ctx, p.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || utf8.RuneCountInString(name) > 80 {
			http.Error(w, "o nome tem de 1 a 80 caracteres", http.StatusBadRequest)
			return
		}
		cur.Name = name
	}
	if req.Avatar != nil {
		av := strings.TrimSpace(*req.Avatar)
		if len(av) > 400*1024 {
			http.Error(w, "a foto é grande demais", http.StatusBadRequest)
			return
		}
		cur.Avatar = av
	}
	if req.About != nil {
		about := strings.TrimSpace(*req.About)
		if utf8.RuneCountInString(about) > 2000 {
			http.Error(w, "o texto sobre você tem até 2000 caracteres", http.StatusBadRequest)
			return
		}
		cur.About = about
	}
	if req.Presence != nil {
		pr := strings.TrimSpace(*req.Presence)
		if !validPresence(pr) {
			http.Error(w, "presença inválida", http.StatusBadRequest)
			return
		}
		cur.Presence = pr
	}
	if req.StatusText != nil {
		st := strings.TrimSpace(*req.StatusText)
		if utf8.RuneCountInString(st) > 140 {
			http.Error(w, "o status tem até 140 caracteres", http.StatusBadRequest)
			return
		}
		cur.StatusText = st
	}
	if req.Password != nil && *req.Password != "" {
		if !isAdminToken(ctx) {
			if req.OldPassword == nil || !verifyPassword(*req.OldPassword, cur.PasswordHash) {
				http.Error(w, "senha atual incorreta", http.StatusBadRequest)
				return
			}
		}
		pwHash, err := hashPassword(*req.Password)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cur.PasswordHash = pwHash
	}
	saved, err := s.store.UpdatePerson(ctx, cur)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}
