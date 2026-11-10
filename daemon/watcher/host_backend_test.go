package watcher

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// TestSessionHostLeavesNoTraceUntilUsed: until a Session is created, the
// daemon inventories as tmux alone and starts, creates or dials nothing for
// the host; a new Session is placed in the host.
func TestSessionHostLeavesNoTraceUntilUsed(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime", "mewla")
	logPath := filepath.Join(root, "state", "session-host", "host.log")
	noTmux := filepath.Join(root, "no-tmux")

	plain := New(50 * time.Millisecond)
	plain.SetTmuxServer(noTmux, t.TempDir())
	plainPanes, plainErr := plain.sessions().listOwned()

	w := New(50 * time.Millisecond)
	w.SetTmuxServer(noTmux, t.TempDir())
	w.SetSessionHost(SessionHostConfig{
		SocketPath: filepath.Join(runtimeDir, "host-00000000.sock"),
		Executable: "/nonexistent/mewla",
		StateDir:   filepath.Join(root, "state"),
		LogPath:    logPath,
		Version:    "t",
	})
	if _, ok := w.sessions().(*compositeBackend); !ok {
		t.Fatalf("backend = %T", w.sessions())
	}
	panes, err := w.sessions().listOwned()
	if len(panes) != len(plainPanes) || errors.Is(err, ErrTmuxServerGone) != errors.Is(plainErr, ErrTmuxServerGone) || (err == nil) != (plainErr == nil) {
		t.Fatalf("inventory = %v, %v; tmux alone = %v, %v", panes, err, plainPanes, plainErr)
	}
	if services, err := w.sessions().listServices(); err != nil || len(services) != 0 {
		t.Fatalf("services = %v, %v", services, err)
	}
	if at, err := w.sessions().place("%3", false, "x"); err != nil || at.backend != backendHost {
		t.Fatalf("a new Session was placed in %v (%v), not the host", at.backend, err)
	}
	host := "mh-0123456789abcdef01234567"
	if gone, err := w.sessions().resolveAbsence(host, false); err != nil || !gone {
		t.Fatalf("a host Session from an earlier run, no host now: %v, %v", gone, err)
	}
	if present, _, err := w.sessions().probeOwnership(host); err != nil || present {
		t.Fatalf("probeOwnership without a host = %v, %v", present, err)
	}
	for _, path := range []string{filepath.Dir(runtimeDir), filepath.Dir(logPath)} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s exists before any Session was created: %v", path, err)
		}
	}
}

func TestHostSessionIDs(t *testing.T) {
	b := &hostBackend{}
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := newHostSessionID()
		if err != nil {
			t.Fatal(err)
		}
		if !b.validID(id) || seen[id] {
			t.Fatalf("id %q (seen %v)", id, seen[id])
		}
		seen[id] = true
	}
	for _, bad := range []string{"%1", "mh-", "mh-0123", "mh-0123456789ABCDEF01234567", "main:@1"} {
		if b.validID(bad) {
			t.Errorf("validID(%q)", bad)
		}
	}
	tmux := &tmuxBackend{}
	if tmux.validID("mh-0123456789abcdef01234567") || !tmux.validID("%12") {
		t.Fatal("tmux and host ids overlap")
	}
	if b.observedGeneration("mh-0123456789abcdef01234567") == "" || b.observedGeneration("%1") != "" {
		t.Fatal("generation")
	}
}

