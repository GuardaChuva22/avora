package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/control"
)

func TestCodexArgsSendImagesBeforeResume(t *testing.T) {
	args, err := codexArgs("read", "", "/w", true, "abc", "image=/i/1.png")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--image /i/1.png resume abc") {
		t.Fatalf("args: %s", joined)
	}
}

// Codex draws an image: the app hears it start and end, and the answer
// gets the file Codex saved for the thread.
func TestCodexGeneratedImages(t *testing.T) {
	stream := `{"type":"thread.started","thread_id":"th-1"}
{"type":"item.started","item":{"id":"ig","type":"image_generation"}}
{"type":"item.completed","item":{"id":"ig","type":"image_generation"}}
{"type":"item.completed","item":{"id":"m","type":"agent_message","text":"Aqui está."}}
{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`
	steps := []string{}
	answer, isErr, stats, err := parseCodexStream(strings.NewReader(stream), func(st control.AgentStep) {
		steps = append(steps, st.Tool+":"+st.Summary)
	})
	if err != nil || isErr || answer != "Aqui está." || stats.SessionID != "th-1" {
		t.Fatalf("%q %v %v %+v", answer, isErr, err, stats)
	}
	if strings.Join(steps[:2], " ") != control.StepImage+":gerando "+control.StepImage+":pronta" {
		t.Fatalf("steps: %v", steps)
	}

	home := t.TempDir()
	dir := filepath.Join(home, "generated_images", "th-1")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "exec-1.png"), []byte("\x89PNG\r\n\x1a\nfake"), 0o600)
	sent := ""
	md := generatedImages(home, []byte(`{"session_id":"th-1"}`), func(name string, data []byte) (string, error) {
		sent = name
		return "abc123", nil
	})
	if sent != "exec-1.png" || md != "\n\n![imagem gerada](avora:file/abc123)" {
		t.Fatalf("sent %q md %q", sent, md)
	}
	// A thread id never leaves the folder.
	if md := generatedImages(home, []byte(`{"session_id":".."}`), func(string, []byte) (string, error) { return "x", nil }); md != "" {
		t.Fatalf("escaped: %q", md)
	}
}
