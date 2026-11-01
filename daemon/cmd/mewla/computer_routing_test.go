package main

import (
	"errors"
	"fmt"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/computers"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
	"strings"
	"testing"
)

func TestComputerReplyWakesWorkerAndRecordsWorkEvent(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.workers["%7"] = &classifier.Worker{ID: "%7", Delegated: true, Command: "codex"}
	store := newControlBrainStore(t)
	item, err := store.CreateWork(brain.Work{Title: "remote job", Objective: "reply to asker", CompletionPolicy: brain.CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{watcher: fw, brainStore: store}
	d := computers.Delivery{Identity: "computer:peer:reply", Receipt: "computer:peer:reply:1", Body: "remote answer", Origin: computers.Origin{SessionID: "%7", WorkID: item.ID}}
	result, err := app.deliverComputerMessage(d)
	if err != nil || result != computers.DispositionAccepted {
		t.Fatalf("%s %v", result, err)
	}
	if len(fw.submitted) != 1 || fw.submitted[0].id != "%7" || !strings.Contains(fw.submitted[0].text, d.Body) {
		t.Fatalf("submissions=%+v", fw.submitted)
	}
	events, err := store.ListWorkEvents(item.ID)
	if err != nil || len(events) != 1 || events[0].Actionable {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	delete(fw.workers, "%7")
	d.Identity = "computer:peer:question"
	d.Receipt = d.Identity + ":1"
	result, err = app.deliverComputerMessage(d)
	if err != nil || result != computers.DispositionAccepted || len(fw.submitted) != 1 {
		t.Fatalf("fallback=%s %v", result, err)
	}
	events, _ = store.ListWorkEvents(item.ID)
	if len(events) != 2 || !events[1].Actionable {
		t.Fatalf("fallback events=%+v", events)
	}
}

func TestComputerOriginRejectsUnknownAndExternalSessions(t *testing.T) {
	fw := newFakeControlWatcher()
	app := &controlApp{watcher: fw}
	if _, err := app.computerOrigin("missing"); err == nil {
		t.Fatal("unknown sender accepted")
	}
	fw.workers["external"] = &classifier.Worker{ID: "external"}
	if _, err := app.computerOrigin("external"); err == nil {
		t.Fatal("external sender accepted")
	}
	fw.workers["brain"] = &classifier.Worker{ID: "brain", Hidden: true}
	if o, err := app.computerOrigin("brain"); err != nil || o != (computers.Origin{}) {
		t.Fatalf("host=%+v %v", o, err)
	}
}

func TestComputerBrainOriginsAndLostWorkReachBrain(t *testing.T) {
	for _, origin := range []computers.Origin{{}, {SessionID: "gone", WorkID: "deleted"}} {
		t.Run(origin.SessionID, func(t *testing.T) {
			fw := newFakeControlWatcher()
			fw.workers["host"] = &classifier.Worker{ID: "host", Hidden: true, State: classifier.StateRunning}
			store := newControlBrainStore(t)
			if err := store.SetHostSession("host", "codex"); err != nil {
				t.Fatal(err)
			}
			if err := store.SetChatState(brain.ChatState{ThreadID: "original"}); err != nil {
				t.Fatal(err)
			}
			app := &controlApp{watcher: fw, brainStore: store, brainService: brain.NewService(store, fw, nil)}
			d := computers.Delivery{Identity: "computer:peer:brain", Receipt: "computer:peer:brain:1", Body: "Brain answer", Origin: origin}
			result, err := app.deliverComputerMessage(d)
			if err != nil || result != computers.DispositionAccepted {
				t.Fatalf("result=%s err=%v", result, err)
			}
			if len(fw.sent) != 1 || fw.sent[0].id != "host" {
				t.Fatalf("sent=%+v", fw.sent)
			}
			if err := store.SetChatState(brain.ChatState{ThreadID: "next"}); err != nil {
				t.Fatal(err)
			}
			result, err = app.deliverComputerMessage(d)
			if err != nil || result != computers.DispositionAccepted || len(fw.sent) != 1 {
				t.Fatalf("replay=%s %v sent=%+v", result, err, fw.sent)
			}
		})
	}
}

func TestComputerOriginKeepsCompletedWorkAssociation(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.workers["%7"] = &classifier.Worker{ID: "%7", Delegated: true, Command: "codex"}
	store := newControlBrainStore(t)
	fw.turnStore = store
	item, err := store.CreateWork(brain.Work{Title: "original Work", Objective: "reply after completion", CompletionPolicy: brain.CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{watcher: fw, brainStore: store}
	response := app.handleWorkerSend(control.Request{WorkerID: "%7", WorkID: item.ID, Text: "job", Submit: true})
	if !response.OK {
		t.Fatalf("send=%+v", response)
	}
	if _, err := store.FSM().Complete(lifecycle.WorkID(item.ID), 0, "test", "completed before late send"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	origin, err := app.computerOrigin("%7")
	if err != nil || origin.WorkID != item.ID {
		t.Fatalf("origin=%+v err=%v", origin, err)
	}
}

type computerPresenceWatcher struct {
	*fakeControlWatcher
	presence watcher.SessionPresence
	probeErr error
	pane     string
}

func (w *computerPresenceWatcher) ProbeSession(string) (watcher.SessionPresence, error) {
	return w.presence, w.probeErr
}
func (w *computerPresenceWatcher) ResolveOwnedGeneration(id string) (watcher.OwnedGeneration, error) {
	return watcher.OwnedGeneration{SessionID: id, PaneGeneration: w.pane}, nil
}

func TestComputerReplyDoesNotEnterRecycledOrUnprovenSession(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown=%t", unknown), func(t *testing.T) {
			fw := &computerPresenceWatcher{fakeControlWatcher: newFakeControlWatcher(), presence: watcher.SessionPresencePresent, pane: "replacement"}
			fw.workers["%7"] = &classifier.Worker{ID: "%7", Delegated: true}
			if unknown {
				fw.presence = watcher.SessionPresenceUnknown
				fw.probeErr = errors.New("probe unavailable")
			}
			store := newControlBrainStore(t)
			item, err := store.CreateWork(brain.Work{Title: "original", Objective: "no misdelivery", CompletionPolicy: brain.CompletionBounded})
			if err != nil {
				t.Fatal(err)
			}
			app := &controlApp{watcher: fw, brainStore: store}
			d := computers.Delivery{Identity: "computer:peer:reply", Receipt: "computer:peer:reply:1", Body: "original reply", Origin: computers.Origin{SessionID: "%7", PaneGeneration: "original", WorkID: item.ID}}
			result, err := app.deliverComputerMessage(d)
			events, _ := store.ListWorkEvents(item.ID)
			if len(fw.submitted) != 0 {
				t.Fatal("reply entered a replacement Session")
			}
			if unknown {
				if err == nil || result != computers.DispositionNotSubmitted || len(events) != 0 {
					t.Fatalf("unknown=%s %v events=%+v", result, err, events)
				}
			} else if err != nil || result != computers.DispositionAccepted || len(events) != 1 || !events[0].Actionable {
				t.Fatalf("replacement=%s %v events=%+v", result, err, events)
			}
		})
	}
}
