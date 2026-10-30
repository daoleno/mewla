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

// Brain's `work update` is its disposition while a handling of the Work is
// live: the real pattern is a Worker result Brain only logs to its worklog and
// then parks with -status waiting, with no handling id passed.
func TestBrainWorkUpdateIsTheDispositionOfTheLiveHandling(t *testing.T) {
	endHostTurn := func(t *testing.T, store *Store, service *Service, claimed WorkReviewAction) {
		t.Helper()
		settleCanonicalHostTurnForTest(t, store, "host", claimed.ProviderTurnID)
		if _, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
			Type: "worker_state_change", WorkerID: "host", TurnID: claimed.ProviderTurnID,
			Worker:   &classifier.Worker{ID: "host", Hidden: true, State: classifier.StateDone},
			OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
		}); err != nil {
			t.Fatal(err)
		}
	}
	status := func(s WorkStatus) *WorkStatus { return &s }
	text := func(s string) *string { return &s }
	dispositionOf := func(t *testing.T, store *Store, workID string) WorkDisposition {
		t.Helper()
		for _, event := range mustWorkEvents(t, store, workID) {
			if event.HandledAt != nil && event.Disposition != "" {
				return event.Disposition
			}
		}
		return ""
	}

	for _, tc := range []struct {
		name   string
		update WorkUpdate
		want   WorkDisposition
	}{
		{"waiting waits on the Brain thread", WorkUpdate{Status: status(WorkWaiting), NextAction: text("Waiting for the user's approval.")}, WorkDispositionWait},
		{"needs_input waits on the Brain thread", WorkUpdate{Status: status(WorkNeedsInput)}, WorkDispositionWait},
		{"a question continues", WorkUpdate{Question: text("Ship it?")}, WorkDispositionContinue},
		{"running continues", WorkUpdate{Status: status(WorkRunning)}, WorkDispositionContinue},
		{"prose continues", WorkUpdate{NextAction: text("Review the next PR.")}, WorkDispositionContinue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, service, item, claimed := hostHandlingFixture(t)
			if _, err := service.BrainUpdateWork(item.ID, tc.update); err != nil {
				t.Fatal(err)
			}
			after, _ := store.Work(item.ID)
			if after.Review != nil {
				t.Fatalf("update left the review open: %+v", after.Review)
			}
			if got := dispositionOf(t, store, item.ID); got != tc.want {
				t.Fatalf("disposition = %q, want %q", got, tc.want)
			}
			if tc.want == WorkDispositionWait && after.Status != WorkWaiting {
				t.Fatalf("status = %s, want waiting", after.Status)
			}
			endHostTurn(t, store, service, claimed)
			if hasReconcileRequired(t, store, item.ID) {
				t.Fatal("a disposed handling still raised reconcile_required at the end of the Host turn")
			}
		})
	}

	t.Run("done accepts", func(t *testing.T) {
		store, service, item, claimed := hostHandlingFixture(t)
		if _, err := service.BrainUpdateWork(item.ID, WorkUpdate{Status: status(WorkDone)}); err != nil {
			t.Fatal(err)
		}
		if after, _ := store.Work(item.ID); after.Status != WorkDone || after.Review != nil {
			t.Fatalf("after done: %+v", after)
		}
		endHostTurn(t, store, service, claimed)
		if hasReconcileRequired(t, store, item.ID) {
			t.Fatal("accepted Work still raised reconcile_required")
		}
	})

	t.Run("without a live handling it is an ordinary update", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		service := NewService(store, &fakeWatcher{turnStore: store, outcomes: map[string]watcher.InputOutcome{}}, nil)
		item, err := store.CreateWork(Work{Title: "idle", Objective: "nothing running"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.BrainUpdateWork(item.ID, WorkUpdate{Status: status(WorkWaiting)}); err != nil {
			t.Fatal(err)
		}
		if after, _ := store.Work(item.ID); after.Status != WorkWaiting {
			t.Fatalf("status = %s", after.Status)
		}
		if _, err := service.BrainUpdateWork(item.ID, WorkUpdate{Status: status(WorkRunning)}); err == nil {
			t.Fatal("-status running must stay rejected without a live handling")
		}
	})
}
