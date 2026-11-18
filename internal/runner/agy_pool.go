package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Many Antigravity accounts on one PC (D61). agy keeps its login in one
// Windows credential ("gemini:antigravity"), whatever HOME it runs with,
// and reads it once when it starts. So each pool account keeps its own copy
// of that credential in accounts/<id>/token.json, and a run takes the slot:
// Avora writes the account's login there, starts agy, waits until agy has
// read it (its first output), keeps the token agy may have refreshed, and
// puts back whatever login was there before (the owner's own agy). Starts
// go one at a time; the runs themselves go on side by side.
//
// Each account also has its own HOME (accounts/<id>/home), so agy's caches,
// conversations and onboarding never mix between accounts. A run that uses
// Avora's tools gets a fresh HOME instead, holding only that run's bridge.

const (
	agySlotTarget = "gemini:antigravity"
	agySlotUser   = "antigravity"
	// agyReadyWait bounds how long a starting agy keeps the slot when it
	// says nothing; agyReadyGrace is the margin after its first output.
	agyReadyWait  = 45 * time.Second
	agyReadyGrace = 1500 * time.Millisecond
)

// agySlotLock is held while the credential holds a pool account's login.
var agySlotLock = make(chan struct{}, 1)

func agyTokenFile(dir string) string   { return filepath.Join(dir, "token.json") }
func agyAccountHome(dir string) string { return filepath.Join(dir, "home") }

// agyEmail is the account a saved login belongs to.
func agyEmail(blob []byte) string {
	var t struct {
		Email string `json:"id_token_email"`
	}
	if json.Unmarshal(blob, &t) != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(t.Email))
}

// loadAgyToken reads a pool account's login; os.ErrNotExist means the
// account has none (it uses the PC's own agy login).
func loadAgyToken(dir string) ([]byte, string, error) {
	b, err := os.ReadFile(agyTokenFile(dir))
	if err != nil {
		return nil, "", err
	}
	email := agyEmail(b)
	if email == "" {
		return nil, "", errors.New("o login salvo dessa conta do Antigravity está estragado; entre de novo")
	}
	return b, email, nil
}

// writePrivate replaces a file only this user can read, atomically.
func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func saveAgyToken(dir string, blob []byte) error {
	if old, err := os.ReadFile(agyTokenFile(dir)); err == nil && bytes.Equal(old, blob) {
		return nil
	}
	return writePrivate(agyTokenFile(dir), blob)
}

// agyPoolDirs lists the folders of this node's pool accounts with a login.
func agyPoolDirs(cfg AgentConfig) map[string]string {
	out := map[string]string{}
	if cfg.AgyHome == "" {
		return out
	}
	entries, _ := os.ReadDir(filepath.Join(filepath.Dir(cfg.AgyHome), "accounts"))
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "antigravity.") && accountID.MatchString(e.Name()) {
			dir := accountDir(cfg.AgyHome, e.Name())
			if _, err := os.Stat(agyTokenFile(dir)); err == nil {
				out[e.Name()] = dir
			}
		}
	}
	return out
}

func agyPoolEmails(cfg AgentConfig) map[string]bool {
	out := map[string]bool{}
	for _, dir := range agyPoolDirs(cfg) {
		if _, email, err := loadAgyToken(dir); err == nil {
			out[email] = true
		}
	}
	return out
}

// agyPrincipalFile keeps the owner's own agy login while a pool account
// holds the slot, should the runtime stop before putting it back.
func agyPrincipalFile(cfg AgentConfig) string { return filepath.Join(cfg.AgyHome, "principal.json") }

type agySlotHold struct {
	cfg    AgentConfig
	before []byte
	once   sync.Once
}

// takeAgySlot waits its turn and puts token (nil: no login) where agy
// reads its login. Call release when agy has read it.
func takeAgySlot(ctx context.Context, cfg AgentConfig, token []byte) (*agySlotHold, error) {
	if !agyKeyringSupported {
		return nil, errors.New("contas extras do Antigravity só funcionam no Windows")
	}
	select {
	case agySlotLock <- struct{}{}:
	case <-ctx.Done():
		return nil, errors.New("o Antigravity não chegou a abrir: " + ctx.Err().Error())
	}
	before, err := readAgySlot()
	if err != nil {
		<-agySlotLock
		return nil, errors.New("não consegui ler o login do agy no Windows: " + err.Error())
	}
	if before != nil && !agyPoolEmails(cfg)[agyEmail(before)] {
		_ = writePrivate(agyPrincipalFile(cfg), before)
	}
	h := &agySlotHold{cfg: cfg, before: before}
	if err := writeAgySlot(token); err != nil {
		h.release("", "")
		return nil, errors.New("não consegui pôr o login da conta no Windows: " + err.Error())
	}
	return h, nil
}

