//go:build linux

package sessionhost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func cgroupOf(t *testing.T, pid int) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(line, "0::"); ok {
			return rest
		}
	}
	t.Fatalf("no cgroup v2 path in %q", raw)
	return ""
}

// The host must not share a cgroup, or an out-of-memory policy, with whatever
// started the daemon. The kernel killed one Chrome helper of one agent for
// lack of memory; systemd's OOMPolicy=stop then stopped the whole scope the
// daemon had been started in, and the host and every Session with it.
func TestHostRunsInAUnitOfItsOwnThatKeepsRunningWhenTheKernelKillsAProcess(t *testing.T) {
	if !systemdUserUnits() {
		t.Skip("needs a systemd user manager")
	}
	opts := testStartOptions(t, "serve")
	ctx := ctxT(t)
	client, started, err := Connect(ctx, opts)
	if err != nil || !started {
		t.Fatalf("Connect: started=%v err=%v", started, err)
	}
	host := client.Welcome().PID
	t.Cleanup(func() {
		if c, err := Dial(context.Background(), opts.SocketPath, "cleanup"); err == nil {
			_ = c.Shutdown(context.Background(), true)
			c.Close()
		}
		waitFor(t, 10*time.Second, "test host gone", func() bool { return !processAlive(host) })
	})
	client.Close()

	mine, theirs := cgroupOf(t, os.Getpid()), cgroupOf(t, host)
	if mine == theirs {
		t.Fatalf("the host runs in the cgroup of the process that started it: %s", theirs)
	}
	unit := filepath.Base(theirs)
	if !strings.HasPrefix(unit, "mewla-session-host-") || !strings.HasSuffix(unit, ".service") {
		t.Fatalf("host cgroup %s, want a mewla-session-host service", theirs)
	}
	out, err := exec.Command("systemctl", "--user", "show", "-p", "OOMPolicy", unit).Output()
	if err != nil || strings.TrimSpace(string(out)) != "OOMPolicy=continue" {
		t.Fatalf("%s: %q, %v; want OOMPolicy=continue", unit, out, err)
	}
}
