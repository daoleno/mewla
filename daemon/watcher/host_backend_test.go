package watcher

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSessionHostLeavesNoTraceUntilUsed: until a Session is created, the
// daemon starts, creates or dials nothing for the host, and its inventory is
// "nothing running".
func TestSessionHostLeavesNoTraceUntilUsed(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime", "mewla")
	logPath := filepath.Join(root, "state", "session-host", "host.log")

	w := New(50 * time.Millisecond)
	w.SetSessionHost(SessionHostConfig{
		SocketPath: filepath.Join(runtimeDir, "host-00000000.sock"),
		Executable: "/nonexistent/mewla",
		StateDir:   filepath.Join(root, "state"),
		LogPath:    logPath,
		Version:    "t",
	})
	if _, ok := w.sessions().(*hostBackend); !ok {
		t.Fatalf("backend = %T", w.sessions())
	}
	if panes, err := w.sessions().listOwned(); !errors.Is(err, ErrSessionHostGone) || len(panes) != 0 {
		t.Fatalf("inventory = %v, %v; want nothing running", panes, err)
	}
	if services, err := w.sessions().listServices(); err != nil || len(services) != 0 {
		t.Fatalf("services = %v, %v", services, err)
	}
	if _, err := w.sessions().place("", false, "x"); err != nil {
		t.Fatalf("place: %v", err)
	}
	host := "mh-0123456789abcdef01234567"
	if gone, err := w.sessions().resolveAbsence(host); err != nil || !gone {
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
	if b.observedGeneration("mh-0123456789abcdef01234567") == "" || b.observedGeneration("%1") != "" {
		t.Fatal("generation")
	}
}

// TestHostGoneAfterARestartIsAbsent: after a reboot or a restart the session
// host is not running and this process never reached one, so a host Session
// an earlier run recorded (Brain's) is gone with it. It read as unknown, so
// Brain was neither reused nor replaced and never started again (FAILURES
// Round 4 #15).
func TestHostGoneAfterARestartIsAbsent(t *testing.T) {
	root := t.TempDir()
	w := New(50 * time.Millisecond)
	w.SetSessionHost(SessionHostConfig{
		SocketPath: filepath.Join(root, "run", "host-00000000.sock"),
		Executable: "/nonexistent/mewla",
		StateDir:   filepath.Join(root, "state"),
		Version:    "t",
	})
	recorded := "mh-0123456789abcdef01234567"
	if gone, err := w.ResolveDelegatedAbsence(recorded); err != nil || !gone {
		t.Fatalf("Brain's Session from before the restart = gone %v, %v; want gone", gone, err)
	}
}

// TestFirstInventoryBeforeTheHostRuns: the first inventory is empty until the
// host runs, so the watcher becomes ready and Brain's host can start.
func TestFirstInventoryBeforeTheHostRuns(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", filepath.Join(root, "empty-path"))
	w := New(50 * time.Millisecond)
	w.SetSessionHost(SessionHostConfig{
		SocketPath: filepath.Join(root, "run", "host-00000000.sock"),
		Executable: "/nonexistent/mewla",
		StateDir:   filepath.Join(root, "state"),
		LogPath:    filepath.Join(root, "state", "session-host", "host.log"),
		Version:    "t",
	})
	// The first poll is an empty, ready inventory that saw no host, so a
	// Session an earlier run recorded (Brain's, before a reboot) is gone.
	w.poll()
	if !w.SnapshotReady() {
		t.Fatal("watcher not ready after the first poll")
	}
	recorded := "mh-0123456789abcdef01234567"
	if gone, err := w.ResolveDelegatedAbsence(recorded); err != nil || !gone {
		t.Fatalf("a Session from before the reboot = gone %v, %v", gone, err)
	}
}

// TestSessionHostUnreachableIsUnknown: a host that is there and cannot answer
// leaves a Session neither present nor gone; once no host runs, it is absent.
func TestSessionHostUnreachableIsUnknown(t *testing.T) {
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
	w.SetSessionHost(SessionHostConfig{SocketPath: socket, Executable: "/nonexistent/mewla", StateDir: dir, Version: "t"})
	id := "mh-0123456789abcdef01234567"
	if presence, err := w.ProbeSession(id); !errors.Is(err, ErrOwnershipProbeUnavailable) || presence != SessionPresenceUnknown {
		t.Fatalf("ProbeSession with the host unreachable = %v, %v", presence, err)
	}
	if gone, err := w.sessions().resolveAbsence(id); !errors.Is(err, ErrOwnershipProbeUnavailable) || gone {
		t.Fatalf("resolveAbsence with the host unreachable = %v, %v", gone, err)
	}
	// The host stops: no host holds nothing.
	listener.Close()
	os.Remove(socket)
	if presence, err := w.ProbeSession(id); err != nil || presence != SessionPresenceAbsent {
		t.Fatalf("ProbeSession with no host = %v, %v", presence, err)
	}
}
