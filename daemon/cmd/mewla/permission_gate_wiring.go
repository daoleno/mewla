package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

// permissionGateHooks connects the permission gate to Brain's Work and the
// Worker's turn. It acts only for a Worker Brain delegated, whose turn and
// Work it can name; for any other session every question goes to the user.
func permissionGateHooks(w *watcher.Watcher, store *brain.Store, service *brain.Service, changed func(workerID string)) permissionGate {
	workFor := func(workerID string) (brain.Work, bool) {
		item, found, err := service.WorkForSession(workerID)
		if err != nil || !found || item.Status == brain.WorkDone || item.Status == brain.WorkCancelled {
			return brain.Work{}, false
		}
		return item, true
	}
	turnOf := func(workerID string) (string, bool) {
		turn, found, err := store.Turn(workerID)
		if err != nil || !found || strings.TrimSpace(turn.TurnID) == "" {
			return "", false
		}
		return turn.TurnID, true
	}
	progress := func(workerID string, p classifier.WorkerProgress) error {
		turnID, ok := turnOf(workerID)
		if !ok {
			return fmt.Errorf("worker %s has no delegated turn", workerID)
		}
		p.TurnID = turnID
		_, err := w.UpdateWorkerProgress(workerID, p)
		return err
	}
	return permissionGate{
		changed: changed,
		delegated: func(workerID string) bool {
			worker := w.GetWorker(workerID)
			if worker == nil || !worker.Delegated {
				return false
			}
			_, hasTurn := turnOf(workerID)
			_, hasWork := workFor(workerID)
			return hasTurn && hasWork
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
			return progress(workerID, classifier.WorkerProgress{
				Status: "blocked", Phase: "working", Attention: "user_input",
				Summary: summary, EventKind: "needs_judgment", DetailsJSON: string(raw), LeaseSeconds: 900,
			})
		},
		resumed: func(workerID, summary string) {
			err := progress(workerID, classifier.WorkerProgress{
				Status: "running", Phase: "working", Attention: "none",
				Summary: summary, EventKind: "progress", LeaseSeconds: 300,
			})
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
