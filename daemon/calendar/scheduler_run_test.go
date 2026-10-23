package calendar

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

type fakeTimer struct {
	d       time.Duration
	c       chan time.Time
	stopped atomic.Bool
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }
func (t *fakeTimer) Stop() bool          { return !t.stopped.Swap(true) }
func (t *fakeTimer) fire()               { t.c <- time.Time{} }

// runHarness drives Run with a clock and timers the test controls. Every timer
// the loop arms is delivered on armed, so a test asserts what the loop waits
// for without sleeping.
type runHarness struct {
	t         *testing.T
	scheduler *Scheduler
	store     *Store
	armed     chan *fakeTimer
	mu        sync.Mutex
	clock     time.Time
	done      chan struct{}
}

func (h *runHarness) now() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.clock
}

func (h *runHarness) advance(to time.Time) {
	h.mu.Lock()
	h.clock = to
	h.mu.Unlock()
}

func startRun(t *testing.T, runner ActionRunner) *runHarness {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &runHarness{t: t, store: store, armed: make(chan *fakeTimer, 64), clock: localTime(t, "America/New_York", 2026, time.July, 14, 9, 0), done: make(chan struct{})}
	store.now = h.now
	h.scheduler = NewScheduler(store, runner)
	h.scheduler.now = h.now
	h.scheduler.newTimer = func(d time.Duration) wakeTimer {
		timer := &fakeTimer{d: d, c: make(chan time.Time, 1)}
		h.armed <- timer
		return timer
	}
	return h
}

func (h *runHarness) start() {
	ctx, cancel := context.WithCancel(context.Background())
	h.t.Cleanup(func() {
		cancel()
		<-h.done
	})
	go func() {
		defer close(h.done)
		h.scheduler.Run(ctx)
	}()
}

func (h *runHarness) nextTimer() *fakeTimer {
	h.t.Helper()
	select {
	case timer := <-h.armed:
		return timer
	case <-time.After(2 * time.Second):
		h.t.Fatal("the scheduler armed no timer")
		return nil
	}
}

func (h *runHarness) assertNoTimer() {
	h.t.Helper()
	select {
	case timer := <-h.armed:
		h.t.Fatalf("the scheduler woke without a trigger and armed %v", timer.d)
	case <-time.After(60 * time.Millisecond):
	}
}

func TestRunSleepsUntilTheEarliestItemIsDue(t *testing.T) {
	h := startRun(t, nil)
	at := h.now().Add(30 * time.Second)
	if _, err := h.store.Create(reminder(at)); err != nil {
		t.Fatal(err)
	}
	h.start()
	// Create happened before Run: the first pass sees it.
	timer := h.nextTimer()
	if timer.d != 30*time.Second {
		t.Fatalf("armed %v, want the 30s until the item is due", timer.d)
	}
	if got := h.scheduler.passes.Load(); got != 1 {
		t.Fatalf("passes = %d", got)
	}
}

func TestRunDoesNoWorkWhileIdle(t *testing.T) {
	h := startRun(t, nil)
	at := h.now().Add(time.Hour)
	if _, err := h.store.Create(reminder(at)); err != nil {
		t.Fatal(err)
	}
	h.start()
	timer := h.nextTimer()
	if timer.d != safetyNet {
		t.Fatalf("armed %v, want the %v safety net for an item an hour away", timer.d, safetyNet)
	}
	h.assertNoTimer()
	if got := h.scheduler.passes.Load(); got != 1 {
		t.Fatalf("an idle scheduler made %d passes, want only the first", got)
	}
}

func TestRunFiresAnItemAtItsTime(t *testing.T) {
	h := startRun(t, nil)
	at := h.now().Add(30 * time.Second)
	item, err := h.store.Create(reminder(at))
	if err != nil {
		t.Fatal(err)
	}
	h.start()
	timer := h.nextTimer()
	if got, _ := h.store.Get(item.ID); got.Status != StatusScheduled {
		t.Fatalf("fired early: %#v", got)
	}
	h.advance(at)
	timer.fire()
	eventually(t, "the reminder to fire", func() bool {
		got, _ := h.store.Get(item.ID)
		return got.Status == StatusWaiting
	})
}

