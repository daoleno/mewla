package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

// A signal Turn's own check-in usually reaches the ledger before the watcher
// first reads the provider transcript, so the turn is Running with only its
// payload digest recorded. The provider Activity that carried the exact input
// must still bind it; otherwise every later provider fact is unbound evidence.

const signalTestDigest = "9cb6449628d71f0e92108097852575d47076e4f9ca87a7c96ae8215051791332"

func runningSignalTurn(status watcher.TurnStatus) *TurnRecord {
	accepted := time.Date(2026, 10, 24, 4, 43, 4, 0, time.UTC)
	return &TurnRecord{
		SessionID: "%1", TurnID: "turn:signal", WorkID: "work", Status: status,
		Receipt: "turn:signal", PayloadSHA256: signalTestDigest, SignalProtocol: true,
		Admission:  watcher.TurnAdmission{SHA256: signalTestDigest},
		AcceptedAt: accepted, LeaseDeadline: accepted.Add(time.Hour),
	}
}

func signalProviderFact(kind, sha string, startedAfter time.Duration) watcher.TurnFact {
	accepted := time.Date(2026, 10, 24, 4, 43, 4, 0, time.UTC)
	return watcher.TurnFact{
		SessionID: "%1", TurnID: "turn:signal", Class: watcher.EvidenceProvider, Kind: kind,
		SourceID: "provider\x00%1\x00stream\x00activity-1\x0042", Cursor: 42,
		Admission: watcher.TurnAdmission{
			Stream: "claude_code_transcript\x00s\x00/t.jsonl", ID: "s:u:user", Cursor: 42,
			SHA256: sha, At: accepted.Add(startedAfter),
		},
		ActivityID: "s:activity:u", StartedAt: accepted.Add(startedAfter),
		SettledAt: accepted.Add(startedAfter + time.Minute), At: accepted.Add(startedAfter + time.Minute),
		Summary: "Delegated provider activity " + kind,
	}
}

func TestRunningSignalTurnAdoptsTheActivityThatCarriedItsInput(t *testing.T) {
	now := time.Date(2026, 10, 24, 4, 44, 0, 0, time.UTC)
	mutation, err := reduceTurnFact(runningSignalTurn(watcher.TurnRunning), signalProviderFact("running", signalTestDigest, time.Second), now)
	if err != nil {
		t.Fatal(err)
	}
	if mutation.admission.Empty() || mutation.activityID != "s:activity:u" || !mutation.recordAdmission {
		t.Fatalf("running provider fact did not adopt the Activity: %+v", mutation)
	}
	if mutation.eventKind != "session.running" || mutation.eventActionable {
		t.Fatalf("adoption event = %q actionable=%v", mutation.eventKind, mutation.eventActionable)
	}
}

func TestRunningSignalTurnIgnoresAnotherInputsActivity(t *testing.T) {
	now := time.Date(2026, 10, 24, 4, 44, 0, 0, time.UTC)
	mutation, err := reduceTurnFact(runningSignalTurn(watcher.TurnRunning), signalProviderFact("running", strings.Repeat("a", 64), time.Second), now)
	if err != nil {
		t.Fatal(err)
	}
	if !mutation.admission.Empty() || mutation.activityID != "" || mutation.eventKind == "session.running" {
		t.Fatalf("a different input's Activity adopted the turn: %+v", mutation)
	}
	// An Activity that began before the turn was accepted never adopts it.
	mutation, err = reduceTurnFact(runningSignalTurn(watcher.TurnRunning), signalProviderFact("running", signalTestDigest, -time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	if !mutation.admission.Empty() || mutation.eventKind == "session.running" {
		t.Fatalf("a stale Activity adopted the turn: %+v", mutation)
	}
}

func TestBlockedSignalTurnIsNotAdopted(t *testing.T) {
	now := time.Date(2026, 10, 24, 4, 44, 0, 0, time.UTC)
	mutation, err := reduceTurnFact(runningSignalTurn(watcher.TurnBlocked), signalProviderFact("running", signalTestDigest, time.Second), now)
	if err != nil {
		t.Fatal(err)
	}
	if !mutation.admission.Empty() || mutation.status != "" {
		t.Fatalf("a blocked turn must keep waiting for Brain's answer: %+v", mutation)
	}
}

// A provider Activity ends whenever the model stops answering — a background
// wake or an automatic retry resumes it — so on a signal Turn even a bound
// provider terminal is evidence, never the settlement.
func TestSignalTurnProviderTerminalIsEvidenceOnly(t *testing.T) {
	now := time.Date(2026, 10, 24, 4, 50, 0, 0, time.UTC)
	for _, kind := range []string{"done", "failed"} {
		t.Run(kind, func(t *testing.T) {
			// Unbound (hash-only admission, first fact is the terminal): adopts
			// the identity but does not settle.
			mutation, err := reduceTurnFact(runningSignalTurn(watcher.TurnRunning), signalProviderFact(kind, signalTestDigest, time.Second), now)
			if err != nil {
				t.Fatal(err)
			}
			if mutation.status != watcher.TurnRunning || mutation.eventActionable || mutation.settledAt != nil {
				t.Fatalf("adopted provider %s settled the signal turn: %+v", kind, mutation)
			}
			if mutation.admission.Empty() || mutation.hint == nil || mutation.hint.Kind != "session."+kind {
				t.Fatalf("adopted provider %s left no identity or hint: %+v", kind, mutation)
			}
			// Already bound: a later terminal on the same turn is still evidence.
			bound := runningSignalTurn(watcher.TurnRunning)
			bound.Admission = signalProviderFact("running", signalTestDigest, time.Second).Admission
			bound.ActivityID = "s:activity:u"
			mutation, err = reduceTurnFact(bound, signalProviderFact(kind, signalTestDigest, time.Second), now)
			if err != nil {
				t.Fatal(err)
			}
			if mutation.status != "" || mutation.eventActionable || mutation.settledAt != nil || mutation.hint == nil {
				t.Fatalf("bound provider %s settled the signal turn: %+v", kind, mutation)
			}
		})
	}
}

// Provider-native (non-signal) turns keep settling on a bound provider terminal.
func TestNonSignalTurnStillSettlesOnBoundProviderTerminal(t *testing.T) {
	now := time.Date(2026, 10, 24, 4, 50, 0, 0, time.UTC)
	turn := runningSignalTurn(watcher.TurnRunning)
	turn.SignalProtocol = false
	turn.Admission = signalProviderFact("running", signalTestDigest, time.Second).Admission
	turn.ActivityID = "s:activity:u"
	mutation, err := reduceTurnFact(turn, signalProviderFact("done", signalTestDigest, time.Second), now)
	if err != nil {
		t.Fatal(err)
	}
	if mutation.status != watcher.TurnDone || !mutation.eventActionable || mutation.eventKind != "session.done" {
		t.Fatalf("bound provider done = %+v", mutation)
	}
}
