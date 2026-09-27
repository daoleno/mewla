package watcher

import (
	"github.com/daoleno/zen/daemon/classifier"
	"testing"
	"time"
)

func TestDelegatedApprovalCannotClaimRunning(t *testing.T) {
	now := time.Now()
	w := &classifier.Worker{LastProgressAt: &now, LeaseSeconds: 300}
	turn := TurnSnapshot{Status: TurnRunning, Summary: "working"}
	activity := classifier.NewCodexActivityAdapter().Infer(classifier.ActivityInput{PaneContent: "OpenAI Codex\nWould you like to run the following command?\n1. Yes\n2. No\nWorking (esc to interrupt)"})
	state, summary := projectDelegatedActivity(w, turn, activity)
	if state != classifier.StateBlocked || !w.NeedsAttention || w.LeaseSeconds != 0 || summary != "Waiting for Codex approval" {
		t.Fatalf("%s %s %+v", state, summary, w)
	}
	state, _ = projectDelegatedActivity(w, turn, classifier.ActivitySignal{})
	if state != classifier.StateRunning || w.NeedsAttention {
		t.Fatal("approval gate did not clear")
	}
	state, _ = projectDelegatedActivity(w, TurnSnapshot{Status: TurnDone}, activity)
	if state != classifier.StateDone {
		t.Fatal("approval overrode settlement")
	}
	state, _ = projectDelegatedTurn(w, TurnSnapshot{Status: TurnAdmitted})
	if state != classifier.StateUnknown {
		t.Fatal("unknown delivery reported as running")
	}
}
