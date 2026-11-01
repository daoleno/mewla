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
	generation, err := a.watcher.ResolveOwnedGeneration(sessionID)
	if err != nil {
		return computers.Origin{}, err
	}
	origin := computers.Origin{SessionID: sessionID, PaneGeneration: generation.PaneGeneration}
	if a.brainStore != nil {
		origin.WorkID, _ = a.brainStore.WorkOfSession(sessionID)
	}
	return origin, nil
}

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
		generation, err := a.watcher.ResolveOwnedGeneration(d.Origin.SessionID)
		if err != nil {
			return computers.DispositionNotSubmitted, err
		}
		if generation.PaneGeneration == d.Origin.PaneGeneration {
			if a.openPrompt(d.Origin.SessionID, a.watcher.GetWorker(d.Origin.SessionID)) != nil {
				return computers.DispositionNotSubmitted, fmt.Errorf("Worker has an open prompt; reply waits until it is answered")
			}
			hasWork, err := a.recordComputerEvent(d, false)
			if err != nil {
				return computers.DispositionNotSubmitted, err
			}
			turnID := "turn:" + d.Receipt
			payload := watcher.DelegatedLifecyclePayload(d.Body, turnID)
			var result watcher.InputResult
			if hasWork {
				result, err = a.watcher.SubmitDelegatedWorkInput(d.Origin.SessionID, payload, d.Origin.WorkID, turnID, "", "", time.Now().UTC())
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
		// The original pane is gone; a recycled target is not the original asker.
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

// An audit event lets Brain see a delivered Worker reply without consuming it
// as chat input. Only loss of the original Session makes the event actionable.
func (a *controlApp) recordComputerEvent(d computers.Delivery, actionable bool) (bool, error) {
	if d.Origin.WorkID == "" || a.brainStore == nil {
		return false, nil
	}
	if _, err := a.brainStore.Work(d.Origin.WorkID); errors.Is(err, brain.ErrWorkNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
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
