package watcher

import (
	"strings"
	"testing"
	"time"
)

func TestRealTmuxWorkerOwnsPaneAcrossSplitAndRemoval(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	target, err := h.w.CreateSession("", CreateSessionOptions{Name: "pane-owner", Command: "exec /bin/sh", Detached: true, Delegated: true})
	if err != nil {
		t.Fatal(err)
	}
	io := realSessionInputIO{socketFor: h.w.socketPathFor}
	owned := io.pane(h.selected, target).paneID
	t.Logf("created target=%q owned=%q", target, owned)
	out, err := tmuxHarnessCommand(h.selected, "split-window", "-h", "-t", owned, "-P", "-F", "#{pane_id}", "exec /bin/sh").Output()
	if err != nil {
		t.Fatal(err)
	}
	other := strings.TrimSpace(string(out))
	identity := targetProcessIdentity{Command: "sh", PanePID: 1, PaneStart: 1, ForegroundID: 1, ForegroundStart: 1, ProcessID: 1, ProcessStart: 1}
	resolver := func(string) (targetProcessIdentity, bool) { return identity, true }
	owner := newLedgerSessionInputOwner(io, newFakeTurnLedger())
	turn := testTurnDraft("split-delivery", time.Now().UTC(), identity)
	turn.SignalProtocol = true
	result, err := owner.submitDelegated(target, identity, resolver, "sh", "printf 'OWNED_%s\\n' DELIVERY", turn, scriptedCorrelatedAdmission("scratch delegated payload"))
	if err != nil {
		t.Fatal(err)
	}
	// Transport acceptance precedes execution by the scratch shell.
	waitForHarness(t, "owned pane execution", func() bool {
		return strings.Contains(captureHarnessPane(t, h.selected, owned), "OWNED_DELIVERY")
	})
	a, b := captureHarnessPane(t, h.selected, owned), captureHarnessPane(t, h.selected, other)
	t.Logf("target=%s owned=%s sibling=%s outcome=%s owned_received=%t sibling_received=%t", target, owned, other, result.Outcome, strings.Contains(a, "OWNED_DELIVERY"), strings.Contains(b, "OWNED_DELIVERY"))
	if !strings.Contains(a, "OWNED_DELIVERY") || strings.Contains(b, "OWNED_") {
		t.Fatalf("misdelivery: owned=%q sibling=%q", a, b)
	}
	if out, err := tmuxHarnessCommand(h.selected, "kill-pane", "-t", owned).CombinedOutput(); err != nil {
		t.Fatalf("kill owned: %v %s", err, out)
	}
	turn = testTurnDraft("after-removal", time.Now().UTC(), identity)
	turn.SignalProtocol = true
	result, err = owner.submitDelegated(target, identity, resolver, "sh", "printf REFUSED_PAYLOAD", turn, scriptedCorrelatedAdmission("scratch delegated payload"))
	if err == nil || result.Outcome != InputNotSubmitted {
		t.Fatalf("removed pane: result=%+v error=%v", result, err)
	}
	if strings.Contains(captureHarnessPane(t, h.selected, other), "REFUSED_PAYLOAD") {
		t.Fatal("replacement received input")
	}
	t.Logf("removed owned pane: outcome=%s error=%v; sibling untouched", result.Outcome, err)
}

