package watcher

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// No backend is needed: stale ownership must fail before any transport IO.
// BDD-007: Given old cleanup waiting for the input lock, when a new turn is
// admitted first, then cleanup rejects the old identity before transport IO.
func TestBDD_007_CompletedCleanupSerializesWithNewInput(t *testing.T) {
	w := New(time.Second)
	ledger := &fakeTurnLedger{turns: map[string]TurnSnapshot{
		"worker": {SessionID: "worker", TurnID: "old", Status: TurnDone, SignalProtocol: true},
	}}
	w.SetTurnLedger(ledger)
	owner := w.sessionInputOwner()
	session := owner.session("worker")
	session.mu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); done <- w.KillCompletedSession("worker", "old") }()
	<-started
	// Represents the new admission committed inside the shared input lock.
	ledger.turns["worker"] = TurnSnapshot{SessionID: "worker", TurnID: "new", Status: TurnRunning, SignalProtocol: true}
	session.mu.Unlock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("old cleanup accepted a newer turn")
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not release input serialization")
	}
}

func TestBDD_008_AlreadyReclaimedSessionCleanupIsIdempotent(t *testing.T) {
	// Given exact completed ownership in the ledger but a Session that is gone.
	w := New(0)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"%1": {SessionID: "%1", TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	// When cleanup is reconciled repeatedly, proven absence is success, not an
	// ownership conflict. Other ownership checks remain covered separately.
	for range 2 {
		if err := w.KillCompletedSession("%1", "completed"); err != nil {
			t.Fatalf("already reclaimed: %v", err)
		}
	}
}

// A nonterminal turn of a closed Work is reclaimed once idle (or already
// gone): TestCompletedCleanupOfAnUnreportedTurnWaitsForIdle.
func TestCompletedSessionCleanupRejectsUncontractedTurns(t *testing.T) {
	for _, turn := range []TurnSnapshot{
		{SessionID: "worker", TurnID: "turn", Status: TurnDone},
	} {
		w := New(time.Second)
		w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{"worker": turn}})
		if err := w.KillCompletedSession("worker", "turn"); err == nil {
			t.Fatalf("unsafe cleanup accepted: %+v", turn)
		}
	}
}

func TestBDD_015_UnownedPresentCompletedCleanupIsProtected(t *testing.T) {
	w := New(0)
	backend := useFakeSessionBackend(w, newFakeSessionBackend())
	backend.set("%1", fakeUnowned)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"%1": {SessionID: "%1", TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	for range 2 {
		err := w.KillCompletedSession("%1", "completed")
		if !errors.Is(err, ErrUnownedSession) {
			t.Fatalf("unowned present err=%v", err)
		}
	}
	if kills := backend.kills(); len(kills) != 0 {
		t.Fatalf("unowned target was killed: %v", kills)
	}
}

func TestCompletedCleanupRejectsReusedOwnedGeneration(t *testing.T) {
	w := New(0)
	useFakeSessionBackend(w, newFakeSessionBackend()).set("%1", fakeOwned)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"%1": {
			SessionID: "%1", TurnID: "old", Status: TurnDone, SignalProtocol: true,
			ProcessIdentity: "old-process", PaneGeneration: "old-pane",
		},
	}})
	w.targetProcessResolver = func(string) (targetProcessIdentity, bool) {
		return targetProcessIdentity{Command: "cursor-agent", ProcessID: 99, ProcessStart: 99}, true
	}
	w.SetPollSources(PollSources{PaneGeneration: func(string) string { return "new-pane" }})
	if err := w.KillCompletedSession("%1", "old"); err == nil || !strings.Contains(err.Error(), "generation changed") {
		t.Fatalf("reused identity err=%v", err)
	}
}

func TestCompletedCleanupUnreachableHostIsNotAbsence(t *testing.T) {
	w := New(0)
	useFakeSessionBackend(w, newFakeSessionBackend()).set("%1", fakeUnreachable)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"%1": {SessionID: "%1", TurnID: "done", Status: TurnDone, SignalProtocol: true},
	}})
	err := w.KillCompletedSession("%1", "done")
	if err == nil || errors.Is(err, ErrUnownedSession) {
		t.Fatalf("unreachable classified as success or unowned: %v", err)
	}
	if presence, probeErr := w.ProbeSession("%1"); presence != SessionPresenceUnknown || probeErr == nil {
		t.Fatalf("unreachable probe presence=%v err=%v", presence, probeErr)
	}
}

func TestCompletedCleanupMissingScratchAfterAbsenceSucceeds(t *testing.T) {
	manager := newTestPortableResourceManager(t, "abc123")
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	tempDir, err := manager.createOwnedTempDir(unit)
	if err != nil {
		t.Fatal(err)
	}
	manager.Bind("%1", unit)
	if err := os.RemoveAll(tempDir); err != nil {
		t.Fatal(err)
	}
	w := New(0)
	w.resources = manager
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"%1": {SessionID: "%1", TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	if err := w.KillCompletedSession("%1", "completed"); err != nil {
		t.Fatalf("temp dir already gone: %v", err)
	}
}

func TestCompletedCleanupRaceDoesNotKillAppearingUnownedTarget(t *testing.T) {
	w := New(0)
	backend := useFakeSessionBackend(w, newFakeSessionBackend())
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"%1": {SessionID: "%1", TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- w.KillCompletedSession("%1", "completed")
	}()
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		backend.set("%1", fakeUnowned)
		errs <- w.KillCompletedSession("%1", "completed")
	}()
	wg.Wait()
	close(errs)
	sawUnowned := false
	for err := range errs {
		if err == nil {
			continue
		}
		if errors.Is(err, ErrUnownedSession) {
			sawUnowned = true
			continue
		}
		t.Fatalf("unexpected race err=%v", err)
	}
	if kills := backend.kills(); len(kills) != 0 {
		t.Fatalf("race killed unowned target: %v", kills)
	}
	if !sawUnowned {
		t.Fatal("appearing unowned target was not protected")
	}
}

