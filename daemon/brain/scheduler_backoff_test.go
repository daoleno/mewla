package brain

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// heldClaimEngine returns an engine whose only Work holds a review claim that
// expired while a live admission owns it, which is what an ambiguous Brain
// submission leaves behind: Sweep must not release it, yet NextWakeAt keeps
// reporting the past ClaimExpiresAt.
func heldClaimEngine(t *testing.T) *lifecycle.Engine {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e, err := lifecycle.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	e.SetNow(func() time.Time { return now })
	if _, err := e.DefineWork("work", lifecycle.DefineWorkInput{Title: "T", Objective: "o", Policy: lifecycle.PolicyBounded}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.OpenReviewEvent("work", "turn_lost", "worker-turn", "event"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ClaimReview("work", "host", "handling", "host-turn"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.PrepareAdmission("work", lifecycle.PrepareAdmissionInput{
		SessionID: "host", TurnToken: "host-turn", Receipt: "host-turn", ClaimToken: "handling",
		PayloadSHA256: "digest", ProcessIdentity: "process", PaneGeneration: "pane",
		Mode: lifecycle.AdmissionFresh, Purpose: lifecycle.AdmissionPurposeReview,
		PurposeID: "handling", AttemptedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.MarkAdmissionAmbiguous("work", "host-turn", "unknown"); err != nil {
		t.Fatal(err)
	}
	e.SetNow(func() time.Time { return now.Add(lifecycle.EventClaimTTL + time.Second) })
	return e
}

func runLoopFor(t *testing.T, l wakeLoop, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runWakeLoop(ctx, l); close(done) }()
	time.Sleep(d)
	cancel()
	<-done
}

// A due item the pass cannot clear must not make the scheduler run passes
// back to back; it is retried with a growing delay and each step is logged.
func TestSchedulerBacksOffWhenAPassCannotSettleADueWake(t *testing.T) {
	e := heldClaimEngine(t)
	if next, ok := e.NextWakeAt(); !ok || next.After(time.Now().Add(lifecycle.EventClaimTTL)) {
		t.Fatalf("setup: held claim is not due (next=%v ok=%v)", next, ok)
	}
	var passes atomic.Int64
	var mu sync.Mutex
	var logs []string
	runLoopFor(t, wakeLoop{
		next: e.NextWakeAt, wakeups: e.Wakeups(),
		now: func() time.Time { return time.Now().Add(lifecycle.EventClaimTTL + time.Second) },
		pass: func() {
			passes.Add(1)
			if err := e.Sweep(); err != nil {
				t.Error(err)
			}
		},
		base: 40 * time.Millisecond, max: 160 * time.Millisecond,
		logf: func(format string, args ...any) {
			mu.Lock()
			logs = append(logs, format)
			mu.Unlock()
		},
	}, 700*time.Millisecond)

	got := passes.Load()
	// First pass at once, then retries after 40, 80, 160, 160ms...: about 6.
	if got < 3 || got > 10 {
		t.Fatalf("passes in 700ms = %d, want a few retries (3..10), not a spin or a stall", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 3 {
		t.Fatalf("log lines = %d, want one per backoff step up to the cap (3): %q", len(logs), logs)
	}
}

// A pass that settles the wake time leaves nothing backed off, and a commit
// someone else makes restarts the delay.
func TestSchedulerRunsPromptlyAfterSettlingAndAfterAnExternalCommit(t *testing.T) {
	var mu sync.Mutex
	due := true
	var passes atomic.Int64
	wakeups := make(chan struct{}, 1)
	l := wakeLoop{
		next: func() (time.Time, bool) {
			mu.Lock()
			defer mu.Unlock()
			return time.Now().Add(-time.Second), due
		},
		wakeups: wakeups, now: time.Now,
		pass: func() {
			passes.Add(1)
			if passes.Load() == 2 {
				mu.Lock()
				due = false
				mu.Unlock()
			}
		},
		base: 50 * time.Millisecond, max: time.Second,
		logf: func(string, ...any) {},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runWakeLoop(ctx, l); close(done) }()
	defer func() { cancel(); <-done }()

	deadline := time.Now().Add(time.Second)
	for passes.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if passes.Load() != 2 {
		t.Fatalf("passes = %d, want the retry to run (2)", passes.Load())
	}
	time.Sleep(150 * time.Millisecond)
	if passes.Load() != 2 {
		t.Fatalf("settled wake time kept running passes: %d", passes.Load())
	}
	mu.Lock()
	due = true
	mu.Unlock()
	wakeups <- struct{}{}
	deadline = time.Now().Add(time.Second)
	for passes.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if passes.Load() < 3 {
		t.Fatalf("external commit did not run a pass: %d", passes.Load())
	}
}
