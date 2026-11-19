package watcher

import (
	"errors"
	"testing"
	"time"
)

const idleClaudePane = "● Done. RESULT.md is written.\n────\n❯ \n────\n  ⏵⏵ bypass permissions on (shift+tab to cycle)"

func TestIdleAtPromptNeedsAPromptAndNothingRunning(t *testing.T) {
	settled := &ProviderActivityObservation{ProbeState: ProbeStateOK, Status: "completed"}
	running := &ProviderActivityObservation{ProbeState: ProbeStateOK, Status: "running"}
	unreadable := &ProviderActivityObservation{ProbeState: "unreadable", Status: "running"}
	for _, tc := range []struct {
		name     string
		content  string
		provider *ProviderActivityObservation
		want     bool
	}{
		{"idle at the prompt, turn settled", idleClaudePane, settled, true},
		{"idle at the prompt, no provider record", idleClaudePane, nil, true},
		{"provider record unreadable falls back to the screen", idleClaudePane, unreadable, true},
		{"provider still running", idleClaudePane, running, false},
		{"background shell in the footer", "● Waiting on the render\n────\n❯ \n────\n  ⏵⏵ bypass permissions on · 1 shell · esc to interrupt", settled, false},
		{"empty screen", "", settled, false},
		{"not at a prompt", "Compiling…\n", settled, false},
	} {
		if got := idleAtPrompt("claude", tc.content, tc.provider); got != tc.want {
			t.Errorf("%s: idle = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSettledIdleNeedsAnUnchangedScreenForTheWholeWindow(t *testing.T) {
	var tracker idleTracker
	start := time.Now()
	if tracker.observe("%1", idleClaudePane, true, start) {
		t.Fatal("a first idle look is not yet settled")
	}
	if tracker.observe("%1", idleClaudePane, true, start.Add(reclaimIdleStable/2)) {
		t.Fatal("settled before the window passed")
	}
	if tracker.observe("%1", idleClaudePane+"● one more line\n", true, start.Add(reclaimIdleStable)) {
		t.Fatal("a changed screen counted as settled")
	}
	changed := start.Add(reclaimIdleStable)
	if !tracker.observe("%1", idleClaudePane+"● one more line\n", true, changed.Add(reclaimIdleStable)) {
		t.Fatal("an unchanged idle screen for the window is settled")
	}
	if tracker.observe("%1", idleClaudePane, false, changed.Add(2*reclaimIdleStable)) {
		t.Fatal("a busy look is never settled")
	}
	if tracker.observe("%1", idleClaudePane, true, changed.Add(2*reclaimIdleStable+time.Second)) {
		t.Fatal("a busy look must restart the window")
	}
}

// A Worker whose turn never reported (its Work closed under it) is torn down
// only once idle: one that is not is left for a later pass, never killed.
func TestCompletedCleanupOfAnUnreportedTurnWaitsForIdle(t *testing.T) {
	for _, status := range []TurnStatus{TurnRunning, TurnUnknown, TurnBlocked} {
		w := New(0)
		backend := useFakeSessionBackend(w, newFakeSessionBackend())
		backend.set("%1", fakeOwned)
		w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
			"%1": {SessionID: "%1", TurnID: "turn", Status: status, SignalProtocol: true},
		}})
		if err := w.KillCompletedSession("%1", "turn"); !errors.Is(err, ErrSessionNotIdle) {
			t.Fatalf("%s: err = %v, want ErrSessionNotIdle", status, err)
		}
		if kills := backend.kills(); len(kills) != 0 {
			t.Fatalf("%s: a Session not proven idle was killed: %v", status, kills)
		}
	}
	// A turn without the delegated contract is still never Mewla's to close.
	w := New(0)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"%1": {SessionID: "%1", TurnID: "turn", Status: TurnDone},
	}})
	if err := w.KillCompletedSession("%1", "turn"); err == nil || errors.Is(err, ErrSessionNotIdle) {
		t.Fatalf("an uncontracted turn: err = %v", err)
	}
}
