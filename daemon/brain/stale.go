package brain

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// The stale list is information for Brain, never a gate: Work that has been
// open with nothing live on it for a while, and delegated Sessions left idle
// with no open Work. Nothing here closes or stops anything; Brain decides.
const (
	// staleWorkAfter is how long an open Work with nothing live on it stays
	// quiet before Brain's context lists it. A kept Work is listed at once.
	staleWorkAfter = 2 * time.Hour
	// staleSessionAfter is how long a delegated Session with no open Work
	// stays unchanged before it is listed.
	staleSessionAfter = 30 * time.Minute
	staleListLimit    = 8
)

// Stale reasons.
const (
	StaleKept       = "kept"        // the closure judge kept it; Reason says on what
	StaleResultRead = "result_read" // Brain read its result and recorded nothing since
	StaleIdle       = "idle"        // open, nothing live, no result to read
)

// StaleList is what Brain's context lists for a decision.
type StaleList struct {
	Work     []StaleWork    `json:"work,omitempty"`
	Sessions []StaleSession `json:"sessions,omitempty"`
}

// StaleWork is one open Work nothing live is pending on.
type StaleWork struct {
	ID     string     `json:"work_id"`
	Title  string     `json:"title"`
	Status WorkStatus `json:"status"`
	Stale  string     `json:"stale"`
	// Since is when it went quiet: the keep verdict, the end of Brain's
	// reading, or its last lifecycle change.
	Since time.Time `json:"since"`
	// WaitFor is what a keep verdict says it waits on.
	WaitFor string `json:"wait_for,omitempty"`
}

// StaleSession is a delegated Worker Session left with no open Work.
type StaleSession struct {
	ID      string    `json:"session_id"`
	Name    string    `json:"name,omitempty"`
	Status  string    `json:"status"`
	Since   time.Time `json:"since"`
	WorkID  string    `json:"work_id,omitempty"` // its last Work, closed
	Summary string    `json:"summary,omitempty"`
}

func (s *Service) staleList(workers []WorkerRef) *StaleList {
	now := s.nowUTC()
	list := &StaleList{}
	items, err := s.store.ListWork()
	if err != nil {
		return nil
	}
	openAttempt := map[string]bool{}
	for _, item := range items {
		if item.Status == WorkDone || item.Status == WorkCancelled {
			continue
		}
		if item.AttemptSessionID != "" {
			openAttempt[item.AttemptSessionID] = true
		}
		if (s.standing != nil && s.standing(item.ID)) || (item.SnoozedUntil != nil && now.Before(*item.SnoozedUntil)) {
			continue
		}
		st, err := s.store.fsmState(item.ID)
		if err != nil || st == nil || st.Status.Terminal() || st.Attempt != nil || st.Wake != nil ||
			st.ActiveAdmission() != nil || strings.TrimSpace(item.Question) != "" {
			continue
		}
		entry := StaleWork{ID: item.ID, Title: item.Title, Status: item.Status}
		switch {
		case item.Kept != nil && item.Kept.Revision == st.Revision:
			entry.Stale, entry.Since, entry.WaitFor = StaleKept, item.Kept.At, item.Kept.Reason
		case st.Review != nil:
			handler := st.Review.Handler
			if handler == nil || handler.DeliveredAt == nil || handler.EndedAt == nil {
				continue // not read yet: delivery is still pending
			}
			entry.Stale, entry.Since = StaleResultRead, *handler.EndedAt
		default:
			entry.Stale, entry.Since = StaleIdle, st.UpdatedAt
		}
		if entry.Stale != StaleKept && now.Sub(entry.Since) < staleWorkAfter {
			continue
		}
		list.Work = append(list.Work, entry)
	}
	sort.Slice(list.Work, func(i, j int) bool { return list.Work[i].Since.Before(list.Work[j].Since) })
	if len(list.Work) > staleListLimit {
		list.Work = list.Work[:staleListLimit]
	}
	for _, worker := range workers {
		if !worker.Delegated || worker.Hidden || openAttempt[worker.ID] || now.Sub(worker.Updated) < staleSessionAfter {
			continue
		}
		entry := StaleSession{ID: worker.ID, Name: worker.Name, Status: worker.Status, Since: worker.Updated, Summary: judgeClip(worker.Summary, 160)}
		if workID := s.store.currentTurnWork(worker.ID); workID != "" {
			if st, err := s.store.fsmState(workID); err == nil && st != nil && !st.Status.Terminal() {
				continue // its Work is still open
			}
			entry.WorkID = workID
		}
		list.Sessions = append(list.Sessions, entry)
	}
	sort.Slice(list.Sessions, func(i, j int) bool { return list.Sessions[i].Since.Before(list.Sessions[j].Since) })
	if len(list.Sessions) > staleListLimit {
		list.Sessions = list.Sessions[:staleListLimit]
	}
	if len(list.Work) == 0 && len(list.Sessions) == 0 {
		return nil
	}
	return list
}

// currentTurnWork is the Work of the Session's current delegated Turn.
func (s *Store) currentTurnWork(sessionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	turn, found := currentTurnForSession(s.readPresentationLocked(), sessionID)
	if !found {
		return ""
	}
	return turn.WorkID
}

// ReclaimFinishedRunSession tears down the Worker Session of a finished
// scheduled-action run once it sits idle at its prompt, through Brain's
// teardown so its model route and Codex resources are released too. A
// Session Brain has since given open Work (a follow-up, an Attempt, a prompt
// in flight) is left alone. It reports whether the Session was torn down.
func (s *Service) ReclaimFinishedRunSession(sessionID string) (bool, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return false, nil
	}
	reclaimer, ok := s.watcher.(interface{ ReclaimIdleSession(string) error })
	if !ok || s.store.sessionHoldsOpenWork(sessionID) {
		return false, nil
	}
	err := s.teardownSessionWith(sessionID, reclaimer.ReclaimIdleSession)
	if errors.Is(err, watcher.ErrSessionNotIdle) || errors.Is(err, watcher.ErrNotOwnedDelegatedWorker) {
		return false, nil
	}
	return err == nil, err
}

// sessionHoldsOpenWork: an open Work's Attempt or prompt is on the Session,
// or its current delegated Turn belongs to an open Work.
func (s *Store) sessionHoldsOpenWork(sessionID string) bool {
	held := false
	s.fsm.Each(func(st *lifecycle.State) {
		if st.Status.Terminal() {
			return
		}
		if st.Attempt != nil && st.Attempt.SessionID == sessionID {
			held = true
		}
		if admission := st.ActiveAdmission(); admission != nil && admission.SessionID == sessionID {
			held = true
		}
	})
	if held {
		return true
	}
	if workID := s.currentTurnWork(sessionID); workID != "" {
		if st, err := s.fsmState(workID); err != nil || (st != nil && !st.Status.Terminal()) {
			return true
		}
	}
	return false
}
