package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

const testAdmin = "admin-test-token-abcdefghijklmnopqrstuvwxyz1234"

func adminCall(t *testing.T, base, method, path string, body any, out any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testAdmin)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		t.Fatalf("%s %s: status %d", method, path, res.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPairRunAndRevoke(t *testing.T) {
	core, err := control.NewServer(testAdmin, store.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(core.Handler())
	defer server.Close()
	ctx := context.Background()

	var code control.PairingCodeResponse
	adminCall(t, server.URL, "POST", "/v1/nodes/pairing-codes", control.PairingCodeRequest{Name: "my-pc", Kind: "windows"}, &code)

	statePath := filepath.Join(t.TempDir(), "node.json")
	state, err := LoadOrPair(ctx, statePath, server.URL, code.Code)
	if err != nil {
		t.Fatal(err)
	}
	// Second start reads the saved state and never needs the (now used) code.
	again, err := LoadOrPair(ctx, statePath, server.URL, code.Code)
	if err != nil || again.Token != state.Token || again.NodeID != state.NodeID {
		t.Fatalf("state not reused: %+v %v", again, err)
	}
	if _, err := LoadOrPair(ctx, filepath.Join(t.TempDir(), "x.json"), server.URL, code.Code); err == nil {
		t.Fatal("used code paired a second node")
	}

	c, err := NewClient(server.URL, state.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Heartbeat(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var job control.Job
	adminCall(t, server.URL, "POST", "/v1/jobs", control.JobRequest{Target: "windows", Action: "ping"}, &job)
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	adminCall(t, server.URL, "GET", "/v1/jobs/"+job.ID, nil, &job)
	if job.Status != store.StatusSucceeded || job.Output != "pong from "+state.NodeID {
		t.Fatalf("job did not complete: %+v", job)
	}

	adminCall(t, server.URL, "POST", "/v1/nodes/"+state.NodeID+"/revoke", nil, nil)
	if err := c.Run(ctx, nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked node kept running: %v", err)
	}
}

func TestLoadOrPairNeedsCodeOnFirstRun(t *testing.T) {
	if _, err := LoadOrPair(context.Background(), filepath.Join(t.TempDir(), "node.json"), "http://127.0.0.1:1", ""); err == nil {
		t.Fatal("started without state or pairing code")
	}
}

func TestRefusePlaintextRemote(t *testing.T) {
	token := control.NodeTokenPrefix + "abcdefghijklmnopqrstuvwxyz"
	if _, err := NewClient("http://example.org:8080", token); err == nil {
		t.Fatal("accepted insecure remote HTTP")
	}
	if _, err := NewClient("https://example.org/evil", token); err == nil {
		t.Fatal("accepted URL path")
	}
	if _, err := NewClient("https://example.org", "not-a-node-token"); err == nil {
		t.Fatal("accepted malformed token")
	}
}
