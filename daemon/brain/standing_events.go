package brain

import (
	"errors"

	"github.com/daoleno/mewla/daemon/lifecycle"
)

// RaiseWorkEvent appends a producer's event to a standing Work and, when it
// is actionable, opens the Review that wakes Brain for it (the same path as
// machine resource pressure). A generic actionable append alone wakes
// nobody: without a canonical Review the projection clears it. opened is
// false when the Work could not take this event as its Review now (another
// Review is still with Brain, the Work waits on something typed, or a
// Worker owns it); the producer may raise it again later under a new key.
func (s *Service) RaiseWorkEvent(event WorkEvent) (recorded WorkEvent, opened bool, err error) {
	if s == nil || s.store == nil {
		return WorkEvent{}, false, errors.New("brain service is not configured")
	}
	actionable := event.Actionable
	recorded, created, err := s.store.AppendWorkEvent(event)
	if err != nil || !actionable {
		return recorded, false, err
	}
	if !created && recorded.HandledAt != nil {
		return recorded, true, nil
	}
	if !recorded.Actionable || recorded.DiscardedAt != nil {
		return recorded, false, nil
	}
	st, err := s.store.FSM().OpenReviewEvent(lifecycle.WorkID(recorded.WorkID), recorded.Kind, recorded.ID, recorded.ID)
	if err != nil {
		return recorded, false, err
	}
	opened = st != nil && st.Review != nil && st.Review.EventID == recorded.ID
	if err := s.store.SyncWorkProjection(recorded.WorkID); err != nil {
		return recorded, opened, err
	}
	if opened {
		_, err = s.ReconcileHostLane()
	}
	return recorded, opened, err
}

// WorkEventHandled says whether Brain is done with an event RaiseWorkEvent
// opened: its Review was delivered and Brain's handling of it ended, or the
// Review was resolved or replaced. found is false when the Work no longer
// has the event.
func (s *Service) WorkEventHandled(workID, eventID string) (handled, found bool, err error) {
	if s == nil || s.store == nil {
		return false, false, errors.New("brain service is not configured")
	}
	st, err := s.store.FSM().State(lifecycle.WorkID(workID))
	if err != nil && !errors.Is(err, lifecycle.ErrUnknownWork) {
		return false, false, err
	}
	if st != nil && st.Review != nil && st.Review.EventID == eventID {
		handler := st.Review.Handler
		return handler != nil && handler.DeliveredAt != nil && handler.EndedAt != nil, true, nil
	}
	events, err := s.store.ListWorkEvents(workID)
	if errors.Is(err, ErrWorkNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	for _, event := range events {
		if event.ID == eventID {
			return true, true, nil
		}
	}
	return false, false, nil
}