func TestRealTmuxWorkerEnvironmentAndOwnershipStayInPane(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	target, err := h.w.CreateSession("", CreateSessionOptions{Name: "env-owner", Command: `printf 'OWN_CONTEXT=%s ID=%s\n' "$MEWLA_PANE_TEST_CONTEXT" "$MEWLA_WORKER_ID"; exec /bin/sh`, Detached: true, ProgressEnv: true, Env: map[string]string{"MEWLA_PANE_TEST_CONTEXT": "private-context"}})
	if err != nil {
		t.Fatal(err)
	}
	waitForHarness(t, "owned context", func() bool {
		return strings.Contains(captureHarnessPane(t, h.selected, target), "OWN_CONTEXT=private-context ID="+target)
	})
	out, err := tmuxHarnessCommand(h.selected, "split-window", "-h", "-t", target, "-P", "-F", "#{pane_id}", `printf 'USER_CONTEXT=%s ID=%s\n' "$MEWLA_PANE_TEST_CONTEXT" "$MEWLA_WORKER_ID"; exec /bin/sh`).Output()
	if err != nil {
		t.Fatal(err)
	}
	other := strings.TrimSpace(string(out))
	waitForHarness(t, "user environment", func() bool { return strings.Contains(captureHarnessPane(t, h.selected, other), "USER_CONTEXT= ID=") })
	if content := captureHarnessPane(t, h.selected, other); strings.Contains(content, "private-context") {
		t.Fatalf("user inherited Worker environment: %q", content)
	}
	if h.w.HasSession(other) {
		t.Fatal("sibling inherited ownership")
	}
	h.w.poll()
	if h.w.GetWorker(target) == nil || h.w.GetWorker(other) != nil {
		t.Fatal("inventory followed active pane")
	}
	if err := h.w.KillSession(target); err != nil {
		t.Fatal(err)
	}
	if _, err := tmuxHarnessCommand(h.selected, "capture-pane", "-p", "-t", other).Output(); err != nil {
		t.Fatalf("close removed user pane: %v", err)
	}
	t.Log("pane-local launch environment, inventory, and cleanup preserved the user sibling")
}

func TestRealTmuxWorkersRemainDistinctInOneWindow(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	first, err := h.w.CreateSession("", CreateSessionOptions{Name: "first", Command: "exec /bin/sh", Detached: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.w.CreateSession("", CreateSessionOptions{Name: "second", Command: "exec /bin/sh", Detached: true})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := tmuxHarnessCommand(h.selected, "join-pane", "-h", "-s", second, "-t", first).CombinedOutput(); err != nil {
		t.Fatalf("join: %v %s", err, out)
	}
	h.w.poll()
	if h.w.GetWorker(first) == nil || h.w.GetWorker(second) == nil || first == second {
		t.Fatal("moving a pane changed Worker identity")
	}
	owner := newSessionInputOwner(realSessionInputIO{socketFor: h.w.socketPathFor})
	identity := testSessionInputIdentity("sh")
	for _, pane := range []string{first, second} {
		result, err := owner.submit(pane, identity, fixedSessionInputResolver(identity), "sh", "printf 'PANE_%s\\n' RECEIPT", "same-receipt")
		if err != nil || result.Duplicate {
			t.Fatalf("pane=%s receipt crossed Worker boundary: %+v %v", pane, result, err)
		}
		waitForHarness(t, "owned pane receipt", func() bool {
			return strings.Contains(captureHarnessPane(t, h.selected, pane), "PANE_RECEIPT")
		})
	}
	if err := h.w.KillSession(first); err != nil {
		t.Fatal(err)
	}
	if !h.w.HasSession(second) {
		t.Fatal("closing sibling removed surviving Worker")
	}
	t.Logf("independent Workers %s and %s share one window; receipts, capture and cleanup stay pane-local", first, second)
}

func TestRealTmuxDefaultServerDoesNotInheritWorkerContext(t *testing.T) {
	h := newSharedTmuxHarness(t, true)
	t.Setenv("MEWLA_WORKER_ID", "parent-worker")
	t.Setenv("MEWLA_BRAIN_CONTEXT", "private-parent-context")
	target, err := h.w.CreateSession("", CreateSessionOptions{Name: "default-env", Command: "exec /bin/sh", Detached: true, ProgressEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	global, err := tmuxHarnessCommand(h.selected, "show-environment", "-g").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(global), "MEWLA_WORKER_ID=") || strings.Contains(string(global), "MEWLA_BRAIN_CONTEXT=") {
		t.Fatal("server inherited launch context")
	}
	out, err := tmuxHarnessCommand(h.selected, "split-window", "-h", "-t", target, "-P", "-F", "#{pane_id}", `printf 'USER_ID=%s CONTEXT=%s\n' "$MEWLA_WORKER_ID" "$MEWLA_BRAIN_CONTEXT"; exec /bin/sh`).Output()
	if err != nil {
		t.Fatal(err)
	}
	other := strings.TrimSpace(string(out))
	waitForHarness(t, "clean default-server split", func() bool { return strings.Contains(captureHarnessPane(t, h.selected, other), "USER_ID= CONTEXT=") })
}
