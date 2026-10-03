package main

import (
	"testing"

	"github.com/daoleno/zen/daemon/brain"
)

func TestBrainWorkListDefaultsToOpenWorkWithoutObjectives(t *testing.T) {
	items := []brain.Work{
		{ID: "open", Status: brain.WorkOpen, Objective: "long brief"},
		{ID: "waiting", Status: brain.WorkWaiting, Objective: "long brief"},
		{ID: "done", Status: brain.WorkDone, Objective: "long brief"},
		{ID: "cancelled", Status: brain.WorkCancelled, Objective: "long brief"},
	}
	ids := func(list []brain.Work) []string {
		out := []string{}
		for _, item := range list {
			out = append(out, item.ID)
		}
		return out
	}

	compact := compactBrainWorkList(items, false, false)
	if got := ids(compact); len(got) != 2 || got[0] != "open" || got[1] != "waiting" {
		t.Fatalf("default ids=%v", got)
	}
	for _, item := range compact {
		if item.Objective != "" {
			t.Fatalf("default listing kept objective for %s", item.ID)
		}
	}
	if items[0].Objective != "long brief" {
		t.Fatal("compaction mutated the response items")
	}

	full := compactBrainWorkList(items, true, true)
	if len(full) != len(items) || full[2].Objective != "long brief" {
		t.Fatalf("-all -full must return every Work unchanged: %+v", full)
	}
}
