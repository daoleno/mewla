package brain

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"
)

const (
	// undecidedAfter is how long a result may wait for Brain's decision before
	// Brain is woken for it (client core/work/surface BRAIN_WORK_STUCK_AFTER_MS).
	undecidedAfter = 24 * time.Hour
	undecidedTick  = 10 * time.Minute
	// UndecidedEventKind is the event that wakes Brain for a result nobody decided.
	UndecidedEventKind = "result.undecided"
)

const undecidedSummary = "This Work's result has waited over a day with no decision, and the person is not asked. " +
	"Settle it now: close it, send a follow-up, or ask the person with mewla brain work update -question \"<one line>\" -choice \"<a>\" -choice \"<b>\" -recommend \"<your pick>\"."

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

// nudgeUndecided delivers one result.undecided event to each Work whose
// result waits undecided, keyed by when the wait began so it fires once.
func (s *Service) nudgeUndecided(now time.Time) int {
	snapshot, err := s.Snapshot()
	if err != nil {
		return 0
	}
	delivered := 0
	for _, work := range snapshot.CurrentWork {
		if !undecided(work, now) {
			continue
		}
		_, err := s.DeliverWorkEvent(WorkEvent{
			WorkID: work.ID, Kind: UndecidedEventKind, SourceName: "Mewla",
			DedupeKey: "undecided:" + work.AttentionSince.UTC().Format(time.RFC3339Nano),
			Summary:   undecidedSummary,
		})
		switch {
		case err == nil:
			delivered++
		case errors.Is(err, ErrWorkEventNotDelivered):
			// Brain has this Work's earlier event, or it cannot take one now; the
			// key stays used, so it is not raised again for this wait.
		default:
			log.Printf("brain undecided result %s: %v", work.ID, err)
		}
	}
	return delivered
}

// undecided is a result back, or the person's reply, waiting a day for
// Brain with nothing asked, no Worker on it and nothing it waits on.
func undecided(work CurrentWork, now time.Time) bool {
	if work.Status == WorkDone || work.Status == WorkCancelled || work.Status == WorkRunning || strings.TrimSpace(work.Question) != "" {
		return false
	}
	if work.AttemptSessionID != "" || work.Wake != nil || work.AttentionState == WorkAttentionReviewing {
		return false
	}
	if work.AttentionSince == nil || now.Sub(*work.AttentionSince) < undecidedAfter {
		return false
	}
	return work.UnreadResult || work.UserAction != nil && work.UserAction.Kind == WorkUserReply
}
