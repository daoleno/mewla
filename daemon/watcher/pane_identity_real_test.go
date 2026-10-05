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
	target, err := h.w.CreateSession("", CreateSessionOptions{Name: "env-owner", Command: `printf 'OWN_CONTEXT=%s ID=%s\n' "$ZEN_PANE_TEST_CONTEXT" "$ZEN_WORKER_ID"; exec /bin/sh`, Detached: true, ProgressEnv: true, Env: map[string]string{"ZEN_PANE_TEST_CONTEXT": "private-context"}})
	if err != nil {
		t.Fatal(err)
	}
	waitForHarness(t, "owned context", func() bool {
		return strings.Contains(captureHarnessPane(t, h.selected, target), "OWN_CONTEXT=private-context ID="+target)
	})
	out, err := tmuxHarnessCommand(h.selected, "split-window", "-h", "-t", target, "-P", "-F", "#{pane_id}", `printf 'USER_CONTEXT=%s ID=%s\n' "$ZEN_PANE_TEST_CONTEXT" "$ZEN_WORKER_ID"; exec /bin/sh`).Output()
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

func TestRealTmuxLegacyMigrationPinsEvidenceAndNeverRebinds(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	owned := createHarnessPane(t, h.selected, "zen-worker-legacy", "exec /bin/sh")
	raw, err := tmuxHarnessCommand(h.selected, "display-message", "-p", "-t", owned, "#{session_name}:#{window_id}").Output()
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.TrimSpace(string(raw))
	for _, key := range []string{"zen_worker_created", "zen_worker_delegated"} {
		if out, err := tmuxHarnessCommand(h.selected, "set-option", "-w", "-t", owned, "@"+key, "1").CombinedOutput(); err != nil {
			t.Fatalf("mark legacy: %v %s", err, out)
		}
	}
	if err := tmuxHarnessCommand(h.selected, "set-environment", "-t", "zen-worker-legacy", "ZEN_WORKER_CONTEXT", "private").Run(); err != nil {
		t.Fatal(err)
	}
	out, err := tmuxHarnessCommand(h.selected, "split-window", "-h", "-t", owned, "-P", "-F", "#{pane_id}", "exec /bin/sh").Output()
	if err != nil {
		t.Fatal(err)
	}
	other := strings.TrimSpace(string(out))
	aliases := map[string]string{}
	persisted := false
	save := func(next map[string]string) error { persisted = next[legacy] == owned; return nil }
	generations := map[string][]string{legacy: {sessionInputPaneGeneration(owned)}}
	if err := h.w.MigrateLegacyWorkerPanes(aliases, generations, save); err != nil {
		t.Fatal(err)
	}
	if !persisted || aliases[legacy] != owned {
		t.Fatalf("migration picked active pane: %v", aliases)
	}
	h.w.SetLegacyWorkerIDs(aliases)
	h.w.poll()
	if h.w.CanonicalWorkerID(legacy) != owned || h.w.GetWorker(owned) == nil || h.w.GetWorker(other) != nil {
		t.Fatal("migration did not retain exactly the owned Worker")
	}
	env, err := tmuxHarnessCommand(h.selected, "show-environment", "-t", "zen-worker-legacy").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(env), "ZEN_WORKER_CONTEXT=") {
		t.Fatal("legacy session context remains")
	}
	if err := h.w.KillSession(owned); err != nil {
		t.Fatal(err)
	}
	if err := h.w.MigrateLegacyWorkerPanes(aliases, generations, save); err != nil {
		t.Fatal(err)
	}
	if h.w.HasSession(other) || aliases[legacy] != owned {
		t.Fatal("migration rebound a removed pane")
	}
	t.Logf("legacy=%s pinned=%s active_sibling=%s; restart and removal retain exact binding", legacy, owned, other)
}

func TestRealTmuxLegacyMigrationRefusesAmbiguousSplit(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	owned := createHarnessPane(t, h.selected, "legacy-ambiguous", "exec /bin/sh")
	if err := tmuxHarnessCommand(h.selected, "set-option", "-w", "-t", owned, "@zen_worker_created", "1").Run(); err != nil {
		t.Fatal(err)
	}
	if err := tmuxHarnessCommand(h.selected, "split-window", "-h", "-t", owned, "exec /bin/sh").Run(); err != nil {
		t.Fatal(err)
	}
	err := h.w.MigrateLegacyWorkerPanes(map[string]string{}, nil, func(map[string]string) error { t.Fatal("persisted guessed identity"); return nil })
	if err == nil || !strings.Contains(err.Error(), "cannot prove owned pane") {
		t.Fatalf("ambiguous migration=%v", err)
	}
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
		if !strings.Contains(captureHarnessPane(t, h.selected, pane), "PANE_RECEIPT") {
			t.Fatal("owned pane did not receive its input")
		}
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
	t.Setenv("ZEN_WORKER_ID", "parent-worker")
	t.Setenv("ZEN_BRAIN_CONTEXT", "private-parent-context")
	target, err := h.w.CreateSession("", CreateSessionOptions{Name: "default-env", Command: "exec /bin/sh", Detached: true, ProgressEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	global, err := tmuxHarnessCommand(h.selected, "show-environment", "-g").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(global), "ZEN_WORKER_ID=") || strings.Contains(string(global), "ZEN_BRAIN_CONTEXT=") {
		t.Fatal("server inherited launch context")
	}
	out, err := tmuxHarnessCommand(h.selected, "split-window", "-h", "-t", target, "-P", "-F", "#{pane_id}", `printf 'USER_ID=%s CONTEXT=%s\n' "$ZEN_WORKER_ID" "$ZEN_BRAIN_CONTEXT"; exec /bin/sh`).Output()
	if err != nil {
		t.Fatal(err)
	}
	other := strings.TrimSpace(string(out))
	waitForHarness(t, "clean default-server split", func() bool { return strings.Contains(captureHarnessPane(t, h.selected, other), "USER_ID= CONTEXT=") })
}
