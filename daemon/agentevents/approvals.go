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
// is resolved elsewhere (the agent's own UI answered it: AnsweredInTerminal);
// in the last two cases ok is false and the caller must leave the agent's own
// prompt alone.
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
		if d.By == ByTerminal {
			return d, false
		}
		return d, true
	case <-ctx.Done():
		h.Publish(Event{WorkerID: workerID, Kind: KindApprovalResolved, At: time.Now(), ApprovalID: a.ID, By: "timeout"})
		return Decision{}, false
	}
}

// ByTerminal is who settled an approval answered in the agent's own prompt in
// its terminal. The screen watcher records that answer itself, so nothing else
// records it again.
const ByTerminal = "terminal"

// AnsweredInTerminal settles a pending approval that was answered in the
// agent's own prompt (a hook that asked Mewla is not told when the person
// answers the same question in the terminal, and keeps waiting): it leaves the
// pending list, and its hook stops waiting with no decision. It reports
// whether the approval was pending.
func (h *Hub) AnsweredInTerminal(workerID, approvalID string, now time.Time) bool {
	h.mu.Lock()
	w, found := h.workers[workerID]
	pending := false
	var answer chan Decision
	if found {
		for _, a := range w.Approvals {
			pending = pending || a.ID == approvalID
		}
		answer = w.waiters[approvalID]
	}
	h.mu.Unlock()
	if !pending {
		return false
	}
	if answer != nil {
		select {
		case answer <- Decision{By: ByTerminal}:
		default:
		}
	}
	h.Publish(Event{WorkerID: workerID, Kind: KindApprovalResolved, At: now, ApprovalID: approvalID, By: ByTerminal})
	return true
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
