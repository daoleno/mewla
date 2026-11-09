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

// A plain `tmux` in an agent's session host Session, `tmux kill-server` too,
// reaches a server in the daemon's private TMUX_TMPDIR and never the person's
// own. The directory is the daemon's and is made again when something
// removed it: tmux whose TMUX_TMPDIR does not exist falls back to the default
// server. A person's own shell keeps their environment and their tmux.
//
// Each Session asks tmux where it would go with a read-only `tmux ls` first;
// the agent starts or kills a server only when that is the private directory,
// so a failing build cannot touch the developer's own tmux.
func TestHostSessionPlainTmuxNeverReachesTheDefaultServer(t *testing.T) {
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("needs tmux")
	}
	h := startInProcessHost(t)
	short := func(prefix string) string { // socket paths are short
		dir, err := os.MkdirTemp("", prefix)
		if err != nil {
			t.Fatal(err)
		}
		return dir
	}
	private, own := short("pt"), short("ut")
	uid := "tmux-" + strconv.Itoa(os.Getuid())
	socket := filepath.Join(private, uid, "default")
	t.Cleanup(func() {
		exec.Command(tmux, "-S", socket, "kill-server").Run()
		os.RemoveAll(private)
		os.RemoveAll(own)
	})
	w := New(50 * time.Millisecond)
	w.SetTmuxServer(filepath.Join(t.TempDir(), "no-tmux"), private)
	w.SetSessionHost(SessionHostConfig{SocketPath: h.socket, Executable: "/nonexistent/mewla", StateDir: t.TempDir(), Version: "watcher-test"})
	path := "PATH=" + filepath.Dir(tmux) + ":/usr/bin:/bin"
	result := func(cwd string) string {
		t.Helper()
		var got string
		waitUntil(t, 10*time.Second, "the Session's tmux result", func() bool {
			raw, err := os.ReadFile(filepath.Join(cwd, "result"))
			got = strings.TrimSpace(string(raw))
			return err == nil && got != ""
		})
		return got
	}

	// Something removed the directory after the daemon made it.
	if err := os.RemoveAll(private); err != nil {
		t.Fatal(err)
	}
	agentCwd := t.TempDir()
	createHostSession(t, w, backendCreateSpec{
		name: "agent",
		cwd:  agentCwd,
		// An agent's own TMUX_TMPDIR (a Worker's scratch, say) is not kept.
		env: []string{path, "HOME=" + agentCwd, "TMUX_TMPDIR=" + own},
		command: `out=$(tmux -f /dev/null ls 2>&1)
case "$out" in
*"` + private + `/tmux-"*) tmux -f /dev/null new-session -d -s probe "sleep 60" && tmux kill-server && echo "killed $TMUX_TMPDIR" ;;
*) echo "unsafe: $out" ;;
esac > result
exec sleep 60`,
		agent: true,
	})
	if got := result(agentCwd); got != "killed "+private {
		t.Fatalf("plain tmux in an agent's Session: %q, want it killed in %s", got, private)
	}
	if err := exec.Command(tmux, "-S", socket, "has-session").Run(); err == nil {
		t.Fatal("the private tmux server outlived tmux kill-server")
	}

	// A person's own shell: their TMUX_TMPDIR, so their own tmux.
	shellCwd := t.TempDir()
	createHostSession(t, w, backendCreateSpec{
		name:    "shell",
		cwd:     shellCwd,
		env:     []string{path, "HOME=" + shellCwd, "TMUX_TMPDIR=" + own},
		command: `{ tmux -f /dev/null ls 2>&1; echo "TMUX_TMPDIR=$TMUX_TMPDIR"; } > result; exec sleep 60`,
	})
	if got := result(shellCwd); !strings.Contains(got, own+"/"+uid+"/default") || !strings.HasSuffix(got, "TMUX_TMPDIR="+own) {
		t.Fatalf("plain tmux in a person's shell: %q, want their own %s", got, own)
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
