package runner

import (
	"slices"
	"strings"
	"testing"
)

func TestShellSandboxArgs(t *testing.T) {
	off := bwrapArgs("/w/agents/atlas", false)
	if !slices.Contains(off, "--unshare-all") || slices.Contains(off, "--share-net") || slices.Contains(off, "/etc/resolv.conf") {
		t.Fatalf("offline: %q", off)
	}
	net := bwrapArgs("/w/agents/atlas", true)
	if slices.Contains(net, "--unshare-all") || slices.Contains(net, "--share-net") || !slices.Contains(net, "ALL") {
		t.Fatalf("net sandbox keeps pasta's namespace and drops every capability: %q", net)
	}
	p := strings.Join(pastaArgs("/usr/bin/bwrap", net), " ")
	for _, want := range []string{
		"-t none -u none -T none -U none",
		"--no-map-gw",
		"PATH=/usr/sbin:/sbin:/usr/bin:/bin command -v ip",
		`"$ip" route add blackhole 169.254.0.0/16`,
		`"$ip" route add blackhole 100.64.0.0/10`,
		`via 192.0.2.1 && exec "$@" sh /usr/bin/bwrap --unshare-ipc`,
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("pasta args miss %q: %s", want, p)
		}
	}
}
