package brain

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/calendar"
)

// launchingRunner stands in for the Worker launch: it hands out one fresh
// Session per run, exactly like the Calendar's own runner.
type launchingRunner struct{ launches atomic.Int32 }

func (r *launchingRunner) RunScheduledAction(_ context.Context, item calendar.Item, run calendar.Run) (calendar.ActionResult, error) {
	n := r.launches.Add(1)
	return calendar.ActionResult{WorkID: run.ID, WorkerSession: "%" + string(rune('0'+n)), Launched: true}, nil
}

// routeDrained feeds every Calendar event the real store has emitted so far to
// Brain, the way the daemon's broadcast loop does.
func routeDrained(t *testing.T, service *Service, events <-chan calendar.Event) int {
	t.Helper()
	routed := 0
	for {
		select {
		case event := <-events:
			if _, err := service.RouteCalendarEvent(event); err != nil {
				t.Fatalf("routing %s: %v", event.Item.Runs[len(event.Item.Runs)-1].Status, err)
			}
			routed++
		default:
			return routed
		}
	}
}

func TestScheduledActionRunThroughCalendarSchedulerGivesOneWorkAndOneWorker(t *testing.T) {
	store, service := newCalendarRouteService(t)
	calStore, err := calendar.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runner := &launchingRunner{}
	scheduler := calendar.NewScheduler(calStore, runner)
	_, events := calStore.Subscribe()

	due := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	item, err := calStore.Create(calendar.Item{
		Kind: calendar.KindScheduledAction, Title: "Midnight release", DueAt: &due,
		Timezone: "UTC", Recurrence: calendar.RecurrenceDaily,
		ActionInstruction: "Run the release checklist", SourceThreadID: calendarRouteThread,
	})
	if err != nil {
		t.Fatal(err)
	}
	routeDrained(t, service, events)

	// The run fires: Claim and RecordLaunch each emit an item change.
	if _, err := scheduler.RunNow(context.Background(), item.ID); err != nil {
		t.Fatal(err)
	}
	routeDrained(t, service, events)
	// A second firing while the first is in flight is refused, not duplicated.
	if _, err := scheduler.RunNow(context.Background(), item.ID); err == nil {
		t.Fatal("a run in flight must refuse a second claim")
	}
	routeDrained(t, service, events)
	if got := runner.launches.Load(); got != 1 {
		t.Fatalf("one run must launch one Worker, launched %d", got)
	}

	running, _ := calStore.Get(item.ID)
	run := running.Runs[0]
	works := calendarRouteWorks(t, store, calendarWorkPrefix)
	if len(works) != 1 || run.WorkerSession == "" || works[0].ContextRef != "calendar:"+item.ID+":"+run.ID {
		t.Fatalf("one Work owned by the launched Worker expected: run=%+v works=%+v", run, works)
	}

	// The Worker finishes; Calendar delivers the result and Brain's Work ends once.
	if _, err := calStore.FinishRun(item.ID, run.ID, "released", ""); err != nil {
		t.Fatal(err)
	}
	routeDrained(t, service, events)
	done, _ := store.Work(works[0].ID)
	kinds := calendarRouteEventKinds(t, store, works[0].ID)
	if done.Status != WorkDone || kinds["calendar.result"] != 1 || kinds["calendar.launched"] != 1 || kinds["calendar.due"] != 1 {
		t.Fatalf("finished run: work=%+v events=%+v", done, kinds)
	}

	// Daemon restart: Brain starts over from disk and Calendar re-announces its items.
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewService(reopened, nil, nil)
	finished, _ := calStore.Get(item.ID)
	for _, replay := range []calendar.Item{running, finished} {
		if _, err := restarted.RouteCalendarEvent(calendar.Event{Item: replay}); err != nil {
			t.Fatalf("restart replay: %v", err)
		}
	}
	if after := calendarRouteWorks(t, reopened, calendarWorkPrefix); len(after) != 1 || after[0].Status != WorkDone {
		t.Fatalf("restart replay changed the Work: %+v", after)
	}
	if kinds := calendarRouteEventKinds(t, reopened, works[0].ID); kinds["calendar.result"] != 1 {
		t.Fatalf("restart replay duplicated the result: %+v", kinds)
	}

	// The recurring item's next occurrence is its own run, Work and Worker.
	if _, err := scheduler.RunNow(context.Background(), item.ID); err != nil {
		t.Fatal(err)
	}
	routeDrained(t, restarted, events)
	if got := runner.launches.Load(); got != 2 {
		t.Fatalf("next occurrence must launch its own Worker, launched %d", got)
	}
	all := calendarRouteWorks(t, reopened, calendarWorkPrefix)
	if len(all) != 2 || all[0].ID == all[1].ID {
		t.Fatalf("next occurrence must own a second Work: %+v", all)
	}
}
