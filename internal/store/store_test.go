package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Set AVORA_TEST_DATABASE_URL to also run the contract against Postgres.
const testDatabaseEnv = "AVORA_TEST_DATABASE_URL"

var t0 = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

type opener func(t *testing.T) Store

func stores(t *testing.T) map[string]opener {
	t.Helper()
	all := map[string]opener{"memory": func(*testing.T) Store { return NewMemory() }}
	if url := os.Getenv(testDatabaseEnv); url != "" {
		all["postgres"] = func(t *testing.T) Store { return openTestPostgres(t, url) }
	}
	return all
}

// openTestPostgres gives each test its own schema, dropped afterwards.
func openTestPostgres(t *testing.T, url string) Store {
	t.Helper()
	ctx := context.Background()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	schema := "avora_test_" + hex.EncodeToString(b)
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	s, err := OpenPostgresConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close(ctx)
	})
	return s
}

func eachStore(t *testing.T, fn func(t *testing.T, s Store)) {
	for name, open := range stores(t) {
		t.Run(name, func(t *testing.T) { fn(t, open(t)) })
	}
}

func newJob(id, node string, at time.Time) Job {
	return Job{ID: id, Action: "ping", Target: scheduler.Windows, DeviceID: node, CreatedAt: at}
}

func eventTypes(t *testing.T, s Store, jobID string) []string {
	t.Helper()
	events, err := s.Events(context.Background(), EventFilter{JobID: jobID})
	if err != nil {
		t.Fatal(err)
	}
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestJobLifecycle(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		created, err := s.CreateJob(ctx, newJob("j1", "pc", t0), "PC online", ActorAdmin)
		if err != nil || created.Status != StatusQueued {
			t.Fatalf("create: %+v %v", created, err)
		}
		if _, err := s.CreateJob(ctx, newJob("j1", "pc", t0), "", ActorAdmin); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate id: %v", err)
		}
		if job, err := s.ClaimJob(ctx, "vps", t0, time.Minute, ClaimSlots{}); err != nil || job != nil {
			t.Fatalf("other node claimed: %+v %v", job, err)
		}
		claimed, err := s.ClaimJob(ctx, "pc", t0.Add(time.Second), time.Minute, ClaimSlots{})
		if err != nil || claimed == nil || claimed.Status != StatusRunning || claimed.Attempts != 1 || claimed.LeaseExpiresAt == nil {
			t.Fatalf("claim: %+v %v", claimed, err)
		}
		if _, err := s.CompleteJob(ctx, "j1", "vps", Result{Success: true}, t0); !errors.Is(err, ErrForbidden) {
			t.Fatalf("cross-node completion: %v", err)
		}
		done, err := s.CompleteJob(ctx, "j1", "pc", Result{Success: true, Output: "pong"}, t0.Add(2*time.Second))
		if err != nil || done.Status != StatusSucceeded || done.Output != "pong" || done.CompletedAt == nil || done.LeaseExpiresAt != nil {
			t.Fatalf("complete: %+v %v", done, err)
		}
		if _, err := s.CompleteJob(ctx, "j1", "pc", Result{Success: true}, t0); !errors.Is(err, ErrConflict) {
			t.Fatalf("double completion: %v", err)
		}
		if _, err := s.GetJob(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing job: %v", err)
		}
		got, err := s.GetJob(ctx, "j1")
		if err != nil || !got.CreatedAt.Equal(t0) {
			t.Fatalf("get: %+v %v", got, err)
		}
		want := []string{EventJobCreated, EventJobClaimed, EventJobSucceeded}
		if types := eventTypes(t, s, "j1"); !equal(types, want) {
			t.Fatalf("events %v, want %v", types, want)
		}
	})
}

func TestClaimOrderIsFIFO(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for i, id := range []string{"b", "a", "c"} {
			if _, err := s.CreateJob(ctx, newJob(id, "pc", t0.Add(time.Duration(i)*time.Second)), "", ActorAdmin); err != nil {
				t.Fatal(err)
			}
		}
		for _, want := range []string{"b", "a", "c"} {
			job, err := s.ClaimJob(ctx, "pc", t0, time.Minute, ClaimSlots{})
			if err != nil || job == nil || job.ID != want {
				t.Fatalf("claimed %+v, want %s (%v)", job, want, err)
			}
		}
	})
}

