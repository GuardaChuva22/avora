//go:build windows

package runner

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *lockedBuf) String() string               { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// Manual: AVORA_AGY_LIVE=1, with agy signed out (the slot empty), checks a
// code typed into agy's hidden console reaches its sign-in prompt.
func TestAgyLiveTypesCode(t *testing.T) {
	if os.Getenv("AVORA_AGY_LIVE") == "" {
		t.Skip("set AVORA_AGY_LIVE=1")
	}
	if b, _ := readAgySlot(); b != nil {
		t.Skip("agy is signed in: no prompt to type into")
	}
	cmd := exec.Command(DefaultAgentConfig().AgyBin, "--output-format", "json")
	cmd.Stdin = strings.NewReader("Responda apenas: OK")
	cmd.Dir = t.TempDir()
	agyLoginConsole(cmd)
	var out lockedBuf
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	for i := 0; i < 60 && !strings.Contains(out.String(), "paste the authorization code"); i++ {
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("before typing: %s", loginURL.ReplaceAllString(out.String(), "<url> "))
	if err := agyTypeLine(cmd.Process.Pid, "4/0FAKECODEabc123"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(25 * time.Second):
		t.Fatalf("agy ignored the typed code:\n%s", out.String())
	}
	t.Logf("agy said:\n%s", strings.Join(strings.Fields(loginURL.ReplaceAllString(out.String(), "<url> ")), " "))
}
