//go:build linux || darwin

package sessionhost

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// probeEnv makes the test binary act as another user's client: it dials the
// socket in the variable and exits 0 when the host refused it.
const probeEnv = "MEWLA_SESSIONHOST_PROBE"

// otherUserEnv overrides how the probe is run as another user, as a command
// prefix such as "docker run --rm -u 65534:65534 -v /tmp:/tmp alpine". The
// default is sudo -n -u nobody.
const otherUserEnv = "MEWLA_SESSIONHOST_OTHER_USER_CMD"

func TestMain(m *testing.M) {
	if path := os.Getenv(probeEnv); path != "" {
		os.Exit(probeHost(path))
	}
	os.Exit(m.Run())
}

func probeHost(path string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Probe the host's half of the boundary only: this client would refuse
	// a host of another user on its own.
	serverUIDAllowed = func(uint32) bool { return true }
	c, err := Dial(ctx, path, "probe")
	if err != nil {
		fmt.Printf("probe uid %d: refused: %v\n", os.Getuid(), err)
		return 0
	}
	defer c.Close()
	list, err := c.List(ctx)
	fmt.Printf("probe uid %d: CONNECTED, listed %d sessions (%v)\n", os.Getuid(), len(list), err)
	return 3
}

// TestAnotherUserCannotConnect is the socket's security contract: whoever can
// talk to the host can type into every agent, so a process of any other uid
// is refused even when the file system would let it reach the socket.
func TestAnotherUserCannotConnect(t *testing.T) {
	prefix := otherUserCommand(t)

	// A directory every user can enter, holding the probe and the socket.
	dir, err := os.MkdirTemp("/tmp", "mewla-peer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe")
	if err := copyExecutable(probe); err != nil {
		t.Fatal(err)
	}

	var logMu sync.Mutex
	var logs []string
	socket := filepath.Join(dir, "h.sock")
	h := startHost(t, ServerOptions{SocketPath: socket, Logf: func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		logMu.Lock()
		logs = append(logs, line)
		logMu.Unlock()
		t.Logf("host: %s", line)
	}})
	// Model a permission mistake: the socket file open to everyone.
	if err := os.Chmod(socket, 0o777); err != nil {
		t.Fatal(err)
	}
	owner := dialHost(t, h)
	if _, err := owner.Create(ctxT(t), shellSession("guarded", shortTempDir(t), "exec sleep 60")); err != nil {
		t.Fatal(err)
	}

	// The owner's own probe connects: the probe itself works.
	self := exec.Command(probe)
	self.Env = append(os.Environ(), probeEnv+"="+socket)
	if out, err := self.CombinedOutput(); exitCode(err) != 3 {
		t.Fatalf("the owner's probe did not connect (%v): %s", err, out)
	}

	args := append(append([]string(nil), prefix[1:]...), "env", probeEnv+"="+socket, probe)
	other := exec.Command(prefix[0], args...)
	out, err := other.CombinedOutput()
	t.Logf("%s: %s", strings.Join(other.Args, " "), out)
	if exitCode(err) != 0 || !strings.Contains(string(out), "refused") {
		t.Fatalf("another user's probe was not refused (%v): %s", err, out)
	}
	if strings.Contains(string(out), fmt.Sprintf("probe uid %d:", os.Getuid())) {
		t.Fatalf("the probe ran as this user, not another one: %s", out)
	}
	waitFor(t, 5*time.Second, "refusal logged", func() bool {
		logMu.Lock()
		defer logMu.Unlock()
		for _, line := range logs {
			if strings.HasPrefix(line, "refused a connection from uid ") {
				return true
			}
		}
		return false
	})
	if list, err := owner.List(ctxT(t)); err != nil || len(list) != 1 || !list[0].Alive {
		t.Fatalf("sessions after the refusal: %+v, %v", list, err)
	}
}

// otherUserCommand returns the command prefix that runs a program as another
// user, or skips the test where there is none. CI must run the contract.
func otherUserCommand(t *testing.T) []string {
	t.Helper()
	if custom := strings.Fields(os.Getenv(otherUserEnv)); len(custom) > 0 {
		return custom
	}
	if os.Getuid() == 0 {
		if path, err := exec.LookPath("setpriv"); err == nil {
			return []string{path, "--reuid=65534", "--regid=65534", "--clear-groups"}
		}
	}
	if path, err := exec.LookPath("sudo"); err == nil && exec.Command(path, "-n", "-u", "nobody", "true").Run() == nil {
		return []string{path, "-n", "-u", "nobody"}
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		t.Fatal("CI must run the socket contract: it needs passwordless sudo to act as nobody")
	}
	t.Skipf("acting as another user needs root, passwordless sudo, or %s (CI runs this)", otherUserEnv)
	return nil
}

func copyExecutable(to string) error {
	from, err := os.Executable()
	if err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	return -1
}
