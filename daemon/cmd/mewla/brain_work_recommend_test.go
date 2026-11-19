package main

import (
	"io"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/control"
)

// `mewla brain work update -question … -choice … -recommend …` reaches the Work
// over control; a -recommend that names no -choice is refused before sending.
func TestBrainWorkUpdateCarriesTheRecommendedChoice(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(brain.Work{Title: "trial", Objective: "pick a trial length"})
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{watcher: newFakeControlWatcher(), brainStore: store, brainService: brain.NewService(store, newFakeControlWatcher(), nil)}
	response := app.HandleControlRequest(control.Request{
		Type: "brain_work_update", WorkID: item.ID, WorkFields: []string{"question", "choices", "recommended"},
		BrainWork: &brain.Work{Question: "How long a trial?", Choices: []string{"7 days", "14 days"}, Recommended: "14 days"},
	})
	if !response.OK || response.BrainWork == nil || response.BrainWork.Recommended != "14 days" {
		t.Fatalf("response = %+v", response)
	}
	err = runBrainWorkUpdate([]string{"-id", item.ID, "-question", "How long?", "-choice", "7 days", "-recommend", "30 days"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-recommend must be one of the -choice values") {
		t.Fatalf("a pick outside the choices = %v", err)
	}
}

// `work update -status waiting` with nothing asked parks the Work on a
// user_input wake: Brain is told the person is not asked, and how to ask
// (owner 2026-11-19). With a question, or a terminal status, nothing is said.
func TestBrainWorkWaitingWithNothingAskedTellsBrain(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(brain.Work{Title: "video", Objective: "ship the H.264 path"})
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{watcher: newFakeControlWatcher(), brainStore: store, brainService: brain.NewService(store, newFakeControlWatcher(), nil)}
	response := app.HandleControlRequest(control.Request{
		Type: "brain_work_update", WorkID: item.ID, WorkFields: []string{"status"}, BrainWork: &brain.Work{Status: brain.WorkWaiting},
	})
	notice, _ := response.Context.(map[string]string)
	if !response.OK || !strings.Contains(notice["notice"], "-question") || !strings.Contains(notice["notice"], item.ID) {
		t.Fatalf("a wait with nothing asked tells Brain how to ask: %+v", response)
	}
	response = app.HandleControlRequest(control.Request{
		Type: "brain_work_update", WorkID: item.ID, WorkFields: []string{"status", "wait_for"},
		BrainWork: &brain.Work{Status: brain.WorkWaiting, WaitFor: "the Mac mini comes back online"},
	})
	if !response.OK || response.Context != nil {
		t.Fatalf("a wait Brain names needs no notice: %+v", response)
	}
	response = app.HandleControlRequest(control.Request{
		Type: "brain_work_update", WorkID: item.ID, WorkFields: []string{"status", "question", "choices", "recommended"},
		BrainWork: &brain.Work{Status: brain.WorkWaiting, Question: "Ship it now?", Choices: []string{"Ship", "Wait"}, Recommended: "Ship"},
	})
	if !response.OK || response.Context != nil {
		t.Fatalf("a question needs no notice: %+v", response)
	}
}
