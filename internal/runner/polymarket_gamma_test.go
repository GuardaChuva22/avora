package runner

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/GuardaChuva22/avora/internal/mcp"
)

func TestPolymarketGammaCompat(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.MkdirAll("repo/.git", 0o700); err != nil {
		t.Fatal(err)
	}
	job := mcp.PluginJob{Runtime: "node", Entry: "build/index.js", Repository: polymarketMCPRepository}
	if err := root.WriteFile("repo/.git/HEAD", []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if polymarketMCPCheckout(root, job) {
		t.Fatal("compatibility loaded for an unknown revision")
	}
	if err := root.WriteFile("repo/.git/HEAD", []byte(polymarketMCPRevision+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !polymarketMCPCheckout(root, job) {
		t.Fatal("compatibility did not load for an installed checkout")
	}
	if node, err := exec.LookPath("node"); err == nil {
		checkPolymarketGammaFixture(t, root, dir, node)
	}
}

func checkPolymarketGammaFixture(t *testing.T, root *os.Root, dir, node string) {
	t.Helper()
	if err := root.MkdirAll("repo/build/lib", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("repo/package.json", []byte(`{"type":"module"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const fixture = `
const market = {outcomes: '["Yes","No"]', startDate: '2026-01-01', conditionId: '0x01', enableOrderBook: false};
export const polymarketClient = {
  async getMarkets(params) { return [{...market, id: params.id}, {outcomes: ['Up','Down']}, {outcomes: 'invalid'}]; },
  async getEvents() { return [{endDate: '2026-12-01', markets: [market]}, {markets: null}]; },
};
`
	if err := root.WriteFile("repo/build/lib/polymarketClient.js", []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	const assertions = `
import assert from 'node:assert/strict';
import { polymarketClient } from './build/lib/polymarketClient.js';
const markets = await polymarketClient.getMarkets({id: '42'});
assert.equal(markets[0].id, '42');
assert.deepEqual(markets[0].outcomes, ['Yes','No']);
assert.equal(markets[0].start_date, '2026-01-01');
assert.equal(markets[0].condition_id, '0x01');
assert.equal(markets[0].enable_order_book, false);
assert.deepEqual(markets[1].outcomes, ['Up','Down']);
assert.deepEqual(markets[2].outcomes, []);
const events = await polymarketClient.getEvents();
assert.deepEqual(events[0].markets[0].outcomes, ['Yes','No']);
assert.equal(events[0].end_date, '2026-12-01');
assert.equal(events[1].markets, undefined);
`
	preload := "data:text/javascript;base64," + base64.StdEncoding.EncodeToString(polymarketGammaCompat)
	cmd := exec.CommandContext(t.Context(), node, "--import", preload, "--input-type=module", "-e", assertions)
	cmd.Dir = filepath.Join(dir, "repo")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Polymarket fixture: %v\n%s", err, output)
	}
}
