package main

import (
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/lifecycle"
)

// mewla brain work event -actionable delivers (opens the Work's Review) or
// fails saying why; it is never accepted and silently left undelivered.
func TestControlBrainWorkEventActionableDeliversOrRefuses(t *testing.T) {
	store := newControlBrainStore(t)
	service := brain.NewService(store, nil, nil)
	app := &controlApp{brainStore: store, brainService: service}
	item, err := store.CreateWork(brain.Work{Title: "PRLoopWatcher", Objective: "standing", CompletionPolicy: brain.CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	send := func(dedupe string, actionable bool) control.Response {
		return app.HandleControlRequest(control.Request{Type: "brain_work_event", BrainWorkEvent: &brain.WorkEvent{
			WorkID: item.ID, Kind: "pr.new", DedupeKey: dedupe, PayloadRef: "worklog/new.txt", Actionable: actionable,
		}})
	}

	resp := send("new-1", true)
	if !resp.OK || resp.BrainWorkEvent == nil || !strings.Contains(resp.Confirmation, "Delivered") {
		t.Fatalf("actionable event: %+v", resp)
	}
	st, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || st.Review == nil || st.Review.EventID != resp.BrainWorkEvent.ID {
		t.Fatalf("no Review opened for the event: %+v %v", st, err)
	}

	resp = send("new-2", true)
	if resp.OK || resp.Error == nil || resp.Error.Code != "brain_work_event_not_delivered" ||
		!strings.Contains(resp.Error.Message, "earlier event of this Work") {
		t.Fatalf("second actionable event while the first is open: %+v", resp)
	}

	resp = send("note-1", false)
	if !resp.OK || !strings.Contains(resp.Confirmation, "Brain is not woken") {
		t.Fatalf("history event: %+v", resp)
	}
}

// mewla brain context shows Watch's last 24 hours: the daemon's own Service
// answers it, not a fresh one that was never told (11-20 01:00: watch_judge
// was missing on the live daemon).
func TestControlBrainContextShowsWatchsDay(t *testing.T) {
	store := newControlBrainStore(t)
	service := brain.NewService(store, nil, nil)
	service.SetWatchJudgeDay(func() brain.WatchJudgeDay { return brain.WatchJudgeDay{Runs: 61, Signals: 900, USD: 7.5} })
	app := &controlApp{brainStore: store, brainService: service}
	resp := app.HandleControlRequest(control.Request{Type: "brain_context"})
	context, ok := resp.Context.(brain.BrainContext)
	if !resp.OK || !ok || context.WatchJudge == nil || context.WatchJudge.Runs != 61 {
		t.Fatalf("brain context = %#v (%#v)", resp, context.WatchJudge)
	}
}
