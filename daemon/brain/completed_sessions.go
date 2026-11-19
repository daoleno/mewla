package brain

import (
	"sort"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// CompletedOwnedTurns is the cleanup set: the current delegated Turn of each
// Session whose Work is closed and whose Session has not been reclaimed yet.
// The Turn need not have reported an end: a Worker that stopped without a
// report, was released as lost, or had its Work closed under it is in the set
// too, and the teardown itself waits until such a Session sits idle at its
// prompt (watcher.ErrSessionNotIdle). A Turn leaves the set once
// MarkTurnsReclaimed records its finished teardown, so the set does not grow
// with history.
func (s *Store) CompletedOwnedTurns(workID string) ([]watcher.TurnSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	// Only each Session's current Turn can be reclaimed. They are picked in
	// one walk (in currentTurnForSession's order), so the set costs one pass
	// over the Turns however long their history is.
	current := map[string]TurnRecord{}
	for _, turn := range database.BrainTurns {
		best, seen := current[turn.SessionID]
		if !seen || turn.AcceptedAt.After(best.AcceptedAt) ||
			(turn.AcceptedAt.Equal(best.AcceptedAt) && turn.TurnID > best.TurnID) {
			current[turn.SessionID] = turn
		}
	}
	var result []watcher.TurnSnapshot
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
		if st == nil || !st.Status.Terminal() {
			continue
		}
		result = append(result, turn.snapshot())
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SessionID < result[j].SessionID })
	return result, nil
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
