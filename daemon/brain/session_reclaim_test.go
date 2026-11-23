package brain

import (
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
)

// parkedWorkerFixture is a Worker ("worker") that reported its result, and Brain
// reading it and parking the Work with update, as `work update -status
// waiting` does while it handles the result; Brain's turn then ends.
func parkedWorkerFixture(t *testing.T, update WorkUpdate) (*Store, *Service, *fakeWatcher, Work) {
	t.Helper()
	store, service, item, claimed := hostHandlingFixture(t)
	waiting := WorkWaiting
	update.Status = &waiting
	if _, err := service.BrainUpdateWork(item.ID, update); err != nil {
		t.Fatal(err)
	}
	endLiveCaseHostTurn(t, store, service, claimed)
	st, err := store.fsmState(item.ID)
	if err != nil || st == nil || st.Wake == nil || st.Review != nil || st.Attempt != nil {
		t.Fatalf("Work is not parked on a wait: %+v %v", st, err)
	}
	return store, service, service.watcher.(*fakeWatcher), item
}

func reclaimSet(t *testing.T, store *Store) map[string]ReclaimableTurn {
	t.Helper()
	turns, err := store.CompletedOwnedTurns("")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ReclaimableTurn{}
	for _, turn := range turns {
		out[turn.SessionID] = turn
	}
	return out
}

