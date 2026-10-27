package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/calendar"
)

const calendarRouteThread = "brain-thread:calendar-route"

func newCalendarRouteService(t *testing.T) (*Store, *Service) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: calendarRouteThread}); err != nil {
		t.Fatal(err)
	}
	return store, NewService(store, nil, nil)
}

func calendarRouteItem(itemID string, runs ...calendar.Run) calendar.Item {
	return calendar.Item{
		ID:                itemID,
		Kind:              calendar.KindScheduledAction,
		Title:             "Midnight release",
		ActionInstruction: "Run the release checklist",
		SourceThreadID:    calendarRouteThread,
		Runs:              runs,
	}
}

func calendarRouteRun(runID string, status calendar.Status, session string) calendar.Run {
	return calendar.Run{
		ID: runID, Title: "Midnight release", SourceThreadID: calendarRouteThread,
		StartedAt: time.Date(2026, 10, 24, 0, 20, 0, 0, time.UTC),
		Status:    status, WorkerSession: session,
	}
}

func calendarRouteResult(item calendar.Item, run calendar.Run) *calendar.ScheduledResult {
	return &calendar.ScheduledResult{
		ID: "result-" + run.ID, ThreadID: calendarRouteThread, Body: "released",
		Status: run.Status, Title: run.Title, CalendarItemID: item.ID, CalendarRunID: run.ID,
	}
}

func calendarRouteRoute(t *testing.T, service *Service, item calendar.Item, result *calendar.ScheduledResult) {
	t.Helper()
	if _, err := service.RouteCalendarEvent(calendar.Event{Item: item, ScheduledResult: result}); err != nil {
		t.Fatalf("route %s: %v", item.Runs[len(item.Runs)-1].Status, err)
	}
}

func calendarRouteWorks(t *testing.T, store *Store, prefix string) []Work {
	t.Helper()
	all, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	var out []Work
	for _, item := range all {
		if strings.HasPrefix(item.ID, prefix) {
			out = append(out, item)
		}
	}
	return out
}

