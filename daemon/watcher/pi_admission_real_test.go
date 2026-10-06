package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealPiStartupReadinessAdmission exercises the production CreateSession
// and SendInputWhenReady boundary against the installed Pi binary. It submits
// one inert probe prompt only when explicitly enabled; the owned pane is then
// torn down. Opt-in keeps ordinary unit runs provider-free.
func TestRealPiStartupReadinessAdmission(t *testing.T) {
	if os.Getenv("ZEN_TEST_REAL_PI_ADMISSION") != "1" {
		t.Skip("set ZEN_TEST_REAL_PI_ADMISSION=1 for installed Pi startup evidence")
	}
	h := newSharedTmuxHarness(t, false)
	cwd := t.TempDir()
	sessionPath := filepath.Join(cwd, "pi-session.jsonl")
	command := "pi --session " + shellQuoteForLaunch(sessionPath) + " --no-extensions"
	target, err := h.w.CreateSession("", CreateSessionOptions{
		Cwd: cwd, Command: command, Name: "pi-admission", Detached: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.w.KillSession(target) })
	if !h.w.waitForInputReadyGuarded(h.selected, target, command, 20*time.Second, nil) {
		pane, _, _ := h.w.capturePaneContent(target)
		t.Fatalf("installed Pi did not reach input-ready state; pane:\n%s", pane)
	}
	pane, alive, _ := h.w.capturePaneContent(target)
	if !alive || !isPiInputReady(pane) {
		t.Fatalf("installed Pi admission returned non-ready pane (alive=%v):\n%s", alive, pane)
	}
	if !strings.Contains(strings.ToLower(pane), "escape interrupt") {
		t.Fatalf("installed Pi capture lacks startup chrome:\n%s", pane)
	}
	if err := h.w.SendInputWhenReady(target, command, "Reply with exactly ZEN_PI_ADMISSION_PROBE and nothing else.\n"); err != nil {
		pane, _, _ := h.w.capturePaneContent(target)
		t.Fatalf("fresh Pi pane rejected input admission: %v; pane:\n%s", err, pane)
	}
}
