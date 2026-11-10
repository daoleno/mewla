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

	"github.com/daoleno/mewla/daemon/modelprofiles"
	"github.com/daoleno/mewla/daemon/workerproc"
)

// oldTmuxSession starts a Session in tmux as Mewla before 0.3 did.
func oldTmuxSession(t *testing.T, h *sharedTmuxHarness, name string, labels sessionLabels) string {
	t.Helper()
	tmux := &tmuxBackend{w: h.w}
	at, err := tmux.place("", true, name)
	if err != nil {
		t.Fatal(err)
	}
	id, err := tmux.create(at, backendCreateSpec{name: name, cwd: t.TempDir(), command: "exec sleep 300", labels: labels})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func tmuxPaneExists(h *sharedTmuxHarness, id string) bool {
	out, err := exec.Command("tmux", "-S", h.physical, "list-panes", "-a", "-F", "#{pane_id}").Output()
	return err == nil && strings.Contains("\n"+string(out), "\n"+id+"\n")
}

func TestTmuxSessionMovesToTheHostUnderItsID(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	host := startInProcessHost(t)
	id := oldTmuxSession(t, h, "agent", sessionLabels{delegated: true, executorID: "claude", resourceUnit: "unit-1", resourceOwner: "owner-1"})
	tmuxIO := (&tmuxBackend{w: h.w}).inputIO()
	ledger := sessionInputReceiptLedger{SchemaVersion: sessionInputReceiptLedgerSchema, Entries: []sessionInputReceiptEntry{{Receipt: "r-1", PayloadSHA256: strings.Repeat("ab", 32), Outcome: InputAccepted}}}
	if err := tmuxIO.writeReceiptLedger(h.w.socketPathFor(""), id, ledger); err != nil {
		t.Fatal(err)
	}

	state := t.TempDir()
	cfg := SessionHostConfig{SocketPath: host.socket, Executable: "/nonexistent/mewla", StateDir: state, Version: "t"}
	h.w.SetSessionHost(cfg)
	if err := h.w.SessionHostReady(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := h.w.MoveToHost(id, TmuxMove{Command: "exec sleep 300", Env: []string{"PATH=/usr/bin:/bin", "HOME=" + dir}, Cwd: dir}); err != nil {
		t.Fatal(err)
	}
	if tmuxPaneExists(h, id) {
		t.Fatalf("%s still runs in tmux", id)
	}
	c := h.w.composite()
	info, present, err := c.host.info(id)
	if err != nil || !present || !info.Alive {
		t.Fatalf("host Session %s = %+v, %v, %v", id, info, present, err)
	}
	for key, want := range map[string]string{hostLabelMovedFrom: "tmux", hostLabelName: "agent", hostLabelDelegated: "1", hostLabelExecutor: "claude", hostLabelResourceUnit: "unit-1"} {
		if info.Labels[key] != want {
			t.Errorf("label %s = %q, want %q (%v)", key, info.Labels[key], want, info.Labels)
		}
	}
	moved, err := c.host.inputIO().receiptLedger(hostInputServer, id)
	if err != nil || len(moved.Entries) != 1 || moved.Entries[0].Receipt != "r-1" {
		t.Fatalf("receipts after the move = %+v, %v", moved, err)
	}
	if got := h.w.sessions().terminal(id); got.Backend != "host" {
		t.Fatalf("terminal backend = %q", got)
	}
	countIn := func(w *Watcher) int {
		panes, err := w.sessions().listOwned()
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, pane := range panes {
			if pane.target == id {
				if pane.sessionName != hostSessionGroup {
					t.Fatalf("%s listed from %q", id, pane.sessionName)
				}
				n++
			}
		}
		return n
	}
	if n := countIn(h.w); n != 1 {
		t.Fatalf("%s listed %d times", id, n)
	}
	// A restarted daemon decides whether its Sessions live before it lists
	// anything: its route sweep runs first. The moved Session is the host's
	// from the first question on, never a tmux pane that is gone.
	fresh := func() *Watcher {
		w := New(10 * time.Millisecond)
		w.SetTmuxServer(h.selected, h.scratch)
		w.SetSessionHost(cfg)
		return w
	}
	if presence, err := fresh().ProbeSession(id); err != nil || presence != SessionPresencePresent {
		t.Fatalf("after a restart, before any inventory, %s is %v (%v)", id, presence, err)
	}
	if gone, err := fresh().sessions().resolveAbsence(id, false); err != nil || gone {
		t.Fatalf("after a restart, before any inventory, %s gone = %v (%v)", id, gone, err)
	}
	// After a daemon restart the moved Session is still the host's.
	restarted := New(10 * time.Millisecond)
	restarted.SetTmuxServer(h.selected, h.scratch)
	restarted.SetSessionHost(cfg)
	if n := countIn(restarted); n != 1 {
		t.Fatalf("after a restart %s is listed %d times", id, n)
	}
	if got := restarted.sessions().terminal(id); got.Backend != "host" {
		t.Fatalf("after a restart the terminal backend = %q", got)
	}
	// A restarted tmux server can number a new pane with the moved id; it is
	// never listed as that Session.
	if out, err := exec.Command("tmux", "-S", h.physical, "kill-server").CombinedOutput(); err != nil {
		t.Fatalf("restart tmux: %v %s", err, out)
	}
	bootstrapHarnessServer(t, h.selected)
	for !tmuxPaneExists(h, id) {
		oldTmuxSession(t, h, "reused", sessionLabels{})
	}
	if n := countIn(h.w); n != 1 {
		t.Fatalf("with a tmux pane reusing %s it is listed %d times", id, n)
	}
	// A Session that is not in tmux is refused.
	if err := h.w.MoveToHost(id, TmuxMove{Command: "exec sleep 1", Cwd: dir}); err == nil {
		t.Fatal("a host Session was moved again")
	}
}

func TestTmuxSessionLeftInTmuxIsNoLongerMewlas(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	host := startInProcessHost(t)
	id := oldTmuxSession(t, h, "grok", sessionLabels{})
	state := t.TempDir()
	cfg := SessionHostConfig{SocketPath: host.socket, Executable: "/nonexistent/mewla", StateDir: state, Version: "t"}
	h.w.SetSessionHost(cfg)
	listed := func(w *Watcher) bool {
		panes, err := w.sessions().listOwned()
		if err != nil {
			t.Fatal(err)
		}
		for _, pane := range panes {
			if pane.target == id {
				return true
			}
		}
		return false
	}
	if !listed(h.w) {
		t.Fatalf("%s is not listed before it is left", id)
	}
	h.w.LeaveInTmux(id, "Mewla cannot resume grok")
	if listed(h.w) {
		t.Fatalf("%s is still listed after it was left in tmux", id)
	}
	if !tmuxPaneExists(h, id) {
		t.Fatalf("%s no longer runs in tmux", id)
	}
	if n := h.w.LeftInTmux(); n != 1 {
		t.Fatalf("LeftInTmux = %d", n)
	}
	// It has ended as far as Mewla goes, and Mewla no longer owns it.
	if gone, err := h.w.sessions().resolveAbsence(id, true); err != nil || !gone {
		t.Fatalf("resolveAbsence of a left Session = %v, %v", gone, err)
	}
	if present, owned, err := h.w.sessions().probeOwnership(id); err != nil || present || owned {
		t.Fatalf("probeOwnership of a left Session = %v, %v, %v", present, owned, err)
	}
	restarted := New(10 * time.Millisecond)
	restarted.SetTmuxServer(h.selected, h.scratch)
	restarted.SetSessionHost(cfg)
	if listed(restarted) || restarted.LeftInTmux() != 1 {
		t.Fatalf("after a restart the left Session is listed (%v) or not counted (%d)", listed(restarted), restarted.LeftInTmux())
	}
	if _, err := os.Stat(state + "/session-host/left-in-tmux.json"); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("tmux", "-S", h.physical, "kill-pane", "-t", id).CombinedOutput(); err != nil {
		t.Fatalf("kill %s: %v %s", id, err, out)
	}
	if n := h.w.LeftInTmux(); n != 0 {
		t.Fatalf("LeftInTmux after it ended = %d", n)
	}
}

// Closing the Work of a Session left in tmux only lets go of it: its programs,
// lease and TempDir (maybe its TMUX_TMPDIR) stay while its pane runs, also
// across a daemon restart, and are released once the pane ends.
func TestLeftSessionKeepsItsResourcesUntilItsPaneEnds(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	host := startInProcessHost(t)
	manager := newTestPortableResourceManager(t, "daemon-left")
	spec, err := manager.Prepare(0)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := workerproc.LeasePath(manager.leaseDir, spec.Unit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lease); err != nil {
		t.Fatalf("lease before: %v", err)
	}
	id := oldTmuxSession(t, h, "grok", sessionLabels{delegated: true, resourceUnit: spec.Unit, resourceOwner: spec.Owner})
	cfg := SessionHostConfig{SocketPath: host.socket, Executable: "/nonexistent/mewla", StateDir: t.TempDir(), Version: "t"}
	h.w.resources = manager
	h.w.SetSessionHost(cfg)
	// The first inventories bound its unit before the move left it in tmux.
	manager.Bind(id, spec.Unit)
	h.w.LeaveInTmux(id, "Mewla cannot resume grok")

	held := func(w *Watcher, what string) {
		t.Helper()
		w.releaseEndedLeftInTmux(true)
		if !tmuxPaneExists(h, id) {
			t.Fatalf("%s: %s no longer runs in tmux", what, id)
		}
		if _, err := os.Stat(spec.TempDir); err != nil {
			t.Fatalf("%s: TempDir of a running left Session: %v", what, err)
		}
	}
	if err := h.w.KillSession(id); err != nil {
		t.Fatal(err)
	}
	held(h.w, "after its Work closed")
	restarted := New(10 * time.Millisecond)
	restarted.SetTmuxServer(h.selected, h.scratch)
	restarted.resources = manager
	restarted.SetSessionHost(cfg)
	held(restarted, "after a restart")

	if out, err := exec.Command("tmux", "-S", h.physical, "kill-pane", "-t", id).CombinedOutput(); err != nil {
		t.Fatalf("kill %s: %v %s", id, err, out)
	}
	restarted.releaseEndedLeftInTmux(true)
	if _, err := os.Stat(spec.TempDir); !os.IsNotExist(err) {
		t.Fatalf("TempDir after the pane ended: %v", err)
	}
	if _, err := os.Stat(lease); !os.IsNotExist(err) {
		t.Fatalf("lease after the pane ended: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(cfg.StateDir, "session-host", "left-in-tmux.json"))
	if err != nil || strings.Contains(string(raw), spec.Unit) || !strings.Contains(string(raw), id) {
		t.Fatalf("left-in-tmux.json after release = %s, %v", raw, err)
	}
}

// A Codex that talked to an app-server beside it resumes only once that
// server ended: it holds the conversation for a few seconds after the pane
// closes, and a Codex resumed before then finds it open in another app.
func TestTmuxMoveWaitsForTheCodexAppServer(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	host := startInProcessHost(t)
	dir := t.TempDir()
	socket := filepath.Join(dir, "codex-ctl-1.sock")
	// As Mewla's Codex launch: the server in the background, its pid beside
	// the socket, the TUI in front. This server ends 2s after a hangup.
	server := `sh -c 'trap "sleep 2; exit 0" HUP; while :; do sleep 0.1; done' app-server --listen unix://` + socket
	tmux := &tmuxBackend{w: h.w}
	at, err := tmux.place("", true, "codex")
	if err != nil {
		t.Fatal(err)
	}
	id, err := tmux.create(at, backendCreateSpec{name: "codex", cwd: dir, command: "set +m; " + server + " & echo $! > " + socket + ".pid; exec sleep 300"})
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	waitUntil(t, 10*time.Second, "the app-server's pid", func() bool {
		pid = modelprofiles.CodexControlProcess(socket)
		return pid > 0
	})
	h.w.SetSessionHost(SessionHostConfig{SocketPath: host.socket, Executable: "/nonexistent/mewla", StateDir: t.TempDir(), Version: "t"})
	if err := h.w.SessionHostReady(); err != nil {
		t.Fatal(err)
	}
	resumed := filepath.Join(dir, "resumed")
	move := TmuxMove{
		Command:      "if kill -0 " + strconv.Itoa(pid) + " 2>/dev/null; then echo open; else echo ended; fi > " + resumed + "; exec sleep 300",
		Env:          []string{"PATH=/usr/bin:/bin", "HOME=" + dir},
		Cwd:          dir,
		CodexControl: socket,
	}
	if err := h.w.MoveToHost(id, move); err != nil {
		t.Fatal(err)
	}
	var got string
	waitUntil(t, 10*time.Second, "the resumed program", func() bool {
		raw, err := os.ReadFile(resumed)
		got = strings.TrimSpace(string(raw))
		return err == nil && got != ""
	})
	if got != "ended" {
		t.Fatalf("the resumed Codex started while its app-server ran (%q)", got)
	}
	if _, err := os.Stat(socket + ".pid"); !os.IsNotExist(err) {
		t.Fatalf("the app-server's pid file is left: %v", err)
	}
}
