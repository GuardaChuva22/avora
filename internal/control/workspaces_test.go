package control

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

func requestWS(t *testing.T, s *Server, method, path, token, workspace string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		var err error
		if b, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if workspace != "" {
		r.Header.Set("X-Avora-Workspace", workspace)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestWorkspacesInvitesApprovalAndLeave(t *testing.T) {
	s, c := makeServer(t)
	ana := signupPerson(t, s, "", SignupRequest{
		Email: "ana@exemplo.com", Name: "Ana", Password: "senha-da-ana",
	})
	bruno := signupPerson(t, s, adminTestToken, SignupRequest{
		Email: "bruno@exemplo.com", Name: "Bruno", Password: "senha-do-bruno",
	})

	// Create a new workspace: creator becomes owner.
	w := request(t, s, "POST", "/v1/workspaces", ana.Token, CreateWorkspaceRequest{Name: " Estúdio ", Icon: "🎨"})
	if w.Code != 201 {
		t.Fatalf("create workspace: %d %s", w.Code, w.Body.String())
	}
	ws := decode[store.Workspace](t, w)
	if ws.ID != "estudio" || ws.Name != "Estúdio" || ws.Icon != "🎨" || ws.Mode != "team" || ws.OwnerID != ana.Person.ID {
		t.Fatalf("workspace: %+v", ws)
	}

	// "pessoal" cannot be deleted.
	if got := request(t, s, "DELETE", "/v1/workspaces/pessoal", ana.Token, nil); got.Code != 403 {
		t.Fatalf("delete pessoal: %d", got.Code)
	}

	// Invite without approval -> accept becomes active immediately.
	noApp := false
	inv1 := decode[store.Invite](t, request(t, s, "POST", "/v1/workspaces/"+ws.ID+"/invites", ana.Token, InviteRequest{
		Role: "member", Approval: &noApp,
	}))
	if inv1.Code == "" || inv1.Approval || inv1.Role != "member" {
		t.Fatalf("invite 1: %+v", inv1)
	}
	// Listing invites never exposes the plaintext code.
	listedInv := request(t, s, "GET", "/v1/workspaces/"+ws.ID+"/invites", ana.Token, nil)
	if listedInv.Code != 200 || strings.Contains(listedInv.Body.String(), inv1.Code) {
		t.Fatalf("list invites leaked code: %d %s", listedInv.Code, listedInv.Body.String())
	}
	// Public GET /v1/invites/{code} works with NO authentication.
	pub := request(t, s, "GET", "/v1/invites/"+inv1.Code, "", nil)
	if pub.Code != 200 || !strings.Contains(pub.Body.String(), "Estúdio") {
		t.Fatalf("public invite: %d %s", pub.Code, pub.Body.String())
	}

	// Bruno accepts invite without approval -> active.
	acc1 := decode[struct {
		WorkspaceID string `json:"workspace_id"`
		Status      string `json:"status"`
	}](t, request(t, s, "POST", "/v1/invites/"+inv1.Code+"/accept", bruno.Token, nil))
	if acc1.WorkspaceID != ws.ID || acc1.Status != store.MemberActive {
		t.Fatalf("accept without approval: %+v", acc1)
	}

	// Invite WITH approval -> pending until owner/admin approves.
	withApp := true
	inv2 := decode[store.Invite](t, request(t, s, "POST", "/v1/workspaces/"+ws.ID+"/invites", ana.Token, InviteRequest{
		Role: "member", Approval: &withApp,
	}))
	carla := signupPerson(t, s, "", SignupRequest{
		Email: "carla@exemplo.com", Name: "Carla", Password: "senha-da-carla", Invite: inv2.Code,
	})
	acc2 := decode[struct {
		WorkspaceID string `json:"workspace_id"`
		Status      string `json:"status"`
	}](t, request(t, s, "POST", "/v1/invites/"+inv2.Code+"/accept", carla.Token, nil))
	if acc2.WorkspaceID != ws.ID || acc2.Status != store.MemberPending {
		t.Fatalf("accept with approval: %+v", acc2)
	}

	// Pending member cannot access the workspace yet, and a regular member cannot approve.
	if got := requestWS(t, s, "GET", "/v1/agents", carla.Token, ws.ID, nil); got.Code != 403 {
		t.Fatalf("pending member accessed agents: %d", got.Code)
	}
	if got := request(t, s, "POST", "/v1/workspaces/"+ws.ID+"/members/"+carla.Person.ID+"/approve", bruno.Token, nil); got.Code != 403 {
		t.Fatalf("member approved join: %d", got.Code)
	}

	// Ana promotes Bruno to admin; Bruno can now approve Carla.
	promoted := decode[store.Member](t, request(t, s, "PATCH", "/v1/workspaces/"+ws.ID+"/members/"+bruno.Person.ID, ana.Token, MemberRoleRequest{Role: "admin"}))
	if promoted.Role != "admin" {
		t.Fatalf("promote: %+v", promoted)
	}
	approved := decode[store.Member](t, request(t, s, "POST", "/v1/workspaces/"+ws.ID+"/members/"+carla.Person.ID+"/approve", bruno.Token, nil))
	if approved.Status != store.MemberActive || !approved.Online {
		t.Fatalf("approved: %+v", approved)
	}
	if got := requestWS(t, s, "GET", "/v1/agents", carla.Token, ws.ID, nil); got.Code != 200 {
		t.Fatalf("approved member blocked: %d %s", got.Code, got.Body.String())
	}

	// Presence invisible hides online; past 2 minutes without requests also goes offline.
	inv := store.PresenceInvisible
	request(t, s, "PATCH", "/v1/me", carla.Token, PatchMeRequest{Presence: &inv})
	members := decode[[]store.Member](t, request(t, s, "GET", "/v1/workspaces/"+ws.ID+"/members", ana.Token, nil))
	if len(members) != 3 {
		t.Fatalf("members: %+v", members)
	}
	for _, m := range members {
		if m.PersonID == carla.Person.ID && m.Online {
			t.Fatalf("invisible member reported online: %+v", m)
		}
	}
	c.advance(3 * time.Minute)
	members = decode[[]store.Member](t, request(t, s, "GET", "/v1/workspaces/"+ws.ID+"/members", adminTestToken, nil))
	for _, m := range members {
		if m.Online {
			t.Fatalf("stale member reported online: %+v", m)
		}
	}

	// Owner cannot leave; Carla leaves on her own and loses access.
	if got := request(t, s, "DELETE", "/v1/workspaces/"+ws.ID+"/members/"+ana.Person.ID, ana.Token, nil); got.Code != 400 {
		t.Fatalf("owner left workspace: %d", got.Code)
	}
	if got := request(t, s, "DELETE", "/v1/workspaces/"+ws.ID+"/members/"+carla.Person.ID, carla.Token, nil); got.Code != 204 {
		t.Fatalf("carla leave: %d %s", got.Code, got.Body.String())
	}
	if got := requestWS(t, s, "GET", "/v1/agents", carla.Token, ws.ID, nil); got.Code != 403 {
		t.Fatalf("former member still has access: %d", got.Code)
	}

	// Deleting an invite revokes its public link; non-owner cannot delete workspace, owner can.
	if got := request(t, s, "DELETE", "/v1/workspaces/"+ws.ID+"/invites/"+inv1.ID, ana.Token, nil); got.Code != 204 {
		t.Fatalf("delete invite: %d", got.Code)
	}
	if got := request(t, s, "GET", "/v1/invites/"+inv1.Code, "", nil); got.Code != 404 {
		t.Fatalf("deleted invite still public: %d", got.Code)
	}
	if got := request(t, s, "DELETE", "/v1/workspaces/"+ws.ID, bruno.Token, nil); got.Code != 403 {
		t.Fatalf("admin deleted workspace: %d", got.Code)
	}
	if got := request(t, s, "DELETE", "/v1/workspaces/"+ws.ID, ana.Token, nil); got.Code != 204 {
		t.Fatalf("owner delete workspace: %d", got.Code)
	}
}

func TestWorkspaceHeaderScopesAgentsConversationsAndDocs(t *testing.T) {
	s := chatServer(t, &fakeEngine{})
	ana := signupPerson(t, s, "", SignupRequest{
		Email: "ana@exemplo.com", Name: "Ana", Password: "senha-da-ana",
	})
	ws := decode[store.Workspace](t, request(t, s, "POST", "/v1/workspaces", ana.Token, CreateWorkspaceRequest{Name: "Cliente X"}))

	// Default (no header) sees the 5 starting agents in "pessoal"; new workspace starts empty.
	personalAgents := decode[[]AgentStatus](t, request(t, s, "GET", "/v1/agents", ana.Token, nil))
	if len(personalAgents) != len(teamTemplates) || personalAgents[0].WorkspaceID != store.DefaultWorkspaceID {
		t.Fatalf("personal agents: %+v", personalAgents)
	}
	if list := decode[[]AgentStatus](t, requestWS(t, s, "GET", "/v1/agents", ana.Token, ws.ID, nil)); len(list) != 0 {
		t.Fatalf("new workspace should have 0 agents, got %+v", list)
	}

	// Creating an agent in ws.ID (even named "Atlas", like in "pessoal") scopes it to ws.ID.
	w := requestWS(t, s, "POST", "/v1/agents", ana.Token, ws.ID, AgentRequest{Name: "Atlas", Role: "Dev do Cliente"})
	if w.Code != 201 {
		t.Fatalf("create scoped agent: %d %s", w.Code, w.Body.String())
	}
	scopedAgent := decode[store.Agent](t, w)
	if scopedAgent.WorkspaceID != ws.ID {
		t.Fatalf("scoped agent workspace_id: %+v", scopedAgent)
	}

	wsAgents := decode[[]AgentStatus](t, requestWS(t, s, "GET", "/v1/agents", ana.Token, ws.ID, nil))
	if len(wsAgents) != 1 || wsAgents[0].ID != scopedAgent.ID {
		t.Fatalf("ws agents: %+v", wsAgents)
	}
	if list := decode[[]AgentStatus](t, request(t, s, "GET", "/v1/agents", ana.Token, nil)); len(list) != len(teamTemplates) {
		t.Fatalf("personal agents polluted: %d", len(list))
	}

	// Admin token always passes workspaceFrom(r), even for workspaces created by users.
	adminWSAgents := decode[[]AgentStatus](t, requestWS(t, s, "GET", "/v1/agents", adminTestToken, ws.ID, nil))
	if len(adminWSAgents) != 1 || adminWSAgents[0].ID != scopedAgent.ID {
		t.Fatalf("admin ws agents: %+v", adminWSAgents)
	}

	// Conversations, projects and docs are also isolated per workspace.
	conv := decode[store.Conversation](t, requestWS(t, s, "POST", "/v1/conversations", ana.Token, ws.ID, ConversationRequest{Title: "Kickoff"}))
	if conv.WorkspaceID != ws.ID || conv.AgentID != scopedAgent.ID {
		t.Fatalf("scoped conv: %+v", conv)
	}
	if list := decode[[]store.Conversation](t, request(t, s, "GET", "/v1/conversations", ana.Token, nil)); len(list) != 0 {
		t.Fatalf("personal conversations saw ws conv: %+v", list)
	}
	if list := decode[[]store.Conversation](t, requestWS(t, s, "GET", "/v1/conversations", ana.Token, ws.ID, nil)); len(list) != 1 {
		t.Fatalf("ws conversations: %+v", list)
	}

	title, body := "Contrato Cliente X", "Escopo fechado."
	doc := decode[store.Doc](t, requestWS(t, s, "POST", "/v1/docs", ana.Token, ws.ID, DocRequest{Title: &title, Content: &body}))
	if doc.WorkspaceID != ws.ID {
		t.Fatalf("scoped doc: %+v", doc)
	}
	if list := decode[[]store.Doc](t, request(t, s, "GET", "/v1/docs", ana.Token, nil)); len(list) != 0 {
		t.Fatalf("personal docs saw ws doc: %+v", list)
	}
	if list := decode[[]store.Doc](t, requestWS(t, s, "GET", "/v1/docs", ana.Token, ws.ID, nil)); len(list) != 1 || list[0].ID != doc.ID {
		t.Fatalf("ws docs: %+v", list)
	}

	proj := decode[store.Project](t, requestWS(t, s, "POST", "/v1/projects", ana.Token, ws.ID, ProjectRequest{Name: "Portal"}))
	if proj.WorkspaceID != ws.ID {
		t.Fatalf("scoped project: %+v", proj)
	}
	if list := decode[[]store.Project](t, request(t, s, "GET", "/v1/projects", ana.Token, nil)); len(list) != 0 {
		t.Fatalf("personal projects saw ws project: %+v", list)
	}
}