// TestHostOnlyInventoryNeedsNoTmux: on a computer without tmux (Windows) the
// first inventory is empty until the host runs, so the watcher becomes ready
// and Brain's host can start; tmux is never asked.
func TestHostOnlyInventoryNeedsNoTmux(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", filepath.Join(root, "empty-path"))
	config := SessionHostConfig{
		SocketPath: filepath.Join(root, "run", "host-00000000.sock"),
		Executable: "/nonexistent/mewla",
		StateDir:   filepath.Join(root, "state"),
		LogPath:    filepath.Join(root, "state", "session-host", "host.log"),
		Version:    "t",
		HostOnly:   true,
	}
	w := New(50 * time.Millisecond)
	w.SetTmuxServer(filepath.Join(root, "tmux.sock"), t.TempDir())
	w.SetSessionHost(config)
	if panes, err := w.sessions().listOwned(); !errors.Is(err, ErrTmuxServerGone) || len(panes) != 0 {
		t.Fatalf("host-only inventory before the host runs = %v, %v; want nothing running", panes, err)
	}
	if services, err := w.sessions().listServices(); err != nil || len(services) != 0 {
		t.Fatalf("host-only services = %v, %v", services, err)
	}
	// The first poll is an empty, ready inventory that saw no host, so a
	// Session an earlier run recorded (Brain's, before a reboot) is gone.
	w.poll()
	if !w.SnapshotReady() {
		t.Fatal("watcher not ready after the first host-only poll")
	}
	recorded := "mh-0123456789abcdef01234567"
	if gone, err := w.ResolveDelegatedAbsence(recorded); err != nil || !gone {
		t.Fatalf("a Session from before the reboot = gone %v, %v", gone, err)
	}
	// Once the watcher holds a host Session, a vanished host is unknown.
	w.mu.Lock()
	w.workers[recorded] = &classifier.Worker{}
	w.mu.Unlock()
	if panes, err := w.sessions().listOwned(); err == nil {
		t.Fatalf("host-only inventory with a known Session and no host = %v, nil", panes)
	}
}

// TestNoTmuxIsNoSessions: Mewla no longer needs tmux. Without it installed,
// the inventory is the host's alone, not a failure.
func TestNoTmuxIsNoSessions(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	w := New(50 * time.Millisecond)
	w.SetTmuxServer(filepath.Join(root, "tmux.sock"), t.TempDir())
	w.SetSessionHost(SessionHostConfig{SocketPath: filepath.Join(root, "host.sock"), Executable: "/nonexistent/mewla", StateDir: root, Version: "t"})
	// With no host running either, this is the "no server" answer the poll
	// reads as nothing running; it is never a tmux failure.
	panes, err := w.sessions().listOwned()
	if (err != nil && !errors.Is(err, ErrTmuxServerGone)) || len(panes) != 0 {
		t.Fatalf("inventory without tmux = %v, %v", panes, err)
	}
	if services, err := w.sessions().listServices(); err != nil || len(services) != 0 {
		t.Fatalf("services without tmux = %v, %v", services, err)
	}
	if n := w.LeftInTmux(); n != 0 {
		t.Fatalf("LeftInTmux without tmux = %d", n)
	}
}

// TestSessionHostUnreachableLeavesMovedIDsUnknown: while the host cannot say
// which Sessions moved from tmux, a %id is neither tmux's nor gone; once it
// answers, the next question gets the host's answer.
func TestSessionHostUnreachableLeavesMovedIDsUnknown(t *testing.T) {
	dir, err := os.MkdirTemp("", "wu")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "h.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	// Something listens but never says hello: a host that is there and
	// cannot answer, not one that is gone.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	w := New(50 * time.Millisecond)
	w.SetTmuxServer(filepath.Join(dir, "tmux.sock"), t.TempDir())
	w.SetSessionHost(SessionHostConfig{SocketPath: socket, Executable: "/nonexistent/mewla", StateDir: dir, Version: "t"})
	if presence, err := w.ProbeSession("%7"); !errors.Is(err, ErrOwnershipProbeUnavailable) || presence != SessionPresenceUnknown {
		t.Fatalf("ProbeSession with the host unreachable = %v, %v", presence, err)
	}
	if gone, err := w.sessions().resolveAbsence("%7", false); !errors.Is(err, ErrOwnershipProbeUnavailable) || gone {
		t.Fatalf("resolveAbsence with the host unreachable = %v, %v", gone, err)
	}
	// The host stops: no host holds nothing, and a %id is tmux's again.
	listener.Close()
	os.Remove(socket)
	if presence, err := w.ProbeSession("%7"); err != nil || presence != SessionPresenceAbsent {
		t.Fatalf("ProbeSession with no host = %v, %v", presence, err)
	}
}
