package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// executar: an agent runs a shell command in its own folder, inside a
// bubblewrap sandbox. The sandbox sees the system read-only, the agent's
// folder at /work (read-write), a private /tmp, no other files of the node
// (no Avora state, no CLI logins, no /home), and no network unless the job
// says so (and the owner approved it). Limits: the job's time, 1 GB of
// virtual memory, 400 MB per file written, 256 processes, 64 KB of output.
//
// With network the sandbox gets its own network namespace from pasta, which
// reaches the internet through the node's sockets but forwards no ports:
// the node's localhost (Avora, Postgres, other services) stays out of reach.
// Routes inside the namespace drop private ranges too (the cloud network
// and its metadata service, the tailnet and the owner's PC), except the
// tailnet DNS. The command then runs with no capabilities, so it cannot
// change those routes.

const (
	shellOutput = 64 << 10
	// ShellNetMode is the AgentSpec.Mode that gives the command network.
	ShellNetMode = "net"
	// pastaGateway is the namespace's made-up gateway (TEST-NET-1).
	pastaGateway = "192.0.2.1"
)

// blockedRoutes drop traffic to private ranges inside the sandbox's network
// namespace before the sandbox starts.
var blockedRoutes = strings.Join([]string{
	// Oracle Linux puts ip in /usr/sbin, outside the workload's PATH.
	`ip=$(PATH=/usr/sbin:/sbin:/usr/bin:/bin command -v ip) || { echo "o sandbox precisa do ip (iproute)" >&2; exit 127; }`,
	`"$ip" route add blackhole 10.0.0.0/8`,
	`"$ip" route add blackhole 172.16.0.0/12`,
	`"$ip" route add blackhole 192.168.0.0/16`,
	`"$ip" route add blackhole 169.254.0.0/16`,
	`"$ip" route add blackhole 100.64.0.0/10`,
	`"$ip" route add 100.100.100.100/32 via ` + pastaGateway, // tailnet DNS
	`exec "$@"`,
}, " && ")

// limitedBuffer keeps the first max bytes written to it.
type limitedBuffer struct {
	buf bytes.Buffer
	max int
	cut bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	room := l.max - l.buf.Len()
	if room <= 0 {
		l.cut = true
		return len(p), nil
	}
	if len(p) > room {
		l.buf.Write(p[:room])
		l.cut = true
		return len(p), nil
	}
	return l.buf.Write(p)
}

// bwrapArgs builds the sandbox around workdir. With network it runs inside
// pasta's namespace, which already is a user namespace of its own.
func bwrapArgs(workdir string, network bool) []string {
	return bwrapSharedArgs(workdir, "", network)
}

func bwrapSharedArgs(workdir, sharedDir string, network bool) []string {
	// Limits first, then the command, read from the environment so no
	// quoting can break out of them.
	args := sandboxArgs(workdir, "/work", network, true)
	if sharedDir != "" {
		args = append(args, "--bind", sharedDir, "/shared")
	}
	return append(args, "/bin/sh", "-c",
		`ulimit -v 1048576 2>/dev/null; ulimit -f 409600 2>/dev/null; ulimit -u 256 2>/dev/null; eval "$AVORA_CMD"`)
}

func sandboxArgs(workdir, cwd string, network, writable bool) []string {
	args := []string{"--unshare-all"}
	if network {
		args = []string{"--unshare-ipc", "--unshare-pid", "--unshare-uts", "--unshare-cgroup-try", "--cap-drop", "ALL"}
	}
	mount := "--ro-bind"
	if writable {
		mount = "--bind"
	}
	args = append(args,
		"--die-with-parent", "--new-session", "--hostname", "avora",
		"--ro-bind", "/usr", "/usr",
		"--symlink", "usr/bin", "/bin", "--symlink", "usr/sbin", "/sbin",
		"--symlink", "usr/lib", "/lib", "--symlink", "usr/lib64", "/lib64",
		"--ro-bind-try", "/etc/alternatives", "/etc/alternatives",
		"--ro-bind-try", "/etc/pki", "/etc/pki",
		"--ro-bind-try", "/etc/ssl", "/etc/ssl",
		"--ro-bind-try", "/etc/localtime", "/etc/localtime",
		"--ro-bind-try", "/etc/passwd", "/etc/passwd",
		"--ro-bind-try", "/etc/group", "/etc/group",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		mount, workdir, "/work", "--chdir", cwd,
	)
	if network {
		args = append(args,
			"--ro-bind-try", "/etc/resolv.conf", "/etc/resolv.conf",
			"--ro-bind-try", "/etc/hosts", "/etc/hosts",
			"--ro-bind-try", "/etc/nsswitch.conf", "/etc/nsswitch.conf")
	}
	return args
}

