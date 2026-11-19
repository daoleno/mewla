package watcher

import (
	"strings"
	"testing"
	"time"
)

// tooLarge is the error as Claude's transcript records it; hookTooLarge as its
// StopFailure hook reports it (the Hub's observation): no status.
var (
	tooLarge = &ProviderActivityError{Code: "invalid_request", HTTPStatus: 413,
		Message: "Request too large (max 32MB). Accumulated images and attachments in the conversation pushed the request over the limit. Run /compact, or double press esc to go back and remove attachments."}
	hookTooLarge = &ProviderActivityError{Code: "invalid_request"}
)

func compactingWatcher(ledger *recoveryLedger, submits *[]string, commands *[]string, compactedAfter time.Duration) *Watcher {
	w := newRecoveryWatcher(ledger, submits)
	var sentAt time.Time
	w.recovery.command = func(sessionID, text string) error {
		*commands = append(*commands, text)
		sentAt = time.Now().UTC()
		return nil
	}
	w.recovery.observe = func(string) (ProviderActivityObservation, bool) {
		observation := ProviderActivityObservation{ProbeState: ProbeStateOK, Error: tooLarge}
		if compactedAfter >= 0 && !sentAt.IsZero() {
			observation.CompactedAt = sentAt.Add(compactedAfter + time.Millisecond)
		}
		return observation, true
	}
	w.recovery.sleep = func(time.Duration) {}
	return w
}

func claudeStall(now, settled time.Time) probedPollWorker {
	item := stalledPollItem(now, settled, "a1", hookTooLarge)
	item.workerSnap.Command = "claude --model claude-opus-5-5"
	return item
}

// 11-18 KidsScoreLook: a Claude Worker stopped on "Request too large … Run
// /compact" went to Brain, who could not send it /compact. Mewla compacts
// and resumes it itself, and says so in the Work's events.
func TestTooLargeClaudeTurnIsCompactedAndResumed(t *testing.T) {
	ledger := &recoveryLedger{fakeTurnLedger: newFakeTurnLedger(), workID: "work-1"}
	submits, commands := []string{}, []string{}
	w := compactingWatcher(ledger, &submits, &commands, 0)
	start := time.Now().UTC()
	w.superviseDelegatedTurns([]probedPollWorker{claudeStall(start.Add(5*time.Second), start)})
	if len(commands) != 1 || commands[0] != "/compact" {
		t.Fatalf("commands %v, want one /compact", commands)
	}
	if len(submits) != 1 || !strings.HasPrefix(submits[0], CompactedContinuationPrompt) || !strings.Contains(submits[0], "--turn-id turn:") {
		t.Fatalf("continuation %v", submits)
	}
	if len(ledger.records) != 2 || !strings.HasPrefix(ledger.records[0].TurnID, compactTurnPrefix) ||
		!strings.Contains(ledger.records[0].Reason, "too large") || !strings.HasPrefix(ledger.records[1].TurnID, "turn:") {
		t.Fatalf("work events %+v", ledger.records)
	}
	if len(ledger.stalls) != 0 {
		t.Fatalf("escalated: %+v", ledger.stalls)
	}

	// The resumed Turn is too large again: compaction did not help; Brain is
	// told once, and no second /compact goes out.
	resumed := claudeStall(start.Add(time.Minute), start.Add(50*time.Second))
	resumed.turn.TurnID = ledger.records[1].TurnID
	for i := 0; i < 3; i++ {
		resumed.now = start.Add(time.Minute + time.Duration(i)*time.Minute)
		w.superviseDelegatedTurns([]probedPollWorker{resumed})
	}
	if len(commands) != 1 || len(ledger.stalls) != 1 || ledger.stalls[0].Reason != "compaction_failed" ||
		!strings.Contains(ledger.stalls[0].Summary, "still too large") {
		t.Fatalf("too large again: commands %v stalls %+v", commands, ledger.stalls)
	}
}

// A compaction the provider fails (its request is refused too) goes to Brain,
// saying so.
func TestFailedCompactionGoesToBrain(t *testing.T) {
	ledger := &recoveryLedger{fakeTurnLedger: newFakeTurnLedger(), workID: "work-1"}
	submits, commands := []string{}, []string{}
	w := compactingWatcher(ledger, &submits, &commands, -1)
	clock := time.Now().UTC()
	w.recovery.sleep = func(d time.Duration) {}
	calls := 0
	w.recovery.observe = func(string) (ProviderActivityObservation, bool) {
		calls++
		if calls > 3 {
			// The compaction's own request fails after the send.
			return ProviderActivityObservation{ProbeState: ProbeStateOK, Error: tooLarge, SettledAt: time.Now().UTC().Add(time.Second)}, true
		}
		return ProviderActivityObservation{ProbeState: ProbeStateOK, Error: tooLarge, SettledAt: clock}, true
	}
	w.superviseDelegatedTurns([]probedPollWorker{claudeStall(clock.Add(5*time.Second), clock)})
	if len(submits) != 0 || len(ledger.stalls) != 1 || ledger.stalls[0].Reason != "compaction_failed" ||
		!strings.Contains(ledger.stalls[0].Summary, "did not compact it") || !strings.Contains(ledger.stalls[0].Summary, "compaction itself failed") {
		t.Fatalf("submits %v stalls %+v", submits, ledger.stalls)
	}
}

