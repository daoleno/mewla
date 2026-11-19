package brain

import (
	"context"
	"log"
	"strings"
	"time"
)

const (
	// undecidedAfter is how long a result may wait for Brain's decision before
	// Brain is woken for it (client core/work/surface BRAIN_WORK_STUCK_AFTER_MS).
	undecidedAfter = 24 * time.Hour
	// parkedAfter is how long a Work may wait on the person with nothing asked
	// before it goes back to Brain: the person is never shown such a wait.
	parkedAfter   = 2 * time.Hour
	undecidedTick = 10 * time.Minute
	// UndecidedEventKind is the event that wakes Brain for a decision nobody took.
	UndecidedEventKind = "result.undecided"
)

const askHow = "ask the person with mewla brain work update -question \"<one line>\" -choice \"<a>\" -choice \"<b>\" -recommend \"<your pick>\""

const undecidedSummary = "This Work's result has waited over a day with no decision, and the person is not asked. " +
	"Settle it now: close it, send a follow-up, or " + askHow + "."

const parkedSummary = "This Work has waited on the person for over two hours with nothing asked, so they see a quiet wait and do nothing. " +
	"Decide it yourself, or " + askHow + "."

// RunUndecidedNudge wakes Brain, once per wait, for a result that has waited
// a day with no decision. An undecided result is Brain's to settle, never a
// red dot for the person (owner 2026-11-19: FreerideOpsBatch sat a day after
// its result and reached the phone as Needs you with nothing asked).
func (s *Service) RunUndecidedNudge(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	ticker := time.NewTicker(undecidedTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.nudgeUndecided(s.nowUTC())
		}
	}
}

// nudgeUndecided raises one result.undecided event on each Work whose
// decision nobody takes, keyed by when its wait began so it fires once. It is
// raised as the producer the Work waits on, so a wait parked on the person
// ends and the Work goes back to Brain with the event.
func (s *Service) nudgeUndecided(now time.Time) int {
	snapshot, err := s.Snapshot()
	if err != nil {
		return 0
	}
	delivered := 0
	for _, work := range snapshot.CurrentWork {
		parked, ok := undecided(work, now)
		if !ok {
			continue
		}
		event := WorkEvent{
			WorkID: work.ID, Kind: UndecidedEventKind, SourceName: "Mewla", Actionable: true,
			DedupeKey: "undecided:" + work.AttentionSince.UTC().Format(time.RFC3339Nano), Summary: undecidedSummary,
		}
		if parked {
			event.DedupeKey += ":" + work.Wake.Ref
			event.Summary = parkedSummary
		}
		_, opened, err := s.RaiseWorkEvent(event)
		switch {
		case err != nil:
			log.Printf("brain undecided Work %s: %v", work.ID, err)
		case opened:
			delivered++
		}
		// Not opened: Brain has this Work's earlier event, or it cannot take one
		// now; the key stays used, so it is not raised again for this wait.
	}
	return delivered
}

// undecided is a decision nobody takes, with nothing asked and no Worker on
// it: a result back, or the person's reply, waiting a day for Brain; or a wait
// parked on the person (a user_input wake) for two hours, which they are
// never shown (parked).
func undecided(work CurrentWork, now time.Time) (parked, ok bool) {
	if work.Status == WorkDone || work.Status == WorkCancelled || work.Status == WorkRunning || strings.TrimSpace(work.Question) != "" {
		return false, false
	}
	if work.AttemptSessionID != "" || work.AttentionState != "" || work.AttentionSince == nil {
		return false, false
	}
	waited := now.Sub(*work.AttentionSince)
	if work.Wake != nil {
		parked = work.Wake.Kind == WorkWakeUserInput
		return parked, parked && waited >= parkedAfter
	}
	return false, waited >= undecidedAfter && (work.UnreadResult || work.UserAction != nil && work.UserAction.Kind == WorkUserReply)
}
