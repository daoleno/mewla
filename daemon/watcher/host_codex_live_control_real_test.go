//go:build ghosttyvt && cgo && (linux || darwin)

package watcher

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHostRealCodexLiveControlNodeWrapperFirstSendOnce is the first-send
// regression proof for the real installed-Codex process shape: a node wrapper
// plus native descendant for each of the two live-control halves (headless
// app-server sibling subtree + --remote TUI subtree) sharing the pane's
// foreground process group. The freshly created Session must prove its target
// provider identity immediately and accept its first Interface message exactly
// once, through the production identity/send path.
func TestHostRealCodexLiveControlNodeWrapperFirstSendOnce(t *testing.T) {
	h := newHostHarness(t)
	wrapper, native, record := buildFakeCodexInstall(t, h.root)
	socket := filepath.Join(h.root, "codex-ctl.sock")
	logPath := filepath.Join(h.root, "app-server.log")
	pidPath := filepath.Join(h.root, "app-server.pid")

	target, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "live-control-first-send",
		Cwd:  h.root,
		Env: map[string]string{
			"MEWLA_TEST_CODEX_NATIVE": native,
			"MEWLA_TEST_CODEX_RECORD": record,
		},
		Command: "set +m; " + wrapper + " app-server --listen unix://" + socket +
			" > " + logPath + " 2>&1 & echo $! > " + pidPath +
			"; exec " + wrapper + " --remote unix://" + socket + " --model gpt-5",
		Detached: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = h.w.KillSession(target)
	})

	// The Session's PID is stable across the exec chain (login shell -> wrapper).
	var panePID int
	waitUntil(t, 10*time.Second, "live-control Session process", func() bool {
		pid, err := h.w.sessions().processRoot(target)
		if err != nil || pid <= 0 {
			return false
		}
		panePID = pid
		return true
	})

	// Wait for the full real tree (both node wrappers and both native
	// descendants in the Session's foreground group), then prove the production
	// resolver picks the native --remote TUI client.
	waitUntil(t, 10*time.Second, "full live-control process tree", func() bool {
		processes := snapshotProcesses()
		if !liveControlProcessTreeComplete(panePID, processes) {
			return false
		}
		authority, ok := resolveForegroundTargetProcess(panePID, processes)
		return ok && authority.command == "codex" && authority.provider.pid != panePID &&
			isCodexTUIClientProcess(authority.provider.args)
	})

	// First Interface message: the exact product path (SendInputWithReceipt
	// -> targetForSession -> submit) must accept once without weakening the
	// fail-closed identity guard.
	payload := "mewla live-control first send proof"
	result, err := h.w.SendInputWithReceiptResult(target, payload, "receipt-live-control-first")
	if err != nil {
		t.Fatalf("first send failed: %v", err)
	}
	if result.Outcome != InputAccepted {
		t.Fatalf("first send outcome = %q, want %q", result.Outcome, InputAccepted)
	}

	// The provider received the message exactly once.
	waitUntil(t, 10*time.Second, "provider receipt of first send", func() bool {
		return countRecordedLines(record, payload) >= 1
	})
	time.Sleep(500 * time.Millisecond)
	if count := countRecordedLines(record, payload); count != 1 {
		t.Fatalf("provider received %d copies of %q, want exactly 1", count, payload)
	}
}

// TestHostRealCodexLiveControlImmediateFirstSendWhileTreeForms drives the
// spawn handoff path (SendInputWhenReadyBudgeted, the exact identity-wait +
// mutation-boundary re-proof used for the initial delegated prompt) starting
// immediately after CreateSession, while the node-wrapper/app-server process
// tree is still forming. The frozen identity must converge on the native TUI
// client and the re-proof must reproduce it, so the first message submits
// exactly once.
func TestHostRealCodexLiveControlImmediateFirstSendWhileTreeForms(t *testing.T) {
	h := newHostHarness(t)
	wrapper, native, record := buildFakeCodexInstall(t, h.root)
	socket := filepath.Join(h.root, "codex-ctl.sock")
	logPath := filepath.Join(h.root, "app-server.log")
	pidPath := filepath.Join(h.root, "app-server.pid")

	launch := "set +m; " + wrapper + " app-server --listen unix://" + socket +
		" > " + logPath + " 2>&1 & echo $! > " + pidPath +
		"; exec " + wrapper + " --remote unix://" + socket + " --model gpt-5"
	target, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "live-control-immediate",
		Cwd:  h.root,
		Env: map[string]string{
			"MEWLA_TEST_CODEX_NATIVE": native,
			"MEWLA_TEST_CODEX_RECORD": record,
		},
		Command:  launch,
		Detached: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = h.w.KillSession(target)
	})

	// Send before the tree is complete: the identity wait must poll through
	// the launcher/app-server transitions and converge on the native TUI.
	// The trailing newline makes this a submit (the same shape the delegated
	// initial handoff submits); a draft without Enter would never be sent.
	payload := "mewla immediate first send proof"
	if err := h.w.SendInputWhenReadyBudgeted(target, launch, payload+"\n", 20*time.Second); err != nil {
		t.Fatalf("immediate first send failed: %v", err)
	}
	waitUntil(t, 10*time.Second, "provider receipt of immediate first send", func() bool {
		return countRecordedLines(record, payload) >= 1
	})
	time.Sleep(500 * time.Millisecond)
	if count := countRecordedLines(record, payload); count != 1 {
		t.Fatalf("provider received %d copies of %q, want exactly 1", count, payload)
	}
}

func countRecordedLines(path, payload string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == payload {
			count++
		}
	}
	return count
}