// Claude says its /compact failed (the summary request is too large as
// well): Brain hears it at once, in Claude's words, not after compactWait.
func TestCompactionTheAgentReportsFailedGoesToBrainAtOnce(t *testing.T) {
	ledger := &recoveryLedger{fakeTurnLedger: newFakeTurnLedger(), workID: "work-1"}
	submits, commands := []string{}, []string{}
	w := compactingWatcher(ledger, &submits, &commands, -1)
	start := time.Now().UTC()
	looks := 0
	w.recovery.observe = func(string) (ProviderActivityObservation, bool) {
		looks++
		observation := ProviderActivityObservation{ProbeState: ProbeStateOK, Error: tooLarge, SettledAt: start}
		if len(commands) > 0 {
			observation.CompactFailedAt = time.Now().UTC().Add(time.Second)
			observation.CompactFailure = "Compaction failed · attached media exceeds size limits"
		}
		return observation, true
	}
	w.superviseDelegatedTurns([]probedPollWorker{claudeStall(start.Add(5*time.Second), start)})
	if looks > 3 || len(submits) != 0 || len(ledger.stalls) != 1 || ledger.stalls[0].Reason != "compaction_failed" ||
		!strings.Contains(ledger.stalls[0].Summary, "attached media exceeds size limits") {
		t.Fatalf("looks %d submits %v stalls %+v", looks, submits, ledger.stalls)
	}
}

// A restart between the compaction and the continuation sends the
// continuation, not another /compact.
func TestCompactedTurnIsResumedAfterARestart(t *testing.T) {
	ledger := &recoveryLedger{fakeTurnLedger: newFakeTurnLedger(), workID: "work-1"}
	start := time.Now().UTC()
	ledger.records = []TurnRecovery{{WorkID: "work-1", SessionID: "mewla-worker-a:@1", FromTurnID: "turn:original",
		TurnID: compactTurnPrefix + "x", Attempt: 1, Reason: "compact: too large", Outcome: InputAccepted, At: start.Add(10 * time.Second)}}
	submits, commands := []string{}, []string{}
	w := compactingWatcher(ledger, &submits, &commands, 0)
	w.recovery.observe = func(string) (ProviderActivityObservation, bool) {
		return ProviderActivityObservation{ProbeState: ProbeStateOK, Error: tooLarge, CompactedAt: start.Add(20 * time.Second)}, true
	}
	item := claudeStall(start.Add(time.Minute), start)
	w.superviseDelegatedTurns([]probedPollWorker{item})
	if len(commands) != 0 || len(submits) != 1 || !strings.HasPrefix(submits[0], CompactedContinuationPrompt) {
		t.Fatalf("commands %v submits %v", commands, submits)
	}
}

// Codex and the rest keep their own handling: a too-large error still goes to
// Brain as before.
func TestTooLargeIsCompactedOnlyForClaude(t *testing.T) {
	if _, handled := decideCompaction(DefaultTurnRecoveryPolicy(), time.Now(), tooLarge, time.Time{}, "codex", nil); handled {
		t.Fatal("codex compacted by Mewla")
	}
	if _, handled := decideCompaction(DefaultTurnRecoveryPolicy(), time.Now(), &ProviderActivityError{Code: "server_error"}, time.Time{}, "claude", nil); handled {
		t.Fatal("a transient error treated as too large")
	}
	// The Hub names max_output_tokens a context error ("request_too_large");
	// compacting does not help it.
	if _, handled := decideCompaction(DefaultTurnRecoveryPolicy(), time.Now(), &ProviderActivityError{Code: "request_too_large"}, time.Time{}, "claude", nil); handled {
		t.Fatal("an error code without a 413 treated as too large")
	}
}

// The hook's error alone (no status) is not enough, and with no transcript
// nothing is compacted: the stall goes on as any persistent error.
func TestHookErrorWithoutTranscriptIsNotCompacted(t *testing.T) {
	ledger := &recoveryLedger{fakeTurnLedger: newFakeTurnLedger(), workID: "work-1"}
	submits, commands := []string{}, []string{}
	w := compactingWatcher(ledger, &submits, &commands, 0)
	w.recovery.observe = func(string) (ProviderActivityObservation, bool) { return ProviderActivityObservation{}, false }
	start := time.Now().UTC()
	w.superviseDelegatedTurns([]probedPollWorker{claudeStall(start.Add(time.Minute), start)})
	if len(commands) != 0 {
		t.Fatalf("compacted on the hook's error alone: %v", commands)
	}
}
