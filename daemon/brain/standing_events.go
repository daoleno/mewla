package brain

import (
	"errors"
	"fmt"

	"github.com/daoleno/mewla/daemon/lifecycle"
)

// ErrWorkEventNotDelivered says an actionable event was recorded on its Work
// as history but could not be delivered to Brain; its message says why.
var ErrWorkEventNotDelivered = errors.New("work event not delivered")

// RaiseWorkEvent appends a producer's event to a standing Work and, when it
// is actionable, opens the Review that wakes Brain for it (the same path as
// machine resource pressure). A generic actionable append alone wakes
// nobody: without a canonical Review the projection clears it. opened is
// false when the Work could not take this event as its Review now (another
// Review is still with Brain, the Work waits on something typed, or a
// Worker owns it); the producer may raise it again later under a new key.
func (s *Service) RaiseWorkEvent(event WorkEvent) (recorded WorkEvent, opened bool, err error) {
	recorded, _, opened, err = s.raiseWorkEvent(event)
	return recorded, opened, err
}

// DeliverWorkEvent is an actionable event from outside a Session (mewla
// brain work event -actionable): it is appended and its Review opened, so
// Brain is woken with it, or it fails with ErrWorkEventNotDelivered and the
// reason. An event is never accepted as actionable and then left undelivered.
// A repeated dedupe key is the same event: delivered again it is a no-op.
func (s *Service) DeliverWorkEvent(event WorkEvent) (WorkEvent, error) {
	event.Actionable = true
	recorded, created, opened, err := s.raiseWorkEvent(event)
	if err != nil || opened {
		return recorded, err
	}
	return recorded, fmt.Errorf("%w: %s", ErrWorkEventNotDelivered, s.undeliveredReason(recorded, created))
}

func (s *Service) undeliveredReason(recorded WorkEvent, created bool) string {
	if !created {
		return fmt.Sprintf("an event with dedupe key %q is already on this Work and was not delivered; send it again with a new -dedupe", recorded.DedupeKey)
	}
	prefix := fmt.Sprintf("event %s is recorded on Work %s as history only", recorded.ID, recorded.WorkID)
	st, _ := s.store.FSM().State(lifecycle.WorkID(recorded.WorkID))
	switch {
	case st == nil:
		return prefix + ": the Work has no lifecycle state"
	case st.Status.Terminal():
		return prefix + ": the Work is closed"
	case st.Review != nil:
		return prefix + fmt.Sprintf(": Brain still has an earlier event of this Work (%s) to handle; send this one again with a new -dedupe once that is done", st.Review.EventID)
	case st.Wake != nil:
		return prefix + fmt.Sprintf(": the Work waits on its %s wake (%s)", st.Wake.Kind, st.Wake.Ref)
	case st.Attempt != nil:
		return prefix + ": a Worker's turn owns this Work; its result reaches Brain instead"
	default:
		return prefix + ": the Work cannot take a Review now"
	}
}

func (s *Service) raiseWorkEvent(event WorkEvent) (recorded WorkEvent, created, opened bool, err error) {
	if s == nil || s.store == nil {
		return WorkEvent{}, false, false, errors.New("brain service is not configured")
	}
	actionable := event.Actionable
	recorded, created, err = s.store.AppendWorkEvent(event)
	if err != nil || !actionable {
		return recorded, created, false, err
	}
	if !created && recorded.HandledAt != nil {
		return recorded, created, true, nil
	}
	if !created {
		// The same event again: delivered when its Review is (or was) open.
		if st, _ := s.store.FSM().State(lifecycle.WorkID(recorded.WorkID)); st != nil && st.Review != nil && st.Review.EventID == recorded.ID {
			return recorded, created, true, nil
		}
		return recorded, created, false, nil
	}
	if !recorded.Actionable || recorded.DiscardedAt != nil {
		return recorded, created, false, nil
	}
	st, err := s.store.FSM().OpenReviewEvent(lifecycle.WorkID(recorded.WorkID), recorded.Kind, recorded.ID, recorded.ID)
	if err != nil {
		return recorded, created, false, err
	}
	opened = st != nil && st.Review != nil && st.Review.EventID == recorded.ID
	if err := s.store.SyncWorkProjection(recorded.WorkID); err != nil {
		return recorded, created, opened, err
	}
	if opened {
		_, err = s.ReconcileHostLane()
	}
	return recorded, created, opened, err
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
