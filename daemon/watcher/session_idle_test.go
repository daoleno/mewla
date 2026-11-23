package watcher

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const idleClaudePane = "● Done. RESULT.md is written.\n────\n❯ \n────\n  ⏵⏵ bypass permissions on (shift+tab to cycle)"

// providerErrorPane is the screen a Claude Worker is left at once its turn
// ended on a provider error: the shape of mh-8c3e (11-23), a scheduled action
// that ended on 429s and was never reclaimed because the composer shows a
// placeholder.
const providerErrorPane = "❯ The previous response was cut off by a provider error. Continue the task from where you stopped; don't redo finished steps.\n\n" +
	"● API Error: Request rejected (429) · All credentials for model claude-sonnet-5-5 are cooling down via provider claude (last error: rate_limit_error: This request would exceed your account's\n" +
	"  rate limit. Please try again later.) (reset after 39h47m51s)\n\n" +
	"────────────────────────────────────────\n❯ Resend your message to try again\n────────────────────────────────────────\n" +
	"  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"

func TestRestComesFromTheAgentsRecordAndTheScreenOnlyVetoes(t *testing.T) {
	settledAt := time.Date(2026, 11, 23, 0, 12, 16, 0, time.UTC)
	settled := &ProviderActivityObservation{ProbeState: ProbeStateOK, Status: "completed", SettledAt: settledAt}
	failed := &ProviderActivityObservation{ProbeState: ProbeStateOK, Status: "completed", SettledAt: settledAt,
		Error: &ProviderActivityError{Code: "rate_limit", HTTPStatus: 429}}
	running := &ProviderActivityObservation{ProbeState: ProbeStateOK, Status: "running"}
	unreadable := &ProviderActivityObservation{ProbeState: "unreadable", Status: "running"}
	noTurn := &ProviderActivityObservation{ProbeState: ProbeStateOK}
	draft := "● Done.\n────\n❯ tell me more about\n────\n  ⏵⏵ bypass permissions on (shift+tab to cycle)"
	for _, tc := range []struct {
		name     string
		content  string
		provider *ProviderActivityObservation
		want     bool
		settled  bool
	}{
		{"idle at the prompt, turn settled", idleClaudePane, settled, true, true},
		{"ended on a provider error, the composer shows its placeholder", providerErrorPane, failed, true, true},
		{"provider error placeholder with no readable record", providerErrorPane, unreadable, true, false},
		{"403 placeholder with no readable record", "● Please run /login · API Error: 403\n────\n❯ Once fixed, resend your message\n────\n  ⏵⏵ bypass permissions on (shift+tab to cycle)", unreadable, true, false},
		{"idle at the prompt, no provider record", idleClaudePane, nil, true, false},
		{"provider record unreadable falls back to the screen", idleClaudePane, unreadable, true, false},
		{"a readable record with no turn yet falls back to the screen", idleClaudePane, noTurn, true, false},
		{"a draft in the composer without a record is not idle", draft, nil, false, false},
		{"provider still running", idleClaudePane, running, false, false},
		{"provider still running whatever the composer shows", providerErrorPane, running, false, false},
		{"background shell in the footer", "● Waiting on the render\n────\n❯ \n────\n  ⏵⏵ bypass permissions on · 1 shell · esc to interrupt", settled, false, false},
		{"background shell after a settled turn", "✻ Cooked for 27m 3s · done 19:01 · 1 shell still running\n────\n❯ \n────\n  ⏵⏵ bypass permissions on · 1 shell · ↓ to manage", settled, false, false},
		{"empty screen", "", settled, false, false},
		{"not at a prompt", "Compiling…\n", nil, false, false},
	} {
		rest := restOf("claude --session-id x", tc.content, tc.provider)
		if rest.idle != tc.want || (!rest.settledAt.IsZero()) != tc.settled {
			t.Errorf("%s: idle = %v settled = %v (%s), want %v %v", tc.name, rest.idle, !rest.settledAt.IsZero(), rest.reason, tc.want, tc.settled)
		}
		if !rest.idle && rest.reason == "" {
			t.Errorf("%s: a Session that is kept must say why", tc.name)
		}
	}
}

