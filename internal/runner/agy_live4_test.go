//go:build windows

package runner

import (
	"os"
	"path/filepath"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Manual: AVORA_AGY_LIVE=1 checks agy, started with agyEnv, signs in
// without opening the browser: the do-nothing rundll32 is the one it runs.
func TestAgyLiveNoBrowser(t *testing.T) {
	if os.Getenv("AVORA_AGY_LIVE") == "" {
		t.Skip("set AVORA_AGY_LIVE=1")
	}
	dir := agyNoBrowserDir()
	mark := filepath.Join(dir, "called.txt")
	_ = os.Remove(mark)
	if err := os.WriteFile(filepath.Join(dir, "rundll32.cmd"), []byte("@echo x>\"%~dp0called.txt\"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.WriteFile(filepath.Join(dir, "rundll32.cmd"), []byte("@exit /b 0\r\n"), 0o600)
	cfg := DefaultAgentConfig()
	h, err := takeAgySlot(t.Context(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.release("", "")
	cmd := exec.Command(cfg.AgyBin, "--output-format", "json")
	cmd.Stdin = strings.NewReader("Responda apenas: OK")
	cmd.Dir, cmd.Env = os.TempDir(), agyEnv("")
	agyLoginConsole(cmd)
	var out lockedBuf
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40 && !strings.Contains(out.String(), "paste the authorization code"); i++ {
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if _, err := os.Stat(mark); err != nil {
		t.Fatalf("agy did not run the do-nothing rundll32: %s", out.String())
	}
}