// mh-f7e9 (11-23): a Worker of a Work that is still running, whose Attempt
// moved to a newer Worker, sat idle for a day: nothing reclaimed a Session of
// an open Work. Brain does go back to an earlier Worker of the same Work (an
// author and a reviewer taking turns: DpMcpFeedback, DpGlobalResearch 11-21),
// so the earlier one is kept for a day after its last turn, then reclaimed.
func TestASupersededWorkerIsKeptForADayThenReclaimed(t *testing.T) {
	store, service, fw, item := parkedWorkerFixture(t, WorkUpdate{WaitFor: reclaimText("the next stage")})
	fw.sessions["newer"] = &classifier.Worker{ID: "newer", Delegated: true, State: classifier.StateRunning}
	if _, err := fw.SubmitDelegatedWorkInput("newer", "next stage", item.ID, "turn:newer", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if set := reclaimSet(t, store); len(set) != 0 {
		t.Fatalf("an author and a reviewer taking turns lost one: %+v", set)
	}
	later := time.Now().UTC().Add(WaitDefaultEnd + time.Minute)
	store.now = func() time.Time { return later }
	set := reclaimSet(t, store)
	if entry, ok := set["worker"]; !ok || !entry.Open || entry.Why != reclaimSuperseded {
		t.Fatalf("a Worker superseded a day ago is not reclaimable: %+v", set)
	}
	if _, ok := set["newer"]; ok {
		t.Fatal("the Worker on the Work's Attempt was put in the cleanup set")
	}
	if err := service.ReconcileCompletedSessions(); err != nil {
		t.Fatal(err)
	}
	if !containsSession(fw.killed, "worker") || containsSession(fw.killed, "newer") {
		t.Fatalf("killed=%v", fw.killed)
	}
}

// MewlaCalendar (11-22/23): its Worker delivered, Brain parked the Work on the
// owner's feedback, the wait ended at 08:38Z and the Session stayed. The
// Work's own Worker keeps its conversation while the wait stands (a reviewer
// waiting for the author's push, a Worker waiting on an answer) and goes once
// the wait has ended, when it rests.
func TestTheWorkersSessionIsKeptWhileItsWorksWaitStandsAndGoesAfter(t *testing.T) {
	end := time.Now().UTC().Add(3 * time.Hour)
	store, service, fw, _ := parkedWorkerFixture(t, WorkUpdate{WaitFor: reclaimText("the owner's feedback"), WaitUntil: &end})
	if _, ok := reclaimSet(t, store)["worker"]; ok {
		t.Fatal("a Worker whose Work's wait stands was put in the cleanup set")
	}
	if err := service.ReconcileCompletedSessions(); err != nil || containsSession(fw.killed, "worker") {
		t.Fatalf("a Worker whose wait stands was torn down: %v %v", fw.killed, err)
	}
	now := end.Add(time.Minute)
	store.now = func() time.Time { return now }
	entry, ok := reclaimSet(t, store)["worker"]
	if !ok || !entry.Open || entry.Why != reclaimWaitEnded {
		t.Fatalf("a Worker whose wait ended is not reclaimable: %+v", entry)
	}
	// It still waits while the Worker runs something of its own, even though
	// its turn reported done.
	fw.busy = map[string]bool{"worker": true}
	if err := service.ReconcileCompletedSessions(); err != nil || containsSession(fw.killed, "worker") {
		t.Fatalf("a Worker at work was killed: %v %v", fw.killed, err)
	}
	fw.busy = nil
	if err := service.ReconcileCompletedSessions(); err != nil || !containsSession(fw.killed, "worker") {
		t.Fatalf("rested Worker was not reclaimed: %v %v", fw.killed, err)
	}
}

// A question to the person has no end of its own: its Worker is kept for
// WaitDefaultEnd from when it was asked, then reclaimed (an answer later
// starts a new Worker that reads the report).
func TestAQuestionKeepsItsWorkerForADay(t *testing.T) {
	store, _, _, _ := parkedWorkerFixture(t, WorkUpdate{Question: reclaimText("Ship A or B?")})
	if _, ok := reclaimSet(t, store)["worker"]; ok {
		t.Fatal("the Worker of an open question was put in the cleanup set")
	}
	later := time.Now().UTC().Add(WaitDefaultEnd + time.Minute)
	store.now = func() time.Time { return later }
	if _, ok := reclaimSet(t, store)["worker"]; !ok {
		t.Fatal("a question a day old still holds its Worker")
	}
}

// until_done Work keeps its Worker while it is open, however long it waits;
// a result Brain has not settled keeps its Worker too.
func TestUntilDoneWorkAndAnUnsettledResultKeepTheirWorker(t *testing.T) {
	end := time.Now().UTC().Add(time.Hour)
	untilDone := CompletionUntilDone
	store, _, _, item := parkedWorkerFixture(t, WorkUpdate{WaitFor: reclaimText("the release"), WaitUntil: &end,
		CompletionPolicy: &untilDone, DoneCriteriaRef: reclaimText("the release is verified on a phone")})
	if st, _ := store.fsmState(item.ID); st.Policy != lifecycle.PolicyUntilDone {
		t.Fatalf("policy = %s", st.Policy)
	}
	later := time.Now().UTC().Add(48 * time.Hour)
	store.now = func() time.Time { return later }
	if _, ok := reclaimSet(t, store)["worker"]; ok {
		t.Fatal("an until_done Work's Worker was put in the cleanup set")
	}

	unsettled, _, _, _ := hostHandlingFixture(t)
	unsettled.now = func() time.Time { return later }
	if _, ok := reclaimSet(t, unsettled)["worker"]; ok {
		t.Fatal("a Worker whose result Brain has not settled was put in the cleanup set")
	}
}

// A Worker mid-turn (its Work's Attempt) is never in the set, whatever its
// Work's state; a restart (a new Store on the same state) neither adds it nor
// brings back a Session already reclaimed.
func TestMidTurnWorkerIsNeverReclaimedAcrossARestart(t *testing.T) {
	dir := t.TempDir()
	store, service, fw := newRefusingService(t, dir, "%live", "%gone")
	item, err := store.CreateWork(Work{Title: "live", Objective: "mid-turn"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.SubmitDelegatedWorkInput("%live", "task", item.ID, "live-turn", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	completeWorkOn(t, store, service, fw, "%gone")
	if err := service.ReconcileCompletedSessions(); err != nil {
		t.Fatal(err)
	}
	if fw.calls["%live"] != 0 || fw.calls["%gone"] != 1 {
		t.Fatalf("calls = %v", fw.calls)
	}
	reopened, reopenedService, reopenedFw := newRefusingService(t, dir, "%live", "%gone")
	set := reclaimSet(t, reopened)
	if len(set) != 0 {
		t.Fatalf("after a restart the set is %+v", set)
	}
	if err := reopenedService.ReconcileCompletedSessions(); err != nil || len(reopenedFw.calls) != 0 {
		t.Fatalf("restart tore down %v (%v)", reopenedFw.calls, err)
	}
}

func containsSession(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func reclaimText(value string) *string { return &value }
