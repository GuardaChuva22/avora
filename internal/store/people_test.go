package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPeopleWorkspacesAndScopedEntities(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()

		// "pessoal" exists from the start with no owner.
		pessoal, err := s.GetWorkspace(ctx, DefaultWorkspaceID)
		if err != nil || pessoal.Mode != WorkspacePersonal || pessoal.OwnerID != "" {
			t.Fatalf("initial pessoal: %+v %v", pessoal, err)
		}

		// First person becomes owner of "pessoal".
		ana, first, err := s.CreatePerson(ctx, Person{
			ID: "p1", Email: "Ana@Exemplo.com", Name: "Ana", PasswordHash: "argon2id$x$y",
		}, true, t0)
		if err != nil || !first || ana.Email != "ana@exemplo.com" {
			t.Fatalf("create first person: %+v %v %v", ana, first, err)
		}
		pessoal, _ = s.GetWorkspace(ctx, DefaultWorkspaceID)
		if pessoal.OwnerID != "p1" {
			t.Fatalf("pessoal owner: %+v", pessoal)
		}

		// Second person with onlyFirst=true is rejected with ErrForbidden.
		if _, _, err := s.CreatePerson(ctx, Person{
			ID: "p2", Email: "b@exemplo.com", Name: "B", PasswordHash: "argon2id$x$y",
		}, true, t0); !errors.Is(err, ErrForbidden) {
			t.Fatalf("second person with onlyFirst: %v", err)
		}

		// Session lifecycle.
		if err := s.CreateSession(ctx, Session{
			TokenHash: []byte("tok-ana"), PersonID: "p1", CreatedAt: t0, ExpiresAt: t0.Add(SessionTTL),
		}); err != nil {
			t.Fatal(err)
		}
		if got, err := s.PersonBySession(ctx, []byte("tok-ana"), t0.Add(time.Hour)); err != nil || got.ID != "p1" {
			t.Fatalf("by session: %+v %v", got, err)
		}
		if _, err := s.PersonBySession(ctx, []byte("tok-ana"), t0.Add(SessionTTL)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expired session: %v", err)
		}
		_ = s.DeleteSession(ctx, []byte("tok-ana"))
		if _, err := s.PersonBySession(ctx, []byte("tok-ana"), t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted session: %v", err)
		}

		// Workspace + invite + approval flow.
		ws, err := s.CreateWorkspace(ctx, Workspace{ID: "equipe", Name: "Equipe", Icon: "⚡", Mode: WorkspaceTeam, OwnerID: "p1"}, t0)
		if err != nil || ws.ID != "equipe" {
			t.Fatalf("create workspace: %+v %v", ws, err)
		}
		bruno, _, err := s.CreatePerson(ctx, Person{
			ID: "p2", Email: "bruno@exemplo.com", Name: "Bruno", PasswordHash: "argon2id$x$y",
		}, false, t0)
		if err != nil || bruno.ID != "p2" {
			t.Fatalf("create bruno: %+v %v", bruno, err)
		}
		inv, err := s.CreateInvite(ctx, Invite{
			ID: "inv1", WorkspaceID: "equipe", CodeHash: []byte("code-1"), Role: RoleMember,
			Approval: true, CreatedBy: "p1", CreatedAt: t0, ExpiresAt: t0.AddDate(0, 0, 7),
		})
		if err != nil || inv.Uses != 0 {
			t.Fatalf("create invite: %+v %v", inv, err)
		}
		mem, err := s.AcceptInvite(ctx, []byte("code-1"), "p2", t0.Add(time.Minute))
		if err != nil || mem.Status != MemberPending || mem.Role != RoleMember {
			t.Fatalf("accept invite: %+v %v", mem, err)
		}
		mem, err = s.ApproveMember(ctx, "equipe", "p2")
		if err != nil || mem.Status != MemberActive {
			t.Fatalf("approve: %+v %v", mem, err)
		}
		pws, err := s.ListPersonWorkspaces(ctx, "p2")
		if err != nil || len(pws) != 1 || pws[0].Members != 2 {
			t.Fatalf("person workspaces: %+v %v", pws, err)
		}

		// Workspace scoping on agents, conversations, channels, projects, docs, providers.
		for _, wsID := range []string{DefaultWorkspaceID, "equipe"} {
			if _, err := s.SaveAgent(ctx, Agent{ID: "ag-" + wsID, WorkspaceID: wsID, Name: "Ag", Engine: "native"}, ActorAdmin, t0); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateConversation(ctx, Conversation{ID: "cv-" + wsID, WorkspaceID: wsID, AgentID: "ag-" + wsID, Engine: "native", CreatedAt: t0}, ActorAdmin); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveChannel(ctx, Channel{ID: "ch-" + wsID, WorkspaceID: wsID, Name: "geral", Everyone: true}, ActorAdmin, t0); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveProject(ctx, Project{ID: "pr-" + wsID, WorkspaceID: wsID, Name: "Proj"}, ActorAdmin, t0); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateDoc(ctx, Doc{ID: "dc-" + wsID, WorkspaceID: wsID, Title: "Doc", Kind: DocKindDoc, Content: "oi"}, "", ActorAdmin, t0); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveProvider(ctx, Provider{ID: "pv-" + wsID, WorkspaceID: wsID, Kind: "local", Name: "Local", Status: "ok"}, ActorAdmin, t0); err != nil {
				t.Fatal(err)
			}
		}

		scoped := WithWorkspace(ctx, "equipe")
		if list, err := s.ListAgents(scoped); err != nil || len(list) != 1 || list[0].ID != "ag-equipe" {
			t.Fatalf("scoped agents: %+v %v", list, err)
		}
		if list, err := s.ListConversations(scoped, "", 50); err != nil || len(list) != 2 { // direct + channel conv
			t.Fatalf("scoped conversations: %+v %v", list, err)
		}
		if list, err := s.ListChannels(scoped); err != nil || len(list) != 1 || list[0].ID != "ch-equipe" {
			t.Fatalf("scoped channels: %+v %v", list, err)
		}
		if list, err := s.ListProjects(scoped); err != nil || len(list) != 1 || list[0].ID != "pr-equipe" {
			t.Fatalf("scoped projects: %+v %v", list, err)
		}
		if list, err := s.ListDocs(scoped, DocFilter{}); err != nil || len(list) != 1 || list[0].ID != "dc-equipe" {
			t.Fatalf("scoped docs: %+v %v", list, err)
		}
		if list, err := s.ListProviders(scoped); err != nil || len(list) != 1 || list[0].ID != "pv-equipe" {
			t.Fatalf("scoped providers: %+v %v", list, err)
		}
	})
}
