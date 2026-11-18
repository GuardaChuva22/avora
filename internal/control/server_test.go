package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
)

const adminTestToken = "admin-test-token-abcdefghijklmnopqrstuvwxyz1234"

// clock is a controllable time source for the server under test.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func makeServer(t *testing.T) (*Server, *clock) {
	t.Helper()
	s, err := NewServer(adminTestToken, store.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)}
	s.now = c.now
	// The tests were written on the owner's clock.
	if err := s.SetTimeZone("America/Sao_Paulo"); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureTeam(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, c
}

func request(t *testing.T, s *Server, method, path, token string, body any) *httptest.ResponseRecorder {
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
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return v
}

func pairingCode(t *testing.T, s *Server, name, kind string) PairingCodeResponse {
	t.Helper()
	w := request(t, s, "POST", "/v1/nodes/pairing-codes", adminTestToken, PairingCodeRequest{Name: name, Kind: kind})
	if w.Code != 201 {
		t.Fatalf("pairing code status=%d: %s", w.Code, w.Body.String())
	}
	return decode[PairingCodeResponse](t, w)
}

// pairNode creates a code and redeems it, returning the node's ID and token.
func pairNode(t *testing.T, s *Server, name, kind string) PairResponse {
	t.Helper()
	code := pairingCode(t, s, name, kind)
	w := request(t, s, "POST", "/v1/runtime/pair", "", PairRequest{Code: code.Code})
	if w.Code != 201 {
		t.Fatalf("pair status=%d: %s", w.Code, w.Body.String())
	}
	return decode[PairResponse](t, w)
}

func submit(t *testing.T, s *Server, req JobRequest) Job {
	t.Helper()
	w := request(t, s, "POST", "/v1/jobs", adminTestToken, req)
	if w.Code != 201 {
		t.Fatalf("submit status=%d: %s", w.Code, w.Body.String())
	}
	return decode[Job](t, w)
}

func TestPairingFlow(t *testing.T) {
	s, c := makeServer(t)
	if got := request(t, s, "POST", "/v1/nodes/pairing-codes", "", PairingCodeRequest{Name: "pc", Kind: "windows"}); got.Code != 401 {
		t.Fatalf("pairing code without admin: %d", got.Code)
	}
	for _, bad := range []PairingCodeRequest{{Name: "", Kind: "windows"}, {Name: "pc", Kind: "android"}, {Name: strings.Repeat("x", 65), Kind: "vps"}} {
		if got := request(t, s, "POST", "/v1/nodes/pairing-codes", adminTestToken, bad); got.Code != 400 {
			t.Fatalf("%+v accepted: %d", bad, got.Code)
		}
	}
	code := pairingCode(t, s, "meu-pc", "windows")
	if len(code.Code) != 19 || !code.ExpiresAt.Equal(c.now().Add(PairingCodeTTL)) {
		t.Fatalf("code: %+v", code)
	}

	// Typed in lowercase without dashes, it still works; but only once.
	typed := strings.ToLower(strings.ReplaceAll(code.Code, "-", ""))
	paired := decode[PairResponse](t, request(t, s, "POST", "/v1/runtime/pair", "", PairRequest{Code: typed}))
	if paired.Name != "meu-pc" || paired.Kind != scheduler.Windows || !strings.HasPrefix(paired.Token, NodeTokenPrefix) {
		t.Fatalf("pair: %+v", paired)
	}
	if got := request(t, s, "POST", "/v1/runtime/pair", "", PairRequest{Code: code.Code}); got.Code != 401 {
		t.Fatalf("code reused: %d", got.Code)
	}

	expired := pairingCode(t, s, "late", "vps")
	c.advance(PairingCodeTTL)
	if got := request(t, s, "POST", "/v1/runtime/pair", "", PairRequest{Code: expired.Code}); got.Code != 401 {
		t.Fatalf("expired code accepted: %d", got.Code)
	}
	if got := request(t, s, "POST", "/v1/runtime/pair", "", PairRequest{Code: "nope"}); got.Code != 401 {
		t.Fatalf("garbage code: %d", got.Code)
	}

	if got := request(t, s, "POST", "/v1/runtime/heartbeat", paired.Token, nil); got.Code != 200 {
		t.Fatalf("heartbeat with new token: %d", got.Code)
	}
	nodes := decode[[]NodeStatus](t, request(t, s, "GET", "/v1/nodes", adminTestToken, nil))
	if len(nodes) != 1 || nodes[0].ID != paired.NodeID || !nodes[0].Online {
		t.Fatalf("nodes: %+v", nodes)
	}
	if strings.Contains(request(t, s, "GET", "/v1/events", adminTestToken, nil).Body.String(), paired.Token) {
		t.Fatal("token leaked into the event log")
	}
}

