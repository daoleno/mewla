package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// Many running Works (11-19) read "Delegated Session reported done; awaiting
// provider confirmation" for hours: a provider's passing done hint on a signal
// Turn was projected into next_action, and the projection then read its own
// output back as Brain's note, so the words outlived the hint.
func TestNextActionProseDoesNotOutliveTheStateItDescribed(t *testing.T) {
	now := time.Now()
	st := &lifecycle.State{Attempt: &lifecycle.Attempt{SessionID: "mh-worker"}}
	database := &presentationDatabase{BrainTurns: []TurnRecord{{
		SessionID: "mh-worker", TurnID: "turn:1", WorkID: "w", Status: watcher.TurnRunning, SignalProtocol: true,
		AcceptedAt: now, Hints: []watcher.TurnHint{{Kind: "session.done", At: now}},
	}}}
	stale := Work{NextAction: "Delegated Session reported done; awaiting provider confirmation"}
	status, next, _ := fsmProjectLifecycle(st, database, stale)
	if status != WorkRunning || next != "Wait for the delegated Session." {
		t.Fatalf("projection = %s %q, want running and the plain wait", status, next)
	}
	st.NextAction = "Check the deploy at 14:00."
	if _, next, _ := fsmProjectLifecycle(st, database, stale); next != "Check the deploy at 14:00." {
		t.Fatalf("Brain's own note was not projected: %q", next)
	}
	// A pre-contract Turn keeps its hint prose, which is accurate there.
	database.BrainTurns[0].SignalProtocol = false
	if _, next, _ := fsmProjectLifecycle(&lifecycle.State{Attempt: st.Attempt}, database, Work{}); next != "Delegated Session reported done; awaiting provider confirmation" {
		t.Fatalf("pre-contract hint prose = %q", next)
	}
}
