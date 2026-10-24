package brain

import (
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// CompletedOwnedTurns is the cleanup set: the latest exact delegated result of
// an explicitly closed Work whose Session has not been reclaimed yet. A Turn
// leaves the set once MarkTurnsReclaimed records its finished teardown, so the
// set does not grow with history.
func (s *Store) CompletedOwnedTurns(workID string) ([]watcher.TurnSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return nil, err
	}
	busy := map[string]bool{}
	s.fsm.Each(func(st *lifecycle.State) {
		if st.Attempt != nil {
			busy[st.Attempt.SessionID] = true
		}
		if admission := st.ActiveAdmission(); admission != nil {
			busy[admission.SessionID] = true
		}
	})
	var result []watcher.TurnSnapshot
	for _, turn := range database.BrainTurns {
		if workID != "" && turn.WorkID != workID {
			continue
		}
		if turn.Reclaimed || busy[turn.SessionID] || isHostHandlingTurn(turn) || !turn.SignalProtocol ||
			(turn.Status != watcher.TurnDone && turn.Status != watcher.TurnFailed) {
			continue
		}
		st, err := s.fsmState(turn.WorkID)
		if err != nil {
			return nil, err
		}
		if !st.Status.Terminal() {
			continue
		}
		current, found := currentTurnForSession(database, turn.SessionID)
		if found && current.TurnID == turn.TurnID {
			result = append(result, turn.snapshot())
		}
	}
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
