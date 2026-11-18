package watcher

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// ownershipFixture is one delegated Session whose current process is
// current, with a canonical Turn seeded as turn.
func ownershipFixture(t *testing.T, sessionID string, current targetProcessIdentity, turn TurnSnapshot) (*Watcher, *fakeTurnLedger) {
	t.Helper()
	w := New(time.Second)
	w.workers[sessionID] = &classifier.Worker{
		ID: sessionID, Delegated: true, State: classifier.StateRunning,
		Command: current.Command, Attention: "none",
	}
	w.workerOrder = append(w.workerOrder, sessionID)
	w.targetOwnershipResolver = func(string) (bool, error) { return true, nil }
	w.targetProcessResolver = fixedSessionInputResolver(current)
	input := newFakeSessionInputIO()
	input.paneValue.generation = "pane-current"
	w.sessionInput = newSessionInputOwner(input)
	ledger := newFakeTurnLedger()
	turn.SessionID = sessionID
	if turn.TurnID == "" {
		turn.TurnID = sessionID + ":turn:1"
	}
	if turn.PaneGeneration == "" {
		turn.PaneGeneration = "pane-current"
	}
	ledger.seed(sessionID, turn)
	w.SetTurnLedger(ledger)
	return w, ledger
}

// launchReading is the identity a launch records when it reads the agent
// before the kernel has made its command line readable: the same process,
// only the bare command (mh-e30d4425, 2026-11-18).
func launchReading(current targetProcessIdentity) string {
	bare := current
	bare.Command = "claude"
	return delegatedTurnIdentity(bare)
}

func TestSendDuringFirstTurnKeepsControlWhenLaunchReadTheBareCommand(t *testing.T) {
	sessionID := "mh-launch-bare"
	current := testSessionInputIdentity("claude --session-id 24cc4c40-87fd-4ee4-848f-3e12a59bbf56")
	w, ledger := ownershipFixture(t, sessionID, current, TurnSnapshot{
		Status: TurnBlocked, Attention: "user_input", SignalProtocol: true,
		ProcessIdentity: launchReading(current),
	})

	if _, err := w.ResolveOwnedGeneration(sessionID); err != nil {
		t.Fatalf("the Turn's own live process was refused: %v", err)
	}
	if turn := ledger.snapshot(sessionID); turn.ControlState == TurnControlOwnershipLost {
		t.Fatalf("the Turn's own live process was disowned: %+v", turn)
	}
	if len(ledger.applied) != 0 {
		t.Fatalf("facts = %+v, want none", ledger.applied)
	}
}

func TestGatedTurnWhoseOwnProcessIsStillTheSessionsGetsControlBack(t *testing.T) {
	sessionID := "mh-gated-same-process"
	current := testSessionInputIdentity("claude --session-id 24cc4c40-87fd-4ee4-848f-3e12a59bbf56")
	w, ledger := ownershipFixture(t, sessionID, current, TurnSnapshot{
		// What the Worker's own report leaves after a wrong loss: revived,
		// still gated.
		Status: TurnBlocked, Attention: "user_input", SignalProtocol: true,
		ControlState: TurnControlOwnershipLost, ProcessIdentity: launchReading(current),
	})

	if _, err := w.ResolveOwnedGeneration(sessionID); err != nil {
		t.Fatalf("send to a gated Turn whose process is proven = %v", err)
	}
	turn := ledger.snapshot(sessionID)
	if turn.ControlState != TurnControlOwned {
		t.Fatalf("gate not lifted: %+v", turn)
	}
	if len(ledger.applied) != 1 || ledger.applied[0].Kind != TurnFactOwnershipReproved {
		t.Fatalf("facts = %+v, want one %s", ledger.applied, TurnFactOwnershipReproved)
	}
	if projected := w.GetWorker(sessionID); projected == nil || projected.Attention == "ownership_lost" {
		t.Fatalf("projection still says ownership_lost: %+v", projected)
	}
	if _, err := w.ResolveOwnedGeneration(sessionID); err != nil || len(ledger.applied) != 1 {
		t.Fatalf("second send = %v, facts %+v", err, ledger.applied)
	}
}

