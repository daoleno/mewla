package watcher

import (
	"strings"
	"testing"
	"time"
)

// What a dead agent's conversation says about the prompt that was in flight:
// held only by the prompt's own digest, not held only when a readable
// conversation's newest user message is older than the prompt, else unknown.
func TestDeadPromptDeliveryNeedsEvidenceEitherWay(t *testing.T) {
	sent := time.Date(2026, 11, 22, 3, 0, 0, 0, time.UTC)
	pending := InputAdmission{ProposedTurnID: "turn:p", PayloadSHA256: "digest-p", AcceptedAt: sent, State: InputAdmissionPending, SignalProtocol: true}
	older := ProviderActivityObservation{ID: "msg-1", Status: "completed", AdmissionAt: sent.Add(-time.Minute), InputSHA256: "digest-old", ProbeState: ProbeStateOK}
	cases := []struct {
		name     string
		provider ProviderActivityObservation
		want     PromptDelivery
	}{
		{"its digest", ProviderActivityObservation{ID: "msg-2", Status: "running", AdmissionAt: sent.Add(time.Second), InputSHA256: "digest-p", ProbeState: ProbeStateOK}, PromptHeld},
		{"its digest inside a paste envelope", ProviderActivityObservation{ID: "msg-2", InputSHA256: "wrapped", InputUnwrappedSHA256: "digest-p", ProbeState: ProbeStateOK}, PromptHeld},
		{"only older messages", older, PromptNotHeld},
		{"a newer message that is not it", ProviderActivityObservation{ID: "msg-3", AdmissionAt: sent.Add(time.Second), InputSHA256: "digest-other", ProbeState: ProbeStateOK}, PromptUnknown},
		{"an unreadable conversation", ProviderActivityObservation{ID: "msg-1", AdmissionAt: sent.Add(-time.Minute), ProbeState: ProbeStateUnreadable}, PromptUnknown},
		{"an empty conversation", ProviderActivityObservation{ProbeState: ProbeStateOK}, PromptUnknown},
	}
	for _, tc := range cases {
		if got := deadPromptDelivery(pending, tc.provider); got != tc.want {
			t.Errorf("%s: delivery = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The prompt in flight is a Worker's signal-protocol prompt not yet
// accepted; a Brain claim's delivery or a plain one is not.
func TestInFlightAdmissionIsTheNewestWorkerPrompt(t *testing.T) {
	at := time.Now()
	pending := []InputAdmission{
		{ProposedTurnID: "turn:old", State: InputAdmissionPending, SignalProtocol: true, AcceptedAt: at.Add(-time.Hour)},
		{ProposedTurnID: "turn:new", State: InputAdmissionPending, SignalProtocol: true, AcceptedAt: at},
		{ProposedTurnID: "turn:host", State: InputAdmissionPending, SignalProtocol: true, ClaimToken: "claim", AcceptedAt: at.Add(time.Minute)},
		{ProposedTurnID: "turn:plain", State: InputAdmissionPending, AcceptedAt: at.Add(time.Minute)},
	}
	if got, ok := inFlightAdmission(pending); !ok || got.ProposedTurnID != "turn:new" {
		t.Fatalf("in flight = %+v, %v", got, ok)
	}
	if _, ok := inFlightAdmission(pending[2:]); ok {
		t.Fatal("a Brain delivery or a plain prompt read as a Worker prompt in flight")
	}
}

// A delegated prompt is kept on its Session before it is sent, for its own
// turn only; one over the host's value limit is marked, not kept.
func TestDelegatedPromptIsKeptForItsTurn(t *testing.T) {
	w := New(0)
	useFakeSessionBackend(w, newFakeSessionBackend())
	payload := "render the trailer\n--turn-id turn:p"
	w.keepDelegatedPrompt("s1", payload, delegatedTurnDraft{ID: "turn:p", WorkID: "work-1"})
	kept, ok := w.keptPrompt("s1", "turn:p")
	if !ok || kept.Payload != payload || kept.WorkID != "work-1" || kept.TooLarge {
		t.Fatalf("kept = %+v, %v", kept, ok)
	}
	if _, ok := w.keptPrompt("s1", "turn:other"); ok {
		t.Fatal("a prompt was kept for another turn")
	}
	w.keepDelegatedPrompt("s1", strings.Repeat("x", maxKeptPromptBytes+1), delegatedTurnDraft{ID: "turn:big"})
	if kept, ok := w.keptPrompt("s1", "turn:big"); !ok || !kept.TooLarge || kept.Payload != "" {
		t.Fatalf("large prompt = %+v, %v", kept, ok)
	}
	if _, ok := w.keptPrompt("s1", "turn:p"); ok {
		t.Fatal("the older prompt outlived the newer one")
	}
}

// A Turn whose agent is being, or was just, started again is not a stall:
// its old process is gone on purpose and its continuation is on its way, so
// the stall supervisor sends nothing for it (M4).
func TestStallSupervisorLeavesARelaunchedTurnAlone(t *testing.T) {
	ledger := &recoveryLedger{fakeTurnLedger: newFakeTurnLedger(), workID: "work-1"}
	submits := []string{}
	drop := &ProviderActivityError{Code: "server_error", Message: "API Error: Connection lost mid-response."}
	start := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	w := newRecoveryWatcher(ledger, &submits)
	relaunch := w.agentRelaunch()
	relaunch.now = func() time.Time { return start }
	item := stalledPollItem(start.Add(DefaultTurnRecoveryPolicy().Backoff[0]), start, "a1", drop)
	relaunch.held[item.id] = agentRelaunchHeld{turnID: item.turn.TurnID, until: start.Add(time.Hour)}
	w.superviseDelegatedTurns([]probedPollWorker{item})
	if len(submits) != 0 || len(ledger.records) != 0 {
		t.Fatalf("a relaunched Turn was resumed as a stall: submits %v records %+v", submits, ledger.records)
	}
	// Once the hold ends, the same stall is the supervisor's again.
	delete(relaunch.held, item.id)
	w.superviseDelegatedTurns([]probedPollWorker{item})
	if len(submits) != 1 {
		t.Fatalf("after the hold: submits %v", submits)
	}
}

// Relaunches of different Sessions are spaced; one Session's own retries are
// not held behind themselves.
func TestRelaunchStartsAreStaggeredAcrossSessions(t *testing.T) {
	s := newAgentRelaunchSupervisor()
	at := time.Date(2026, 11, 22, 3, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return at }
	// c's own retry (its backoff already spaced it) starts at once, and the
	// next Session still waits its turn behind c's first slot.
	waits := []time.Duration{s.startSlot("a"), s.startSlot("b"), s.startSlot("c"), s.startSlot("c"), s.startSlot("e")}
	want := []time.Duration{0, agentRelaunchStagger, 2 * agentRelaunchStagger, 0, 3 * agentRelaunchStagger}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits = %v, want %v", waits, want)
		}
	}
	at = at.Add(time.Minute)
	if wait := s.startSlot("d"); wait != 0 {
		t.Fatalf("a start after a quiet minute waited %v", wait)
	}
}