func TestRunRearmsWhenAnEarlierItemIsAdded(t *testing.T) {
	h := startRun(t, nil)
	if _, err := h.store.Create(reminder(h.now().Add(50 * time.Second))); err != nil {
		t.Fatal(err)
	}
	h.start()
	first := h.nextTimer()
	if first.d != 50*time.Second {
		t.Fatalf("armed %v", first.d)
	}
	earlier, err := h.store.Create(reminder(h.now().Add(20 * time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	second := h.nextTimer()
	if second.d != 20*time.Second {
		t.Fatalf("re-armed %v, want the 20s of the new item", second.d)
	}
	if !first.stopped.Load() {
		t.Fatal("the superseded timer was left running")
	}
	h.advance(h.now().Add(20 * time.Second))
	second.fire()
	eventually(t, "the earlier item to fire", func() bool {
		got, _ := h.store.Get(earlier.ID)
		return got.Status == StatusWaiting
	})
}

func TestRunWakesWhenAnOwnerRegisters(t *testing.T) {
	h := startRun(t, nil)
	first := localTime(t, "Asia/Shanghai", 2026, time.July, 14, 20, 59)
	seeded, _, err := h.store.Seed(OwnerWatchBrief, briefItem(first))
	if err != nil {
		t.Fatal(err)
	}
	h.start()
	h.nextTimer()
	fired := make(chan struct{}, 1)
	h.scheduler.Own(OwnerWatchBrief, OwnerDuty{Window: time.Hour, Fire: func(Item, time.Time) error {
		fired <- struct{}{}
		return nil
	}})
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("a due item waited for the safety net after its owner registered")
	}
	eventually(t, "the occurrence to be completed", func() bool {
		got, _ := h.store.Get(seeded.ID)
		return got.NextAt.After(first)
	})
}

// progressRunner is a runner whose Work progress the test signals by hand.
type progressRunner struct {
	progress chan struct{}
	status   atomic.Value
	inspects atomic.Int64
	stopped  atomic.Bool
}

func (r *progressRunner) RunScheduledAction(context.Context, Item, Run) (ActionResult, error) {
	return ActionResult{WorkID: "work-1", WorkerSession: "agent-1", Launched: true}, nil
}

func (r *progressRunner) InspectScheduledAction(context.Context, Item, Run) (Status, string, string, bool) {
	r.inspects.Add(1)
	if status, _ := r.status.Load().(Status); status == StatusCompleted {
		return StatusCompleted, "the deliverable", "", true
	}
	return StatusRunning, "", "", true
}

func (r *progressRunner) ScheduledActionProgress() (<-chan struct{}, func()) {
	return r.progress, func() { r.stopped.Store(true) }
}

func runningAction(t *testing.T, h *runHarness) Item {
	t.Helper()
	due := h.now().Add(time.Hour)
	item, err := h.store.Create(Item{Title: "Report", Kind: KindScheduledAction, DueAt: &due, Timezone: "America/New_York", Recurrence: RecurrenceNone, ActionInstruction: "Run it", SourceThreadID: "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := h.store.Claim(item.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RecordLaunch(item.ID, run.ID, "work-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestRunReconcilesAFinishedActionWhenItsWorkProgresses(t *testing.T) {
	runner := &progressRunner{progress: make(chan struct{}, 1)}
	runner.status.Store(StatusRunning)
	h := startRun(t, runner)
	item := runningAction(t, h)
	h.start()
	h.nextTimer()
	if got := h.scheduler.passes.Load(); got != 1 {
		t.Fatalf("passes = %d", got)
	}
	h.assertNoTimer() // nothing polls the running action
	inspectsBefore := runner.inspects.Load()

	runner.status.Store(StatusCompleted)
	runner.progress <- struct{}{}
	eventually(t, "the action to be reconciled", func() bool {
		got, _ := h.store.Get(item.ID)
		return len(got.Runs) == 1 && got.Runs[0].Status == StatusCompleted
	})
	// Finishing the run changed the store, which earns exactly one more pass;
	// the Work signal itself did not.
	eventually(t, "the store change to be seen", func() bool { return h.scheduler.passes.Load() == 2 })
	h.nextTimer() // back to sleep after the Work signal
	h.nextTimer() // back to sleep after the pass the store change earned
	h.assertNoTimer()
	if got := h.scheduler.passes.Load(); got != 2 {
		t.Fatalf("passes = %d, want the first plus one for the store change", got)
	}
	if runner.inspects.Load() <= inspectsBefore {
		t.Fatal("the running action was never inspected on the Work signal")
	}
}

func TestRunStopsListeningWhenTheWorkSignalCloses(t *testing.T) {
	runner := &progressRunner{progress: make(chan struct{}, 1)}
	runner.status.Store(StatusRunning)
	h := startRun(t, runner)
	runningAction(t, h)
	h.start()
	h.nextTimer()
	close(runner.progress)
	h.nextTimer() // the loop notices the closure once and goes back to sleep
	h.assertNoTimer()
	before := runner.inspects.Load()
	time.Sleep(60 * time.Millisecond)
	if after := runner.inspects.Load(); after != before {
		t.Fatalf("a closed Work signal made the loop spin: %d inspections", after-before)
	}
}

func TestRunStopsItsWorkSubscriptionOnExit(t *testing.T) {
	runner := &progressRunner{progress: make(chan struct{}, 1)}
	runner.status.Store(StatusRunning)
	h := startRun(t, runner)
	h.start()
	h.nextTimer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.scheduler.Run(ctx)
	if !runner.stopped.Load() {
		t.Fatal("Run left its Work subscription open")
	}
}

func TestWorkRunnerSignalsOnlyForScheduledActionWork(t *testing.T) {
	workStore, err := work.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runner := &WorkRunner{Store: workStore}
	progress, stop := runner.ScheduledActionProgress()
	defer stop()
	write := func(path string) {
		t.Helper()
		_, err := workStore.Write(&work.Item{Path: path, Project: "p", Body: "body", Frontmatter: work.Frontmatter{ID: filepath.Base(path), Kind: "calendar_action", Created: time.Now().UTC(), Title: "t"}}, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(workStore.Root, "other", "x.md"))
	select {
	case <-progress:
		t.Fatal("unrelated Work woke the scheduler")
	case <-time.After(60 * time.Millisecond):
	}
	write(filepath.Join(workStore.Root, "calendar", "item-run.md"))
	select {
	case <-progress:
	case <-time.After(2 * time.Second):
		t.Fatal("a scheduled action's Work file did not wake the scheduler")
	}
	stop()
	stop() // idempotent
}

func TestRunReconcilesWhenAWorkerChangesWithoutAFullPass(t *testing.T) {
	runner := &progressRunner{progress: make(chan struct{}, 1)}
	runner.status.Store(StatusRunning)
	h := startRun(t, runner)
	item := runningAction(t, h)
	h.start()
	h.nextTimer()
	runner.status.Store(StatusCompleted)
	h.scheduler.WorkerChanged()
	eventually(t, "the action to be reconciled", func() bool {
		got, _ := h.store.Get(item.ID)
		return len(got.Runs) == 1 && got.Runs[0].Status == StatusCompleted
	})
	var nilScheduler *Scheduler
	nilScheduler.WorkerChanged()
	nilScheduler.Wake()
}