func TestDifferentProcessIsALossThatSaysWhatChanged(t *testing.T) {
	sessionID := "mh-replaced-process"
	current := testSessionInputIdentity("claude --session-id x")
	recorded := current
	recorded.ProcessID, recorded.ProcessStart = 51, 61
	w, ledger := ownershipFixture(t, sessionID, current, TurnSnapshot{
		Status: TurnRunning, SignalProtocol: true, ProcessIdentity: delegatedTurnIdentity(recorded),
	})

	_, err := w.ResolveOwnedGeneration(sessionID)
	if !errors.Is(err, ErrOwnedGenerationMismatch) || !strings.Contains(err.Error(), "is not the process this turn was given to") {
		t.Fatalf("replaced process = %v", err)
	}
	if len(ledger.applied) != 1 || !strings.Contains(ledger.applied[0].Summary, "pid 50") {
		t.Fatalf("loss fact does not say why: %+v", ledger.applied)
	}
	_, err = w.ResolveOwnedGeneration(sessionID)
	if err == nil || !strings.Contains(err.Error(), "is not the process this turn was given to") {
		t.Fatalf("gated refusal does not say why: %v", err)
	}
	if projected := w.GetWorker(sessionID); projected == nil || !strings.Contains(projected.Summary, "pid 50") {
		t.Fatalf("status does not say why: %+v", projected)
	}
}

func TestSessionNotYetInventoriedIsUndecidedNotLost(t *testing.T) {
	sessionID := "mh-not-listed-yet"
	current := testSessionInputIdentity("claude --session-id x")
	w, ledger := ownershipFixture(t, sessionID, current, TurnSnapshot{
		Status: TurnRunning, SignalProtocol: true, ProcessIdentity: delegatedTurnIdentity(current),
	})
	// A daemon that just started: the session host runs it as Mewla's, the
	// inventory has not listed it yet.
	delete(w.workers, sessionID)
	w.workerOrder = nil

	if _, err := w.ResolveOwnedGeneration(sessionID); !errors.Is(err, ErrOwnershipProbeUnavailable) {
		t.Fatalf("unlisted owned Session = %v, want ErrOwnershipProbeUnavailable", err)
	}
	if turn := ledger.snapshot(sessionID); turn.ControlState == TurnControlOwnershipLost || len(ledger.applied) != 0 {
		t.Fatalf("unlisted owned Session was disowned: %+v facts %+v", turn, ledger.applied)
	}
}

func TestSendToAReclaimedSessionSaysMewlaClosedIt(t *testing.T) {
	sessionID := "mh-reclaimed"
	current := testSessionInputIdentity("claude --session-id x")
	w, ledger := ownershipFixture(t, sessionID, current, TurnSnapshot{
		Status: TurnDone, SignalProtocol: true, Reclaimed: true,
		ProcessIdentity: delegatedTurnIdentity(current),
	})
	delete(w.workers, sessionID)
	w.workerOrder = nil
	w.targetOwnershipResolver = func(string) (bool, error) { return false, nil }

	_, err := w.ResolveOwnedGeneration(sessionID)
	if !errors.Is(err, ErrSessionReclaimed) {
		t.Fatalf("send to a reclaimed Session = %v, want ErrSessionReclaimed", err)
	}
	if strings.Contains(err.Error(), "not found") || len(ledger.applied) != 0 {
		t.Fatalf("reclaimed Session: err %v, facts %+v", err, ledger.applied)
	}
}

func TestProviderWithUnreadCommandLineIsUndecided(t *testing.T) {
	start := time.Unix(1795010762, 0)
	processes := map[int]processInfo{
		100: {pid: 100, ppid: 1, pgid: 100, tpgid: 100, startedAt: start, comm: "claude", args: "[claude]", argsUnread: true},
	}
	if _, ok := resolveForegroundTargetProcess(100, processes); ok {
		t.Fatal("a just-exec'd agent with an unread command line got an identity")
	}
	processes[100] = processInfo{pid: 100, ppid: 1, pgid: 100, tpgid: 100, startedAt: start, comm: "claude", args: "claude --session-id x"}
	authority, ok := resolveForegroundTargetProcess(100, processes)
	if !ok || authority.command != "claude --session-id x" {
		t.Fatalf("readable agent = %+v, %v", authority, ok)
	}
}
