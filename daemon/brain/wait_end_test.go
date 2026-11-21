package brain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// waitClock is one clock for the store, its lifecycle engine and the service.
type waitClock struct{ now time.Time }

func (c *waitClock) at() time.Time { return c.now }

func (c *waitClock) advance(d time.Duration) { c.now = c.now.Add(d) }

func waitEndFixture(t *testing.T) (*Store, *Service, *waitClock) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clock := &waitClock{now: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)}
	store.now = clock.at
	service := newTestService(t, store, &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Worker{}}, nil)
	service.now = clock.at
	return store, service, clock
}

func parkedWork(t *testing.T, store *Store, service *Service, title, waitFor string, until *time.Time) Work {
	t.Helper()
	item, err := store.CreateWork(Work{Title: title, Objective: title})
	if err != nil {
		t.Fatal(err)
	}
	waiting := WorkWaiting
	item, err = service.BrainUpdateWork(item.ID, WorkUpdate{Status: &waiting, WaitFor: &waitFor, WaitUntil: until})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func overdueBatches(t *testing.T, store *Store) []Work {
	t.Helper()
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	var out []Work
	for _, item := range items {
		if strings.HasPrefix(item.ID, overdueWorkPrefix) {
			out = append(out, item)
		}
	}
	return out
}

// batchList is the list one batch Work's event carries, by Work id.
func batchList(t *testing.T, store *Store, batch Work) map[string]OverdueWait {
	t.Helper()
	listed := map[string]OverdueWait{}
	for _, event := range mustWorkEvents(t, store, batch.ID) {
		if event.Kind != overdueEventKind {
			continue
		}
		var brief overdueBrief
		if err := json.Unmarshal([]byte(event.DetailsJSON), &brief); err != nil {
			t.Fatal(err)
		}
		for _, entry := range brief.Overdue {
			listed[entry.WorkID] = entry
		}
	}
	return listed
}

func raise(t *testing.T, service *Service) {
	t.Helper()
	if err := service.raiseOverdueWaits(); err != nil {
		t.Fatal(err)
	}
}

func TestNamedWaitGetsTheDefaultEndOrItsOwn(t *testing.T) {
	store, service, clock := waitEndFixture(t)
	own := clock.now.Add(3 * time.Hour)
	withTime := parkedWork(t, store, service, "with a time", "the 0.4.18 release", &own)
	if withTime.Wake == nil || withTime.Wake.Until == nil || !withTime.Wake.Until.Equal(own) {
		t.Fatalf("wait with -until = %+v", withTime.Wake)
	}
	without := parkedWork(t, store, service, "without", "the owner's try", nil)
	if without.Wake == nil || without.Wake.Until == nil || !without.Wake.Until.Equal(clock.now.Add(WaitDefaultEnd)) {
		t.Fatalf("wait without -until = %+v, want the default end", without.Wake)
	}
	// Setting the same wait again with a new end keeps its start.
	clock.advance(time.Hour)
	later := clock.now.Add(48 * time.Hour)
	got, err := service.BrainUpdateWork(withTime.ID, WorkUpdate{WaitUntil: &later})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := store.fsmState(got.ID)
	if got.Wake.Until == nil || !got.Wake.Until.Equal(later) || !st.Wake.Since.Equal(clock.now.Add(-time.Hour)) {
		t.Fatalf("moved end = %+v since %v", got.Wake, st.Wake.Since)
	}
	past := clock.now.Add(-time.Minute)
	if _, err := service.BrainUpdateWork(without.ID, WorkUpdate{WaitUntil: &past}); err == nil {
		t.Fatal("an end in the past was accepted")
	}
	question := "Which build?"
	waiting := WorkWaiting
	asked, _ := store.CreateWork(Work{Title: "asked", Objective: "asked"})
	if _, err := service.BrainUpdateWork(asked.ID, WorkUpdate{Status: &waiting, Question: &question, WaitUntil: &own}); err == nil {
		t.Fatal("a question took an end: it waits on the person's reply, which the runtime delivers")
	}
}

// The wait Brain sets while it reads a result (the review path) gets an end too.
func TestWaitSetWhileReadingAResultGetsAnEnd(t *testing.T) {
	store, service, item, _ := hostHandlingFixture(t)
	waitFor := "the reviewer's second round"
	waiting := WorkWaiting
	until := time.Now().Add(5 * time.Hour).UTC().Truncate(time.Second)
	if _, err := service.BrainUpdateWork(item.ID, WorkUpdate{Status: &waiting, WaitFor: &waitFor, WaitUntil: &until}); err != nil {
		t.Fatal(err)
	}
	st, err := store.fsmState(item.ID)
	if err != nil || st.Wake == nil || st.Wake.Kind != lifecycle.WakeNamed || st.Wake.Until == nil || !st.Wake.Until.Equal(until) {
		t.Fatalf("wake after a review wait = %+v err=%v", st.Wake, err)
	}
	resolved := false
	if events, err := store.FSM().ReviewResolutions(lifecycle.WorkID(item.ID)); err == nil {
		for _, event := range events {
			if p, ok := event.Payload.(lifecycle.ReviewResolvedPayload); ok && p.WakeUntil != nil && p.WakeUntil.Equal(until) {
				resolved = true
			}
		}
	}
	if !resolved {
		t.Fatal("the wait did not go through the review's own disposition")
	}

	// Without -until it gets the default end.
	store2, service2, item2, _ := hostHandlingFixture(t)
	if _, err := service2.BrainUpdateWork(item2.ID, WorkUpdate{Status: &waiting, WaitFor: &waitFor}); err != nil {
		t.Fatal(err)
	}
	st2, _ := store2.fsmState(item2.ID)
	if st2.Wake == nil || st2.Wake.Until == nil || (st2.Wake.Until.Sub(st2.Wake.Since)-WaitDefaultEnd).Abs() > time.Second {
		t.Fatalf("review wait without -until = %+v", st2.Wake)
	}
}

func TestOverdueWaitsRaiseOneBatchedEventAfterTheirEnd(t *testing.T) {
	store, service, clock := waitEndFixture(t)
	twoHours := clock.now.Add(2 * time.Hour)
	release := parkedWork(t, store, service, "Release check", "the 0.4.18 release", &twoHours)
	device := parkedWork(t, store, service, "Device test", "the owner's real-device test", nil)
	oneHour := clock.now.Add(time.Hour)
	watcherWork := parkedWork(t, store, service, "Trigger: Mac back online", "trigger.fired", &oneHour)
	service.SetStandingWork(func(id string) bool { return id == watcherWork.ID })

	// Nothing before an end.
	clock.advance(time.Hour + 59*time.Minute)
	raise(t, service)
	if got := overdueBatches(t, store); len(got) != 0 {
		t.Fatalf("a batch before any end: %+v", got)
	}

	// After the first end: exactly one event, listing only that Work; the
	// Trigger Work, past its own end, is never listed.
	clock.advance(2 * time.Minute)
	raise(t, service)
	raise(t, service)
	batches := overdueBatches(t, store)
	if len(batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(batches))
	}
	listed := batchList(t, store, batches[0])
	if len(listed) != 1 || listed[release.ID].WaitsFor != "the 0.4.18 release" || listed[release.ID].SetBy != "brain" {
		t.Fatalf("first list = %+v", listed)
	}
	st, _ := store.fsmState(batches[0].ID)
	if st.Review == nil {
		t.Fatal("the batch opened no Review: Brain is not woken")
	}
	// Nothing happened to the overdue Work itself.
	if got, _ := store.Work(release.ID); got.Status != WorkWaiting {
		t.Fatalf("the overdue Work changed: %+v", got)
	}

	// The device test runs out the same day: it waits for the next day's event.
	clock.advance(23 * time.Hour)
	raise(t, service)
	if got := overdueBatches(t, store); len(got) != 1 {
		t.Fatalf("a second event within a day: %d", len(got))
	}

	// The release Work is closed in between; the second day lists only what
	// is still open, and yesterday's unread list is replaced, not stacked.
	closed := WorkDone
	if _, err := service.BrainUpdateWork(release.ID, WorkUpdate{Status: &closed}); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Hour)
	raise(t, service)
	batches = overdueBatches(t, store)
	if len(batches) != 2 {
		t.Fatalf("batches on the second day = %d, want 2", len(batches))
	}
	var newest, oldest Work
	for _, batch := range batches {
		if newest.ID == "" || batch.CreatedAt.After(newest.CreatedAt) {
			newest = batch
		}
	}
	for _, batch := range batches {
		if batch.ID != newest.ID {
			oldest = batch
		}
	}
	listed = batchList(t, store, newest)
	if len(listed) != 1 || listed[device.ID].SetBy != "brain" || listed[device.ID].Age != "26h" {
		t.Fatalf("second list = %+v", listed)
	}
	if got, _ := store.Work(oldest.ID); got.Status != WorkCancelled {
		t.Fatalf("yesterday's unread list = %s, want replaced", got.Status)
	}
}

