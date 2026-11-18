package runner

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/mcp"
	"github.com/GuardaChuva22/avora/internal/store"
)

// The runtime service bounds RAM. Node reserves virtual address space for
// V8 and WebAssembly, so the shell's virtual memory cap is unsuitable here.
const mcpLimits = `ulimit -f 409600 2>/dev/null; ulimit -u 256 2>/dev/null;`

//go:embed polymarket_gamma.mjs
var polymarketGammaCompat []byte

const (
	polymarketMCPRepository = "https://github.com/guangxiangdebizi/polymarket-mcp"
	polymarketMCPRevision   = "c1c41ea772a625aaf96c39961e608f361788aa34"
)

// Work in a staging directory so a failed or canceled build never becomes
// the active checkout. Replaying a completed installation keeps that checkout.
const mcpInstallScript = mcpLimits + `
set -eu
if test -d repo; then exit 0; fi
stage=$(mktemp -d /work/.install-XXXXXXXX)
trap 'rm -rf -- "$stage"' EXIT
cd "$stage"
git init --quiet
git fetch --quiet --depth=1 -- "$1" "$2"
git checkout --quiet --detach FETCH_HEAD
npm --cache /tmp/avora-npm-cache ci --no-audit --no-fund --include=dev
npm --cache /tmp/avora-npm-cache run build --if-present
test -f "./$3"
mv -T -- "$stage" /work/repo
`

// RunMCP installs, checks, or invokes a stdio plugin on the Linux runtime.
// Each check and invocation opens its own subprocess inside the sandbox.
func RunMCP(ctx context.Context, cfg AgentConfig, spec store.AgentSpec) control.JobResult {
	if runtime.GOOS != "linux" {
		return control.JobResult{Error: "plugins MCP stdio rodam no nó Linux (VPS)"}
	}
	var job mcp.PluginJob
	if err := json.Unmarshal([]byte(spec.Prompt), &job); err != nil {
		return control.JobResult{Error: "pedido MCP inválido"}
	}
	if err := job.Validate(); err != nil {
		return control.JobResult{Error: err.Error()}
	}
	dir, err := mcpWorkdir(cfg.Workspace, spec.Workdir, job.Mode == "install")
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	limit := time.Duration(spec.MaxMinutes) * time.Minute
	if limit <= 0 || limit > 10*time.Minute {
		limit = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	if job.Mode == "install" {
		if err := installMCP(runCtx, cfg, dir, job); err != nil {
			return mcpFailure(runCtx, err)
		}
	}
	res, err := runMCPStdio(runCtx, cfg, dir, job, spec.Mode == ShellNetMode)
	if err != nil {
		return mcpFailure(runCtx, err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		return control.JobResult{Error: "resposta MCP inválida"}
	}
	result := control.JobResult{Success: true, Output: string(b)}
	wire, _ := json.Marshal(result)
	if len(b) > control.MaxJobOutput || len(wire) > 2*control.MaxJobOutput {
		return control.JobResult{Error: "o MCP devolveu dados demais para uma só resposta; tente reduzir o volume pedido"}
	}
	return result
}

func mcpWorkdir(workspace, rel string, create bool) (string, error) {
	if !mcp.ValidPluginDir(rel) {
		return "", errors.New("pasta inválida do plugin MCP")
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return "", fmt.Errorf("workspace do runtime indisponível: %w", err)
	}
	defer root.Close()
	for _, name := range []string{"plugins", rel} {
		if create {
			if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return "", fmt.Errorf("não consegui criar a pasta do MCP: %w", err)
			}
		}
		if st, err := root.Lstat(name); err != nil || !st.IsDir() {
			return "", errors.New("a pasta do plugin MCP não está disponível neste nó")
		}
	}
	return filepath.Join(workspace, filepath.FromSlash(rel)), nil
}

func mcpCommand(ctx context.Context, cfg AgentConfig, dir, cwd string, network, writable bool, argv ...string) (*exec.Cmd, error) {
	args := sandboxArgs(dir, cwd, network, writable)
	// On the VPS, /usr/local/bin/node and npm link into this distribution.
	args = append(args, "--ro-bind-try", "/opt/node", "/opt/node")
	cmd, err := sandboxCommand(ctx, cfg, network, append(args, argv...))
	if err != nil {
		return nil, err
	}
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/tmp", "LANG=C.UTF-8", "LC_ALL=C.UTF-8",
		"NODE_OPTIONS=--max-old-space-size=256", "PYTHONDONTWRITEBYTECODE=1",
		"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_ALLOW_PROTOCOL=https",
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd, nil
}

func installMCP(ctx context.Context, cfg AgentConfig, dir string, job mcp.PluginJob) error {
	revision := strings.ToLower(job.Revision)
	if revision == "" {
		revision = "HEAD"
	}
	cmd, err := mcpCommand(ctx, cfg, dir, "/work", true, true,
		"/bin/sh", "-c", mcpInstallScript, "sh", job.Repository, revision, job.Entry)
	if err != nil {
		return err
	}
	out := &tailBuffer{max: 1600}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("instalação do MCP falhou: %w\n%s", err, out.String())
	}
	return nil
}

