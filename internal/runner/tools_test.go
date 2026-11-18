package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestToolFilesStayInTheFolder(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "agents", "cora")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "a.mp4"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(ws, "fora.mp4"), []byte("x"), 0o600)
	if _, err := inside(dir, "a.mp4", true); err != nil {
		t.Fatal(err)
	}
	if _, err := inside(dir, "nada.mp4", true); err == nil {
		t.Fatal("missing input accepted")
	}
	if _, err := inside(dir, "sub/novo.mp4", false); err != nil {
		t.Fatal(err)
	}
	// A link out of the folder is refused even with a clean name.
	if err := os.Symlink(ws, filepath.Join(dir, "link")); err == nil {
		if _, err := inside(dir, "link/fora.mp4", true); err == nil {
			t.Fatal("followed a link out of the folder")
		}
	}

	if _, err := os.Stat(filepath.Join(ws, "agents", "cora")); err != nil {
		t.Fatal(err)
	}
	cfg := AgentConfig{Workspace: ws}
	run := func(c control.ToolCall) control.JobResult {
		return RunTool(t.Context(), cfg, store.AgentSpec{Engine: "tool", Prompt: control.EncodeToolCall(c), Workdir: "agents/cora", MaxMinutes: 1})
	}
	tools.mu.Lock()
	tools.bins = map[string]string{"ffmpeg": "ffmpeg-fake"}
	tools.mu.Unlock()
	if r := run(control.ToolCall{Tool: "ffmpeg", Action: "converter", Args: map[string]any{"entrada": "a.mp4", "saida": "a.mp4"}}); !strings.Contains(r.Error, "já existe") {
		t.Fatalf("overwrite: %+v", r)
	}
	if r := run(control.ToolCall{Tool: "ffmpeg", Action: "converter", Args: map[string]any{"entrada": "b.mp4", "saida": "c.mp4"}}); !strings.Contains(r.Error, "não existe") {
		t.Fatalf("missing input: %+v", r)
	}
	if r := run(control.ToolCall{Tool: "ffmpeg", Action: "converter", Args: map[string]any{"entrada": "../fora.mp4", "saida": "c.mp4"}}); r.Success {
		t.Fatalf("escaped: %+v", r)
	}
	tools.mu.Lock()
	tools.bins = nil
	tools.mu.Unlock()

	// A real run where ffprobe exists (not in the Linux sandbox here).
	if runtime.GOOS == "linux" || toolBin("ffprobe") == "" {
		t.Skip("no ffprobe here")
	}
	_ = os.WriteFile(filepath.Join(dir, "vazio.txt"), []byte("não é mídia"), 0o600)
	r := run(control.ToolCall{Tool: "ffprobe", Action: "info", Args: map[string]any{"entrada": "vazio.txt"}})
	if !r.Success || !strings.Contains(r.Output, "código de saída") || !strings.Contains(r.Output, "Nenhum arquivo novo") {
		t.Fatalf("ffprobe: %+v", r)
	}
}