func TestNoEventWhenNothingIsOverdue(t *testing.T) {
	store, service, clock := waitEndFixture(t)
	parkedWork(t, store, service, "Device test", "the owner's real-device test", nil)
	clock.advance(WaitDefaultEnd - time.Minute)
	raise(t, service)
	if got := overdueBatches(t, store); len(got) != 0 {
		t.Fatalf("batch with nothing overdue: %+v", got)
	}
}

func TestRestartNeitherRepeatsNorLosesTheEvent(t *testing.T) {
	store, service, clock := waitEndFixture(t)
	soon := clock.now.Add(time.Hour)
	item := parkedWork(t, store, service, "Release check", "the 0.4.18 release", &soon)
	clock.advance(2 * time.Hour)
	raise(t, service)

	restart := func() (*Store, *Service) {
		reopened, err := NewStore(store.Root)
		if err != nil {
			t.Fatal(err)
		}
		reopened.now = clock.at
		next := newTestService(t, reopened, &fakeWatcher{turnStore: reopened, sessions: map[string]*classifier.Worker{}}, nil)
		next.now = clock.at
		return reopened, next
	}
	// A restart (which also runs the backfill) does not raise it again.
	restarted, restartedService := restart()
	if err := restartedService.backfillWaitEnds(); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Minute)
	raise(t, restartedService)
	if got := overdueBatches(t, restarted); len(got) != 1 {
		t.Fatalf("batches after a restart = %d, want 1", len(got))
	}

	// A raise cut short (its Work written, its event not) is finished after
	// the restart, on the same Work.
	store2, service2, clock2 := waitEndFixture(t)
	soon2 := clock2.now.Add(time.Hour)
	item2 := parkedWork(t, store2, service2, "Release check", "the 0.4.18 release", &soon2)
	clock2.advance(2 * time.Hour)
	half := overdueWorkPrefix + clock2.now.Format("20060102T150405Z")
	if _, _, err := store2.EnsureWork(Work{ID: half, Title: "Waits past their end", Objective: "x", CompletionPolicy: CompletionBounded}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(store2.Root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.now = clock2.at
	next := newTestService(t, reopened, &fakeWatcher{turnStore: reopened, sessions: map[string]*classifier.Worker{}}, nil)
	next.now = clock2.at
	clock2.advance(time.Minute)
	raise(t, next)
	batches := overdueBatches(t, reopened)
	if len(batches) != 1 || batches[0].ID != half {
		t.Fatalf("batches = %+v, want the half-made one", batches)
	}
	if listed := batchList(t, reopened, batches[0]); listed[item2.ID].WorkID == "" {
		t.Fatalf("the finished batch does not list the Work: %+v", listed)
	}
	if st, _ := reopened.fsmState(half); st.Review == nil {
		t.Fatal("the finished batch opened no Review")
	}
	_ = item
}

// A keep verdict is a wait with an end. Judged again at the same revision it
// keeps its end, so a Work the judge keeps every day still runs out.
func TestKeepVerdictEndDoesNotSlide(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictKeep, Reason: "Brain is waiting for the owner's real-device test."}}
	store, service, item := endedHandlingFixture(t, judge)
	start := service.nowUTC()
	clock := &waitClock{now: start}
	service.now, store.now = clock.at, clock.at
	runSettle(t, service)
	got, _ := store.Work(item.ID)
	if got.Kept == nil || got.Kept.Until == nil || !got.Kept.Until.Equal(start.Add(WaitDefaultEnd)) {
		t.Fatalf("kept = %+v", got.Kept)
	}
	firstUntil := *got.Kept.Until
	clock.advance(keptRejudgeAfter + time.Minute)
	runSettle(t, service)
	got, _ = store.Work(item.ID)
	if len(judge.briefs) != 2 || got.Kept == nil || !got.Kept.Until.Equal(firstUntil) || !got.Kept.Since.Equal(start) {
		t.Fatalf("rejudged keep = %+v (briefs %d)", got.Kept, len(judge.briefs))
	}
	raise(t, service)
	batches := overdueBatches(t, store)
	if len(batches) != 1 {
		t.Fatalf("batches = %d", len(batches))
	}
	listed := batchList(t, store, batches[0])
	if entry := listed[item.ID]; entry.SetBy != "judge" || !strings.Contains(entry.WaitsFor, "real-device test") {
		t.Fatalf("kept Work in the list = %+v", listed)
	}
	// The judge's own earlier keep is not shown to it as a fact.
	if strings.Contains(judge.briefs[1], "brain.settle_kept") {
		t.Fatalf("the judge was shown its own earlier keep:\n%s", judge.briefs[1])
	}
}