// release keeps the login agy left for the account in dir (it refreshes an
// expired token when it starts), then puts the owner's own login back.
func (h *agySlotHold) release(dir, email string) {
	h.once.Do(func() {
		defer func() { <-agySlotLock }()
		if dir != "" && email != "" {
			if now, err := readAgySlot(); err == nil && now != nil && agyEmail(now) == email {
				_ = saveAgyToken(dir, now)
			}
		}
		back := h.before
		// A pool account's login left over (agy refreshed it mid-run after
		// the slot was given back): the owner's own one goes back instead.
		if back != nil && agyPoolEmails(h.cfg)[agyEmail(back)] {
			if p, err := os.ReadFile(agyPrincipalFile(h.cfg)); err == nil && agyEmail(p) != "" {
				back = p
			}
		}
		_ = writeAgySlot(back)
	})
}

// agyGate is the agentProcess gate of a pool account: the slot is its
// login from just before agy starts until agy has read it.
func agyGate(cfg AgentConfig, dir string, token []byte, email string) func(context.Context) (func(), error) {
	return func(ctx context.Context) (func(), error) {
		h, err := takeAgySlot(ctx, cfg, token)
		if err != nil {
			return nil, err
		}
		return func() { h.release(dir, email) }, nil
	}
}

// agyRunHome is a fresh HOME for one run that uses Avora's tools: its MCP
// config names the bridge with the run's key, and its settings let agy call
// those tools without asking (Avora's own gate asks the owner). Only this
// user can read it, and cleanup removes it.
func agyRunHome(cfg AgentConfig, key string) (string, func(), error) {
	base := filepath.Join(cfg.AgyHome, "runs")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", nil, err
	}
	// Homes a crash left behind.
	if old, err := os.ReadDir(base); err == nil {
		for _, e := range old {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
				_ = os.RemoveAll(filepath.Join(base, e.Name()))
			}
		}
	}
	home, err := os.MkdirTemp(base, "run-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(home) }
	mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{bridgeServer: map[string]any{
		"serverUrl": bridgeURL(cfg.ServerURL), "headers": map[string]string{"Authorization": "Bearer " + key},
	}}})
	settings, _ := json.Marshal(map[string]any{"permissions": map[string]any{"allow": []string{"mcp(" + bridgeServer + "/*)"}}})
	if err := writePrivate(filepath.Join(home, ".gemini", "config", "mcp_config.json"), mcp); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := writePrivate(filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"), settings); err != nil {
		cleanup()
		return "", nil, err
	}
	return home, cleanup, nil
}

// agyPoolAccount is the folder, login and e-mail of spec's account when it
// is a pool account; ok is false for the PC's own login.
func agyPoolAccount(cfg AgentConfig, account string) (dir string, token []byte, email string, ok bool, err error) {
	if account == "" {
		return "", nil, "", false, nil
	}
	if !accountID.MatchString(account) || !strings.HasPrefix(account, "antigravity.") {
		return "", nil, "", false, errors.New("invalid account " + account)
	}
	dir = accountDir(cfg.AgyHome, account)
	token, email, err = loadAgyToken(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, "", false, nil
	}
	if err != nil {
		return "", nil, "", false, err
	}
	return dir, token, email, true, nil
}

// loginAgy signs a pool account in: with the slot empty, agy prints a
// Google link and waits up to a minute for the sign-in (it finishes by
// itself when the browser is on this PC, or takes the code the owner
// pastes). When the minute runs out it starts over with a new link.
func loginAgy(ctx context.Context, cfg AgentConfig, spec store.AgentSpec, onStep func(control.AgentStep), input func(context.Context) string) control.JobResult {
	if _, err := os.Stat(cfg.AgyBin); err != nil {
		return control.JobResult{Error: "o Antigravity CLI (agy) não está instalado neste computador: instale pelo site antigravity.google (aba CLI)"}
	}
	if !accountID.MatchString(spec.Account) || !strings.HasPrefix(spec.Account, "antigravity.") {
		return control.JobResult{Error: "invalid account " + spec.Account}
	}
	dir := accountDir(cfg.AgyHome, spec.Account)
	home := agyAccountHome(dir)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return control.JobResult{Error: "não consegui criar a pasta da conta"}
	}
	runCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	hold, err := takeAgySlot(runCtx, cfg, nil)
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	defer hold.release("", "")
	email, last := "", ""
	for attempt := 0; attempt < 15 && email == "" && runCtx.Err() == nil; attempt++ {
		out := agyLoginOnce(runCtx, cfg, home, onStep, input)
		if blob, err := readAgySlot(); err == nil && agyEmail(blob) != "" {
			if err := saveAgyToken(dir, blob); err != nil {
				return control.JobResult{Error: "não consegui guardar o login: " + err.Error()}
			}
			email = agyEmail(blob)
			break
		}
		if out != "" {
			last = out
		}
		if strings.Contains(strings.ToLower(last), "not installed") {
			break
		}
	}
	switch {
	case email != "":
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return control.JobResult{Error: "o login expirou"}
	case ctx.Err() != nil:
		return control.JobResult{Error: "login cancelado"}
	default:
		return control.JobResult{Error: "o login não terminou: " + loginTail(last)}
	}
	// Who else already uses this Google account in the pool.
	for id, other := range agyPoolDirs(cfg) {
		if id == spec.Account {
			continue
		}
		if _, e, err := loadAgyToken(other); err == nil && e == email {
			_ = os.RemoveAll(dir)
			return control.JobResult{Error: email + " já está no Antigravity do Avora"}
		}
	}
	b, _ := json.Marshal(map[string]string{"account": email})
	return control.JobResult{Success: true, Output: string(b)}
}

