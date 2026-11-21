package brain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// The live case (11-19, Work cce11001 "ReviewLooperBoardStack", Session
// mh-8d481a1e…): the reviewer reported its verdicts (turn done 09:52Z), and
// Brain kept the Work waiting for a named later step with
// `work update -status waiting -wait-for "<the delta re-check>"` while it was
// reading that result (10:06Z). The daemon then running predated named waits
// in a review, so the wait became a bare park on Brain's thread. After the
// 11:26Z restart the owner's next chat message (11:28Z) released it, and the
// closure judge, seeing only "the Worker's last turn is done" and unrelated
// chat, closed the Work at 11:31Z; its Session was reclaimed and Brain's next
// send got worker_session_reclaimed.
const liveCaseWaitFor = "Delta re-check of looper #103/#104 after LooperRequestBoard pushes fixes (keep this reviewer's session for it)"

// deliverBrainThreadInput is what an owner's chat message does to Works
// parked on Brain's thread.
func deliverBrainThreadInput(t *testing.T, store *Store, thread, occurrence string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	database, err := store.loadPresentationLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.wakeWaitingWorkLocked(&database, WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:" + thread},
		"user.input", occurrence, "User input arrived on the waiting Brain thread.", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.persistPresentationLocked(database); err != nil {
		t.Fatal(err)
	}
}

func endLiveCaseHostTurn(t *testing.T, store *Store, service *Service, claimed WorkReviewAction) {
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

// A Work Brain keeps open for a named later step is not judged, not closed and
// its Session not reclaimed: not by the owner chatting on Brain's thread, not
// by a daemon restart, not after the Worker's last turn is done.
func TestLiveCaseNamedWaitSurvivesChatRestartAndJudgment(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictDone, Reason: "The Worker finished and reported its verdicts."}}
	store, service, item, claimed := hostHandlingFixture(t)
	service.SetWorkJudge(judge)
	waitFor := liveCaseWaitFor
	waiting := WorkWaiting
	if _, err := service.BrainUpdateWork(item.ID, WorkUpdate{Status: &waiting, WaitFor: &waitFor}); err != nil {
		t.Fatal(err)
	}
	endLiveCaseHostTurn(t, store, service, claimed)
	deliverBrainThreadInput(t, store, item.SourceThreadID, "message:owner-chat")

	assertKept := func(t *testing.T, store *Store, service *Service) {
		t.Helper()
		got, err := store.Work(item.ID)
		if err != nil || got.Status != WorkWaiting || got.Wake == nil || got.Wake.Ref != liveCaseWaitFor {
			t.Fatalf("named wait lost: %+v err=%v", got, err)
		}
		later := time.Now().Add(settleDebounce + 5*time.Minute)
		service.now = func() time.Time { return later }
		runSettle(t, service)
		if len(judge.briefs) != 0 {
			t.Fatalf("a Work waiting on a named step was judged: %q", judge.briefs)
		}
		if turns, err := store.CompletedOwnedTurns(item.ID); err != nil || len(turns) != 0 {
			t.Fatalf("its Session is in the cleanup set: %+v %v", turns, err)
		}
		if got, _ := store.Work(item.ID); got.Status == WorkDone || got.Status == WorkCancelled {
			t.Fatalf("closed: %+v", got)
		}
	}
	assertKept(t, store, service)

	// The daemon restarts on the same state.
	restarted, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	restartedService := newTestService(t, restarted, &fakeWatcher{turnStore: restarted}, nil)
	restartedService.SetWorkJudge(judge)
	assertKept(t, restarted, restartedService)
}

