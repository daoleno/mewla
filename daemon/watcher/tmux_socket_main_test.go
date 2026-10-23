package watcher

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// TestMain points the default tmux socket at an empty directory for the whole
// package, so the socket probe that decides "server gone" never reaches the
// developer's real tmux server. Tests that need a reachable server use
// reachableTmuxSocket.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("/tmp", "mewla-tmux-")
	if err != nil {
		panic(err)
	}
	os.Setenv("TMUX_TMPDIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// reachableTmuxSocket makes the default tmux socket accept connections for the
// test, as a running server would; fake tmux scripts then model a reachable
// server that fails a command.
func reachableTmuxSocket(t *testing.T) {
	t.Helper()
	sockDir := filepath.Join(os.Getenv("TMUX_TMPDIR"), "tmux-"+strconv.Itoa(os.Getuid()))
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(sockDir, "default"))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	t.Cleanup(func() { listener.Close() })
}

func TestTmuxFailureDecidesServerGoneBySocket(t *testing.T) {
	exit1 := exitStatusError(t)
	words := []byte("no server running on /tmp/whatever")
	if err := tmuxFailure("", "list-panes", exit1, words); !isNoTmuxServerError(err) {
		t.Fatalf("no listener on the default socket must be server-gone: %v", err)
	}
	reachableTmuxSocket(t)
	if err := tmuxFailure("", "list-panes", exit1, words); isNoTmuxServerError(err) {
		t.Fatalf("reachable server must not read as gone, whatever tmux printed: %v", err)
	}
	if err := tmuxFailure("", "list-panes", os.ErrNotExist, nil); isNoTmuxServerError(err) {
		t.Fatalf("tmux missing is not server-gone: %v", err)
	}
}

func exitStatusError(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 1").Run()
	if !isExitStatus(err) {
		t.Fatalf("fixture did not produce an exit status: %v", err)
	}
	return err
}