func TestNodeAuthAndPresence(t *testing.T) {
	s, c := makeServer(t)
	pc := pairNode(t, s, "pc", "windows")
	for _, token := range []string{"", "wrong", NodeTokenPrefix + "wrong", adminTestToken} {
		if got := request(t, s, "POST", "/v1/runtime/heartbeat", token, nil); got.Code != 401 {
			t.Fatalf("token %q accepted: %d", token, got.Code)
		}
	}
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	c.advance(OnlineTTL + time.Second)
	nodes := decode[[]NodeStatus](t, request(t, s, "GET", "/v1/nodes", adminTestToken, nil))
	if nodes[0].Online {
		t.Fatal("stale heartbeat still online")
	}
}

func TestRevokeCutsAccessAndCancelsJobs(t *testing.T) {
	s, _ := makeServer(t)
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	job := submit(t, s, JobRequest{Target: "windows", Action: "ping"})

	revoked := decode[NodeStatus](t, request(t, s, "POST", "/v1/nodes/"+pc.NodeID+"/revoke", adminTestToken, nil))
	if revoked.RevokedAt == nil || revoked.Online {
		t.Fatalf("revoke: %+v", revoked)
	}
	if got := request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil); got.Code != 401 {
		t.Fatalf("revoked node still accepted: %d", got.Code)
	}
	if job = decode[Job](t, request(t, s, "GET", "/v1/jobs/"+job.ID, adminTestToken, nil)); job.Status != store.StatusCanceled {
		t.Fatalf("job of revoked node: %+v", job)
	}
	if got := request(t, s, "POST", "/v1/nodes/"+pc.NodeID+"/revoke", adminTestToken, nil); got.Code != 409 {
		t.Fatalf("revoke twice: %d", got.Code)
	}
	if got := request(t, s, "POST", "/v1/nodes/missing/revoke", adminTestToken, nil); got.Code != 404 {
		t.Fatalf("revoke missing: %d", got.Code)
	}
	if got := request(t, s, "POST", "/v1/jobs", adminTestToken, JobRequest{Target: "windows", Action: "ping"}); got.Code != 409 {
		t.Fatalf("job routed to revoked node: %d", got.Code)
	}
}

func TestJobLifecycleAndFallback(t *testing.T) {
	s, _ := makeServer(t)
	pc := pairNode(t, s, "pc", "windows")
	vps := pairNode(t, s, "server", "vps")
	// Only VPS online => automatic job goes to VPS.
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)
	job := submit(t, s, JobRequest{Target: "auto", Action: "ping"})
	if job.DeviceID != vps.NodeID || job.Target != scheduler.VPS || job.Status != store.StatusQueued {
		t.Fatalf("unexpected job: %+v", job)
	}
	if got := request(t, s, "POST", "/v1/runtime/claim", pc.Token, nil); got.Code != 409 {
		t.Fatalf("offline worker claim: %d", got.Code)
	}
	if got := request(t, s, "POST", "/v1/runtime/claim", vps.Token, nil); got.Code != 200 {
		t.Fatalf("claim status: %d", got.Code)
	}
	if got := request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token, JobResult{Success: true}); got.Code != 403 {
		t.Fatalf("cross worker complete: %d", got.Code)
	}
	if got := request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", vps.Token, JobResult{Success: true, Output: "pong"}); got.Code != 200 {
		t.Fatalf("completion status=%d", got.Code)
	}
	job = decode[Job](t, request(t, s, "GET", "/v1/jobs/"+job.ID, adminTestToken, nil))
	if job.Status != store.StatusSucceeded || job.Output != "pong" || job.CompletedAt == nil {
		t.Fatalf("bad completion: %+v", job)
	}
	if got := request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", vps.Token, JobResult{Success: true}); got.Code != 409 {
		t.Fatalf("duplicate completion: %d", got.Code)
	}
	// PC joins and wins the next automatic assignment.
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	job = submit(t, s, JobRequest{Target: "auto", Action: "diagnostics"})
	if job.DeviceID != pc.NodeID || job.Target != scheduler.Windows {
		t.Fatalf("PC not preferred: %+v", job)
	}
}