// Should a named wait be lost anyway (as with the binary running on 11-19),
// the judgment still sees what Brain said the Work waits for, not only that
// the Worker's last turn is done, and is told when such a wait keeps it: only
// when what it waits for delivers this Work's own outcome (11-21).
func TestLiveCaseJudgeSeesBrainsNamedWaitAfterItWasReleased(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictKeep, Reason: "Waiting for the delta re-check of looper #103/#104."}}
	store, service, item := endedHandlingFixture(t, judge)
	id := lifecycle.WorkID(item.ID)
	thread := "brain-thread:" + item.SourceThreadID
	if _, err := store.FSM().SetWait(id, lifecycle.WakeUserInput, liveCaseWaitFor); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FSM().SetWait(id, lifecycle.WakeUserInput, thread); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FSM().ClearWait(id, lifecycle.WakeUserInput, thread, "message:owner-chat"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(settleDebounce + 5*time.Minute)
	service.now = func() time.Time { return later }
	runSettle(t, service)
	if len(judge.briefs) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(judge.briefs))
	}
	brief := judge.briefs[0]
	if !strings.Contains(brief, "waits Brain set on this Work") || !strings.Contains(brief, "Delta re-check of looper #103/#104") ||
		!strings.Contains(brief, "it keeps the Work only when what it waits for delivers this Work's own outcome") {
		t.Fatalf("the judge was not shown Brain's named wait:\n%s", brief)
	}
	if got, _ := store.Work(item.ID); got.Status != WorkWaiting {
		t.Fatalf("Work = %+v, want waiting on the kept reason", got)
	}
}

// A daemon restart re-projects every Work; a row that projects the same must
// keep its updated_at, or "what changed since" means nothing.
func TestRestartKeepsUpdatedAtOfUnchangedWork(t *testing.T) {
	store, _, item, _ := hostHandlingFixture(t)
	before, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	restarted, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := restarted.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("restart rewrote updated_at: %s -> %s", before.UpdatedAt, after.UpdatedAt)
	}
}

// The same Work during a judge outage: Brain named the delta re-check with
// -wait-for after reading the result, a reply released it, and the judge
// cannot answer. The fallback must not close it on "last turn done" alone.
func TestLiveCaseJudgeOutageLeavesANamedWaitAlone(t *testing.T) {
	judge := &fakeJudge{err: errors.New("headless judge: the model route http://127.0.0.1:3427 answered HTTP 403: Insufficient account balance")}
	store, service, item := endedHandlingFixture(t, judge)
	id := lifecycle.WorkID(item.ID)
	if _, err := store.FSM().SetWait(id, lifecycle.WakeUserInput, liveCaseWaitFor); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FSM().ClearWait(id, lifecycle.WakeUserInput, liveCaseWaitFor, "message:reply"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(settleDebounce + 5*time.Minute)
	service.now = func() time.Time { return later }
	runSettle(t, service)
	if got, _ := store.Work(item.ID); got.Status == WorkDone || got.Status == WorkCancelled || closedByRuntime(t, store, item.ID) {
		t.Fatalf("a judge outage closed a Work Brain named a later step for: %+v", got)
	}
}

// A finished scheduled run's Session is reclaimed when idle, unless Brain has
// since given it open Work (a follow-up whose result Brain is reading).
func TestFinishedRunSessionIsReclaimedOnlyWithoutOpenWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Worker{
		"mh-run":  {ID: "mh-run", Delegated: true, State: classifier.StateUnknown},
		"mh-used": {ID: "mh-used", Delegated: true, State: classifier.StateDone},
		"mh-busy": {ID: "mh-busy", Delegated: true, State: classifier.StateUnknown},
	}, busy: map[string]bool{"mh-busy": true}}
	service := newTestService(t, store, fw, nil)
	item, err := store.CreateWork(Work{Title: "follow-up", Objective: "Brain reused a finished run's Session"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.SubmitDelegatedWorkInput("mh-used", "follow-up", item.ID, "turn:used", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "mh-used", TurnID: "turn:used", Class: watcher.EvidenceControl, Kind: "done", SourceID: "d", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		want bool
	}{{"mh-used", false}, {"mh-busy", false}, {"mh-run", true}} {
		got, err := service.ReclaimFinishedRunSession(tc.id)
		if err != nil || got != tc.want {
			t.Fatalf("%s: reclaimed=%v err=%v, want %v", tc.id, got, err, tc.want)
		}
	}
	if len(fw.killed) != 1 || fw.killed[0] != "mh-run" {
		t.Fatalf("killed = %v, want only mh-run", fw.killed)
	}
}
