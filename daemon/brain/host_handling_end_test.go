package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// hostHandlingFixture delivers one review to the Host and leaves the Host Turn
// running, as it is for the whole of Brain's reply.
func hostHandlingFixture(t *testing.T) (*Store, *Service, Work, WorkReviewAction) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "deploy", Objective: "deploy the preview build"})
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, outcomes: map[string]watcher.InputOutcome{}, sessions: map[string]*classifier.Worker{
		"host":   {ID: "host", Hidden: true, State: classifier.StateRunning},
		"worker": {ID: "worker", Delegated: true, State: classifier.StateRunning},
	}}
	service := NewService(store, fw, nil)
	if _, err := fw.SubmitDelegatedWorkInput("worker", "deploy", item.ID, "turn:first", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []watcher.TurnFact{
		{Kind: "running", SourceID: "r1"},
		{Kind: "done", SourceID: "d1", Summary: "deployed"},
	} {
		fact.SessionID, fact.TurnID, fact.Class, fact.At = "worker", "turn:first", watcher.EvidenceControl, time.Now()
		if _, err := service.ApplyDelegatedTurnProgress(fact); err != nil {
			t.Fatal(err)
		}
	}
	claimed, _ := claimAndDeliverTestReview(t, store, "host")
	return store, service, item, claimed
}

func hasReconcileRequired(t *testing.T, store *Store, workID string) bool {
	t.Helper()
	for _, event := range mustWorkEvents(t, store, workID) {
		if event.Kind == "brain.reconcile_required" {
			return true
		}
	}
	return false
}

// The live Host reported its pane as "unknown" a few seconds after delivery
// while the exact Turn was still running. That projected state is not the end
// of Brain's turn: the handling must stay open until the ledger Turn ends, so
// a disposition Brain records later (e.g. closing the Work) still lands.
func TestHostStateNoiseDoesNotEndARunningHandling(t *testing.T) {
	store, service, item, claimed := hostHandlingFixture(t)
	host := &classifier.Worker{ID: "host", Hidden: true, State: classifier.StateUnknown}
	if _, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
		Type: "worker_state_change", WorkerID: "host", Worker: host, TurnID: claimed.ProviderTurnID,
		OldState: string(classifier.StateRunning), NewState: string(classifier.StateUnknown),
	}); err != nil {
		t.Fatal(err)
	}
	if hasReconcileRequired(t, store, item.ID) {
		t.Fatal("a running Host Turn was reported as ended without a disposition")
	}
	if lease := requireReviewDelivered(t, store, item.ID); lease.HandlingEndedAt != nil {
		t.Fatalf("handling ended while its Turn runs: %+v", lease)
	}
	resolveDeliveredReview(t, store, item.ID, WorkDispositionComplete)

	// Control: once the Turn really ended without a disposition, it is reported.
	store2, service2, item2, claimed2 := hostHandlingFixture(t)
	settleCanonicalHostTurnForTest(t, store2, "host", claimed2.ProviderTurnID)
	host.State = classifier.StateDone
	if _, err := service2.ObserveHostSessionEvent(watcher.SessionEvent{
		Type: "worker_state_change", WorkerID: "host", Worker: host, TurnID: claimed2.ProviderTurnID,
		OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
	}); err != nil {
		t.Fatal(err)
	}
	if !hasReconcileRequired(t, store2, item2.ID) {
		t.Fatal("a Turn that ended with no disposition was not reported")
	}
}

// A follow-up Brain has sent but the provider has not accepted yet is its
// disposition in flight (live: the Worker pane accepted the prompt ~10 s after
// Brain's turn ended). Ending the Host turn then must not report it as missing.
func TestHostTurnEndWaitsForAFollowUpInFlight(t *testing.T) {
	store, service, item, claimed := hostHandlingFixture(t)
	const followUp = "turn:follow-up"
	now := time.Now()
	if _, _, err := store.FSM().PrepareAdmission(lifecycle.WorkID(item.ID), lifecycle.PrepareAdmissionInput{
		SessionID: "worker", TurnToken: followUp, Receipt: followUp,
		PayloadSHA256: pendingSubmissionDigest("follow-up"), ProcessIdentity: "process-identity",
		PaneGeneration: "pane-generation", Mode: lifecycle.AdmissionFresh, SignalProtocol: true, AttemptedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	settleCanonicalHostTurnForTest(t, store, "host", claimed.ProviderTurnID)
	host := &classifier.Worker{ID: "host", Hidden: true, State: classifier.StateDone}
	if _, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
		Type: "worker_state_change", WorkerID: "host", Worker: host, TurnID: claimed.ProviderTurnID,
		OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
	}); err != nil {
		t.Fatal(err)
	}
	if hasReconcileRequired(t, store, item.ID) {
		t.Fatal("Host turn end reported a missing disposition while a follow-up was in flight")
	}
	if _, err := store.FSM().AcceptAdmissionBySignal(lifecycle.WorkID(item.ID), followUp, "worker"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	if after, _ := store.Work(item.ID); after.Review != nil {
		t.Fatalf("accepted follow-up left the review open: %+v", after.Review)
	}
	if hasReconcileRequired(t, store, item.ID) {
		t.Fatal("accepted follow-up still produced reconcile_required")
	}
}
