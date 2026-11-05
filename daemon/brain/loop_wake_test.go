package brain

import (
	"strings"
	"testing"
	"time"
)

// A persistent loop is an until_done Work whose next check is a durable
// due_retry wake. These tests drive the riskiest boundaries of that shape
// against the real Store and lifecycle engine: the wake must survive a Host
// executor replacement and a restart and reach the CURRENT Host exactly once.

type loopClock struct {
	base   time.Time
	offset time.Duration
}

func (c *loopClock) now() time.Time { return c.base.Add(c.offset) }

func newLoopStore(t *testing.T, root string, clock *loopClock) *Store {
	t.Helper()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	store.now = clock.now
	return store
}

// startTestLoop starts a loop through the production API.
func startTestLoop(t *testing.T, store *Store, clock *loopClock, in LoopStart, first time.Duration) Work {
	t.Helper()
	if first > 0 {
		in.FirstCheckAt = clock.now().Add(first)
	}
	item, err := store.StartLoop(in)
	if err != nil {
		t.Fatalf("start loop: %v", err)
	}
	if item.Loop == nil || item.Status != WorkWaiting || item.Wake == nil || item.Wake.NextAttemptAt == nil {
		t.Fatalf("started loop = %+v", item)
	}
	return item
}

func quotaLoop() LoopStart {
	return LoopStart{
		Title: "loop: check the quota", Objective: "Check the quota; re-arm until approved.", DoneCriteriaRef: "quota approved",
		Loop: WorkLoop{WorkerExecutor: "claude", WorkerModel: "claude-sonnet-5-5"},
	}
}

// armLoopWake resolves the delivered review as a durable timed wait.
func armLoopWake(t *testing.T, store *Store, workID string, at time.Time, summary string) Work {
	t.Helper()
	lease := requireReviewDelivered(t, store, workID)
	_, item, err := store.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: workID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID,
		ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: WorkDispositionWait,
		Wake:    &WorkWake{Kind: WorkWakeDueRetry, Ref: "loop:" + workID, NextAttemptAt: &at},
		Summary: summary,
	})
	if err != nil {
		t.Fatalf("arm wake: %v", err)
	}
	if item.Wake == nil || item.Wake.Kind != WorkWakeDueRetry || item.Status != WorkWaiting {
		t.Fatalf("armed Work = %+v", item)
	}
	return item
}

