package brain

import (
	"sort"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// ReclaimableTurn is one entry of the cleanup set: a delegated Session's
// current Turn and why nothing is owed by it any more.
type ReclaimableTurn struct {
	watcher.TurnSnapshot
	// Open is true when the Turn's Work is still open (a newer Worker took it
	// over, or the Work's wait has ended): its Session is torn down only once
	// it has rested, even when the Turn reported its end.
	Open bool
	// Why names the fact that makes the Session reclaimable, for the log.
	Why string
}

// Reasons a delegated Session has nothing of its own left to do.
const (
	reclaimWorkClosed = "its Work is closed"
	reclaimSuperseded = "a newer Worker took its Work over"
	reclaimWaitEnded  = "its Work's wait has ended"
)

// CompletedOwnedTurns is the cleanup set: the current delegated Turn of each
// Session that has nothing of its own left to do and has not been reclaimed
// yet. A delegated Session exists only while it has something to do:
//
//   - its Work is closed (whether or not the Turn reported an end: a Worker
//     that stopped without a report, was released as lost, ended on a
//     provider error, or had its Work closed under it is in the set too);
//   - its Work is open but another Session holds the Work's newest Turn;
//   - it holds its open Work's newest Turn, the Work is waiting and the wait
//     has ended (wakeEnd). Until then the Session is kept, so a reviewer
//     waiting for the author's push or a Worker waiting on an answer keeps its
//     conversation; an until_done Work keeps its Worker while it is open.
//
// A Session with an Attempt, a prompt on its way in or a review Brain still
// has to settle is never in the set. The teardown itself waits until the
// Session rests (watcher.ErrSessionNotIdle). A Turn leaves the set once
// MarkTurnsReclaimed records its finished teardown, so the set does not grow
// with history.
func (s *Store) CompletedOwnedTurns(workID string) ([]ReclaimableTurn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.nowUTC()
	database := s.readPresentationLocked()
	busy := map[string]bool{}
	s.fsm.Each(func(st *lifecycle.State) {
		if st.Attempt != nil {
			busy[st.Attempt.SessionID] = true
		}
		if admission := st.ActiveAdmission(); admission != nil {
			busy[admission.SessionID] = true
		}
	})
	// Only each Session's current Turn can be reclaimed, and only a Work's
	// newest Turn is its own Worker's. Both are picked in one walk (in
	// currentTurnForSession's order), so the set costs one pass over the
	// Turns however long their history is.
	current := map[string]TurnRecord{}
	newest := map[string]TurnRecord{}
	for _, turn := range database.BrainTurns {
		if best, seen := current[turn.SessionID]; !seen || newerTurn(turn, best) {
			current[turn.SessionID] = turn
		}
		if turn.WorkID != "" && !isHostHandlingTurn(turn) {
			if best, seen := newest[turn.WorkID]; !seen || newerTurn(turn, best) {
				newest[turn.WorkID] = turn
			}
		}
	}
	kept := map[string]*WorkKept{}
	for index := range database.BrainWork {
		if item := &database.BrainWork[index]; item.Kept != nil {
			kept[item.ID] = item.Kept
		}
	}
	var result []ReclaimableTurn
	for _, turn := range current {
		if workID != "" && turn.WorkID != workID {
			continue
		}
		if turn.Reclaimed || busy[turn.SessionID] || isHostHandlingTurn(turn) || !turn.SignalProtocol {
			continue
		}
		st, err := s.fsmState(turn.WorkID)
		if err != nil {
			return nil, err
		}
		if st == nil {
			continue
		}
		entry := ReclaimableTurn{TurnSnapshot: turn.snapshot()}
		switch {
		case st.Status.Terminal():
			entry.Why = reclaimWorkClosed
		case st.Review != nil:
			continue // Brain has yet to settle what this Worker delivered
		case newest[turn.WorkID].SessionID != turn.SessionID:
			entry.Open, entry.Why = true, reclaimSuperseded
		case st.Policy == lifecycle.PolicyUntilDone || st.Wake == nil:
			continue
		default:
			end := wakeEnd(st, kept[turn.WorkID])
			if now.Before(end) {
				continue
			}
			entry.Open, entry.Why = true, reclaimWaitEnded
		}
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SessionID < result[j].SessionID })
	return result, nil
}

// wakeEnd is when an open Work's wait stops holding its Worker's Session: the
// end Brain named (-until, or the default end every named wait gets), the end
// of a keep verdict at this revision, and for any other wait (a question, an
// event the runtime delivers) WaitDefaultEnd after it began.
func wakeEnd(st *lifecycle.State, kept *WorkKept) time.Time {
	end := st.Wake.Since.Add(WaitDefaultEnd)
	if st.Wake.Until != nil {
		end = *st.Wake.Until
	}
	if kept != nil && kept.Revision == st.Revision && kept.Until != nil && kept.Until.After(end) {
		end = *kept.Until
	}
	return end.UTC()
}

func newerTurn(turn, than TurnRecord) bool {
	return turn.AcceptedAt.After(than.AcceptedAt) ||
		(turn.AcceptedAt.Equal(than.AcceptedAt) && turn.TurnID > than.TurnID)
}

// MarkTurnsReclaimed durably records that the Sessions of these exact Turns
// were fully torn down, in one commit. Turns that no longer exist are ignored.
func (s *Store) MarkTurnsReclaimed(turns []watcher.TurnSnapshot) error {
	if len(turns) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return err
	}
	want := make(map[[2]string]struct{}, len(turns))
	for _, turn := range turns {
		want[[2]string{turn.SessionID, turn.TurnID}] = struct{}{}
	}
	changed := false
	for index := range database.BrainTurns {
		turn := &database.BrainTurns[index]
		if _, ok := want[[2]string{turn.SessionID, turn.TurnID}]; ok && !turn.Reclaimed {
			turn.Reclaimed = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.persistPresentationLocked(database)
}
