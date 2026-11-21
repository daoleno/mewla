package brain

import "strings"

// WorkForSession returns the durable Work owned by a delegated Session when
// present: the newest non-terminal Work wins; otherwise the newest terminal one.
func (s *Service) WorkForSession(sessionID string) (Work, bool, error) {
	if s == nil || s.store == nil {
		return Work{}, false, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Work{}, false, nil
	}
	items, err := s.store.ListWork()
	if err != nil {
		return Work{}, false, err
	}
	var best, bestTerminal *Work
	for index := range items {
		item := items[index]
		if strings.TrimSpace(item.AttemptSessionID) != sessionID {
			continue
		}
		if item.Status != WorkDone && item.Status != WorkCancelled {
			if best == nil || item.CreatedAt.After(best.CreatedAt) {
				copy := item
				best = &copy
			}
			continue
		}
		if bestTerminal == nil || item.CreatedAt.After(bestTerminal.CreatedAt) {
			copy := item
			bestTerminal = &copy
		}
	}
	if best != nil {
		return *best, true, nil
	}
	if bestTerminal != nil {
		return *bestTerminal, true, nil
	}
	return Work{}, false, nil
}

// SubmitExternalSessionInput submits one channel-owned receipt to the exact
// delegated Session through the watcher's provider input owner. It uses the
// same receipt semantics as the mobile direct Session input path: the watcher
// may retry a definite pre-mutation failure under the same durable identity,
// but an ambiguous provider admission is never replayed. It never wraps the
// text as a Brain message and never synthesizes Work Events or lifecycle