// agyLoginOnce runs one sign-in attempt and returns what agy said. It ends
// as soon as the login lands in the slot.
func agyLoginOnce(ctx context.Context, cfg AgentConfig, home string, onStep func(control.AgentStep), input func(context.Context) string) string {
	actx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	// On Windows agy reads a pasted code from its console (CONIN$) only when
	// the prompt comes on stdin; the code is then typed into that console.
	// Elsewhere the prompt is an argument and the code goes on stdin.
	const prompt = "Responda apenas: OK"
	args := []string{"--output-format", "json"}
	if !agyKeyringSupported {
		args = append(args, "-p", prompt)
	}
	cmd := exec.CommandContext(actx, cfg.AgyBin, args...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = home, agyEnv(home), 5*time.Second
	agyLoginConsole(cmd)
	var stdin io.Writer = io.Discard
	if agyKeyringSupported {
		cmd.Stdin = strings.NewReader(prompt)
	} else {
		pipe, err := cmd.StdinPipe()
		if err != nil {
			return err.Error()
		}
		stdin = pipe
	}
	out, w := io.Pipe()
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Start(); err != nil {
		return "não consegui abrir o agy: " + err.Error()
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		_ = w.Close()
		close(done)
	}()
	chunks := make(chan string)
	go func() {
		defer close(chunks)
		buf := make([]byte, 4096)
		for {
			n, err := out.Read(buf)
			if n > 0 {
				chunks <- string(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	text, sent := "", false
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case c, ok := <-chunks:
			if !ok {
				<-done
				return text
			}
			if len(text) < 64<<10 {
				text += ansi.ReplaceAllString(c, "")
			}
			if u := loginURL.FindString(text); !sent && u != "" && strings.Contains(text, "paste the authorization code") {
				sent = true
				onStep(control.AgentStep{Tool: "url", Summary: strings.TrimSpace(u)})
				onStep(control.AgentStep{Tool: "paste"})
			}
		case <-tick.C:
			if blob, err := readAgySlot(); err == nil && agyEmail(blob) != "" {
				cancel() // signed in: no need for its answer
				continue
			}
			if !sent {
				continue
			}
			if in := input(actx); in != "" {
				// agy reads the code from its console on Windows, stdin elsewhere.
				if err := agyTypeLine(cmd.Process.Pid, in); err != nil {
					_, _ = io.WriteString(stdin, in+"\n")
				}
			}
		}
	}
}

// The main model's lane on a PC with agy: the pool's accounts answer turns
// side by side (each start still takes the login slot for a moment).
const (
	agyMainSlots   = 6
	agyNestedSlots = 6
)

// RunAgyMain answers one turn (chat or consult) of the main model or an
// agent's brain with agy, or lists the account's models ("models").
func RunAgyMain(ctx context.Context, cfg AgentConfig, spec store.AgentSpec, onStep func(control.AgentStep)) control.JobResult {
	if spec.Mode == store.CodexModelsMode {
		models, err := listAgyModels(ctx, cfg, spec.Account)
		if err != nil {
			return control.JobResult{Error: clip(err.Error(), 2000)}
		}
		b, _ := json.Marshal(models)
		return control.JobResult{Success: true, Output: string(b)}
	}
	cfg, err := cfg.withAccount(store.AgentSpec{Engine: "antigravity", Account: spec.Account})
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	run := spec
	run.Mode, run.ConversationID, run.Resume = "read", "", ""
	return RunAntigravity(ctx, cfg, run, func(st control.AgentStep) {
		// Calls to Avora's tools show up as the turn's own steps already.
		tool := strings.ToLower(st.Tool)
		if !strings.Contains(tool, bridgeServer) && !strings.Contains(tool, "mcp") && onStep != nil {
			onStep(st)
		}
	})
}

// RestoreAgyPrincipal runs as the runtime starts: if it stopped while a pool
// account or a sign-in held the slot, the owner's own agy login goes back.
func RestoreAgyPrincipal(cfg AgentConfig) {
	if !agyKeyringSupported {
		return
	}
	p, err := os.ReadFile(agyPrincipalFile(cfg))
	if err != nil || agyEmail(p) == "" {
		return
	}
	agySlotLock <- struct{}{}
	defer func() { <-agySlotLock }()
	now, err := readAgySlot()
	if err == nil && (now == nil || agyPoolEmails(cfg)[agyEmail(now)]) {
		_ = writeAgySlot(p)
	}
}

// agyWaitSlot is the gate of a run on the PC's own agy login: it waits while
// a pool account or a sign-in holds the slot, and holds it until agy has
// read the login.
func agyWaitSlot(ctx context.Context) (func(), error) {
	select {
	case agySlotLock <- struct{}{}:
	case <-ctx.Done():
		return nil, errors.New("o Antigravity não chegou a abrir: " + ctx.Err().Error())
	}
	var once sync.Once
	return func() { once.Do(func() { <-agySlotLock }) }, nil
}
