package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Extra Claude and Codex subscription accounts live next to the node's own
// login, each in accounts/<id>: the official CLI logs in there and keeps
// its tokens there. The runtime only relays the login link and code, and
// passes back the code the owner pasted; it never reads the tokens.

const loginTimeout = 15 * time.Minute

var (
	accountID = regexp.MustCompile(`^(claude|codex|antigravity)\.[a-z0-9]{6}$`)
	ansi      = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	loginURL  = regexp.MustCompile(`https://\S+\s`)
	// deviceCode is what Codex asks to type on its page ("ABCD-12345").
	deviceCode = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4,5}\b`)
)

func accountDir(def, id string) string {
	return filepath.Join(filepath.Dir(def), "accounts", id)
}

// withAccount points the engine of spec at its account's folder. The quota
// readings of a run there are that account's, not the node's own.
func (cfg AgentConfig) withAccount(spec store.AgentSpec) (AgentConfig, error) {
	if spec.Account == "" {
		return cfg, nil
	}
	if !accountID.MatchString(spec.Account) || !strings.HasPrefix(spec.Account, spec.Engine+".") {
		return cfg, errors.New("invalid account " + spec.Account)
	}
	switch spec.Engine {
	case "claude":
		cfg.ClaudeConfigDir = accountDir(cfg.ClaudeConfigDir, spec.Account)
	case "codex":
		cfg.CodexHome = accountDir(cfg.CodexHome, spec.Account)
	}
	if observe := cfg.OnQuota; observe != nil {
		cfg.OnQuota = func(q control.Quota) {
			q.Account = spec.Account
			observe(q)
		}
	}
	return cfg, nil
}

// RunLogin runs the CLI login of an account. Each piece the owner needs
// goes out as a step: "url" (the page to open), "code" (Codex's device
// code) and "paste" (Claude waits for the code shown after signing in);
// input polls for that code.
func RunLogin(ctx context.Context, cfg AgentConfig, spec store.AgentSpec, onStep func(control.AgentStep), input func(context.Context) string) control.JobResult {
	if spec.Account == "" {
		return control.JobResult{Error: "login without an account"}
	}
	cfg, err := cfg.withAccount(spec)
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	if spec.Engine == "antigravity" {
		// "check" is the PC's own agy login, added before the pool (D44);
		// any other account signs in to a login of its own (D61).
		if spec.Mode == store.AgyCheckMode {
			return checkAgy(ctx, cfg, onStep)
		}
		return loginAgy(ctx, cfg, spec, onStep, input)
	}
	var bin, dir string
	var args, env []string
	switch spec.Engine {
	case "claude":
		bin, dir, args = cfg.ClaudeBin, cfg.ClaudeConfigDir, []string{"auth", "login", "--claudeai"}
		env = claudeEnv(dir)
	case "codex":
		bin, dir, args = cfg.CodexBin, cfg.CodexHome, []string{"login", "--device-auth"}
		env = codexEnv(dir)
	default:
		return control.JobResult{Error: "unsupported engine " + spec.Engine}
	}
	if _, err := os.Stat(bin); err != nil {
		return control.JobResult{Error: engineName(spec.Engine) + " não está instalado neste nó"}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return control.JobResult{Error: "não consegui criar a pasta da conta"}
	}
	runCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin, args...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = dir, env, 5*time.Second
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	out, w := io.Pipe()
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Start(); err != nil {
		return control.JobResult{Error: "não consegui abrir o login: " + err.Error()}
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = w.Close()
		done <- err
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

	text, sentURL, sentCode, asked := "", false, false, false
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	var waitErr error
loop:
	for {
		select {
		case c, ok := <-chunks:
			if !ok {
				waitErr = <-done
				break loop
			}
			if len(text) < 64<<10 {
				text += ansi.ReplaceAllString(c, "")
			}
			if u := loginURL.FindString(text); !sentURL && u != "" {
				sentURL = true
				onStep(control.AgentStep{Tool: "url", Summary: strings.TrimSpace(u)})
			}
			if code := deviceCode.FindString(text); spec.Engine == "codex" && !sentCode && sentURL && code != "" {
				sentCode = true
				onStep(control.AgentStep{Tool: "code", Summary: code})
			}
			if spec.Engine == "claude" && !asked && strings.Contains(strings.ToLower(text), "paste code") {
				asked = true
				onStep(control.AgentStep{Tool: "paste"})
			}
		case <-tick.C:
			if !asked {
				continue
			}
			if in := input(runCtx); in != "" {
				_, _ = io.WriteString(stdin, in+"\n")
			}
		}
	}
	_ = stdin.Close()
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return control.JobResult{Error: "o login expirou"}
	case ctx.Err() != nil:
		return control.JobResult{Error: "login cancelado"}
	case waitErr != nil:
		return control.JobResult{Error: "o login falhou: " + loginTail(text)}
	}
	who := map[string]string{}
	if spec.Engine == "claude" {
		who["account"] = claudeAccount(ctx, bin, env, dir)
	}
	b, _ := json.Marshal(who)
	return control.JobResult{Success: true, Output: string(b)}
}

// loginTail is the end of the login's output, without links or codes.
func loginTail(text string) string {
	text = deviceCode.ReplaceAllString(loginURL.ReplaceAllString(text, "… "), "…")
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 200 {
		text = text[len(text)-200:]
	}
	return text
}

// claudeAccount asks the CLI who is logged in (its e-mail and plan).
func claudeAccount(ctx context.Context, bin string, env []string, dir string) string {
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(sctx, bin, "auth", "status")
	cmd.Dir, cmd.Env = dir, env
	hideWindow(cmd)
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	var st struct {
		Email string `json:"email"`
		Plan  string `json:"subscriptionType"`
	}
	if json.Unmarshal(b, &st) != nil || st.Email == "" {
		return ""
	}
	if st.Plan != "" {
		return st.Email + " · " + strings.ToUpper(st.Plan[:1]) + st.Plan[1:]
	}
	return st.Email
}

func engineName(engine string) string {
	switch engine {
	case "codex":
		return "Codex"
	case "antigravity":
		return "Antigravity"
	}
	return "Claude Code"
}

// Input fetches the code the owner pasted for a running login, or "".
func (c *Client) Input(ctx context.Context, jobID string) string {
	if jobID == "" || strings.ContainsAny(jobID, "/\\?#") {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/runtime/jobs/"+jobID+"/input", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var body struct {
		Input string `json:"input"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
	return body.Input
}
