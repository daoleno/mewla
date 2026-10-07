package brain

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
)

func stalledReviewFixture(t *testing.T) (*Store, *Service, *fakeWatcher, Work) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const host = "host:@review-retry"
	if err := store.SetHostSession(host, "claude"); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "old review", "worker:@old-review")
	appendSignalTestEvent(t, store, item, "old-review")
	fw := &fakeWatcher{sessions: map[string]*classifier.Worker{host: {ID: host, Hidden: true}}, turnStore: store,
		sendErr: &watcher.InputSubmissionError{Result: watcher.InputResult{Outcome: watcher.InputNotSubmitted}, Cause: fmt.Errorf("Brain Host provider activity is unreadable")}}
	return store, NewService(store, fw, nil), fw, item
}

func TestReviewDeliveryFailureDoesNotReclaimOnEveryWake(t *testing.T) {
	store, service, fw, item := stalledReviewFixture(t)
	if _, err := service.reconcileHostLaneLocked(); err == nil {
		t.Fatal("expected rejection")
	}
	before, _ := store.Work(item.ID)
	for i := 0; i < 10; i++ {
		_, _ = service.reconcileHostLaneLocked()
	}
	after, _ := store.Work(item.ID)
	if after.Revision != before.Revision {
		t.Fatalf("failed review churned revisions on every wake: before=%d after=%d submissions=%d", before.Revision, after.Revision, len(fw.sentCalls))
	}
}

func TestReviewDeliveryFailureDoesNotBlockLaterReview(t *testing.T) {
	store, service, fw, first := stalledReviewFixture(t)
	if _, err := service.reconcileHostLaneLocked(); err == nil {
		t.Fatal("expected rejection")
	}
	later := createSignalTestWork(t, store, "later review", "worker:@later-review")
	appendSignalTestEvent(t, store, later, "later-review")
	fw.sendErr = nil
	if delivered, err := service.reconcileHostLaneLocked(); err != nil || !delivered {
		t.Fatalf("deliver later=%v err=%v", delivered, err)
	}
	if reviewLeaseDelivered(t, store, first.ID) {
		t.Fatal("reclaimed the failed head instead of allowing the later review through")
	}
	requireReviewDelivered(t, store, later.ID)
}