func TestLoopWakeSurvivesHostSwitchAndRestartAndFiresOnce(t *testing.T) {
	root := t.TempDir()
	clock := &loopClock{base: time.Now().UTC()}
	store := newLoopStore(t, root, clock)
	if err := store.SetHostSession("host:@claude", "claude"); err != nil {
		t.Fatal(err)
	}
	item := startTestLoop(t, store, clock, quotaLoop(), 10*time.Minute)
	wakeAt := *item.Wake.NextAttemptAt

	// Brain moves from Claude to Codex before the wake is due, then the
	// daemon restarts: the wake is durable lifecycle state, not a Claude timer.
	if err := store.SetHostSession("host:@codex", "codex"); err != nil {
		t.Fatal(err)
	}
	clock.offset = 5 * time.Minute
	restarted := newLoopStore(t, root, clock)
	if next, ok := restarted.FSM().NextWakeAt(); !ok || !next.Equal(wakeAt) {
		t.Fatalf("restarted next wake = %v ok=%v, want %v", next, ok, wakeAt)
	}
	if err := restarted.SweepLifecycle(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := restarted.ClaimNextReviewAction("host:@codex"); err != nil || ok {
		t.Fatalf("a wake fired before it was due (ok=%v err=%v)", ok, err)
	}

	// Due: one actionable event reaches the Codex Host, however often the
	// timer sweeps.
	clock.offset = 11 * time.Minute
	for i := 0; i < 3; i++ {
		if err := restarted.SweepLifecycle(); err != nil {
			t.Fatal(err)
		}
	}
	delivered, work := claimAndDeliverTestReview(t, restarted, "host:@codex")
	if delivered.WorkID != item.ID || work.Loop == nil || work.Loop.WorkerExecutor != "claude" || work.Loop.WorkerModel != "claude-sonnet-5-5" {
		t.Fatalf("delivered %+v work %+v", delivered, work)
	}
	if !strings.Contains(work.NextAction, "A loop check is due") || !strings.Contains(work.NextAction, "worker spawn -executor claude -model claude-sonnet-5-5") {
		t.Fatalf("wake next action = %q", work.NextAction)
	}
	if delivered.DeliveryHostSessionID != "host:@codex" {
		t.Fatalf("wake delivered to %q, want the current Codex Host", delivered.DeliveryHostSessionID)
	}
	if err := restarted.SweepLifecycle(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := restarted.ClaimNextReviewAction("host:@codex"); err != nil || ok {
		t.Fatalf("second delivery of the same wake (ok=%v err=%v)", ok, err)
	}

	// Brain re-arms; the objective and the first check's summary are intact.
	armed := armLoopWake(t, restarted, item.ID, clock.now().Add(10*time.Minute), "second check; still pending")
	if armed.Objective != item.Objective || armed.CompletionPolicy != CompletionUntilDone {
		t.Fatalf("loop lost its goal: %+v", armed)
	}
	full, err := restarted.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Wake == nil || !full.Wake.NextAttemptAt.After(wakeAt) {
		t.Fatalf("re-armed Work = %+v", full)
	}
}

func TestLoopPauseResumeCancelAndCompleteNeverFireWhenInactive(t *testing.T) {
	root := t.TempDir()
	clock := &loopClock{base: time.Now().UTC()}
	store := newLoopStore(t, root, clock)
	if err := store.SetHostSession("host:@claude", "claude"); err != nil {
		t.Fatal(err)
	}
	fires := func() bool {
		t.Helper()
		if err := store.SweepLifecycle(); err != nil {
			t.Fatal(err)
		}
		_, ok, err := store.ClaimNextReviewAction("host:@claude")
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	// Paused: nothing fires however long it stays paused, and the state says so.
	paused := startTestLoop(t, store, clock, quotaLoop(), 10*time.Minute)
	if _, err := store.PauseLoop(paused.ID); err != nil {
		t.Fatal(err)
	}
	status, err := store.LoopStatus(paused.ID)
	if err != nil || status.State != LoopPaused || status.NextCheckAt != nil {
		t.Fatalf("paused status = %+v err=%v", status, err)
	}
	clock.offset = 48 * time.Hour
	if fires() {
		t.Fatal("a paused loop fired")
	}
	// The pause is a valid far-future wake (an older daemon still loads it),
	// never a real timer.
	if next, ok := store.FSM().NextWakeAt(); ok && !isLoopPauseWake(&next) {
		t.Fatalf("a paused loop holds a real timer at %v", next)
	}
	if item, err := store.Work(paused.ID); err != nil || validateWorkWake(item.Wake) != nil || item.Loop == nil || !item.Loop.Paused {
		t.Fatalf("paused Work must carry a valid wake and the persisted intent: %+v err=%v", item, err)
	}

	// Resume re-arms ONE wake from now. The 48 hours offline are not replayed.
	if _, err := store.ResumeLoop(paused.ID, time.Time{}); err == nil {
		t.Fatal("resume without a time or interval was accepted")
	}
	if _, err := store.ResumeLoop(paused.ID, clock.now().Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if status, _ = store.LoopStatus(paused.ID); status.State != LoopScheduled || status.NextCheckAt == nil {
		t.Fatalf("resumed status = %+v", status)
	}
	if fires() {
		t.Fatal("a resumed loop fired before its time")
	}
	clock.offset += 31 * time.Minute
	if !fires() {
		t.Fatal("a resumed loop did not fire when due")
	}
	if fires() {
		t.Fatal("a resumed loop fired twice")
	}
	// The check is being handled. A pause now is accepted (it is the user's
	// intent) and reads "pausing"; resuming with a time is refused until the
	// check resolves.
	if _, err := store.PauseLoop(paused.ID); err != nil {
		t.Fatalf("pause during a check: %v", err)
	}
	if status, _ = store.LoopStatus(paused.ID); status.State != LoopPausing {
		t.Fatalf("pausing status = %+v", status)
	}

	// Cancelled: the timer is gone and cannot be revived.
	cancelled := startTestLoop(t, store, clock, quotaLoop(), 10*time.Minute)
	if _, err := store.CloseWork(WorkCloseRequest{WorkID: cancelled.ID, ExpectedRevision: cancelled.Revision, Status: WorkCancelled, Actor: "user", Reason: "no longer needed"}); err != nil {
		t.Fatal(err)
	}
	clock.offset += 48 * time.Hour
	for _, id := range []string{cancelled.ID} {
		if _, err := store.ResumeLoop(id, clock.now().Add(time.Hour)); err == nil {
			t.Fatal("a cancelled loop was resumed")
		}
		if _, err := store.PauseLoop(id); err == nil {
			t.Fatal("a cancelled loop was paused")
		}
	}
	if status, _ = store.LoopStatus(cancelled.ID); status.State != LoopCancelled {
		t.Fatalf("cancelled status = %+v", status)
	}
}

func TestLoopCadenceDefaultsCatchUpOnceAndFinishes(t *testing.T) {
	root := t.TempDir()
	clock := &loopClock{base: time.Now().UTC()}
	store := newLoopStore(t, root, clock)
	if err := store.SetHostSession("host:@codex", "codex"); err != nil {
		t.Fatal(err)
	}
	in := quotaLoop()
	in.Loop.IntervalSeconds = 600
	item := startTestLoop(t, store, clock, in, 0)
	if want := clock.now().Add(10 * time.Minute); !item.Wake.NextAttemptAt.Equal(want) {
		t.Fatalf("first check = %v, want interval from now %v", item.Wake.NextAttemptAt, want)
	}

	// The daemon is down for three intervals: exactly one wake is delivered,
	// not one per missed interval.
	clock.offset = 35 * time.Minute
	restarted := newLoopStore(t, root, clock)
	for i := 0; i < 3; i++ {
		if err := restarted.SweepLifecycle(); err != nil {
			t.Fatal(err)
		}
	}
	claimAndDeliverTestReview(t, restarted, "host:@codex")
	if _, ok, _ := restarted.ClaimNextReviewAction("host:@codex"); ok {
		t.Fatal("a missed interval produced a second wake")
	}

	// Fixed cadence: a wait with no time re-arms at now+interval.
	lease := requireReviewDelivered(t, restarted, item.ID)
	_, rearmed, err := restarted.ResolveWorkReview(WorkReviewDispositionRequest{
		WorkID: item.ID, HandlingID: lease.HandlingID, ProviderTurnID: lease.ProviderTurnID,
		ExpectedWorkRevision: lease.DeliveryWorkRevision, Disposition: WorkDispositionWait,
		Wake: &WorkWake{Kind: WorkWakeDueRetry}, Summary: "pending",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := clock.now().Add(10 * time.Minute); rearmed.Wake == nil || !rearmed.Wake.NextAttemptAt.Equal(want) || rearmed.Wake.Ref != "loop:"+item.ID {
		t.Fatalf("default cadence wake = %+v, want %v", rearmed.Wake, want)
	}
	status, err := restarted.LoopStatus(item.ID)
	if err != nil || status.Checks != 1 || status.LastSummary != "pending" || status.State != LoopScheduled {
		t.Fatalf("status after one check = %+v err=%v", status, err)
	}

	// The goal is met: complete, and no later wake can fire.
	clock.offset += 11 * time.Minute
	if err := restarted.SweepLifecycle(); err != nil {
		t.Fatal(err)
	}
	claimAndDeliverTestReview(t, restarted, "host:@codex")
	resolveDeliveredReview(t, restarted, item.ID, WorkDispositionComplete)
	clock.offset += 24 * time.Hour
	if err := restarted.SweepLifecycle(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := restarted.ClaimNextReviewAction("host:@codex"); ok {
		t.Fatal("a completed loop fired again")
	}
	if status, _ = restarted.LoopStatus(item.ID); status.State != LoopDone {
		t.Fatalf("completed status = %+v", status)
	}
}

// A pause requested while a check is being handled must stop the loop from
// being re-armed by that check's own late resolution, across a restart.
func TestLoopPauseDuringCheckBlocksLateRearmAndSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	clock := &loopClock{base: time.Now().UTC()}
	store := newLoopStore(t, root, clock)
	if err := store.SetHostSession("host:@claude", "claude"); err != nil {
		t.Fatal(err)
	}
	item := startTestLoop(t, store, clock, quotaLoop(), 10*time.Minute)
	clock.offset = 11 * time.Minute
	if err := store.SweepLifecycle(); err != nil {
		t.Fatal(err)
	}
	claimAndDeliverTestReview(t, store, "host:@claude")

	if _, err := store.PauseLoop(item.ID); err != nil {
		t.Fatalf("pause during check: %v", err)
	}
	if _, err := store.PauseLoop(item.ID); err != nil {
		t.Fatalf("second pause must be idempotent: %v", err)
	}
	if _, err := store.ResumeLoop(item.ID, clock.now().Add(time.Hour)); err == nil {
		t.Fatal("resume with a time while a check is handled was accepted")
	}
	restarted := newLoopStore(t, root, clock)
	status, err := restarted.LoopStatus(item.ID)
	if err != nil || status.State != LoopPausing {
		t.Fatalf("status after restart = %+v err=%v", status, err)
	}

	// The Host did not notice the pause and asks for another check in 10 min.
	armLoopWake(t, restarted, item.ID, clock.now().Add(10*time.Minute), "still pending")
	status, err = restarted.LoopStatus(item.ID)
	if err != nil || status.State != LoopPaused || status.NextCheckAt != nil {
		t.Fatalf("late resolution re-armed a paused loop: %+v err=%v", status, err)
	}
	clock.offset += 24 * time.Hour
	if err := restarted.SweepLifecycle(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := restarted.ClaimNextReviewAction("host:@claude"); err != nil || ok {
		t.Fatalf("a paused loop fired after a late resolution (ok=%v err=%v)", ok, err)
	}
	if card, _ := restarted.Work(item.ID); card.Status != WorkWaiting || !strings.HasPrefix(card.WaitFor, "Paused") {
		t.Fatalf("the Work card must read paused: status=%s wait_for=%q", card.Status, card.WaitFor)
	}

	// Resume re-arms exactly one check.
	if _, err := restarted.ResumeLoop(item.ID, time.Time{}); err == nil {
		t.Fatal("this loop has no interval; resume without a time must be refused")
	}
	if _, err := restarted.ResumeLoop(item.ID, clock.now().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if status, _ = restarted.LoopStatus(item.ID); status.State != LoopScheduled || status.NextCheckAt == nil {
		t.Fatalf("resumed status = %+v", status)
	}
}

// Resuming while a check runs only clears the intent: that check's own
// resolution arms the next one, so there is never a second wake.
func TestLoopResumeDuringCheckClearsIntentOnly(t *testing.T) {
	root := t.TempDir()
	clock := &loopClock{base: time.Now().UTC()}
	store := newLoopStore(t, root, clock)
	if err := store.SetHostSession("host:@claude", "claude"); err != nil {
		t.Fatal(err)
	}
	item := startTestLoop(t, store, clock, quotaLoop(), 10*time.Minute)
	clock.offset = 11 * time.Minute
	if err := store.SweepLifecycle(); err != nil {
		t.Fatal(err)
	}
	claimAndDeliverTestReview(t, store, "host:@claude")
	if _, err := store.PauseLoop(item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResumeLoop(item.ID, time.Time{}); err != nil {
		t.Fatalf("resume during a check: %v", err)
	}
	if status, _ := store.LoopStatus(item.ID); status.State != LoopHandling {
		t.Fatalf("status = %+v", status)
	}
	armLoopWake(t, store, item.ID, clock.now().Add(10*time.Minute), "next")
	if status, _ := store.LoopStatus(item.ID); status.State != LoopScheduled || status.NextCheckAt == nil {
		t.Fatalf("status after resolve = %+v", status)
	}
}