func calendarRouteEventKinds(t *testing.T, store *Store, workID string) map[string]int {
	t.Helper()
	events, err := store.ListWorkEvents(workID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, event := range events {
		kinds[event.Kind]++
	}
	return kinds
}

// A scheduled run is routed on every Calendar item change: once when it is
// claimed, again when the Worker is recorded, and again on any replay.
func TestRouteCalendarEventLaunchedRunKeepsOneWorkAndRecordsEveryStageOnce(t *testing.T) {
	store, service := newCalendarRouteService(t)
	claimed := calendarRouteItem("item-1", calendarRouteRun("run-1", calendar.StatusRunning, ""))
	launched := calendarRouteItem("item-1", calendarRouteRun("run-1", calendar.StatusRunning, "%77"))

	calendarRouteRoute(t, service, claimed, nil)
	calendarRouteRoute(t, service, launched, nil)
	calendarRouteRoute(t, service, launched, nil)
	calendarRouteRoute(t, service, claimed, nil) // a late replay of the older stage

	works := calendarRouteWorks(t, store, calendarWorkPrefix)
	if len(works) != 1 || works[0].ID != calendarWorkID("item-1", "run-1") {
		t.Fatalf("one run must own exactly one Work: %+v", works)
	}
	kinds := calendarRouteEventKinds(t, store, works[0].ID)
	if kinds["calendar.due"] != 1 || kinds["calendar.launched"] != 1 {
		t.Fatalf("each stage must be recorded once: %+v", kinds)
	}
}

func TestRouteCalendarEventLaunchedRunSurvivesRestartAndNeverBecomesLost(t *testing.T) {
	store, service := newCalendarRouteService(t)
	claimed := calendarRouteItem("item-1", calendarRouteRun("run-1", calendar.StatusRunning, ""))
	launched := calendarRouteItem("item-1", calendarRouteRun("run-1", calendar.StatusRunning, "%77"))
	calendarRouteRoute(t, service, claimed, nil)
	calendarRouteRoute(t, service, launched, nil)

	workID := calendarWorkID("item-1", "run-1")
	before, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	// Calendar owns the Worker's execution: it is not a lifecycle Attempt, so
	// nothing can lease-expire it while it works.
	if before.Status != WorkWaiting || before.Wake == nil || before.Wake.Kind != WorkWakeCalendarResult ||
		before.Wake.Ref != "calendar:item-1:run-1" {
		t.Fatalf("a running occurrence must leave its Work waiting on the result, not open: %+v", before)
	}
	if before.AttemptSessionID != "" || before.ContextRef != "calendar:item-1:run-1" {
		t.Fatalf("calendar Work identity: %+v", before)
	}

	// Routing the same stages after a daemon restart changes nothing.
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewService(reopened, nil, nil)
	calendarRouteRoute(t, restarted, claimed, nil)
	calendarRouteRoute(t, restarted, launched, nil)
	after, err := reopened.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision {
		t.Fatalf("restart replay changed the Work: before=%+v after=%+v", before, after)
	}
	if works := calendarRouteWorks(t, reopened, calendarWorkPrefix); len(works) != 1 {
		t.Fatalf("restart replay created a second Work: %+v", works)
	}

	// An action that runs for hours must not wake Brain with a lost Session.
	base := time.Now()
	reopened.now = func() time.Time { return base.Add(5 * time.Hour) }
	if err := reopened.SweepLifecycle(); err != nil {
		t.Fatal(err)
	}
	long, err := reopened.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if long.Status == WorkNeedsInput || long.Review != nil {
		t.Fatalf("a running calendar Worker was declared lost: %+v", long)
	}
	for kind, n := range calendarRouteEventKinds(t, reopened, workID) {
		if kind != "calendar.due" && kind != "calendar.launched" || n != 1 {
			t.Fatalf("unexpected event %s x%d", kind, n)
		}
	}
}

func TestRouteCalendarEventResultIsRecordedOnceAndFinishesTheWork(t *testing.T) {
	store, service := newCalendarRouteService(t)
	workID := calendarWorkID("item-1", "run-1")
	for _, session := range []string{"", "%77"} {
		calendarRouteRoute(t, service, calendarRouteItem("item-1", calendarRouteRun("run-1", calendar.StatusRunning, session)), nil)
	}
	run := calendarRouteRun("run-1", calendar.StatusCompleted, "%77")
	done := calendarRouteItem("item-1", run)
	result := calendarRouteResult(done, run)
	calendarRouteRoute(t, service, done, result)
	calendarRouteRoute(t, service, done, result) // Calendar re-broadcasts the item

	item, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != WorkDone {
		t.Fatalf("completed run must finish its Work: %+v", item)
	}
	kinds := calendarRouteEventKinds(t, store, workID)
	if kinds["calendar.result"] != 1 || kinds["calendar.due"] != 1 || kinds["calendar.launched"] != 1 {
		t.Fatalf("one event per stage expected: %+v", kinds)
	}
	// A stage that arrives after the result changes nothing and is no error.
	calendarRouteRoute(t, service, calendarRouteItem("item-1", calendarRouteRun("run-1", calendar.StatusRunning, "%77")), nil)
	if again, _ := store.Work(workID); again.Status != WorkDone {
		t.Fatalf("late launch stage reopened the Work: %+v", again)
	}
}

func TestRouteCalendarEventFailureReachesBrainWithoutRoutingError(t *testing.T) {
	store, service := newCalendarRouteService(t)
	workID := calendarWorkID("item-1", "run-1")
	calendarRouteRoute(t, service, calendarRouteItem("item-1", calendarRouteRun("run-1", calendar.StatusRunning, "")), nil)
	run := calendarRouteRun("run-1", calendar.StatusFailed, "")
	failed := calendarRouteItem("item-1", run)
	calendarRouteRoute(t, service, failed, calendarRouteResult(failed, run))

	item, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status == WorkDone || item.Status == WorkCancelled {
		t.Fatalf("failure must stay open for Brain: %+v", item)
	}
	if kinds := calendarRouteEventKinds(t, store, workID); kinds["calendar.failure"] != 1 {
		t.Fatalf("failure event must be recorded once: %+v", kinds)
	}
}

func TestRouteCalendarEventGivesEachOccurrenceItsOwnWork(t *testing.T) {
	store, service := newCalendarRouteService(t)
	first := calendarRouteRun("run-1", calendar.StatusCompleted, "%77")
	firstItem := calendarRouteItem("item-1", first)
	calendarRouteRoute(t, service, calendarRouteItem("item-1", calendarRouteRun("run-1", calendar.StatusRunning, "%77")), nil)
	calendarRouteRoute(t, service, firstItem, calendarRouteResult(firstItem, first))

	// The recurring item's next occurrence arrives while the first Work is done.
	second := calendarRouteRun("run-2", calendar.StatusRunning, "")
	secondItem := calendarRouteItem("item-1", first, second)
	calendarRouteRoute(t, service, secondItem, nil)
	secondItem.Runs[1].WorkerSession = "%78"
	calendarRouteRoute(t, service, secondItem, nil)

	works := calendarRouteWorks(t, store, calendarWorkPrefix)
	if len(works) != 2 {
		t.Fatalf("each occurrence must own one Work: %+v", works)
	}
	a, _ := store.Work(calendarWorkID("item-1", "run-1"))
	b, _ := store.Work(calendarWorkID("item-1", "run-2"))
	if a.Status != WorkDone || b.Status == WorkDone || b.ContextRef != "calendar:item-1:run-2" {
		t.Fatalf("occurrences interfered: first=%+v second=%+v", a, b)
	}
}

func TestRouteCalendarEventKeepsInFlightRunWhenNextOccurrenceIsRouted(t *testing.T) {
	store, service := newCalendarRouteService(t)
	first := calendarRouteRun("run-1", calendar.StatusRunning, "%77")
	calendarRouteRoute(t, service, calendarRouteItem("item-1", calendarRouteRun("run-1", calendar.StatusRunning, "")), nil)
	calendarRouteRoute(t, service, calendarRouteItem("item-1", first), nil)

	// The next occurrence is due while run-1 is still running; Calendar claims
	// it separately and each Worker stays attached to its own Work.
	second := calendarRouteRun("run-2", calendar.StatusRunning, "%78")
	calendarRouteRoute(t, service, calendarRouteItem("item-1", first, second), nil)
	// A re-broadcast of the older state must not move run-1's Worker.
	calendarRouteRoute(t, service, calendarRouteItem("item-1", first), nil)

	a, _ := store.Work(calendarWorkID("item-1", "run-1"))
	b, _ := store.Work(calendarWorkID("item-1", "run-2"))
	if a.Status == WorkDone || b.Status == WorkDone || a.ID == b.ID {
		t.Fatalf("in-flight runs must stay separate: first=%+v second=%+v", a, b)
	}
	for _, id := range []string{a.ID, b.ID} {
		if kinds := calendarRouteEventKinds(t, store, id); kinds["calendar.launched"] != 1 {
			t.Fatalf("each run records its own launch once: %s %+v", id, kinds)
		}
	}
}

// Calendar occurrence Work names its schedule and run wherever the app lists
// it, so a running or returned occurrence is never an anonymous new Work.
func TestCalendarOccurrenceWorkNamesItsScheduleAndRun(t *testing.T) {
	store, service := newCalendarRouteService(t)
	calendarRouteRoute(t, service, calendarRouteItem("item-1", calendarRouteRun("run-1", calendar.StatusRunning, "%77")), nil)
	inventory, err := store.ProjectWorkInventory(map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	want := WorkCalendarRef{ItemID: "item-1", RunID: "run-1"}
	found := false
	for _, work := range inventory.Current {
		if work.ID == calendarWorkID("item-1", "run-1") {
			found = work.Calendar != nil && *work.Calendar == want
		}
	}
	for _, work := range inventory.Open {
		if work.ID == calendarWorkID("item-1", "run-1") {
			found = work.Calendar != nil && *work.Calendar == want
		}
	}
	if !found {
		t.Fatalf("calendar ref missing: %+v", inventory)
	}
	if ref := workCalendarRef(Work{ID: calendarWorkID("item-1", "run-1"), ContextRef: "calendar:item-2:run-1"}); ref != nil {
		t.Fatalf("a context that does not match the Work ID was trusted: %+v", ref)
	}
	if ref := workCalendarRef(Work{ID: "ordinary", ContextRef: "calendar:item-1:run-1"}); ref != nil {
		t.Fatalf("ordinary Work got a calendar ref: %+v", ref)
	}
}