func TestBDD_019_PresentOwnedCleanupRequiresProvenIdentity(t *testing.T) {
	identity := targetProcessIdentity{
		Command: "cursor-agent", ProcessID: 7, ProcessStart: 7,
		PanePID: 7, PaneStart: 7, ForegroundID: 7, ForegroundStart: 7,
	}
	recorded := delegatedTurnIdentity(identity)
	for _, tc := range []struct {
		name            string
		known           bool
		current         targetProcessIdentity
		pane            string
		wantKill        bool
		wantUnavailable bool
		wantChanged     bool
	}{
		{name: "unknown-process", known: false, pane: "", wantUnavailable: true},
		{name: "unknown-mismatched-pane", known: false, pane: "other-pane", wantUnavailable: true},
		{name: "dead-pane-matching-generation", known: false, pane: "pane-1", wantKill: true},
		{name: "known-mismatch", known: true, current: targetProcessIdentity{Command: "other", ProcessID: 9, ProcessStart: 9}, pane: "pane-1", wantChanged: true},
		{name: "known-reused-pane", known: true, current: identity, pane: "new-pane", wantChanged: true},
		{name: "known-match", known: true, current: identity, pane: "pane-1", wantKill: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
			manager := &fakeDelegatedResourceManager{boundTarget: "%1", boundUnit: unit}
			turn := TurnSnapshot{
				SessionID: "%1", TurnID: "completed", Status: TurnDone, SignalProtocol: true,
				ProcessIdentity: recorded, PaneGeneration: "pane-1",
			}
			ledger := &fakeTurnLedger{turns: map[string]TurnSnapshot{"%1": turn}}
			w := New(0)
			backend := useFakeSessionBackend(w, newFakeSessionBackend())
			backend.set("%1", fakeOwned)
			w.resources = manager
			w.SetTurnLedger(ledger)
			w.targetProcessResolver = func(string) (targetProcessIdentity, bool) {
				return tc.current, tc.known
			}
			w.SetPollSources(PollSources{PaneGeneration: func(string) string { return tc.pane }})
			err := w.KillCompletedSession("%1", "completed")
			after, found, _ := ledger.Turn("%1")
			if !found || after.TurnID != turn.TurnID || after.ProcessIdentity != turn.ProcessIdentity || after.Status != turn.Status {
				t.Fatalf("ledger mutated: %+v", after)
			}
			killed := len(backend.kills()) > 0
			if tc.wantKill {
				if err != nil {
					t.Fatalf("expected successful cleanup: %v", err)
				}
				if !killed || len(manager.released) != 1 {
					t.Fatalf("kill/release missing: killed=%v released=%#v", killed, manager.released)
				}
				return
			}
			if killed || len(manager.released) != 0 {
				t.Fatalf("present owned Session was mutated: killed=%v released=%#v", killed, manager.released)
			}
			if tc.wantUnavailable && !errors.Is(err, ErrOwnershipProbeUnavailable) {
				t.Fatalf("err=%v, want ErrOwnershipProbeUnavailable", err)
			}
			if tc.wantChanged && (err == nil || !strings.Contains(err.Error(), "generation changed")) {
				t.Fatalf("err=%v, want generation changed", err)
			}
		})
	}
}

func TestCompletedCleanupKnownMatchAndAbsentSucceedRepeatedly(t *testing.T) {
	identity := targetProcessIdentity{Command: "cursor-agent", ProcessID: 3, ProcessStart: 3}
	w := New(0)
	backend := useFakeSessionBackend(w, newFakeSessionBackend())
	backend.set("%1", fakeOwned)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"%1": {
			SessionID: "%1", TurnID: "completed", Status: TurnDone, SignalProtocol: true,
			ProcessIdentity: delegatedTurnIdentity(identity), PaneGeneration: "pane-1",
		},
	}})
	w.targetProcessResolver = func(string) (targetProcessIdentity, bool) { return identity, true }
	w.SetPollSources(PollSources{PaneGeneration: func(string) string { return "pane-1" }})
	for range 2 {
		if err := w.KillCompletedSession("%1", "completed"); err != nil {
			t.Fatalf("known match: %v", err)
		}
	}

	absent := New(0)
	absent.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"%1": {
			SessionID: "%1", TurnID: "completed", Status: TurnDone, SignalProtocol: true,
			ProcessIdentity: delegatedTurnIdentity(identity), PaneGeneration: "pane-1",
		},
	}})
	for range 2 {
		if err := absent.KillCompletedSession("%1", "completed"); err != nil {
			t.Fatalf("absent recorded identity: %v", err)
		}
	}
}