func TestReviewDeliveryFailureBackoffExhaustionSurvivesRestart(t *testing.T) {
	store, service, fw, item := stalledReviewFixture(t)
	now := time.Now().UTC()
	store.now = func() time.Time { return now }
	delays := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	for attempt := 1; attempt <= 5; attempt++ {
		if _, err := service.reconcileHostLaneLocked(); err == nil {
			t.Fatal("expected rejection")
		}
		projected, err := store.Work(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		failure := projected.Review.DeliveryFailure
		if failure == nil || failure.Attempts != attempt || failure.Exhausted != (attempt == 5) || !strings.Contains(failure.Error, "unreadable") {
			t.Fatalf("failure=%+v", failure)
		}
		if attempt < 5 {
			if got := failure.RetryAt.Sub(now); got != delays[attempt-1] {
				t.Fatalf("retry delay=%v", got)
			}
			now = failure.RetryAt.Add(-time.Nanosecond)
			before := projected.Revision
			if _, claimed, err := store.ClaimNextReviewAction("host:@review-retry"); err != nil || claimed {
				t.Fatalf("claimed during backoff: %v %v", claimed, err)
			}
			projected, _ = store.Work(item.ID)
			if projected.Revision != before {
				t.Fatal("backoff mutated state")
			}
			now = failure.RetryAt
		}
	}
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.now = func() time.Time { return now.Add(24 * time.Hour) }
	before, _ := reopened.Work(item.ID)
	for i := 0; i < 10; i++ {
		if _, claimed, err := reopened.ClaimNextReviewAction("host:@review-retry"); err != nil || claimed {
			t.Fatalf("exhausted review reclaimed after restart: %v %v", claimed, err)
		}
	}
	after, _ := reopened.Work(item.ID)
	if after.Revision != before.Revision || after.Review.DeliveryFailure.Attempts != 5 || len(fw.sentCalls) != 5 {
		t.Fatalf("exhaustion churn: %+v", after.Review)
	}
	context, err := NewService(reopened, fw, nil).Context()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range context.CurrentWork {
		if w.ID == item.ID && w.ReviewDelivery != nil && w.ReviewDelivery.Exhausted {
			found = true
		}
	}
	if !found {
		t.Fatal("delivery failure missing from Brain context")
	}
	card := workCardTimelineItem(after, WorkEvent{Summary: "Worker result"}, true)
	if card.Attention != "failed" || !strings.Contains(card.Summary, "Review delivery failed after 5 attempts") || !strings.Contains(card.Summary, "Worker result") {
		t.Fatalf("failure card=%+v", card)
	}
}

func TestReviewDeliveryFailureClearsAfterSuccessfulRetry(t *testing.T) {
	store, service, fw, item := stalledReviewFixture(t)
	if _, err := service.reconcileHostLaneLocked(); err == nil {
		t.Fatal("expected rejection")
	}
	before, _ := store.Work(item.ID)
	store.now = func() time.Time { return before.Review.DeliveryFailure.RetryAt }
	fw.sendErr = nil
	if delivered, err := service.reconcileHostLaneLocked(); err != nil || !delivered {
		t.Fatalf("retry=%v err=%v", delivered, err)
	}
	after, _ := store.Work(item.ID)
	if after.Review.DeliveryFailure != nil {
		t.Fatal("successful delivery retained failure")
	}
	requireReviewDelivered(t, store, item.ID)
}

func TestReviewDeliveryBackoffSurvivesRestart(t *testing.T) {
	store, service, _, item := stalledReviewFixture(t)
	if _, err := service.reconcileHostLaneLocked(); err == nil {
		t.Fatal("expected rejection")
	}
	failed, _ := store.Work(item.ID)
	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.now = func() time.Time { return failed.Review.DeliveryFailure.RetryAt.Add(-time.Nanosecond) }
	if _, claimed, err := reopened.ClaimNextReviewAction("host:@review-retry"); err != nil || claimed {
		t.Fatalf("backoff lost after restart: %v %v", claimed, err)
	}
	reopened.now = func() time.Time { return failed.Review.DeliveryFailure.RetryAt }
	action, claimed, err := reopened.ClaimNextReviewAction("host:@review-retry")
	if err != nil || !claimed || action.EventID != failed.Review.EventID {
		t.Fatalf("retry did not preserve event: %+v %v", action, err)
	}
}

func TestReviewDeliveryMissingReceiptCannotBlockLaterReviewWhenAdmissionIsAmbiguous(t *testing.T) {
	store, service, fw, first := stalledReviewFixture(t)
	action, claimed, err := store.ClaimNextReviewAction("host:@review-retry")
	if err != nil || !claimed {
		t.Fatal(err)
	}
	pending, created, err := store.PrepareInputAdmission(watcher.InputAdmission{WorkID: first.ID, SessionID: action.DeliveryHostSessionID, ProposedTurnID: action.ProviderTurnID, Receipt: action.ProviderTurnID, ClaimToken: action.HandlingID, PayloadSHA256: AdmissionDigest("payload"), ProcessIdentity: "process", PaneGeneration: "pane", Mode: watcher.InputAdmissionFresh, AcceptedAt: time.Now().UTC()})
	if err != nil || !created {
		t.Fatal(err)
	}
	if err := store.MarkInputAdmissionAmbiguous(pending.SessionID, pending.ProposedTurnID, "lost receipt"); err != nil {
		t.Fatal(err)
	}
	later := createSignalTestWork(t, store, "later review", "worker:@later-review")
	appendSignalTestEvent(t, store, later, "later-review")
	fw.sendErr = nil
	if delivered, err := service.reconcileHostLaneLocked(); err != nil || !delivered {
		t.Fatalf("later delivery=%v err=%v", delivered, err)
	}
	held, _ := store.Work(first.ID)
	if held.Review.Lease == nil || held.Review.Lease.HandlingID != action.HandlingID || held.Review.DeliveryFailure != nil {
		t.Fatalf("lost ambiguous capability: %+v", held.Review)
	}
	requireReviewDelivered(t, store, later.ID)
}
