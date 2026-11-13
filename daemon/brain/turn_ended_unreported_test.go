package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// A Worker whose provider turn ended (Claude's Stop hook, the transcript's end
// of turn) without its progress report read "Delegated provider activity
// running" and its Work "running" until the lease expired (owner 2026-11-13).
// The turn now says it ended without a report, and the Work waits on that
// report; a late report still settles the turn.
func TestSignalTurnEndedWithoutReportNoLongerReadsAsRunning(t *testing.T) {
	now := time.Date(2026, 10, 24, 4, 46, 0, 0, time.UTC)
	turn := runningSignalTurn(watcher.TurnRunning)
	turn.Summary = "Delegated provider activity running"
	mutation, err := reduceTurnFact(turn, signalProviderFact("done", signalTestDigest, time.Second), now)
	if err != nil {
		t.Fatal(err)
	}
	if mutation.status != watcher.TurnRunning {
		t.Fatalf("a provider end settled the signal turn: %s", mutation.status)
	}
	const ended = "Turn ended without a progress report; waiting for the Worker's report"
	if mutation.summary != ended {
		t.Fatalf("turn summary = %q, want %q", mutation.summary, ended)
	}

	recorded := *turn
	recorded.Summary = ended
	st := &lifecycle.State{Attempt: &lifecycle.Attempt{SessionID: "%1", Delegated: true, TurnToken: "turn:signal"}}
	status, next, waitFor := fsmProjectLifecycle(st, &presentationDatabase{BrainTurns: []TurnRecord{recorded}}, Work{})
	if status != WorkWaiting || next != "The Worker's turn ended without a progress report; Brain is told if none follows." || waitFor != "Session %1" {
		t.Fatalf("Work = %s / %q / %q", status, next, waitFor)
	}
	// While the provider is still working, the Work runs.
	recorded.Summary = "Delegated provider activity running"
	if status, _, _ := fsmProjectLifecycle(st, &presentationDatabase{BrainTurns: []TurnRecord{recorded}}, Work{}); status != WorkRunning {
		t.Fatalf("a running turn's Work = %s", status)
	}
}

// Once the stall supervisor tells Brain (turn lost, worker_stalled: stopped),
// the Work names what happened instead of the generic lost-turn prose.
func TestStoppedWithoutReportWorkSaysSo(t *testing.T) {
	st := &lifecycle.State{LastSummary: "worker_stalled: stopped", Review: &lifecycle.ReviewState{Reason: "turn_lost"}}
	status, next, _ := fsmProjectLifecycle(st, &presentationDatabase{}, Work{})
	if status != WorkNeedsInput || next != "The Worker's turn ended without a progress report; review its result. Input will not be replayed." {
		t.Fatalf("Work = %s / %q", status, next)
	}
	st.LastSummary = "worker_stalled: idle"
	if _, next, _ := fsmProjectLifecycle(st, &presentationDatabase{}, Work{}); next != "Inspect the delegated Session outcome and exit evidence; input will not be replayed." {
		t.Fatalf("another lost turn = %q", next)
	}
}
