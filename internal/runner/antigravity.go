package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Antigravity: Google's agent through its official CLI (agy) in headless
// mode. agy keeps its login in the OS keyring, so a node has one Antigravity
// account: the one the owner signed in with by running agy once. The prompt
// goes in as one stream-json user event and stdin closes, which ends the
// session after the turn (and avoids the CLI waiting on a terminal).

func findAgy() string {
	if p, err := exec.LookPath("agy"); err == nil && filepath.Ext(p) != ".ps1" && filepath.Ext(p) != ".cmd" {
		return p
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		for _, p := range []string{
			filepath.Join(home, ".local", "bin", "agy.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "agy", "agy.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "agy", "bin", "agy.exe"),
		} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return filepath.Join(home, ".local", "bin", "agy.exe")
	}
	return filepath.Join(home, ".local", "bin", "agy")
}

// agyConfigDir is where agy keeps its settings (~/.gemini/antigravity-cli).
func agyConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

// agyModes: read keeps agy's default, where shell commands and edits need an
// approval nobody gives in headless mode (soft-denied); edit accepts edits
// in the workdir. Shell stays off either way, like the other engines.
var agyModes = map[string][]string{"read": nil, "edit": {"--mode", "accept-edits"}}

func agyArgs(mode, model, effort string) ([]string, error) {
	extra, ok := agyModes[mode]
	if !ok {
		return nil, fmt.Errorf("unsupported mode %q", mode)
	}
	args := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--disable-slash-commands"}
	args = append(args, extra...)
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	return args, nil
}

// agyInput is the prompt as the one user event of the session.
func agyInput(prompt string) string {
	b, _ := json.Marshal(map[string]any{"event": "user", "message": map[string]any{"content": prompt}})
	return string(b) + "\n"
}