// pastaArgs wraps the sandbox in its own network namespace: IPv4 only,
// made-up addresses, no port forwarding either way, private ranges dropped.
func pastaArgs(bwrap string, sandbox []string) []string {
	args := []string{"--config-net", "--quiet", "--no-map-gw", "-4",
		"-a", "192.0.2.2", "-n", "24", "-g", pastaGateway,
		"-t", "none", "-u", "none", "-T", "none", "-U", "none",
		"--", "/bin/sh", "-c", blockedRoutes, "sh", bwrap}
	return append(args, sandbox...)
}

// RunShell runs one executar job.
func RunShell(ctx context.Context, cfg AgentConfig, spec store.AgentSpec) control.JobResult {
	if runtime.GOOS != "linux" {
		return control.JobResult{Error: "executar só roda no nó Linux (VPS)"}
	}
	dir, err := cfg.dirFor(spec)
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	iso, err := cfg.prepareAgentIsolation(ctx, spec)
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	if iso.HomeRoot != filepath.Clean(dir) {
		if _, err := PrepareIsolation(cfg.Workspace, dir, iso.AgentID, iso.AgentName, iso.AgentEmail); err != nil {
			return control.JobResult{Error: err.Error()}
		}
	}
	limit := time.Duration(spec.MaxMinutes) * time.Minute
	if limit <= 0 || limit > 10*time.Minute {
		limit = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	network := spec.Mode == ShellNetMode
	cmd, err := sandboxCommand(runCtx, cfg, network, bwrapSharedArgs(dir, iso.SharedDir, network))
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	env := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/work/.avora-home",
		"USERPROFILE=/work/.avora-home",
		"TMP=/work/.avora-home/tmp",
		"TEMP=/work/.avora-home/tmp",
		"TMPDIR=/work/.avora-home/tmp",
		"GIT_AUTHOR_NAME=" + iso.AgentName,
		"GIT_AUTHOR_EMAIL=" + iso.AgentEmail,
		"GIT_COMMITTER_NAME=" + iso.AgentName,
		"GIT_COMMITTER_EMAIL=" + iso.AgentEmail,
		"GIT_CONFIG_GLOBAL=/work/.avora-home/.gitconfig",
		"LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TERM=dumb",
		"PYTHONDONTWRITEBYTECODE=1", "PIP_DISABLE_PIP_VERSION_CHECK=1",
		"AVORA_CMD=" + spec.Prompt,
	}
	if iso.SharedDir != "" {
		env = append(env, "AVORA_SHARED=/shared")
	}
	cmd.Env = env
	out := &limitedBuffer{max: shellOutput}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 5 * time.Second
	started := time.Now()
	err = cmd.Run()
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
		return control.JobResult{Output: text, Error: "não consegui iniciar o sandbox: " + err.Error()}
	}
	return control.JobResult{Success: true, Output: fmt.Sprintf("código de saída %d, %s\n%s", code, took, text)}
}

func sandboxCommand(ctx context.Context, cfg AgentConfig, network bool, args []string) (*exec.Cmd, error) {
	bwrap := cfg.Bwrap
	if bwrap == "" {
		bwrap = "/usr/bin/bwrap"
	}
	if _, err := os.Stat(bwrap); err != nil {
		return nil, errors.New("o sandbox (bubblewrap) não está instalado neste nó")
	}
	if !network {
		return exec.CommandContext(ctx, bwrap, args...), nil
	}
	// A copy outside the SELinux pasta domain: under NoNewPrivileges the
	// domain change fails and pasta cannot open /dev/net/tun.
	pasta := cfg.Pasta
	if pasta == "" {
		pasta = "/opt/avora/bin/pasta"
	}
	if _, err := os.Stat(pasta); err != nil {
		return nil, errors.New("a internet do sandbox (pasta) não está instalada neste nó")
	}
	return exec.CommandContext(ctx, pasta, pastaArgs(bwrap, args)...), nil
}
