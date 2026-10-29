package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

// permissionGateHooks connects the permission gate to Brain's Work and the
// Worker's turn. It acts only for a Worker Brain delegated, whose turn and
// Work it can name; for any other session every question goes to the user.
func permissionGateHooks(w *watcher.Watcher, store *brain.Store, service *brain.Service, changed func(workerID string)) permissionGate {
	// pendingAdmission is the prompt-carried input of a Worker whose first
	// report has not admitted its turn yet.
	pendingAdmission := func(workerID string) (watcher.InputAdmission, bool) {
		pending, err := store.PendingInputAdmissions(workerID)
		if err != nil {
			return watcher.InputAdmission{}, false
		}
		var newest *watcher.InputAdmission
		for index := range pending {
			candidate := pending[index]
			if candidate.State == watcher.InputAdmissionPending && candidate.SignalProtocol && candidate.ClaimToken == "" && candidate.Purpose == "" &&
				(newest == nil || candidate.AcceptedAt.After(newest.AcceptedAt)) {
				newest = &pending[index]
			}
		}
		if newest == nil {
			return watcher.InputAdmission{}, false
		}
		return *newest, true
	}
	workFor := func(workerID string) (brain.Work, bool) {
		item, found, err := service.WorkForSession(workerID)
		if err == nil && !found {
			if admission, ok := pendingAdmission(workerID); ok {
				item, err = store.Work(admission.WorkID)
				found = err == nil
			} else if workID, ok := store.WorkOfSession(workerID); ok {
				item, err = store.Work(workID)
				found = err == nil
			}
		}
		if err != nil || !found || item.Status == brain.WorkDone || item.Status == brain.WorkCancelled {
			return brain.Work{}, false
		}
		return item, true
	}
	// turnOf names the live turn a question belongs to. A Worker can ask before
	// its first report has admitted its turn; the prompt-carried turn still
	// pending is then the one a report on its behalf promotes. A settled turn is
	// not one: it is immutable, so nothing can be filed on it.
	turnOf := func(workerID string) (string, bool) {
		turn, found, err := store.Turn(workerID)
		if err == nil && found && strings.TrimSpace(turn.TurnID) != "" && !watcher.TurnTerminal(turn.Status) {
			return turn.TurnID, true
		}
		if admission, ok := pendingAdmission(workerID); ok {
			return admission.ProposedTurnID, true
		}
		return "", false
	}
	report := func(workerID, turnID string, p classifier.WorkerProgress) (*classifier.Worker, error) {
		p.TurnID = turnID
		return w.UpdateWorkerProgress(workerID, p)
	}
	// lateTurns holds, per Worker, the turn opened for a request that came after
	// its own turn settled. Answering that request settles the turn again, so
	// Brain reviews the result the Worker had reported, which the new turn's
	// blocked review took the place of.
	var lateMu sync.Mutex
	lateTurns := map[string]string{}
	progress := func(workerID string, p classifier.WorkerProgress) error {
		turnID, ok := turnOf(workerID)
		if !ok {
			return fmt.Errorf("worker %s has no live delegated turn", workerID)
		}
		_, err := report(workerID, turnID, p)
		return err
	}
	return permissionGate{
		changed:   changed,
		paneFiled: w.PanePromptFiled,
		delegated: func(workerID string) bool {
			worker := w.GetWorker(workerID)
			if worker == nil || !worker.Delegated {
				return false
			}
			_, hasWork := workFor(workerID)
			return hasWork
		},
		tellBrain: func(workerID, summary string, details map[string]any) error {
			if item, ok := workFor(workerID); ok {
				details["work_id"] = item.ID
				details["work_title"] = item.Title
				details["brief"] = "Read the Work's objective (mewla's Work " + item.ID + ") for what it authorizes."
			}
			raw, err := json.Marshal(details)
			if err != nil {
				return err
			}
			// A request can come after the Worker's own turn settled: it
			// reported done and stopped while a background sub-agent kept
			// running. The request still reaches Brain, on a turn opened for it.
			turnID, live := turnOf(workerID)
			if !live {
				item, found := workFor(workerID)
				if !found {
					return fmt.Errorf("worker %s has no active Work to raise the request on", workerID)
				}
				if turnID, err = w.OpenPromptTurn(workerID, item.ID); err != nil {
					return err
				}
				lateMu.Lock()
				lateTurns[workerID] = turnID
				lateMu.Unlock()
			}
			raised, err := report(workerID, turnID, classifier.WorkerProgress{
				Status: "blocked", Phase: "working", Attention: "user_input",
				Summary: summary, EventKind: "needs_judgment", DetailsJSON: string(raw), LeaseSeconds: 900,
			})
			if err != nil {
				return err
			}
			// A fact the ledger dropped (the turn had settled) raises nothing.
			if raised == nil || raised.State != classifier.StateBlocked {
				return fmt.Errorf("the request did not block turn %s", turnID)
			}
			return nil
		},
		resumed: func(workerID, summary string) {
			next := classifier.WorkerProgress{
				Status: "running", Phase: "working", Attention: "none",
				Summary: summary, EventKind: "progress", LeaseSeconds: 300,
			}
			turnID, live := turnOf(workerID)
			lateMu.Lock()
			late := live && lateTurns[workerID] == turnID
			delete(lateTurns, workerID)
			lateMu.Unlock()
			if late {
				next = classifier.WorkerProgress{
					Status: "done", Phase: "reporting", Attention: "done",
					Summary: summary + "; the Worker had already reported done", EventKind: "done",
				}
			}
			err := progress(workerID, next)
			if err != nil {
				log.Printf("permission: could not mark %s running again: %v", workerID, err)
			}
		},
		answered: func(workerID string) {
			if _, err := store.ResolveAnsweredWorkerBlock(workerID); err != nil {
				log.Printf("permission: could not record Brain's answer for %s as its disposition: %v", workerID, err)
			}
		},
		audit: func(workerID, outcome, summary string, details map[string]any) {
			item, ok := workFor(workerID)
			if !ok {
				return
			}
			raw, _ := json.Marshal(details)
			approvalID, _ := details["approval_id"].(string)
			_, _, err := service.AppendWorkEvent(brain.WorkEvent{
				WorkID:      item.ID,
				Kind:        "permission." + outcome,
				DedupeKey:   "permission:" + approvalID + ":" + outcome,
				SourceName:  "permission",
				Summary:     summary,
				DetailsJSON: string(raw),
			})
			if err != nil {
				log.Printf("permission: could not record %q on Work %s: %v", outcome, item.ID, err)
			}
		},
	}
}
