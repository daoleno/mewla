package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/computers"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// computerOrigin resolves local launch identity before anything is queued. The
// peer never supplies a Session or Work id; it can only reply to a sent message.
func (a *controlApp) computerOrigin(sessionID string) (computers.Origin, error) {
	if a.watcher == nil {
		return computers.Origin{}, fmt.Errorf("Worker watcher is unavailable")
	}
	worker := a.watcher.GetWorker(sessionID)
	if worker == nil {
		return computers.Origin{}, fmt.Errorf("sending Worker %s was not found", sessionID)
	}
	if worker.Hidden {
		// Brain and Calendar hosts keep the Brain reply path.
		return computers.Origin{}, nil
	}
	if !worker.Delegated {
		return computers.Origin{}, fmt.Errorf("sending Session is not a delegated Worker")
	}
	// Naming the sender is a read; the reply's delivery re-proves control at
	// the mutation boundary.
	generation, err := a.watcher.ResolveDelegatedControl(sessionID)
	if err != nil {
		return computers.Origin{}, err
	}
	origin := computers.Origin{SessionID: sessionID, PaneGeneration: generation.PaneGeneration}
	if a.brainStore != nil {
		origin.WorkID, _ = a.brainStore.WorkOfSession(sessionID)
	}
	return origin, nil
}

// deliverComputerMessage lands a reply where a decision-maker sees it, once:
// the asking Session when it can still take input, else its Work when that
// Work is open, else Brain. Only a Session that can take the reply waits for
// it (an open prompt, a transient refusal); one that never can is skipped.
func (a *controlApp) deliverComputerMessage(d computers.Delivery) (string, error) {
	if d.Origin.SessionID == "" {
		return a.deliverComputerToBrain(d)
	}
	if a.watcher == nil {
		return computers.DispositionNotSubmitted, fmt.Errorf("Worker watcher is unavailable")
	}
	presence, err := a.watcher.ProbeSession(d.Origin.SessionID)
	if err != nil {
		return computers.DispositionNotSubmitted, err
	}
	if presence == watcher.SessionPresenceUnknown {
		return computers.DispositionNotSubmitted, fmt.Errorf("Worker presence is unknown")
	}
	if presence == watcher.SessionPresencePresent {
		workID, takes, err := a.computerReplyTaker(d.Origin)
		if err != nil {
			return computers.DispositionNotSubmitted, err
		}
		if takes {
			generation, err := a.watcher.ResolveOwnedGeneration(d.Origin.SessionID)
			if err != nil {
				return computers.DispositionNotSubmitted, err
			}
			if generation.PaneGeneration == d.Origin.PaneGeneration {
				return a.submitComputerReply(d, workID)
			}
			// The original pane is gone; a recycled target is not the original asker.
		}
		// A Session no open Work owns cannot take Worker input; its reply
		// falls through instead of being refused on every retry.
	}
	if d.Origin.WorkID != "" && a.brainStore != nil {
		if _, _, err := a.brainStore.ReconcileAbsentWorkAttempt(d.Origin.WorkID, d.Origin.SessionID); err != nil && !errors.Is(err, brain.ErrWorkNotFound) {
			return computers.DispositionNotSubmitted, err
		}
	}
	if recorded, err := a.recordComputerEvent(d, true); err != nil {
		return computers.DispositionNotSubmitted, err
	} else if recorded {
		return computers.DispositionAccepted, nil
	}
	return a.deliverComputerToBrain(d)
}

// computerReplyTaker says whether the asking Session can be given the reply
// as delegated input, by the same rule admission applies, and on which Work.
// The origin Work is used while it exists; otherwise the Work whose Attempt
// runs on the Session.
func (a *controlApp) computerReplyTaker(origin computers.Origin) (string, bool, error) {
	if a.brainStore == nil {
		return "", true, nil
	}
	if origin.WorkID != "" {
		takes, err := a.brainStore.DelegatedInputTakes(origin.WorkID, origin.SessionID)
		if !errors.Is(err, brain.ErrWorkNotFound) {
			return origin.WorkID, takes, err
		}
	}
	takes, err := a.brainStore.DelegatedInputTakes("", origin.SessionID)
	return "", takes, err
}

func (a *controlApp) submitComputerReply(d computers.Delivery, workID string) (string, error) {
	if a.openPrompt(d.Origin.SessionID, a.watcher.GetWorker(d.Origin.SessionID)) != nil {
		return computers.DispositionNotSubmitted, fmt.Errorf("Worker has an open prompt; reply waits until it is answered")
	}
	if workID != "" {
		if _, err := a.recordComputerEvent(d, false); err != nil {
			return computers.DispositionNotSubmitted, err
		}
	}
	turnID := "turn:" + d.Receipt
	payload := watcher.DelegatedLifecyclePayload(d.Body, turnID)
	var result watcher.InputResult
	var err error
	if workID != "" {
		result, err = a.watcher.SubmitDelegatedWorkInput(d.Origin.SessionID, payload, workID, turnID, "", "", time.Now().UTC())
	} else {
		result, err = a.watcher.SubmitDelegatedInput(d.Origin.SessionID, payload, turnID, time.Now().UTC())
	}
	if result.Outcome == watcher.InputAccepted && err == nil {
		_, _ = a.watcher.RebindDelegatedTurnProjection(d.Origin.SessionID)
		return computers.DispositionAccepted, nil
	}
	if watcher.InputOutcomeFromError(err) == watcher.InputNotSubmitted || result.Outcome == watcher.InputNotSubmitted {
		return computers.DispositionNotSubmitted, err
	}
	return computers.DispositionUncertain, err
}

// An audit event lets Brain see a delivered Worker reply without consuming it
// as chat input. Only loss of the original Session makes the event actionable,
// and only an open Work can act on it: a review on a closed Work never opens.
func (a *controlApp) recordComputerEvent(d computers.Delivery, actionable bool) (bool, error) {
	if d.Origin.WorkID == "" || a.brainStore == nil {
		return false, nil
	}
	item, err := a.brainStore.Work(d.Origin.WorkID)
	if errors.Is(err, brain.ErrWorkNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if actionable && (item.Status == brain.WorkDone || item.Status == brain.WorkCancelled) {
		return false, nil
	}
	kind := "computer_message"
	if actionable {
		kind = "computer_reply_fallback"
	}
	event := brain.WorkEvent{WorkID: d.Origin.WorkID, Kind: kind, DedupeKey: d.Identity, SourceName: "computer", Summary: d.Body, Actionable: actionable}
	// Store first: reconciliation errors cannot turn an already committed event
	// into a new delivery or a fallback to another recipient.
	recorded, _, err := a.brainStore.AppendWorkEvent(event)
	if err != nil {
		return false, err
	}
	if actionable {
		if _, err := a.brainStore.FSM().OpenReviewEvent(lifecycle.WorkID(d.Origin.WorkID), "computer_reply", d.Identity, recorded.ID); err != nil {
			return false, err
		}
		if err := a.brainStore.SyncWorkProjection(d.Origin.WorkID); err != nil {
			return false, err
		}
		if a.brainService != nil {
			_, _ = a.brainService.ReconcileHostLane()
		}
	}
	return true, nil
}

func (a *controlApp) deliverComputerToBrain(d computers.Delivery) (string, error) {
	if a.brainService == nil {
		return computers.DispositionNotSubmitted, fmt.Errorf("Brain is unavailable")
	}
	disposition, err := a.brainService.SubmitExternalUserInput(d.Receipt, d.Body)
	return string(disposition), err
}
