package agentevents

import (
	"context"
	"errors"
	"time"
)

// ErrNoSuchApproval means the approval is not pending: it was already decided,
// by Mewla, the agent's own UI, or a timeout.
var ErrNoSuchApproval = errors.New("approval is not pending")

// Decision is the answer to an approval.
type Decision struct {
	OptionID string
	// By is "mewla" for an answer given through Mewla.
	By string
}

// Ask publishes an approval request and waits for it to be decided. It is for
// agents that block on a question (a hook that must answer before the agent
// proceeds). The wait ends with the decision, with ctx, or when the approval
// is resolved elsewhere (the agent's own UI answered it); in the last two
// cases ok is false and the caller must leave the agent's own prompt alone.
func (h *Hub) Ask(ctx context.Context, workerID string, a Approval, now time.Time) (d Decision, ok bool) {
	answer := make(chan Decision, 1)
	h.mu.Lock()
	w := h.worker(workerID)
	if w.waiters == nil {
		w.waiters = map[string]chan Decision{}
	}
	w.waiters[a.ID] = answer
	h.mu.Unlock()

	h.Publish(Event{WorkerID: workerID, Kind: KindApprovalRequest, At: now, Approval: &a})

	defer func() {
		h.mu.Lock()
		if w := h.workers[workerID]; w != nil {
			delete(w.waiters, a.ID)
		}
		h.mu.Unlock()
	}()
	select {
	case d = <-answer:
		return d, true
	case <-ctx.Done():
		h.Publish(Event{WorkerID: workerID, Kind: KindApprovalResolved, At: time.Now(), ApprovalID: a.ID, By: "timeout"})
		return Decision{}, false
	}
}

// Decide answers a pending approval. It returns ErrNoSuchApproval when the
// approval is not pending, so a late answer is refused instead of being turned
// into a keystroke.
func (h *Hub) Decide(workerID, approvalID, optionID string, now time.Time) error {
	h.mu.Lock()
	w, found := h.workers[workerID]
	var pending bool
	if found {
		for _, a := range w.Approvals {
			if a.ID == approvalID {
				pending = true
			}
		}
	}
	var answer chan Decision
	if found {
		answer = w.waiters[approvalID]
	}
	h.mu.Unlock()
	if !pending {
		return ErrNoSuchApproval
	}
	if answer != nil {
		select {
		case answer <- Decision{OptionID: optionID, By: "mewla"}:
		default:
		}
	}
	h.Publish(Event{WorkerID: workerID, Kind: KindApprovalResolved, At: now, ApprovalID: approvalID, By: "mewla", OptionID: optionID})
	return nil
}
