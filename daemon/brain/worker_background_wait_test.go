package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// backgroundWaitWatcher answers the sweep's question about the Worker's pane.
type backgroundWaitFake struct {
	*fakeWatcher
	waiting bool
}

func (f *backgroundWaitFake) WorkerWaitingOnBackgroundWork(string) bool { return f.waiting }

type idleWorkerFixture struct {
	store   *Store
	service *Service
	fake    *backgroundWaitFake
	item    Work
	now     time.Time
	workers []*classifier.Worker
}

func (f *idleWorkerFixture) advance(d time.Duration) {
	f.now = f.now.Add(d)
	f.service.ReconcileDelegatedSessions(f.workers)
}

// newIdleWorkerFixture is a delegated signal turn whose Worker reported
// `running` with a lease, then ended its provider turn (the provider shows
// completed) and sits idle at its prompt.
func newIdleWorkerFixture(t *testing.T, lease int) *idleWorkerFixture {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &idleWorkerFixture{store: store, now: time.Now().UTC()}
	store.now = func() time.Time { return f.now }
	f.item = createSignalTestWork(t, store, "idle behind a background task", "worker")
	candidate := delegatedSubmissionCandidate(f.item.ID, "worker", "worker-turn", "task", f.now)
	candidate.SignalProtocol = true
	if _, _, err := store.PrepareInputAdmission(candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: "worker", TurnID: "worker-turn", Class: watcher.EvidenceControl, Kind: "running",
		SourceID: "first-report", Summary: "Released and reported. Waiting for the backfill.",
		LeaseSeconds: lease, At: f.now,
	}); err != nil {
		t.Fatal(err)
	}
	f.fake = &backgroundWaitFake{fakeWatcher: &fakeWatcher{providerEvidence: map[string]watcher.ProviderActivityObservation{
		"worker": {ID: "activity", Status: "completed", StartedAt: f.now, ProgressAt: f.now, SettledAt: f.now.Add(15 * time.Second)},
	}}}
	f.service = NewService(store, f.fake, nil)
	f.service.now = func() time.Time { return f.now }
	f.workers = []*classifier.Worker{{ID: "worker", PaneAlive: true, ProcessID: 123, State: classifier.StateUnknown}}
	return f
}

func (f *idleWorkerFixture) state(t *testing.T) *lifecycle.State {
	t.Helper()
	st, err := f.store.FSM().State(lifecycle.WorkID(f.item.ID))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// Brain 11-10 (Works e4a43dbd, e2da954c): a Worker that waits on its own
// background work cannot check in, so its declared lease ran out and the
// lifecycle released it as lost 30 minutes later while it was only waiting.
func TestWorkerWaitingOnItsOwnBackgroundWorkIsNeverReleasedAsLost(t *testing.T) {
	f := newIdleWorkerFixture(t, 900)
	f.fake.waiting = true
	for i := 0; i < 36; i++ { // three hours, well past lease + LostGrace
		f.advance(5 * time.Minute)
		if st := f.state(t); st.Attempt == nil || st.Review != nil {
			t.Fatalf("after %d min the waiting Worker was released: attempt=%v review=%+v", (i+1)*5, st.Attempt != nil, st.Review)
		}
	}
	// The background work ends and the Worker never reports: the lease runs
	// out as before and Brain hears once.
	f.fake.waiting = false
	for i := 0; i < 20 && f.state(t).Attempt != nil; i++ {
		f.advance(5 * time.Minute)
	}
	lost := f.state(t)
	if lost.Attempt != nil || lost.Review == nil || lost.Review.Reason != "turn_lost" {
		t.Fatalf("without background work the lapsed lease must still escalate: %+v", lost)
	}
}

// The loss shown to Brain states its own reason and time. It used to borrow the
// provider's provisional "reported done" hint (summary and timestamp), which
// read as if the Worker had finished.
func TestLeaseLossNeverBorrowsTheProviderDoneHint(t *testing.T) {
	f := newIdleWorkerFixture(t, 900)
	f.fake.waiting = false
	var lostSeen bool
	for i := 0; i < 20 && !lostSeen; i++ {
		f.advance(5 * time.Minute)
		lostSeen = f.state(t).Review != nil
	}
	st := f.state(t)
	if st.Review == nil || st.Review.Reason != "turn_lost" {
		t.Fatalf("setup: expected a lease loss, got %+v", st)
	}
	events, err := f.store.ListWorkEvents(f.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	hints := 0
	for _, event := range events {
		if strings.HasPrefix(event.Summary, "Delegated provider reported") {
			hints++
		}
	}
	if hints == 0 {
		t.Fatal("setup: the sweep never recorded the provider done hint this test is about")
	}
	var lost *WorkEvent
	for i := range events {
		if events[i].Kind == "turn_lost" {
			lost = &events[i]
		}
	}
	if lost == nil {
		t.Fatalf("no turn_lost event in %+v", events)
	}
	if strings.Contains(lost.Summary, "reported done") || strings.Contains(lost.Summary, "turn binding") ||
		!strings.Contains(lost.Summary, "did not check in before its lease ran out") {
		t.Fatalf("turn_lost carries hint text: %q", lost.Summary)
	}
	if !lost.CreatedAt.Equal(st.Review.OpenedAt) {
		t.Fatalf("turn_lost stamped %s, want the loss time %s", lost.CreatedAt, st.Review.OpenedAt)
	}
}

// A Worker that sends its done report gets turn_done whether the report comes
// while its lease is still running, after the lease lapsed, or after the loss.
func TestWorkerDoneReportIsTurnDoneNeverTurnLost(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after time.Duration
	}{
		{"within the lease", 5 * time.Minute},
		{"after the lease lapsed", 20 * time.Minute},
		{"after the loss", 3 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newIdleWorkerFixture(t, 900)
			f.fake.waiting = false
			end := f.now.Add(tc.after)
			for f.now.Before(end) {
				f.advance(5 * time.Minute)
			}
			if _, err := f.store.ApplyDelegatedTurnProgress(watcher.TurnFact{
				SessionID: "worker", TurnID: "worker-turn", Class: watcher.EvidenceControl, Kind: "done",
				SourceID: "done-report", Summary: "Shipped.", At: f.now,
			}); err != nil {
				t.Fatal(err)
			}
			st := f.state(t)
			if st.Attempt != nil || st.Review == nil || st.Review.Reason != "turn_done" {
				t.Fatalf("done report %s produced %+v", tc.name, st.Review)
			}
		})
	}
}