func TestCancel(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		// "running" is created first, so the claim takes it; "queued" stays queued.
		for i, id := range []string{"running", "queued"} {
			if _, err := s.CreateJob(ctx, newJob(id, "pc", t0.Add(time.Duration(i)*time.Second)), "", ActorAdmin); err != nil {
				t.Fatal(err)
			}
		}
		if job, err := s.ClaimJob(ctx, "pc", t0, time.Minute, ClaimSlots{}); err != nil || job == nil || job.ID != "running" {
			t.Fatalf("claim: %+v %v", job, err)
		}
		for _, id := range []string{"queued", "running"} {
			job, err := s.CancelJob(ctx, id, ActorAdmin, t0.Add(time.Second))
			if err != nil || job.Status != StatusCanceled || job.CompletedAt == nil {
				t.Fatalf("cancel %s: %+v %v", id, job, err)
			}
			if _, err := s.CancelJob(ctx, id, ActorAdmin, t0); !errors.Is(err, ErrConflict) {
				t.Fatalf("cancel twice: %v", err)
			}
		}
		if _, err := s.CompleteJob(ctx, "queued", "pc", Result{Success: true}, t0); !errors.Is(err, ErrConflict) {
			t.Fatalf("completed a canceled job: %v", err)
		}
		if _, err := s.CancelJob(ctx, "missing", ActorAdmin, t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cancel missing: %v", err)
		}
	})
}

func TestExpiredLeasesRequeueThenTimeOut(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if _, err := s.CreateJob(ctx, newJob("j", "pc", t0), "", ActorAdmin); err != nil {
			t.Fatal(err)
		}
		now := t0
		for attempt := 1; attempt <= 2; attempt++ {
			if job, err := s.ClaimJob(ctx, "pc", now, time.Minute, ClaimSlots{}); err != nil || job == nil {
				t.Fatalf("claim %d: %v", attempt, err)
			}
			if changed, err := s.ExpireLeases(ctx, now.Add(30*time.Second), 2); err != nil || len(changed) != 0 {
				t.Fatalf("lease expired early: %+v %v", changed, err)
			}
			now = now.Add(2 * time.Minute)
			changed, err := s.ExpireLeases(ctx, now, 2)
			if err != nil || len(changed) != 1 {
				t.Fatalf("expire %d: %+v %v", attempt, changed, err)
			}
			want := StatusQueued
			if attempt == 2 {
				want = StatusTimedOut
			}
			if changed[0].Status != want {
				t.Fatalf("attempt %d: status %s, want %s", attempt, changed[0].Status, want)
			}
		}
		want := []string{EventJobCreated, EventJobClaimed, EventJobRequeued, EventJobClaimed, EventJobTimedOut}
		if types := eventTypes(t, s, "j"); !equal(types, want) {
			t.Fatalf("events %v, want %v", types, want)
		}
	})
}

func TestHeartbeatLogsOnlyTransitions(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		ttl := 30 * time.Second
		for _, at := range []time.Time{t0, t0.Add(5 * time.Second), t0.Add(10 * time.Second), t0.Add(time.Minute)} {
			if err := s.RecordHeartbeat(ctx, "pc", at, ttl); err != nil {
				t.Fatal(err)
			}
		}
		events, err := s.Events(ctx, EventFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 || events[0].Type != EventNodeOnline || events[0].NodeID != "pc" {
			t.Fatalf("want 2 node.online events, got %+v", events)
		}
	})
}

func pairingCode(hash string, expires time.Time) PairingCode {
	return PairingCode{Hash: []byte(hash), Name: "meu-pc", Kind: scheduler.Windows, CreatedAt: t0, ExpiresAt: expires}
}

func TestPairingCodeIsSingleUse(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if err := s.CreatePairingCode(ctx, pairingCode("code", t0.Add(10*time.Minute)), ActorAdmin); err != nil {
			t.Fatal(err)
		}
		if err := s.CreatePairingCode(ctx, pairingCode("code", t0.Add(10*time.Minute)), ActorAdmin); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate code: %v", err)
		}
		if _, err := s.RedeemPairingCode(ctx, []byte("wrong"), "n1", []byte("tok1"), t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown code: %v", err)
		}
		node, err := s.RedeemPairingCode(ctx, []byte("code"), "n1", []byte("tok1"), t0.Add(time.Minute))
		if err != nil || node.ID != "n1" || node.Name != "meu-pc" || node.Kind != scheduler.Windows || node.RevokedAt != nil {
			t.Fatalf("redeem: %+v %v", node, err)
		}
		if _, err := s.RedeemPairingCode(ctx, []byte("code"), "n2", []byte("tok2"), t0.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("code reused: %v", err)
		}
		found, err := s.NodeByToken(ctx, []byte("tok1"))
		if err != nil || found.ID != "n1" {
			t.Fatalf("by token: %+v %v", found, err)
		}
		if _, err := s.NodeByToken(ctx, []byte("tok2")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown token: %v", err)
		}
	})
}