func runMCPStdio(ctx context.Context, cfg AgentConfig, dir string, job mcp.PluginJob, network bool) (mcp.PluginResult, error) {
	cwd := "/work"
	entry := job.Entry
	if job.Repository != "" {
		cwd = "/work/repo"
		entry = path.Join("repo", entry)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return mcp.PluginResult{}, err
	}
	defer root.Close()
	if job.Repository != "" {
		if st, err := root.Lstat("repo"); err != nil || !st.IsDir() {
			return mcp.PluginResult{}, errors.New("o repositório do MCP não está disponível neste nó")
		}
	}
	if st, err := root.Stat(entry); err != nil || !st.Mode().IsRegular() {
		return mcp.PluginResult{}, fmt.Errorf("arquivo de entrada do MCP indisponível: %s", job.Entry)
	}
	procCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	argv := []string{"/bin/sh", "-c", mcpLimits + ` exec "$@"`, "sh", job.Runtime}
	if polymarketMCPCheckout(root, job) {
		argv = append(argv, "--import", "data:text/javascript;base64,"+base64.StdEncoding.EncodeToString(polymarketGammaCompat))
	}
	cmd, err := mcpCommand(procCtx, cfg, dir, cwd, network, false, append(argv, path.Join(cwd, job.Entry))...)
	if err != nil {
		return mcp.PluginResult{}, err
	}
	stderr := &tailBuffer{max: 1000}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return mcp.PluginResult{}, err
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return mcp.PluginResult{}, err
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return mcp.PluginResult{}, err
	}
	// Interrupt blocked RPC I/O even if a descendant inherited a pipe.
	stopPipes := context.AfterFunc(procCtx, func() {
		_ = stdin.Close()
		_ = stdout.Close()
	})
	defer stopPipes()
	client := mcp.NewStdioClient(stdin, stdout)
	res := mcp.PluginResult{}
	res.Server, err = client.Initialize()
	if err == nil {
		if job.Mode == "call" {
			res.Result, err = client.CallTool(job.Name, job.Arguments)
		} else {
			res.Tools, err = client.ListTools()
		}
	}
	_ = stdin.Close()
	// Stop the sandbox even when a server does not exit on stdin EOF.
	cancel()
	_ = cmd.Wait()
	if err != nil {
		if log := stderr.String(); log != "" {
			err = fmt.Errorf("%w\n%s", err, log)
		}
		return mcp.PluginResult{}, err
	}
	return res, nil
}

// Apply the compatibility fix only to the upstream checkout it was
// written for, including already installed copies of that revision.
func polymarketMCPCheckout(root *os.Root, job mcp.PluginJob) bool {
	if job.Runtime != "node" || !strings.EqualFold(job.Repository, polymarketMCPRepository) || job.Entry != "build/index.js" {
		return false
	}
	const gitHead = "repo/.git/HEAD"
	info, err := root.Lstat(gitHead)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64 {
		return false
	}
	head, err := root.Open(gitHead)
	if err != nil {
		return false
	}
	defer head.Close()
	revision, err := io.ReadAll(io.LimitReader(head, 64))
	return err == nil && strings.TrimSpace(string(revision)) == polymarketMCPRevision
}

func mcpFailure(ctx context.Context, err error) control.JobResult {
	if ctx.Err() != nil {
		return control.JobResult{Error: "a execução do MCP foi interrompida ou passou do limite de tempo"}
	}
	return control.JobResult{Error: clip(err.Error(), 2000)}
}
