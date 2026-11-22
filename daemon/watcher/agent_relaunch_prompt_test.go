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
