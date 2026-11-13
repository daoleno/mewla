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
