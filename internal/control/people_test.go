package control

import (
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

func signupPerson(t *testing.T, s *Server, token string, req SignupRequest) AuthResponse {
	t.Helper()
	w := request(t, s, "POST", "/v1/auth/signup", token, req)
	if w.Code != 201 {
		t.Fatalf("signup %s: %d %s", req.Email, w.Code, w.Body.String())
	}
	return decode[AuthResponse](t, w)
}

func TestFirstSignupOwnsPersonalAndBlocksSecond(t *testing.T) {
	s, _ := makeServer(t)

	ana := signupPerson(t, s, "", SignupRequest{
		Email:    "  Ana@Exemplo.COM ",
		Name:     "Ana Silva",
		Password: "senha-muito-forte",
	})
	if !strings.HasPrefix(ana.Token, SessionTokenPrefix) || ana.Person.Email != "ana@exemplo.com" || ana.Person.Presence != "auto" {
		t.Fatalf("first signup: %+v", ana)
	}

	me := decode[MeResponse](t, request(t, s, "GET", "/v1/me", ana.Token, nil))
	if me.Person == nil || me.Person.ID != ana.Person.ID || len(me.Workspaces) != 1 {
		t.Fatalf("me: %+v", me)
	}
	ws := me.Workspaces[0]
	if ws.ID != store.DefaultWorkspaceID || ws.Role != store.RoleOwner || ws.Status != store.MemberActive || ws.Mode != store.WorkspacePersonal {
		t.Fatalf("personal workspace: %+v", ws)
	}

	// Admin token sees the owner of the first workspace as personFrom(ctx).
	adminMe := decode[MeResponse](t, request(t, s, "GET", "/v1/me", adminTestToken, nil))
	if adminMe.Person == nil || adminMe.Person.ID != ana.Person.ID {
		t.Fatalf("admin me: %+v", adminMe)
	}

	// Second signup without invite or admin token is forbidden (403).
	if w := request(t, s, "POST", "/v1/auth/signup", "", SignupRequest{
		Email: "outro@exemplo.com", Name: "Outro", Password: "senha-muito-forte",
	}); w.Code != 403 {
		t.Fatalf("second signup without invite: %d %s", w.Code, w.Body.String())
	}

	// Bad invite code is also 403.
	if w := request(t, s, "POST", "/v1/auth/signup", "", SignupRequest{
		Email: "outro@exemplo.com", Name: "Outro", Password: "senha-muito-forte", Invite: "codigo-invalido",
	}); w.Code != 403 {
		t.Fatalf("signup with invalid invite: %d %s", w.Code, w.Body.String())
	}

	// Admin token may create accounts directly; short password and duplicate email fail.
	if w := request(t, s, "POST", "/v1/auth/signup", adminTestToken, SignupRequest{
		Email: "curta@exemplo.com", Name: "Curta", Password: "1234567",
	}); w.Code != 400 {
		t.Fatalf("short password: %d", w.Code)
	}
	if w := request(t, s, "POST", "/v1/auth/signup", adminTestToken, SignupRequest{
		Email: "ana@exemplo.com", Name: "Outra Ana", Password: "senha-muito-forte",
	}); w.Code != 409 {
		t.Fatalf("duplicate email: %d", w.Code)
	}
	bruno := signupPerson(t, s, adminTestToken, SignupRequest{
		Email: "bruno@exemplo.com", Name: "Bruno", Password: "senha-do-bruno",
	})
	brunoMe := decode[MeResponse](t, request(t, s, "GET", "/v1/me", bruno.Token, nil))
	if len(brunoMe.Workspaces) != 0 {
		t.Fatalf("admin-created user should not own pessoal: %+v", brunoMe.Workspaces)
	}
}

func TestLoginLogoutProfileAndLastSeen(t *testing.T) {
	s, c := makeServer(t)
	ana := signupPerson(t, s, "", SignupRequest{
		Email: "ana@exemplo.com", Name: "Ana", Password: "senha-certa-123",
	})

	// Wrong password or unknown email -> 401 "e-mail ou senha errados".
	for _, bad := range []LoginRequest{
		{Email: "ana@exemplo.com", Password: "errada-123"},
		{Email: "ninguem@exemplo.com", Password: "senha-certa-123"},
		{Email: "", Password: "x"},
	} {
		w := request(t, s, "POST", "/v1/auth/login", "", bad)
		if w.Code != 401 || !strings.Contains(w.Body.String(), "e-mail ou senha errados") {
			t.Fatalf("bad login %+v: %d %q", bad, w.Code, w.Body.String())
		}
	}

	// Correct login returns a fresh session token.
	w := request(t, s, "POST", "/v1/auth/login", "", LoginRequest{
		Email: " ANA@exemplo.com ", Password: "senha-certa-123",
	})
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	logged := decode[AuthResponse](t, w)
	if !strings.HasPrefix(logged.Token, SessionTokenPrefix) || logged.Token == ana.Token {
		t.Fatalf("login token: %q", logged.Token)
	}

	// last_seen writes at most once per minute per person.
	t0 := c.now()
	c.advance(20 * time.Second)
	me1 := decode[MeResponse](t, request(t, s, "GET", "/v1/me", logged.Token, nil))
	if me1.Person.LastSeen == nil || !me1.Person.LastSeen.Equal(t0) {
		t.Fatalf("last_seen updated before 1m: got %v, want %v", me1.Person.LastSeen, t0)
	}
	c.advance(50 * time.Second)
	me2 := decode[MeResponse](t, request(t, s, "GET", "/v1/me", logged.Token, nil))
	if me2.Person.LastSeen == nil || !me2.Person.LastSeen.Equal(c.now()) {
		t.Fatalf("last_seen not updated after 1m: got %v, want %v", me2.Person.LastSeen, c.now())
	}

	// PATCH /v1/me updates profile and password (checking old_password).
	newName, busy, statusText := "Ana Clara", "busy", "Revisando contratos"
	patched := decode[store.Person](t, request(t, s, "PATCH", "/v1/me", logged.Token, PatchMeRequest{
		Name: &newName, Presence: &busy, StatusText: &statusText,
	}))
	if patched.Name != "Ana Clara" || patched.Presence != "busy" || patched.StatusText != "Revisando contratos" {
		t.Fatalf("patched: %+v", patched)
	}
	badPresence := "sleeping"
	if w := request(t, s, "PATCH", "/v1/me", logged.Token, PatchMeRequest{Presence: &badPresence}); w.Code != 400 {
		t.Fatalf("bad presence accepted: %d", w.Code)
	}
	newPw, wrongOld, rightOld := "nova-senha-456", "errada", "senha-certa-123"
	if w := request(t, s, "PATCH", "/v1/me", logged.Token, PatchMeRequest{Password: &newPw, OldPassword: &wrongOld}); w.Code != 400 {
		t.Fatalf("wrong old_password accepted: %d", w.Code)
	}
	if w := request(t, s, "PATCH", "/v1/me", logged.Token, PatchMeRequest{Password: &newPw, OldPassword: &rightOld}); w.Code != 200 {
		t.Fatalf("password change failed: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, s, "POST", "/v1/auth/login", "", LoginRequest{Email: "ana@exemplo.com", Password: "senha-certa-123"}); w.Code != 401 {
		t.Fatalf("old password still works: %d", w.Code)
	}
	if w := request(t, s, "POST", "/v1/auth/login", "", LoginRequest{Email: "ana@exemplo.com", Password: "nova-senha-456"}); w.Code != 200 {
		t.Fatalf("new password rejected: %d", w.Code)
	}

	// Logout revokes only the current session token.
	if w := request(t, s, "POST", "/v1/auth/logout", logged.Token, nil); w.Code != 204 {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := request(t, s, "GET", "/v1/me", logged.Token, nil); w.Code != 401 {
		t.Fatalf("logged-out session still valid: %d", w.Code)
	}
	if w := request(t, s, "GET", "/v1/me", ana.Token, nil); w.Code != 200 {
		t.Fatalf("other session broken by logout: %d", w.Code)
	}
}

func TestPasswordHashNeverAppearsInJSON(t *testing.T) {
	pw := "segredo-unico-987654"
	h, err := hashPassword(pw)
	if err != nil || !strings.HasPrefix(h, "argon2id$") || !verifyPassword(pw, h) || verifyPassword("outra", h) {
		t.Fatalf("argon2id format/verify: %q %v", h, err)
	}

	s, _ := makeServer(t)
	signupRec := request(t, s, "POST", "/v1/auth/signup", "", SignupRequest{
		Email: "ana@exemplo.com", Name: "Ana", Password: pw,
	})
	auth := decode[AuthResponse](t, signupRec)
	loginOK := request(t, s, "POST", "/v1/auth/login", "", LoginRequest{
		Email: "ana@exemplo.com", Password: pw,
	})
	newStatus := "online"
	patchRec := request(t, s, "PATCH", "/v1/me", auth.Token, PatchMeRequest{StatusText: &newStatus})
	meRec := request(t, s, "GET", "/v1/me", auth.Token, nil)
	wsRec := request(t, s, "GET", "/v1/workspaces", auth.Token, nil)
	memRec := request(t, s, "GET", "/v1/workspaces/pessoal/members", auth.Token, nil)
	evRec := request(t, s, "GET", "/v1/events", adminTestToken, nil)

	for name, body := range map[string]string{
		"signup":     signupRec.Body.String(),
		"login":      loginOK.Body.String(),
		"patch_me":   patchRec.Body.String(),
		"me":         meRec.Body.String(),
		"workspaces": wsRec.Body.String(),
		"members":    memRec.Body.String(),
		"events":     evRec.Body.String(),
	} {
		for _, secret := range []string{pw, "argon2id", "password_hash"} {
			if strings.Contains(body, secret) {
				t.Fatalf("%s leaked %q in JSON: %s", name, secret, body)
			}
		}
	}
}