func TestPairingCodeExpires(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if err := s.CreatePairingCode(ctx, pairingCode("code", t0.Add(10*time.Minute)), ActorAdmin); err != nil {
			t.Fatal(err)
		}
		if _, err := s.RedeemPairingCode(ctx, []byte("code"), "n1", []byte("tok"), t0.Add(10*time.Minute)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expired code accepted: %v", err)
		}
	})
}

func TestRevokeNodeCancelsItsJobs(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for i, code := range []string{"c1", "c2"} {
			if err := s.CreatePairingCode(ctx, pairingCode(code, t0.Add(time.Hour)), ActorAdmin); err != nil {
				t.Fatal(err)
			}
			if _, err := s.RedeemPairingCode(ctx, []byte(code), []string{"pc", "other"}[i], []byte("tok-"+code), t0.Add(time.Duration(i)*time.Second)); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.RecordHeartbeat(ctx, "pc", t0.Add(time.Minute), time.Minute); err != nil {
			t.Fatal(err)
		}
		for i, id := range []string{"running", "queued"} {
			if _, err := s.CreateJob(ctx, newJob(id, "pc", t0.Add(time.Duration(i)*time.Second)), "", ActorAdmin); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.CreateJob(ctx, newJob("elsewhere", "other", t0), "", ActorAdmin); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ClaimJob(ctx, "pc", t0, time.Minute, ClaimSlots{}); err != nil {
			t.Fatal(err)
		}

		revoked, err := s.RevokeNode(ctx, "pc", ActorAdmin, t0.Add(2*time.Minute))
		if err != nil || revoked.RevokedAt == nil {
			t.Fatalf("revoke: %+v %v", revoked, err)
		}
		if _, err := s.RevokeNode(ctx, "pc", ActorAdmin, t0); !errors.Is(err, ErrConflict) {
			t.Fatalf("revoke twice: %v", err)
		}
		if _, err := s.RevokeNode(ctx, "missing", ActorAdmin, t0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("revoke missing: %v", err)
		}
		if _, err := s.NodeByToken(ctx, []byte("tok-c1")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("revoked node still authenticates: %v", err)
		}
		for id, want := range map[string]Status{"running": StatusCanceled, "queued": StatusCanceled, "elsewhere": StatusQueued} {
			job, err := s.GetJob(ctx, id)
			if err != nil || job.Status != want {
				t.Fatalf("job %s: %+v %v, want %s", id, job, err, want)
			}
		}

		nodes, err := s.ListNodes(ctx)
		if err != nil || len(nodes) != 2 || nodes[0].ID != "pc" || nodes[1].ID != "other" {
			t.Fatalf("list: %+v %v", nodes, err)
		}
		if nodes[0].LastSeen == nil || !nodes[0].LastSeen.Equal(t0.Add(time.Minute)) || nodes[0].RevokedAt == nil {
			t.Fatalf("pc presence/revocation: %+v", nodes[0])
		}
		if nodes[1].LastSeen != nil || nodes[1].RevokedAt != nil {
			t.Fatalf("other node: %+v", nodes[1])
		}
	})
}

func TestEventsPagination(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for _, id := range []string{"a", "b", "c"} {
			if _, err := s.CreateJob(ctx, newJob(id, "pc", t0), "", ActorAdmin); err != nil {
				t.Fatal(err)
			}
		}
		first, err := s.Events(ctx, EventFilter{Limit: 2})
		if err != nil || len(first) != 2 {
			t.Fatalf("first page: %+v %v", first, err)
		}
		rest, err := s.Events(ctx, EventFilter{AfterID: first[1].ID})
		if err != nil || len(rest) != 1 || rest[0].JobID != "c" {
			t.Fatalf("second page: %+v %v", rest, err)
		}
		if string(rest[0].Payload) == "" {
			t.Fatal("empty payload")
		}
	})
}

