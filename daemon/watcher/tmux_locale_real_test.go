package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// clearClientLocale reproduces a launchd/LaunchAgent daemon: no TMUX and no
// LC_ALL, LC_CTYPE or LANG. tmux then treats the client as non-UTF-8 and
// rewrites control characters in command output to "_".
func clearClientLocale(t *testing.T) {
	t.Helper()
	for _, key := range []string{"TMUX", "LC_ALL", "LC_CTYPE", "LANG"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTabSeparatedTmuxFormatsSurviveNonUTF8ClientLocale(t *testing.T) {
	requireTmux(t)
	clearClientLocale(t)
	root := shortTmuxTestDir(t)
	socket := filepath.Join(root, "launchd.sock")
	if out, err := exec.Command("tmux", "-S", socket, "-f", "/dev/null", "new-session", "-d", "-s", "launchd-fixture", "-x", "80", "-y", "24", "sleep 300").CombinedOutput(); err != nil {
		t.Fatalf("bootstrap fixture server: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", socket, "kill-server").Run() })
	paneOut, err := exec.Command("tmux", "-S", socket, "display-message", "-p", "-t", "launchd-fixture", "#{pane_id}").Output()
	if err != nil {
		t.Fatal(err)
	}
	paneID := strings.TrimSpace(string(paneOut))

	// Fixture evidence only: a client without -u collapses the separator.
	raw, err := exec.Command("tmux", "-S", socket, "display-message", "-p", "-t", paneID, "#{pane_dead}\t#{pane_pid}").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\t") {
		t.Logf("this tmux keeps tabs for non-UTF-8 clients (%q); checking -u clients only", raw)
	}

	out, err := tmuxCommand(socket, "display-message", "-p", "-t", paneID, "#{pane_dead}\t#{pane_pid}").Output()
	if err != nil {
		t.Fatal(err)
	}
	if fields := strings.Split(strings.TrimSpace(string(out)), "\t"); len(fields) != 2 || fields[0] != "0" {
		t.Fatalf("Mewla tmux client output = %q, want pane_dead<TAB>pane_pid", out)
	}

	panes, err := listTmuxPanesOn(socket)
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 1 || panes[0].target != paneID || panes[0].panePID <= 0 || panes[0].sessionName != "launchd-fixture" {
		t.Fatalf("panes = %#v, want exact %s inventory", panes, paneID)
	}

	w := New(10 * time.Millisecond)
	w.SetTmuxServer(socket, filepath.Join(root, "scratch"))
	identity, err := w.resolveTargetProcessIdentity(paneID)
	if err != nil || !identity.valid() || identity.PanePID != panes[0].panePID {
		t.Fatalf("identity=%#v err=%v, want the pane's live process identity", identity, err)
	}
}

func TestTmuxClientEnvironmentAddsUTF8CTypeOnlyWithoutLocale(t *testing.T) {
	base := []string{"HOME=/Users/u", "PATH=/usr/bin"}
	if got := withUTF8CType(base, "darwin"); !reflect.DeepEqual(got, append(base, "LC_CTYPE=UTF-8")) {
		t.Fatalf("darwin env = %#v", got)
	}
	if got := withUTF8CType(base, "linux"); !reflect.DeepEqual(got, append(base, "LC_CTYPE=C.UTF-8")) {
		t.Fatalf("linux env = %#v", got)
	}
	for _, explicit := range []string{"LANG=zh_CN.UTF-8", "LC_CTYPE=en_US.UTF-8", "LC_ALL=C"} {
		env := append(append([]string(nil), base...), explicit)
		if got := withUTF8CType(env, "darwin"); !reflect.DeepEqual(got, env) {
			t.Fatalf("explicit %s overridden: %#v", explicit, got)
		}
	}
	if got := withUTF8CType(append(append([]string(nil), base...), "LANG="), "darwin"); got[len(got)-1] != "LC_CTYPE=UTF-8" {
		t.Fatalf("empty LANG did not get a UTF-8 LC_CTYPE: %#v", got)
	}
}
