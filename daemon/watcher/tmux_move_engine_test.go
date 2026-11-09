//go:build ghosttyvt && cgo && (linux || darwin)

package watcher

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
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
