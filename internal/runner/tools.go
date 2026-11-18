package runner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Node tools: the programs of control.NodeToolCatalog installed here. The
// node rebuilds every command from the catalog (the server only names the
// tool, the action and the values), checks the files stay inside the run's
// folder and runs the program without a shell; on Linux inside the same
// bubblewrap sandbox as executar.

// toolScan is how often the node looks for newly installed tools.
const toolScan = 10 * time.Minute

type toolCache struct {
	mu    sync.Mutex
	at    time.Time
	found []control.ToolInfo
	bins  map[string]string
}

var tools toolCache

// whisperModel is the ggml model whisper.cpp uses (AVORA_WHISPER_MODEL).
func whisperModel() string {
	p := os.Getenv("AVORA_WHISPER_MODEL")
	if p == "" || !filepath.IsAbs(p) {
		return ""
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return ""
	}
	return p
}

func toolBinOf(t control.NodeTool) string {
	names := append([]string{}, t.Bins...)
	if runtime.GOOS != "windows" {
		names = append(names, t.UnixBins...)
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			if abs, err := filepath.Abs(p); err == nil {
				return abs
			}
		}
	}
	return ""
}

func toolVersion(ctx context.Context, bin string, args []string) string {
	if len(args) == 0 {
		return "instalado"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	hideWindow(cmd)
	out, _ := cmd.CombinedOutput()
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	line, _, _ = strings.Cut(line, " Copyright")
	line = strings.TrimSpace(strings.ToValidUTF8(line, ""))
	if line == "" {
		return "instalado"
	}
	return clip(line, 60)
}

// InstalledTools lists the catalog tools found on this node, rescanning
// every few minutes.
func InstalledTools(ctx context.Context) []control.ToolInfo {
	tools.mu.Lock()
	defer tools.mu.Unlock()
	if !tools.at.IsZero() && time.Since(tools.at) < toolScan {
		return tools.found
	}
	found, bins := []control.ToolInfo{}, map[string]string{}
	for _, t := range control.NodeToolCatalog {
		bin := toolBinOf(t)
		if bin == "" || (t.ID == "whisper" && whisperModel() == "") {
			continue
		}
		bins[t.ID] = bin
		found = append(found, control.ToolInfo{ID: t.ID, Version: toolVersion(ctx, bin, t.VersionArgs)})
	}
	tools.at, tools.found, tools.bins = time.Now(), found, bins
	return found
}

func toolBin(id string) string {
	tools.mu.Lock()
	bin := tools.bins[id]
	tools.mu.Unlock()
	if bin == "" {
		for _, t := range control.NodeToolCatalog {
			if t.ID == id {
				bin = toolBinOf(t)
			}
		}
	}
	return bin
}

// inside resolves a relative file of a call within dir, following links.
func inside(dir, rel string, mustExist bool) (string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	check := p
	if !mustExist {
		check = filepath.Dir(p)
	}
	if real, err := filepath.EvalSymlinks(check); err == nil {
		check = real
	} else if mustExist {
		return "", fmt.Errorf("o arquivo %s não existe na pasta", rel)
	}
	in, err := filepath.Rel(root, check)
	if err != nil || in == ".." || strings.HasPrefix(in, ".."+string(filepath.Separator)) || filepath.IsAbs(in) {
		return "", fmt.Errorf("%s sai da pasta", rel)
	}
	return p, nil
}

type fileStamp struct {
	size int64
	mod  time.Time
}

// snapshot lists the files under dir (up to a bound), to find what a run
// created or changed.
func snapshot(dir string) map[string]fileStamp {
	out := map[string]fileStamp{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || len(out) > 5000 {
			return filepath.SkipDir
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".avora", ".avora-home", "node_modules", ".venv", "__pycache__":
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			rel, _ := filepath.Rel(dir, p)
			out[filepath.ToSlash(rel)] = fileStamp{info.Size(), info.ModTime()}
		}
		return nil
	})
	return out
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

func changedFiles(before, after map[string]fileStamp) string {
	names := []string{}
	for name, st := range after {
		if old, ok := before[name]; !ok || old != st {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "\nNenhum arquivo novo."
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("\nArquivos novos ou alterados:")
	for i, n := range names {
		if i == 30 {
			fmt.Fprintf(&b, "\n- … e mais %d", len(names)-30)
			break
		}
		fmt.Fprintf(&b, "\n- %s (%s)", n, humanSize(after[n].size))
	}
	return b.String()
}

// shellQuote quotes one word for /bin/sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RunTool runs one node tool call (a job with engine "tool").
func RunTool(ctx context.Context, cfg AgentConfig, spec store.AgentSpec) control.JobResult {
	fail := func(msg string) control.JobResult { return control.JobResult{Error: clip(msg, 2000)} }
	call, err := control.DecodeToolCall(spec.Prompt)
	if err != nil {
		return fail(err.Error())
	}
	argv, ins, outs, err := call.Argv()
	if err != nil {
		return fail(err.Error())
	}
	nt, _, _ := control.FindTool(call.Tool, call.Action)
	bin := toolBin(nt.ID)
	if bin == "" {
		return fail(nt.Label + " não está instalado neste nó")
	}
	for i, a := range argv {
		if a == "{$model}" {
			m := whisperModel()
			if m == "" {
				return fail("defina AVORA_WHISPER_MODEL com o caminho de um modelo ggml do whisper.cpp")
			}
			argv[i] = m
		}
	}
	dir, err := cfg.dirFor(spec)
	if err != nil {
		return fail(err.Error())
	}
	for _, f := range ins {
		p, err := inside(dir, f, true)
		if err != nil {
			return fail(err.Error())
		}
		if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
			return fail(f + " não é um arquivo")
		}
	}
	for _, f := range outs {
		p, err := inside(dir, f, false)
		if err != nil {
			return fail(err.Error())
		}
		if _, err := os.Lstat(p); err == nil {
			return fail(f + " já existe; escolha outro nome para a saída")
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return fail("não consegui criar a pasta de " + f)
		}
	}
	iso, err := cfg.prepareAgentIsolation(ctx, spec)
	if err != nil {
		return fail(err.Error())
	}
	before := snapshot(dir)
	minutes := max(min(spec.MaxMinutes, control.MaxAgentMinutes), 1)
	var res control.JobResult
	if runtime.GOOS == "linux" {
		if !strings.HasPrefix(bin, "/usr/") {
			return fail(nt.Label + " precisa estar em /usr (o sandbox só enxerga /usr): " + bin)
		}
		words := []string{shellQuote(bin)}
		for _, a := range argv {
			words = append(words, shellQuote(a))
		}
		res = RunShell(ctx, cfg, store.AgentSpec{Engine: "shell", Prompt: strings.Join(words, " "), Mode: spec.Mode,
			Workdir: spec.Workdir, HomeDir: spec.HomeDir, Owner: spec.Owner, AgentName: spec.AgentName, AgentEmail: spec.AgentEmail,
			MaxMinutes: minutes})
	} else {
		res = runProgram(ctx, bin, argv, dir, iso, time.Duration(minutes)*time.Minute)
	}
	if res.Success {
		res.Output = clip(res.Output, shellOutput) + changedFiles(before, snapshot(dir))
		if strings.HasPrefix(spec.Workdir, "agents/") || spec.Folder == "" {
			res.Output += "\nPasta: " + dir
		}
	}
	return res
}

// runProgram runs a program directly (no shell) with a time limit.
func runProgram(ctx context.Context, bin string, argv []string, dir string, iso AgentIsolation, limit time.Duration) control.JobResult {
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin, argv...)
	cmd.Dir = dir
	baseEnv := cleanEnv([]string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "OPENAI_API_KEY", "CODEX_API_KEY", "AVORA_BRIDGE_KEY"},
		"PYTHONUTF8=1", "PYTHONIOENCODING=utf-8")
	cmd.Env = iso.ApplyEnv(baseEnv, false)
	out := &limitedBuffer{max: shellOutput}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 5 * time.Second
	hideWindow(cmd)
	started := time.Now()
	err := cmd.Run()
	took := time.Since(started).Round(100 * time.Millisecond)
	text := strings.TrimRight(strings.ToValidUTF8(out.buf.String(), "�"), "\n")
	if out.cut {
		text += fmt.Sprintf("\n… (saída cortada em %d KB)", shellOutput>>10)
	}
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return control.JobResult{Output: text, Error: fmt.Sprintf("passou do limite de %s e foi encerrado", limit)}
	case ctx.Err() != nil:
		return control.JobResult{Output: text, Error: "parado"}
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		return control.JobResult{Output: text, Error: "não consegui iniciar: " + err.Error()}
	}
	return control.JobResult{Success: true, Output: fmt.Sprintf("código de saída %d, %s\n%s", code, took, text)}
}