// Waits set before ends existed get the default end from the upgrade, once:
// the first start raises nothing, and a later start does not move the end.
func TestUpgradeGivesOldWaitsTheDefaultEndOnce(t *testing.T) {
	store, service, clock := waitEndFixture(t)
	item, err := store.CreateWork(Work{Title: "old wait", Objective: "old wait"})
	if err != nil {
		t.Fatal(err)
	}
	// An old binary set it three days ago, with no end.
	if _, err := store.FSM().SetWait(lifecycle.WorkID(item.ID), lifecycle.WakeNamed, "PrTick1121c's result"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	clock.advance(72 * time.Hour)
	if err := service.backfillWaitEnds(); err != nil {
		t.Fatal(err)
	}
	raise(t, service)
	if got := overdueBatches(t, store); len(got) != 0 {
		t.Fatalf("the upgrade raised an event: %+v", got)
	}
	st, _ := store.fsmState(item.ID)
	upgradeEnd := clock.now.Add(WaitDefaultEnd)
	if st.Wake == nil || st.Wake.Until == nil || !st.Wake.Until.Equal(upgradeEnd) || !st.Wake.Since.Equal(clock.now.Add(-72*time.Hour)) {
		t.Fatalf("old wait after the upgrade = %+v", st.Wake)
	}
	clock.advance(time.Hour)
	if err := service.backfillWaitEnds(); err != nil {
		t.Fatal(err)
	}
	if st, _ = store.fsmState(item.ID); !st.Wake.Until.Equal(upgradeEnd) {
		t.Fatalf("a later start moved the end to %v", st.Wake.Until)
	}
	clock.advance(WaitDefaultEnd)
	raise(t, service)
	if got := overdueBatches(t, store); len(got) != 1 {
		t.Fatalf("batches after the upgrade end = %d, want 1", len(got))
	}
}

// Brain reads the list once; then it is closed as a fact, with no model call.
func TestReadOverdueListClosesWithoutAJudge(t *testing.T) {
	store, service, clock := waitEndFixture(t)
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	soon := clock.now.Add(time.Hour)
	parkedWork(t, store, service, "Release check", "the 0.4.18 release", &soon)
	clock.advance(2 * time.Hour)
	raise(t, service)
	batch := overdueBatches(t, store)[0]
	claimed, _ := claimAndDeliverTestReview(t, store, "host")
	if claimed.WorkID != batch.ID || claimed.Kind != overdueEventKind {
		t.Fatalf("Brain was handed %+v, want the batch", claimed)
	}
	input, err := marshalDirectWorkEventInput(claimed, batch)
	if err != nil {
		t.Fatal(err)
	}
	parsed, ok := work.ParseCanonicalDirectWorkEventInput(input)
	if !ok || !strings.Contains(string(parsed.OverdueWaits), "the 0.4.18 release") {
		t.Fatalf("Brain's input does not carry the list: %s", input)
	}
	settleCanonicalHostTurnForTest(t, store, "host", claimed.ProviderTurnID)
	if _, ended, err := store.EndReviewDelivery(batch.ID, claimed.HandlingID, claimed.ProviderTurnID); err != nil || !ended {
		t.Fatalf("end handling = %v, %v", ended, err)
	}
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictKeep, Reason: "x"}}
	service.SetWorkJudge(judge)
	clock.advance(settleDebounce + time.Minute)
	if err := service.settleWorkLocked(); err != nil {
		t.Fatal(err)
	}
	service.waitBackground()
	got, _ := store.Work(batch.ID)
	if got.Status != WorkDone || len(judge.briefs) != 0 {
		t.Fatalf("read list = %s, judge calls %d", got.Status, len(judge.briefs))
	}
}

// Twelve entries at their longest still fit Brain's event input.
func TestOverdueListFitsTheEventInput(t *testing.T) {
	long := strings.Repeat("x", 600)
	var listed []OverdueWait
	for i := 0; i < overdueListLimit; i++ {
		listed = append(listed, OverdueWait{WorkID: strings.Repeat("a", 36), Title: judgeClip(long, 120), WaitsFor: judgeClip(long, 300),
			SetBy: "judge", Since: time.Now(), Until: time.Now(), Age: "12d"})
	}
	details, _ := json.Marshal(overdueBrief{Overdue: listed, More: 30})
	action := WorkReviewAction{EventID: "e", WorkID: "w", HandlingID: "h", ProviderTurnID: "p", DeliveryWorkRevision: 1,
		Kind: overdueEventKind, Summary: overdueSummary(listed), DetailsJSON: string(details)}
	if _, err := marshalDirectWorkEventInput(action, Work{Title: "Waits past their end"}); err != nil {
		t.Fatal(err)
	}
}

var _ = watcher.EvidenceControl