func TestPostgresEventsAreAppendOnly(t *testing.T) {
	url := os.Getenv(testDatabaseEnv)
	if url == "" {
		t.Skip(testDatabaseEnv + " not set")
	}
	s := openTestPostgres(t, url).(*Postgres)
	ctx := context.Background()
	if _, err := s.CreateJob(ctx, newJob("j", "pc", t0), "", ActorAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE events SET type = 'tampered'`); err == nil {
		t.Fatal("update on events succeeded")
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM events`); err == nil {
		t.Fatal("delete on events succeeded")
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	url := os.Getenv(testDatabaseEnv)
	if url == "" {
		t.Skip(testDatabaseEnv + " not set")
	}
	s := openTestPostgres(t, url).(*Postgres)
	if err := migrate(context.Background(), s.pool); err != nil {
		t.Fatalf("second migration run: %v", err)
	}
}

func TestConversations(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for i, id := range []string{"old", "new"} {
			c := Conversation{ID: id, Engine: "native", Model: "m", CreatedAt: t0.Add(time.Duration(i) * time.Minute)}
			if _, err := s.CreateConversation(ctx, c, ActorAdmin); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.CreateConversation(ctx, Conversation{ID: "old", Engine: "native", Model: "m", CreatedAt: t0}, ActorAdmin); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate conversation: %v", err)
		}
		user, err := s.AppendMessage(ctx, ChatMessage{ConversationID: "old", Role: "user", Content: "segredo pessoal", CreatedAt: t0.Add(5 * time.Minute)}, ActorAdmin)
		if err != nil || user.ID == 0 {
			t.Fatalf("append user: %+v %v", user, err)
		}
		reply, err := s.AppendMessage(ctx, ChatMessage{ConversationID: "old", Role: "assistant", Content: "ok", Model: "m",
			PromptTokens: 10, CompletionTokens: 2, DurationMS: 900, CreatedAt: t0.Add(6 * time.Minute)}, "engine:native")
		if err != nil || reply.ID <= user.ID || reply.CompletionTokens != 2 {
			t.Fatalf("append reply: %+v %v", reply, err)
		}
		if _, err := s.AppendMessage(ctx, ChatMessage{ConversationID: "missing", Role: "user", Content: "x", CreatedAt: t0}, ActorAdmin); !errors.Is(err, ErrNotFound) {
			t.Fatalf("message to missing conversation: %v", err)
		}

		msgs, err := s.Messages(ctx, "old")
		if err != nil || len(msgs) != 2 || msgs[0].Content != "segredo pessoal" || msgs[1].Role != "assistant" {
			t.Fatalf("messages: %+v %v", msgs, err)
		}
		if _, err := s.Messages(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("messages of missing conversation: %v", err)
		}
		list, err := s.ListConversations(ctx, "", 10)
		if err != nil || len(list) != 2 || list[0].ID != "old" || !list[0].UpdatedAt.Equal(t0.Add(6*time.Minute)) {
			t.Fatalf("most recent first: %+v %v", list, err)
		}

		if err := s.RecordEvent(ctx, Event{At: t0, Type: EventLLMFailed, Actor: "engine:native", Payload: json.RawMessage(`{"status":429}`)}); err != nil {
			t.Fatal(err)
		}
		events, err := s.Events(ctx, EventFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if strings.Contains(string(e.Payload), "segredo") {
				t.Fatalf("message content leaked into the audit log: %s", e.Payload)
			}
		}
		if last := events[len(events)-1]; last.Type != EventLLMFailed {
			t.Fatalf("recorded event: %+v", last)
		}
	})
}

