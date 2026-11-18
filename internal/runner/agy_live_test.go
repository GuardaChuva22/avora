package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Manual: AVORA_AGY_LIVE=1 runs a real turn through a pool account made
// from the PC's own agy login, and checks the login slot comes back as it was.
func TestAgyLivePool(t *testing.T) {
	if os.Getenv("AVORA_AGY_LIVE") == "" {
		t.Skip("set AVORA_AGY_LIVE=1")
	}
	before, err := readAgySlot()
	if err != nil || before == nil {
		t.Fatalf("no agy login on this PC: %v", err)
	}
	base := t.TempDir()
	cfg := DefaultAgentConfig()
	cfg.AgyHome = filepath.Join(base, "agy")
	cfg.Workspace = filepath.Join(base, "work")
	cfg.ServerURL = "http://127.0.0.1:9"
	id := "antigravity.abc123"
	dir := accountDir(cfg.AgyHome, id)
	if err := writePrivate(agyTokenFile(dir), before); err != nil {
		t.Fatal(err)
	}
	for _, bridge := range []string{"", "fake-key"} {
		r := RunAntigravity(t.Context(), cfg, store.AgentSpec{Engine: "antigravity", Account: id, Mode: "read", MaxMinutes: 3,
			Prompt: "Responda apenas: OK", Bridge: bridge}, func(st control.AgentStep) { t.Logf("step %s: %s", st.Tool, st.Summary) })
		t.Logf("bridge=%q success=%v out=%q err=%q", bridge, r.Success, r.Output, r.Error)
		if !r.Success {
			t.Errorf("run failed")
		}
		after, _ := readAgySlot()
		if agyEmail(after) != agyEmail(before) {
			t.Fatalf("slot changed account")
		}
		if !bytes.Equal(after, before) {
			t.Logf("slot bytes differ (token refreshed by agy), same account")
		}
	}
	q := readAgyQuota(t.Context(), cfg, id)
	t.Logf("quota: %+v 5h=%+v week=%+v", q, q.FiveHour, q.Weekly)
	if q.Error != "" {
		t.Errorf("quota error")
	}
}
