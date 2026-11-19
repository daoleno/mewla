package calendar

import (
	"reflect"
	"testing"
	"time"
)

// A scheduled-action run whose result is in is done with its Worker Session;
// eight such Sessions were left idle for 5–22 hours on 11-19.
func TestFinishedRunSessionsListsOnlyRunsThatEnded(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 2, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	launch := func(id, session string) string {
		due := now.Add(-time.Minute)
		item, err := store.Create(Item{
			ID: id, Title: id, Kind: KindScheduledAction, DueAt: &due, Timezone: "UTC",
			Recurrence: RecurrenceNone, ActionInstruction: "Run", SourceThreadID: "thread-1",
		})
		if err != nil {
			t.Fatal(err)
		}
		_, run, err := store.Claim(item.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.RecordLaunch(item.ID, run.ID, run.ID, session); err != nil {
			t.Fatal(err)
		}
		return run.ID
	}
	doneRun := launch("done", "mh-done")
	failedRun := launch("failed", "mh-failed")
	launch("running", "mh-running")
	if _, err := store.FinishRun("done", doneRun, "the deliverable", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun("failed", failedRun, "", "the agent stopped"); err != nil {
		t.Fatal(err)
	}
	if got := store.FinishedRunSessions(now.Add(-time.Hour)); !reflect.DeepEqual(got, []string{"mh-done", "mh-failed"}) {
		t.Fatalf("finished run Sessions = %v", got)
	}
	if got := store.FinishedRunSessions(now.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("runs older than the window are listed: %v", got)
	}
}
