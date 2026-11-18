package control

import (
	"testing"
	"time"
)

func TestQuotasPickBestOnlineReading(t *testing.T) {
	s, c := makeServer(t)
	vps := pairNode(t, s, "vps", "vps")
	pc := pairNode(t, s, "pc", "windows")
	now := c.now()

	// The PC has no Claude login (error); the VPS has a working reading.
	beat := func(token string, quotas []Quota) {
		t.Helper()
		if w := request(t, s, "POST", "/v1/runtime/heartbeat", token, HeartbeatRequest{Quotas: quotas}); w.Code != 200 {
			t.Fatalf("heartbeat %d: %s", w.Code, w.Body.String())
		}
	}
	beat(pc.Token, []Quota{{Engine: "claude", CheckedAt: now, Error: "Not logged in"}})
	beat(vps.Token, []Quota{
		{Engine: "codex", Plan: "plus", Limited: true, CheckedAt: now.Add(-time.Minute),
			FiveHour: &QuotaWindow{UsedPercent: 0}, Weekly: &QuotaWindow{UsedPercent: 100, ResetsAt: 1794431948}},
		{Engine: "claude", CheckedAt: now.Add(-time.Hour), FiveHour: &QuotaWindow{UsedPercent: 1}, Weekly: &QuotaWindow{UsedPercent: 81}},
	})

	got := decode[[]QuotaStatus](t, request(t, s, "GET", "/v1/quotas", adminTestToken, nil))
	if len(got) != 2 || got[0].Engine != "claude" || got[1].Engine != "codex" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Node != "vps" || got[0].Error != "" || got[0].Weekly.UsedPercent != 81 {
		t.Fatalf("claude should come from the VPS reading: %+v", got[0])
	}
	if !got[1].Limited || got[1].Plan != "plus" {
		t.Fatalf("codex: %+v", got[1])
	}

	// Offline nodes drop out.
	c.advance(OnlineTTL + time.Second)
	if got := decode[[]QuotaStatus](t, request(t, s, "GET", "/v1/quotas", adminTestToken, nil)); len(got) != 0 {
		t.Fatalf("offline nodes still listed: %+v", got)
	}
	if w := request(t, s, "GET", "/v1/quotas", "", nil); w.Code != 401 {
		t.Fatalf("quotas without token: %d", w.Code)
	}
}

func TestHeartbeatRejectsBadQuotas(t *testing.T) {
	s, _ := makeServer(t)
	n := pairNode(t, s, "vps", "vps")
	w := request(t, s, "POST", "/v1/runtime/heartbeat", n.Token, HeartbeatRequest{Quotas: []Quota{{Engine: "gemini"}}})
	if w.Code != 400 {
		t.Fatalf("unknown engine accepted: %d", w.Code)
	}
}
