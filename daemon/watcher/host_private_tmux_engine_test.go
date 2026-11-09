//go:build ghosttyvt && cgo && (linux || darwin)

package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A plain `tmux` in a session host Session, `tmux kill-server` too, reaches a
// server in the daemon's private TMUX_TMPDIR and never the user's default
// one. The directory is the daemon's and is made again when something removed
// it: tmux whose TMUX_TMPDIR does not exist falls back to the default server.
//
// The Session asks tmux where it would go with a read-only `tmux ls` first and
// starts or kills a server only when that is the private directory, so a
// failing build cannot touch the developer's own tmux.
func TestHostSessionPlainTmuxNeverReachesTheDefaultServer(t *testing.T) {
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("needs tmux")
	}
	h := startInProcessHost(t)
	private, err := os.MkdirTemp("", "pt") // socket paths are short
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(private, "tmux-"+strconv.Itoa(os.Getuid()), "default")
	t.Cleanup(func() {
		exec.Command(tmux, "-S", socket, "kill-server").Run()
		os.RemoveAll(private)
	})
	w := New(50 * time.Millisecond)
	w.SetTmuxServer(filepath.Join(t.TempDir(), "no-tmux"), private)
	w.SetSessionHost(SessionHostConfig{SocketPath: h.socket, Executable: "/nonexistent/mewla", StateDir: t.TempDir(), Version: "watcher-test"})

	// Something removed the directory after the daemon made it.
	if err := os.RemoveAll(private); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	script := `out=$(tmux -f /dev/null ls 2>&1)
case "$out" in
*"` + private + `/tmux-"*) tmux -f /dev/null new-session -d -s probe "sleep 60" && tmux kill-server && echo "killed $TMUX_TMPDIR" ;;
*) echo "unsafe: $out" ;;
esac > result
exec sleep 60`
	createHostSession(t, w, backendCreateSpec{
		name: "plain-tmux",
		cwd:  cwd,
		// No TMUX_TMPDIR of its own, as a Session from the App has none.
		env:     []string{"PATH=" + filepath.Dir(tmux) + ":/usr/bin:/bin", "HOME=" + cwd},
		command: script,
	})
	var result string
	waitUntil(t, 10*time.Second, "the Session's tmux result", func() bool {
		raw, err := os.ReadFile(filepath.Join(cwd, "result"))
		result = strings.TrimSpace(string(raw))
		return err == nil && result != ""
	})
	if result != "killed "+private {
		t.Fatalf("plain tmux in the Session: %q, want it killed in %s", result, private)
	}
	if err := exec.Command(tmux, "-S", socket, "has-session").Run(); err == nil {
		t.Fatal("the private tmux server outlived tmux kill-server")
	}

	// While Sessions run, the daemon puts the directory back.
	if err := os.RemoveAll(private); err != nil {
		t.Fatal(err)
	}
	w.keepPrivateTmuxDir()
	if info, err := os.Stat(private); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("private tmux directory after it was removed: %v, %v", info, err)
	}
}
