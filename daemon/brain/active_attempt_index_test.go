package brain

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

// The indexed validation must answer exactly what the scanning one did.
// (The reference below is the implementation before the indexes.)

func referenceValidateActiveAttempts(database presentationDatabase) error {
	activeByWork := map[string]map[string]struct{}{}
	for _, turn := range database.BrainTurns {
		if watcher.TurnTerminal(turn.Status) || isHostHandlingTurn(turn) {
			continue
		}
		index := workIndex(database.BrainWork, turn.WorkID)
		if index < 0 {
			continue
		}
		item := database.BrainWork[index]
		if item.Status == WorkDone || item.Status == WorkCancelled {
			continue
		}
		attemptSessionID := strings.TrimSpace(item.AttemptSessionID)
		if turn.SessionID != attemptSessionID {
			state := reduceWorkProgressState(database, item)
			// The canonical Turn reducer may explicitly relinquish a blocked or
			// stale Attempt while retaining its exact Turn as lifecycle evidence.
			// Ready attention, a typed wait, or another canonical Attempt
			// then owns progress; exact continue may promote this same active
			// Session again.
			relinquished := referenceRelinquishmentEvidence(database, item.ID, turn)
			if !relinquished && !state.Ready && (attemptSessionID != "" || !state.Waiting) {
				return fmt.Errorf("brain_turns: active Session %q is not the active Attempt of Work %q", turn.SessionID, item.ID)
			}
			// Once ready attention, a typed wait, or explicit Session-result
			// evidence has replaced execution authority, another Turn is durable
			// lifecycle evidence only. An unrelated Turn beside an active Attempt still
			// fails above unless its own result Event proves relinquishment.
			continue
		}
		if activeByWork[item.ID] == nil {
			activeByWork[item.ID] = map[string]struct{}{}
		}
		activeByWork[item.ID][turn.SessionID] = struct{}{}
		if len(activeByWork[item.ID]) > 1 {
			return fmt.Errorf("brain_work: Work %q has more than one active execution Session", item.ID)
		}
	}
	return nil
}

func referenceRelinquishmentEvidence(database presentationDatabase, workID string, turn TurnRecord) bool {
	workID = strings.TrimSpace(workID)
	sessionID := strings.TrimSpace(turn.SessionID)
	if workID == "" || sessionID == "" {
		return false
	}
	for _, event := range database.BrainWorkEvents {
		if event.WorkID == workID && strings.TrimSpace(event.SourceName) == sessionID &&
			isProjectedWorkResultEvent(event.Kind) &&
			strings.TrimSpace(event.DedupeKey) == sessionTurnEventDedupeKey(sessionID, turn.TurnID, event.Kind) {
			return true
		}
	}
	// A delegated Session is reusable. Once that same Session has a later
	// accepted Turn for the Work, the older nonterminal row is historical
	// lifecycle evidence even if the provider never emitted a terminal fact for
	// it. The exact newer admission is sufficient: a result from merely an older
	// Turn must not relinquish a later rogue Turn.
	for _, candidate := range database.BrainTurns {
		if candidate.WorkID == workID &&
			strings.TrimSpace(candidate.SessionID) == sessionID &&
			candidate.TurnID != turn.TurnID && candidate.AcceptedAt.After(turn.AcceptedAt) {
			return true
		}
	}
	// A canonically resolved loss review for this exact Turn is durable
	// lifecycle evidence that the Turn no longer owns execution, even when the
	// provider session stayed alive and no terminal turn fact was ever
	// observed (for example sweep lease-expiry escalation). The handled review
	// already replaced execution authority; without this excuse the projection
	// that closed the review cannot persist, so canonical state advances while
	// the presentation keeps a ghost open review. Open loss reviews are Ready
	// attention and never reach this excuse. Identity is the exact canonical
	// payload: the loss projection carries the full Turn token as its
	// PayloadRef when no provider evidence exists, and provider evidence is
	// already covered by the result-event rule above. Substrings never confer
	// authority.
	turnID := strings.TrimSpace(turn.TurnID)
	if turnID == "" {
		return false
	}
	for _, event := range database.BrainWorkEvents {
		if event.WorkID != workID || event.HandledAt == nil {
			continue
		}
		switch strings.TrimSpace(event.Kind) {
		case "turn_lost", "lease_expired":
		default:
			continue
		}
		if strings.TrimSpace(event.PayloadRef) == turnID {
			return true
		}
	}
	return false
}

func TestIndexedActiveAttemptValidationMatchesTheScan(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	pick := func(values ...string) string { return values[rng.Intn(len(values))] }
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	kinds := []string{"session.done", "session.failed", "turn_lost", "lease_expired", "note", " session.done "}
	statuses := []watcher.TurnStatus{"running", "done", "failed", "waiting"}
	refused, accepted, excused := 0, 0, 0
	for round := 0; round < 3000; round++ {
		var database presentationDatabase
		for w := 0; w < 1+rng.Intn(3); w++ {
			item := Work{ID: fmt.Sprintf("w%d", w), Status: WorkStatus(pick("open", "running", "done", "cancelled")), AttemptSessionID: pick("", "s0", "s1", " s1")}
			database.BrainWork = append(database.BrainWork, item)
		}
		for e := 0; e < rng.Intn(6); e++ {
			turn := pick("t0", "t1", "t2")
			session := pick("s0", "s1", "s2")
			kind := pick(kinds...)
			dedupe := pick(sessionTurnEventDedupeKey(session, turn, kind), "x", " "+sessionTurnEventDedupeKey(session, turn, kind)+" ")
			event := WorkEvent{ID: fmt.Sprintf("e%d", e), WorkID: pick("w0", "w1", "w2"), Kind: kind, SourceName: pick(session, " "+session), DedupeKey: dedupe, PayloadRef: pick(turn, " "+turn, "zz")}
			if rng.Intn(2) == 0 {
				at := base
				event.HandledAt = &at
			}
			database.BrainWorkEvents = append(database.BrainWorkEvents, event)
		}
		for n := 0; n < 1+rng.Intn(6); n++ {
			database.BrainTurns = append(database.BrainTurns, TurnRecord{
				SessionID: pick("s0", "s1", "s2", " s1"), TurnID: pick("t0", "t1", "t2", " t1"), WorkID: pick("w0", "w1", "w2", "w9"),
				Status: statuses[rng.Intn(len(statuses))], AcceptedAt: base.Add(time.Duration(rng.Intn(4)) * time.Minute), HostHandling: rng.Intn(5) == 0,
			})
		}
		want, got := referenceValidateActiveAttempts(database), validateActiveAttempts(database)
		if (want == nil) != (got == nil) || (want != nil && want.Error() != got.Error()) {
			t.Fatalf("round %d: scan=%v indexed=%v\n%+v", round, want, got, database)
		}
		if want != nil {
			refused++
		} else {
			accepted++
		}
		for _, turn := range database.BrainTurns {
			for _, w := range []string{"w0", "w1", "w2", " w1", ""} {
				a, b := referenceRelinquishmentEvidence(database, w, turn), workTurnHasRelinquishmentEvidence(database, w, turn)
				if a != b {
					t.Fatalf("round %d work %q turn %+v: scan=%v indexed=%v", round, w, turn, a, b)
				}
				if a {
					excused++
				}
			}
		}
	}
	if refused < 100 || accepted < 100 || excused < 100 {
		t.Fatalf("the generator must reach every outcome: refused=%d accepted=%d excused=%d", refused, accepted, excused)
	}
}