// agyEnv drops API keys so a run uses the owner's account, never a billed
// key. home, when set, is the HOME agy keeps its files in for this run.
func agyEnv(home string) []string {
	drop := []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI", "AGY_ADC_AUTH", bridgeKeyEnv}
	extra := []string{}
	// agy opens the browser on every sign-in it starts; the owner signs in
	// from the link Avora shows instead, so it finds a rundll32 that does
	// nothing first on its PATH.
	if d := agyNoBrowserDir(); d != "" {
		drop = append(drop, "PATH")
		extra = append(extra, "PATH="+d+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	if home != "" {
		drop = append(drop, "HOME", "USERPROFILE")
		extra = append(extra, "HOME="+home, "USERPROFILE="+home)
	}
	return cleanEnv(drop, extra...)
}

type agyUsage struct {
	Input    int `json:"input_tokens"`
	Output   int `json:"output_tokens"`
	Thinking int `json:"thinking_tokens"`
	Cache    int `json:"cache_read_tokens"`
}

type agyResult struct {
	ConversationID string    `json:"conversation_id"`
	Status         string    `json:"status"`
	Response       string    `json:"response"`
	Error          any       `json:"error"`
	Turns          int       `json:"num_turns"`
	Duration       float64   `json:"duration_seconds"`
	Usage          *agyUsage `json:"usage"`
}

func (r agyResult) errorText() string {
	switch e := r.Error.(type) {
	case string:
		return e
	case map[string]any:
		if m, ok := e["message"].(string); ok {
			return m
		}
	case nil:
		return ""
	}
	b, _ := json.Marshal(r.Error)
	return string(b)
}

type agyEvent struct {
	Event string `json:"event"`
	Init  *struct {
		Model string `json:"model"`
	} `json:"init"`
	Step *struct {
		ConversationID string `json:"conversation_id"`
		Index          int    `json:"step_index"`
		State          string `json:"state"`
		Type           string `json:"step_type"`
		TextDelta      string `json:"text_delta"`
		ToolName       string `json:"tool_name"`
		ToolInfo       *struct {
			Name       string          `json:"name"`
			Parameters json.RawMessage `json:"parameters"`
			Error      *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"tool_info"`
	} `json:"step_update"`
	Result *agyResult `json:"result"`
}

// toolSummary is a tool step in a few words: its name and main argument.
func agyToolSummary(name string, params json.RawMessage) string {
	var p map[string]any
	_ = json.Unmarshal(params, &p)
	for _, k := range []string{"command", "CommandLine", "path", "file_path", "AbsolutePath", "TargetFile", "query", "Query", "url", "Url", "pattern"} {
		if v, ok := p[k].(string); ok && v != "" {
			return name + ": " + v
		}
	}
	return name
}

// parseAgyStream reads agy's stream-json until the result event.
func parseAgyStream(r io.Reader, onStep func(control.AgentStep)) (answer string, isError bool, stats AgentStats, err error) {
	stats.Engine = "antigravity"
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	texts := map[int]*strings.Builder{}
	toolsDone := map[int]bool{}
	finished := false
	for scanner.Scan() {
		var ev agyEvent
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Event {
		case "init":
			if ev.Init != nil && ev.Init.Model != "" {
				stats.Model = ev.Init.Model
			}
		case "step_update":
			st := ev.Step
			if st == nil {
				continue
			}
			if st.ConversationID != "" {
				stats.SessionID = st.ConversationID
			}
			switch st.Type {
			case "agent_response":
				b := texts[st.Index]
				if b == nil {
					b = &strings.Builder{}
					texts[st.Index] = b
				}
				b.WriteString(st.TextDelta)
				if st.State == "DONE" && onStep != nil {
					if t := strings.TrimSpace(b.String()); t != "" {
						onStep(control.AgentStep{Tool: control.StepText, Summary: clip(t, control.MaxStepText)})
					}
				}
			case "tool":
				if st.State != "DONE" || toolsDone[st.Index] {
					continue
				}
				toolsDone[st.Index] = true
				stats.Steps++
				name, params := st.ToolName, json.RawMessage(nil)
				summary := ""
				if st.ToolInfo != nil {
					name, params = orStr(st.ToolInfo.Name, name), st.ToolInfo.Parameters
					if st.ToolInfo.Error != nil && st.ToolInfo.Error.Message != "" {
						summary = " (erro: " + st.ToolInfo.Error.Message + ")"
					}
				}
				if onStep != nil {
					onStep(control.AgentStep{Tool: orStr(name, "ferramenta"), Summary: clip(agyToolSummary(name, params)+summary, 300)})
				}
			}
		case "result":
			res := ev.Result
			if res == nil {
				continue
			}
			finished = true
			answer = res.Response
			if res.ConversationID != "" {
				stats.SessionID = res.ConversationID
			}
			stats.Turns = max(res.Turns, 1)
			if res.Duration > 0 {
				stats.DurationMS = int(res.Duration * 1000)
			}
			if u := res.Usage; u != nil {
				stats.InputTokens, stats.OutputTokens, stats.CacheRead = u.Input, u.Output+u.Thinking, u.Cache
			}
			if res.Status != "SUCCESS" {
				isError = true
				answer = orStr(res.errorText(), "Antigravity terminou com status "+res.Status)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return answer, true, stats, err
	}
	if !finished {
		// No result event: keep what was said, if anything.
		parts := []string{}
		for i := 0; i < len(texts)+len(toolsDone)+1; i++ {
			if b := texts[i]; b != nil {
				parts = append(parts, b.String())
			}
		}
		answer = strings.TrimSpace(strings.Join(parts, "\n"))
		return answer, true, stats, errors.New("o Antigravity terminou sem resposta (veja se o agy está logado: rode agy uma vez no PC)")
	}
	return answer, isError, stats, nil
}

func orStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// RunAntigravity runs one agent.run job with the official Antigravity CLI.
func RunAntigravity(ctx context.Context, cfg AgentConfig, spec store.AgentSpec, onStep func(control.AgentStep)) control.JobResult {
	args, err := agyArgs(spec.Mode, spec.Model, spec.Effort)
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	p := agentProcess{
		name: "Antigravity", bin: cfg.AgyBin, args: args, configDir: agyConfigDir(),
		stdin: agyInput(spec.Prompt), parse: parseAgyStream,
	}
	home := ""
	if spec.Bridge != "" {
		h, cleanup, err := agyRunHome(cfg, spec.Bridge)
		if err != nil {
			return control.JobResult{Error: "não consegui preparar o Antigravity: " + err.Error()}
		}
		defer cleanup()
		home = h
	}
	dir, token, email, pool, err := agyPoolAccount(cfg, spec.Account)
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	if pool {
		if home == "" {
			home = agyAccountHome(dir)
		}
		p.gate = agyGate(cfg, dir, token, email)
	} else if agyKeyringSupported {
		// The PC's own login: wait out a sign-in holding the slot empty.
		p.gate = agyWaitSlot
	}
	// A conversation lives in the HOME that started it.
	if spec.Resume != "" && spec.Bridge == "" {
		p.args = append(p.args, "--conversation", spec.Resume)
	}
	p.env = agyEnv(home)
	if home != "" {
		p.configDir = filepath.Join(home, ".gemini", "antigravity-cli")
	}
	return p.run(ctx, cfg, spec, onStep)
}

// checkAgy is the Antigravity "login": agy signs in by itself (browser and
// OS keyring) the first time it runs on the PC, so the node only proves it
// answers with the owner's account.
func checkAgy(ctx context.Context, cfg AgentConfig, onStep func(control.AgentStep)) control.JobResult {
	if _, err := os.Stat(cfg.AgyBin); err != nil {
		return control.JobResult{Error: "o Antigravity CLI (agy) não está instalado neste computador: instale pelo site antigravity.google (aba CLI) e rode agy uma vez num terminal para entrar com a sua conta Google"}
	}
	args, _ := agyArgs("read", "", "")
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, cfg.AgyBin, args...)
	cmd.Env, cmd.Stdin, cmd.WaitDelay = agyEnv(""), strings.NewReader(agyInput("Responda apenas: OK")), 5*time.Second
	if dir, err := os.MkdirTemp("", "avora-agy-"); err == nil {
		defer os.RemoveAll(dir)
		cmd.Dir = dir
	}
	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	hideWindow(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	if err := cmd.Start(); err != nil {
		return control.JobResult{Error: "não consegui abrir o agy: " + err.Error()}
	}
	answer, isError, stats, perr := parseAgyStream(out, nil)
	_, _ = io.Copy(io.Discard, out)
	_ = cmd.Wait()
	tail := strings.ToLower(stderr.String() + " " + answer)
	switch {
	case strings.Contains(tail, "authentication required") || strings.Contains(tail, "not authenticated") || strings.Contains(tail, "sign in"):
		return control.JobResult{Error: "o agy ainda não entrou na sua conta: abra um terminal no PC, rode agy, entre com a conta Google e depois tente de novo aqui"}
	case perr != nil || isError:
		msg := answer
		if perr != nil {
			msg = perr.Error()
		}
		if s := strings.TrimSpace(stderr.String()); s != "" {
			msg += ": " + s
		}
		return control.JobResult{Error: clip("o Antigravity não respondeu: "+msg, 300)}
	}
	who := map[string]string{"account": "conta do PC"}
	if stats.Model != "" {
		who["account"] = "conta do PC · " + stats.Model
	}
	b, _ := json.Marshal(who)
	return control.JobResult{Success: true, Output: string(b)}
}

// agyQuotaEvery is how often the node reads agy's limits (/usage costs no
// model call).
const agyQuotaEvery = 5 * time.Minute

type agyUsageGroup struct {
	Name    string `json:"name"`
	Buckets []struct {
		ID        string  `json:"id"`
		Window    string  `json:"window"`
		Remaining float64 `json:"remaining_fraction"`
		Reset     string  `json:"reset_time"`
	} `json:"buckets"`
}

// agyQuota turns agy's /usage into a Quota: the Gemini group's weekly and
// 5-hour windows (the models agents use by default), or the first group's.
func agyQuota(raw []byte, now time.Time) control.Quota {
	q := control.Quota{Engine: "antigravity", CheckedAt: now}
	var env struct {
		Status  string `json:"status"`
		Error   any    `json:"error"`
		Command *struct {
			Data struct {
				Groups []agyUsageGroup `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Command == nil || len(env.Command.Data.Groups) == 0 {
		q.Error = "o agy não devolveu a cota"
		if env.Error != nil {
			q.Error = clip(fmt.Sprint(env.Error), 280)
		}
		return q
	}
	g := env.Command.Data.Groups[0]
	for _, c := range env.Command.Data.Groups {
		if strings.HasPrefix(strings.ToLower(c.Name), "gemini models") {
			g = c
			break
		}
	}
	for _, b := range g.Buckets {
		w := &control.QuotaWindow{UsedPercent: percent((1 - b.Remaining) * 100)}
		if t, err := time.Parse(time.RFC3339, b.Reset); err == nil {
			w.ResetsAt = t.Unix()
		}
		switch b.Window {
		case "weekly":
			q.Weekly = w
		case "5h":
			q.FiveHour = w
		}
		if b.Remaining <= 0 {
			q.Limited = true
		}
	}
	return q
}

// readAgyQuota asks agy for its limits: the PC's own login, or a pool
// account's (account) with its login in the slot while agy starts.
func readAgyQuota(ctx context.Context, cfg AgentConfig, account string) control.Quota {
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	dir, token, email, pool, err := agyPoolAccount(cfg, account)
	if err != nil {
		return control.Quota{Engine: "antigravity", Account: account, CheckedAt: time.Now(), Error: clip(err.Error(), 280)}
	}
	home := ""
	if pool {
		home = agyAccountHome(dir)
	}
	cmd := exec.CommandContext(cctx, cfg.AgyBin, "-p", "/usage", "--output-format", "json")
	cmd.Env, cmd.Stdin, cmd.WaitDelay = agyEnv(home), strings.NewReader(""), 5*time.Second
	if home != "" {
		cmd.Dir = home
	}
	hideWindow(cmd)
	if pool {
		h, err := takeAgySlot(cctx, cfg, token)
		if err != nil {
			return control.Quota{Engine: "antigravity", Account: account, CheckedAt: time.Now(), Error: clip(err.Error(), 280)}
		}
		// /usage answers in seconds and costs no model call: the slot stays
		// with the account until it is done.
		defer h.release(dir, email)
	} else if agyKeyringSupported {
		// A sign-in holds the slot empty: an agy started now would ask for a
		// login too. The quota waits for the next round.
		select {
		case agySlotLock <- struct{}{}:
			defer func() { <-agySlotLock }()
		default:
			return control.Quota{Engine: "antigravity", Account: account, CheckedAt: time.Now(), Error: "esperando o login de uma conta do Antigravity terminar"}
		}
	}
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return control.Quota{Engine: "antigravity", CheckedAt: time.Now(), Error: clip("o agy não respondeu: "+err.Error(), 280)}
	}
	return agyQuota(out, time.Now())
}

const agyModelsCacheTTL = time.Hour

type agyCachedModelList struct {
	models []CodexModel
	at     time.Time
}

var (
	agyModelsMu    sync.Mutex
	agyModelsCache = map[string]agyCachedModelList{}
)

// listAgyModels runs "agy models" for the given account (or the PC's own agy
// login when account is empty) and caches the result for 1 hour.
func listAgyModels(ctx context.Context, cfg AgentConfig, account string) ([]CodexModel, error) {
	key := cfg.AgyBin + "|" + account
	agyModelsMu.Lock()
	if c, ok := agyModelsCache[key]; ok && len(c.models) > 0 && time.Since(c.at) < agyModelsCacheTTL {
		out := append([]CodexModel{}, c.models...)
		agyModelsMu.Unlock()
		return out, nil
	}
	agyModelsMu.Unlock()

	if _, err := os.Stat(cfg.AgyBin); err != nil {
		return nil, errors.New("o Antigravity CLI (agy) não está instalado neste computador")
	}
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	dir, token, email, pool, err := agyPoolAccount(cfg, account)
	if err != nil {
		return nil, err
	}
	home := ""
	if pool {
		home = agyAccountHome(dir)
	}
	if pool {
		h, err := takeAgySlot(cctx, cfg, token)
		if err != nil {
			return nil, err
		}
		defer h.release(dir, email)
	} else if agyKeyringSupported {
		select {
		case agySlotLock <- struct{}{}:
			defer func() { <-agySlotLock }()
		case <-cctx.Done():
			return nil, cctx.Err()
		}
	}

	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(cctx, cfg.AgyBin, args...)
		cmd.Env, cmd.Stdin, cmd.WaitDelay = agyEnv(home), strings.NewReader(""), 5*time.Second
		if home != "" {
			cmd.Dir = home
		}
		hideWindow(cmd)
		return cmd.Output()
	}

	out, err := run("models", "--output-format", "json")
	models := parseAgyModelsOutput(out)
	if len(models) == 0 {
		outPlain, errPlain := run("models")
		if parsed := parseAgyModelsOutput(outPlain); len(parsed) > 0 {
			models = parsed
			err = nil
		} else if err == nil {
			err = errPlain
		}
	}
	if len(models) == 0 {
		if err != nil {
			return nil, fmt.Errorf("agy models falhou: %w", err)
		}
		return nil, errors.New("o agy não devolveu modelos para esta conta")
	}

	agyModelsMu.Lock()
	agyModelsCache[key] = agyCachedModelList{models: append([]CodexModel{}, models...), at: time.Now()}
	agyModelsMu.Unlock()
	return models, nil
}

// parseAgyModelsOutput extracts model IDs and display names from either JSON
// or tabular plain-text output of "agy models".
func parseAgyModelsOutput(raw []byte) []CodexModel {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []CodexModel
	add := func(id, name string) {
		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		if name == "" {
			name = id
		}
		out = append(out, CodexModel{ID: id, Name: name})
	}
	parseItems := func(items []json.RawMessage) {
		for _, item := range items {
			var s string
			if json.Unmarshal(item, &s) == nil && s != "" {
				add(s, s)
				continue
			}
			var obj struct {
				ID          string `json:"id"`
				Model       string `json:"model"`
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				Display     string `json:"display_name"`
				Label       string `json:"label"`
			}
			if json.Unmarshal(item, &obj) == nil {
				id := orStr(obj.ID, obj.Model)
				if id == "" {
					id = obj.Name
				}
				label := orStr(obj.DisplayName, orStr(obj.Display, orStr(obj.Label, obj.Name)))
				add(id, label)
			}
		}
	}

	for _, candidate := range []string{text} {
		var arr []json.RawMessage
		if json.Unmarshal([]byte(candidate), &arr) == nil {
			parseItems(arr)
			if len(out) > 0 {
				return out
			}
		}
		var env struct {
			Models  []json.RawMessage `json:"models"`
			Data    []json.RawMessage `json:"data"`
			Command *struct {
				Data struct {
					Models []json.RawMessage `json:"models"`
				} `json:"data"`
			} `json:"command"`
		}
		if json.Unmarshal([]byte(candidate), &env) == nil {
			parseItems(env.Models)
			parseItems(env.Data)
			if env.Command != nil {
				parseItems(env.Command.Data.Models)
			}
			if len(out) > 0 {
				return out
			}
		}
	}

	// Plain-text fallback: each line starts with a model slug, optionally
	// followed by whitespace and a display name.
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		line = strings.TrimLeft(line, "*•>- ")
		if line == "" || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "===") || strings.HasPrefix(line, "{") || strings.HasPrefix(line, "[") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id := strings.Trim(fields[0], ":,()[]")
		low := strings.ToLower(id)
		if low == "model" || low == "models" || low == "id" || low == "name" || low == "available" || !looksLikeModelSlug(id) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		rest = strings.TrimLeft(rest, "-–—:| ")
		add(id, rest)
	}
	return out
}

func looksLikeModelSlug(s string) bool {
	if len(s) < 3 || len(s) > 120 || !strings.ContainsAny(s, "-0123456789.") {
		return false
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == '/' || r == ':'
		if !ok {
			return false
		}
	}
	return true
}