func TestRestByTheAgentsRecordCountsFromItsEndNotTheScreen(t *testing.T) {
	var tracker idleTracker
	end := time.Now().UTC()
	rest := sessionRest{idle: true, settledAt: end, content: idleClaudePane}
	if tracker.observe("%1", rest, end.Add(reclaimIdleStable/2)) {
		t.Fatal("rested before the grace passed")
	}
	// A screen that keeps changing does not hold back a Session whose agent
	// says its turn ended.
	rest.content += "● a repaint\n"
	if !tracker.observe("%1", rest, end.Add(reclaimIdleStable)) {
		t.Fatal("a turn ended a grace ago is rested")
	}
	if tracker.observe("%1", sessionRest{reason: "its agent's turn is running"}, end.Add(2*reclaimIdleStable)) {
		t.Fatal("a busy look is never rested")
	}
}

func TestRestByTheScreenAloneNeedsItUnchangedForTheWholeWindow(t *testing.T) {
	var tracker idleTracker
	look := func(content string) sessionRest { return sessionRest{idle: true, content: content} }
	start := time.Now()
	if tracker.observe("%1", look(idleClaudePane), start) {
		t.Fatal("a first idle look is not yet settled")
	}
	if tracker.observe("%1", look(idleClaudePane), start.Add(reclaimIdleStable/2)) {
		t.Fatal("settled before the window passed")
	}
	if tracker.observe("%1", look(idleClaudePane+"● one more line\n"), start.Add(reclaimIdleStable)) {
		t.Fatal("a changed screen counted as settled")
	}
	changed := start.Add(reclaimIdleStable)
	if !tracker.observe("%1", look(idleClaudePane+"● one more line\n"), changed.Add(reclaimIdleStable)) {
		t.Fatal("an unchanged idle screen for the window is settled")
	}
	if tracker.observe("%1", sessionRest{}, changed.Add(2*reclaimIdleStable)) {
		t.Fatal("a busy look is never settled")
	}
	if tracker.observe("%1", look(idleClaudePane), changed.Add(2*reclaimIdleStable+time.Second)) {
		t.Fatal("a busy look must restart the window")
	}
}

// A Worker whose turn never reported (its Work closed under it) is torn down
// only once idle: one that is not is left for a later pass, never killed, and
// the wait says why.
func TestCompletedCleanupOfAnUnreportedTurnWaitsForIdle(t *testing.T) {
	for _, status := range []TurnStatus{TurnRunning, TurnUnknown, TurnBlocked} {
		w := New(0)
		backend := useFakeSessionBackend(w, newFakeSessionBackend())
		backend.set("%1", fakeOwned)
		w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
			"%1": {SessionID: "%1", TurnID: "turn", Status: status, SignalProtocol: true},
		}})
		err := w.KillCompletedSession("%1", "turn")
		if !errors.Is(err, ErrSessionNotIdle) || !strings.Contains(err.Error(), ": ") {
			t.Fatalf("%s: err = %v, want ErrSessionNotIdle with its reason", status, err)
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

// An open Work's Session is closed only once it rests, even when its turn
// reported done: a Worker that went on after its report is never cut off.
func TestRestingCleanupWaitsEvenForAReportedTurn(t *testing.T) {
	w := New(0)
	backend := useFakeSessionBackend(w, newFakeSessionBackend())
	backend.set("%1", fakeOwned)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"%1": {SessionID: "%1", TurnID: "turn", Status: TurnDone, SignalProtocol: true},
	}})
	if err := w.KillRestingSession("%1", "turn"); !errors.Is(err, ErrSessionNotIdle) {
		t.Fatalf("err = %v, want ErrSessionNotIdle", err)
	}
	if kills := backend.kills(); len(kills) != 0 {
		t.Fatalf("a Session not proven at rest was killed: %v", kills)
	}
}