func TestCancelAndEvents(t *testing.T) {
	s, _ := makeServer(t)
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	job := submit(t, s, JobRequest{Target: "windows", Action: "ping"})
	if got := request(t, s, "POST", "/v1/jobs/"+job.ID+"/cancel", "", nil); got.Code != 401 {
		t.Fatalf("unauthenticated cancel: %d", got.Code)
	}
	canceled := decode[Job](t, request(t, s, "POST", "/v1/jobs/"+job.ID+"/cancel", adminTestToken, nil))
	if canceled.Status != store.StatusCanceled {
		t.Fatalf("cancel: %+v", canceled)
	}
	if got := request(t, s, "POST", "/v1/jobs/"+job.ID+"/cancel", adminTestToken, nil); got.Code != 409 {
		t.Fatalf("second cancel: %d", got.Code)
	}
	if got := request(t, s, "POST", "/v1/runtime/claim", pc.Token, nil); got.Code != 204 {
		t.Fatalf("canceled job was claimable: %d", got.Code)
	}

	events := decode[[]store.Event](t, request(t, s, "GET", "/v1/events?job="+job.ID, adminTestToken, nil))
	if len(events) != 2 || events[0].Type != store.EventJobCreated || events[1].Type != store.EventJobCanceled {
		t.Fatalf("job events: %+v", events)
	}
	var created struct{ Reason string }
	if err := json.Unmarshal(events[0].Payload, &created); err != nil || created.Reason == "" {
		t.Fatalf("routing reason missing from %s", events[0].Payload)
	}
	all := decode[[]store.Event](t, request(t, s, "GET", "/v1/events?limit=1", adminTestToken, nil))
	if len(all) != 1 || all[0].Type != store.EventConversationCreated {
		t.Fatalf("first event: %+v", all)
	}
	for _, bad := range []string{"?limit=0", "?limit=abc", "?after=-1"} {
		if got := request(t, s, "GET", "/v1/events"+bad, adminTestToken, nil); got.Code != 400 {
			t.Fatalf("%s accepted: %d", bad, got.Code)
		}
	}
	if got := request(t, s, "GET", "/v1/events", "", nil); got.Code != 401 {
		t.Fatalf("unauthenticated events: %d", got.Code)
	}
}

func TestSweepRequeuesAbandonedJob(t *testing.T) {
	s, c := makeServer(t)
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	job := submit(t, s, JobRequest{Target: "windows", Action: "ping"})
	request(t, s, "POST", "/v1/runtime/claim", pc.Token, nil)
	c.advance(JobLease + time.Second)
	if err := s.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	job = decode[Job](t, request(t, s, "GET", "/v1/jobs/"+job.ID, adminTestToken, nil))
	if job.Status != store.StatusQueued || job.Attempts != 1 {
		t.Fatalf("abandoned job not requeued: %+v", job)
	}
}

func TestAllowlistAndManualTarget(t *testing.T) {
	s, _ := makeServer(t)
	for _, action := range []string{"shell", "rm -rf", "codex"} {
		if got := request(t, s, "POST", "/v1/jobs", adminTestToken, JobRequest{Action: action}); got.Code != 400 {
			t.Fatalf("%s was accepted: %d", action, got.Code)
		}
	}
	if got := request(t, s, "POST", "/v1/jobs", adminTestToken, JobRequest{Target: "windows", Action: "ping"}); got.Code != 409 {
		t.Fatalf("offline manual target accepted: %d", got.Code)
	}
	if got := request(t, s, "POST", "/v1/jobs", adminTestToken, JobRequest{Target: "provider_cloud", Action: "ping"}); got.Code != 400 {
		t.Fatalf("unimplemented cloud target accepted: %d", got.Code)
	}
}

func TestTokenValidation(t *testing.T) {
	if _, err := NewServer("short", store.NewMemory()); err == nil {
		t.Fatal("short admin token accepted")
	}
	if _, err := NewServer(adminTestToken, nil); err == nil {
		t.Fatal("missing store accepted")
	}
}
