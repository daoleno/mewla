package server

import (
	"fmt"
	"testing"

	"github.com/daoleno/mewla/daemon/calendar"
)

// A list or broadcast carries the newest runs with a preview, never a result,
// and never a run's failure as the scheduled item's own.
func TestCalendarItemWireCarriesNewestRunPreviews(t *testing.T) {
	item := calendar.Item{ID: "item", Status: calendar.StatusScheduled, FailureReason: "old run failed"}
	for index := 0; index < calendarWireRuns+5; index++ {
		item.Runs = append(item.Runs, calendar.Run{ID: fmt.Sprintf("run-%02d", index), Status: calendar.StatusCompleted, Result: fmt.Sprintf("## Result %d\n\nbody", index)})
	}
	wire := calendarItemWire(item)
	if len(wire.Runs) != calendarWireRuns || wire.Runs[0].ID != "run-05" || wire.Runs[len(wire.Runs)-1].ID != "run-14" {
		t.Fatalf("runs = %+v", wire.Runs)
	}
	for _, run := range wire.Runs {
		if run.Result != "" || run.ResultPreview == "" {
			t.Fatalf("run %s: result %q preview %q", run.ID, run.Result, run.ResultPreview)
		}
	}
	if wire.FailureReason != "" {
		t.Fatalf("scheduled item carries %q", wire.FailureReason)
	}
	if item.Runs[0].Result == "" {
		t.Fatal("the wire copy changed the stored runs")
	}
}