func TestAgentJobs(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		agent := newJob("agent", "pc", t0)
		agent.Action = ActionAgentRun
		agent.Agent = &AgentSpec{Engine: "claude", Prompt: "resuma o README", Mode: "read", MaxMinutes: 5}
		if _, err := s.CreateJob(ctx, agent, "", ActorAdmin); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateJob(ctx, newJob("ping", "pc", t0.Add(time.Second)), "", ActorAdmin); err != nil {
			t.Fatal(err)
		}
		// A node without a free agent slot still gets the ping behind it.
		job, err := s.ClaimJob(ctx, "pc", t0, time.Minute, ClaimSlots{})
		if err != nil || job == nil || job.ID != "ping" {
			t.Fatalf("claim without agent slot: %+v %v", job, err)
		}
		job, err = s.ClaimJob(ctx, "pc", t0, time.Minute, ClaimSlots{Agents: true})
		if err != nil || job == nil || job.ID != "agent" || job.Agent == nil || job.Agent.Prompt != "resuma o README" {
			t.Fatalf("claim agent: %+v %v", job, err)
		}

		// Renewal keeps a long run alive; unknown or foreign jobs come back as stop.
		stop, err := s.RenewLeases(ctx, "pc", []string{"agent", "ghost"}, t0.Add(50*time.Second), time.Minute)
		if err != nil || len(stop) != 1 || stop[0] != "ghost" {
			t.Fatalf("renew: %v %v", stop, err)
		}
		if changed, _ := s.ExpireLeases(ctx, t0.Add(90*time.Second), 3); len(changed) != 1 || changed[0].ID != "ping" {
			t.Fatalf("renewed lease expired: %+v", changed)
		}
		for _, id := range []string{"agent", "ping"} {
			if _, err := s.CancelJob(ctx, id, ActorAdmin, t0.Add(100*time.Second)); err != nil {
				t.Fatal(err)
			}
		}
		if stop, _ := s.RenewLeases(ctx, "pc", []string{"agent"}, t0.Add(101*time.Second), time.Minute); len(stop) != 1 {
			t.Fatalf("canceled job not reported as stop: %v", stop)
		}

		// An expired agent lease is never retried: it could repeat side effects.
		retry := newJob("agent2", "pc", t0)
		retry.Action, retry.Agent = ActionAgentRun, &AgentSpec{Engine: "claude", Prompt: "x", Mode: "read", MaxMinutes: 5}
		if _, err := s.CreateJob(ctx, retry, "", ActorAdmin); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ClaimJob(ctx, "pc", t0, time.Minute, ClaimSlots{Agents: true}); err != nil {
			t.Fatal(err)
		}
		changed, err := s.ExpireLeases(ctx, t0.Add(2*time.Minute), 3)
		if err != nil || len(changed) != 1 || changed[0].Status != StatusTimedOut {
			t.Fatalf("agent lease expiry: %+v %v", changed, err)
		}

		// Completion stats land in the event, not in the job.
		stats := newJob("stats", "pc", t0)
		if _, err := s.CreateJob(ctx, stats, "", ActorAdmin); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ClaimJob(ctx, "pc", t0, time.Minute, ClaimSlots{}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CompleteJob(ctx, "stats", "pc", Result{Success: true, Stats: json.RawMessage(`{"turns":3}`)}, t0); err != nil {
			t.Fatal(err)
		}
		events, _ := s.Events(ctx, EventFilter{JobID: "stats"})
		if last := events[len(events)-1]; !strings.Contains(string(last.Payload), `"turns"`) {
			t.Fatalf("stats missing from %s", last.Payload)
		}
	})
}

func TestMCPJobClaimsAndExpiresIndependently(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		agent := newJob("agent", "vps", t0)
		agent.Action = ActionAgentRun
		plugin := newJob("plugin", "vps", t0.Add(time.Second))
		plugin.Action = ActionMCPRun
		for _, job := range []Job{agent, plugin} {
			if _, err := s.CreateJob(ctx, job, "", ActorAdmin); err != nil {
				t.Fatal(err)
			}
		}
		if job, err := s.ClaimJob(ctx, "vps", t0, time.Minute, ClaimSlots{}); err != nil || job != nil {
			t.Fatalf("claim without slots: %+v %v", job, err)
		}
		if job, err := s.ClaimJob(ctx, "vps", t0, time.Minute, ClaimSlots{MCP: true}); err != nil || job == nil || job.ID != plugin.ID {
			t.Fatalf("claim MCP while agent lane is full: %+v %v", job, err)
		}
		changed, err := s.ExpireLeases(ctx, t0.Add(2*time.Minute), 3)
		if err != nil || len(changed) != 1 || changed[0].ID != plugin.ID || changed[0].Status != StatusTimedOut {
			t.Fatalf("MCP lease expiry: %+v %v", changed, err)
		}
	})
}

func TestListJobsNewestFirst(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for i, id := range []string{"a", "b", "c"} {
			if _, err := s.CreateJob(ctx, newJob(id, "pc", t0.Add(time.Duration(i)*time.Second)), "", ActorAdmin); err != nil {
				t.Fatal(err)
			}
		}
		jobs, err := s.ListJobs(ctx, 2)
		if err != nil || len(jobs) != 2 || jobs[0].ID != "c" || jobs[1].ID != "b" {
			t.Fatalf("list: %+v %v", jobs, err)
		}
	})
}

func TestEventsLatestFirst(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for _, id := range []string{"a", "b", "c"} {
			if _, err := s.CreateJob(ctx, newJob(id, "pc", t0), "", ActorAdmin); err != nil {
				t.Fatal(err)
			}
		}
		events, err := s.Events(ctx, EventFilter{Latest: true, Limit: 2})
		if err != nil || len(events) != 2 || events[0].JobID != "c" || events[1].JobID != "b" {
			t.Fatalf("latest: %+v %v", events, err)
		}
	})
}
