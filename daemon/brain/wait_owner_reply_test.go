package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// The 11-19 evening case: Brain parked three Works on things other than the
// owner (XcodeLogsViewerHang on tomorrow's retention run, MewlaWorkReclaim on
// a restart, LooperRequestBoard on a review) with `-status waiting -wait-for`
// and no question. Every such wait was a user_input wake, and the owner's
// 「看不懂这个，你需要我干啥？」 on each card (12:34Z) cleared it
// (wake.cleared unwake:user_input:<Brain's text>:work-reply:…).
const tomorrowsRun = "11-20 03:17Z retention run; Trigger c57a7c00 pings the Worker after 04:40Z"

func parkOn(t *testing.T, store *Store, title string, update WorkUpdate) Work {
	t.Helper()
	item, err := store.CreateWork(Work{Title: title, Objective: "parked", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	waiting := WorkWaiting
	update.Status = &waiting
	item, err = store.UpdateWork(item.ID, update)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func ownerReplies(t *testing.T, service *Service, workID, text, requestID string) WorkUserActionResult {
	t.Helper()
	result, err := service.ActOnWork(WorkUserActionRequest{WorkID: workID, Kind: WorkUserReply, Text: text, RequestID: requestID})
	if err != nil || result.Admission != ExternalInputAccepted {
		t.Fatalf("reply admission=%q err=%v", result.Admission, err)
	}
	if result.Event.Kind != "user.replied" {
		t.Fatalf("reply event = %+v", result.Event)
	}
	return result
}

func wakeIs(t *testing.T, store *Store, workID string, kind WorkWakeKind, ref string) {
	t.Helper()
	got, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != WorkWaiting || got.Wake == nil || got.Wake.Kind != kind || got.Wake.Ref != ref {
		t.Fatalf("Work %s: status %s wake %+v, want waiting on %s %q", workID, got.Status, got.Wake, kind, ref)
	}
}

// A Work waits on the person only when Brain asks something; -status waiting
// with only -wait-for (or nothing) is Brain's own named wait.
func TestOnlyAQuestionMakesAWaitOnThePerson(t *testing.T) {
	store, _, _, _ := newUserActionService(t)
	text := func(s string) *string { return &s }

	named := parkOn(t, store, "XcodeLogsViewerHang", WorkUpdate{WaitFor: text(tomorrowsRun)})
	wakeIs(t, store, named.ID, WorkWakeNamed, tomorrowsRun)

	bare := parkOn(t, store, "bare park", WorkUpdate{})
	wakeIs(t, store, bare.ID, WorkWakeNamed, "operator:"+bare.ID)

	asked := parkOn(t, store, "asks", WorkUpdate{WaitFor: text("the owner's call"), Question: text("Ship it?")})
	wakeIs(t, store, asked.ID, WorkWakeUserInput, "the owner's call")

	// A standing question still asks when Brain re-parks without repeating it.
	waiting := WorkWaiting
	if _, err := store.UpdateWork(asked.ID, WorkUpdate{Status: &waiting, WaitFor: text("the owner's call, again")}); err != nil {
		t.Fatal(err)
	}
	wakeIs(t, store, asked.ID, WorkWakeUserInput, "the owner's call, again")

	// Clearing the question in the same update makes it Brain's own wait.
	if _, err := store.UpdateWork(asked.ID, WorkUpdate{Status: &waiting, WaitFor: text(tomorrowsRun), Question: text("")}); err != nil {
		t.Fatal(err)
	}
	wakeIs(t, store, asked.ID, WorkWakeNamed, tomorrowsRun)
}

// A reply on a Work that asks nothing reaches Brain as a message about that
// Work and leaves its wait alone; Brain decides whether it still holds.
func TestReplyOnAWorkThatAsksNothingLeavesItsWait(t *testing.T) {
	store, service, fw, _ := newUserActionService(t)
	text := func(s string) *string { return &s }
	item := parkOn(t, store, "XcodeLogsViewerHang", WorkUpdate{WaitFor: text(tomorrowsRun)})

	result := ownerReplies(t, service, item.ID, "看不懂这个，你需要我干啥？", "r1")
	if len(fw.sentCalls) != 1 {
		t.Fatalf("Brain inputs = %+v, want the reply", fw.sentCalls)
	}
	wakeIs(t, store, item.ID, WorkWakeNamed, tomorrowsRun)
	if current, found := currentWorkByID(t, store, item.ID); !found || current.UserAction == nil || current.UserAction.EventID != result.Event.ID {
		t.Fatalf("the slip must show the reply: found=%v %+v", found, current.UserAction)
	}
	st, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || st.Wake == nil || st.Wake.Kind != lifecycle.WakeNamed {
		t.Fatalf("canonical wait after the reply: %+v err=%v", st, err)
	}
}

// The shape live tonight, before named wakes: a user_input wake carrying
// Brain's -wait-for text and no question. A reply leaves it too.
func TestReplyLeavesAnUnaskedUserInputWait(t *testing.T) {
	store, service, _, _ := newUserActionService(t)
	legacy := &WorkWake{Kind: WorkWakeUserInput, Ref: "reviewer's delta re-check of #103/#104/#106 (tick 1119l) and merges"}
	item := parkOn(t, store, "LooperRequestBoard", WorkUpdate{Wake: &legacy})
	wakeIs(t, store, item.ID, WorkWakeUserInput, legacy.Ref)

	ownerReplies(t, service, item.ID, "这个也看不懂。", "r1")
	wakeIs(t, store, item.ID, WorkWakeUserInput, legacy.Ref)
}

// A reply on a Work that asked answers it: the question clears and the wait
// ends (TestWorkReplyReachesBrainAsTurnInputAndReleasesTheWait covers the
// slip); here the asked wait is on Brain's thread, as a review's is.
func TestReplyAnswersAWorkThatAsked(t *testing.T) {
	store, service, _, threadID := newUserActionService(t)
	item, err := store.CreateWork(Work{Title: "asks", Objective: "a call", SourceThreadID: threadID, CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	waiting := WorkWaiting
	question := "Keep both copies, or the newest edit?"
	onThread := &WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:" + threadID}
	if _, err := store.UpdateWork(item.ID, WorkUpdate{Status: &waiting, Wake: &onThread, Question: &question}); err != nil {
		t.Fatal(err)
	}
	ownerReplies(t, service, item.ID, "Keep both", "r1")
	after, err := store.Work(item.ID)
	if err != nil || after.Wake != nil || after.Question != "" || after.Status == WorkWaiting {
		t.Fatalf("an answered question must end the wait: %+v err=%v", after, err)
	}
}

// Input on Brain's thread (a chat message, or a reply on another Work's card)
// ends only the thread parks that ask the person something.
func TestThreadInputEndsOnlyParksThatAsk(t *testing.T) {
	store, _, _, threadID := newUserActionService(t)
	park := func(title, question string) Work {
		t.Helper()
		item, err := store.CreateWork(Work{Title: title, Objective: "parked", SourceThreadID: threadID, CompletionPolicy: CompletionBounded})
		if err != nil {
			t.Fatal(err)
		}
		waiting := WorkWaiting
		onThread := &WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:" + threadID}
		update := WorkUpdate{Status: &waiting, Wake: &onThread}
		if question != "" {
			update.Question = &question
		}
		if item, err = store.UpdateWork(item.ID, update); err != nil {
			t.Fatal(err)
		}
		return item
	}
	quiet := park("asks nothing", "")
	asking := park("asks", "Ship it?")
	deliverBrainThreadInput(t, store, threadID, "chat:1")
	wakeIs(t, store, quiet.ID, WorkWakeUserInput, "brain-thread:"+threadID)
	if after, _ := store.Work(asking.ID); after.Wake != nil {
		t.Fatalf("the asking park must end on the person's input: %+v", after.Wake)
	}
}

// In a review, -status waiting parks on Brain's thread only when Brain asks;
// otherwise on what -wait-for names, or a bare named park.
func TestReviewWaitParksOnTheThreadOnlyWhenBrainAsks(t *testing.T) {
	text := func(s string) *string { return &s }
	waiting := func() *WorkStatus { s := WorkWaiting; return &s }

	store, service, item, _ := hostHandlingFixture(t)
	if _, err := service.BrainUpdateWork(item.ID, WorkUpdate{Status: waiting()}); err != nil {
		t.Fatal(err)
	}
	wakeIs(t, store, item.ID, WorkWakeNamed, "operator:"+item.ID)

	store, service, item, _ = hostHandlingFixture(t)
	if _, err := service.BrainUpdateWork(item.ID, WorkUpdate{Status: waiting(), WaitFor: text(tomorrowsRun)}); err != nil {
		t.Fatal(err)
	}
	wakeIs(t, store, item.ID, WorkWakeNamed, tomorrowsRun)

	store, service, item, _ = hostHandlingFixtureOnThread(t, "review-thread")
	if _, err := service.BrainUpdateWork(item.ID, WorkUpdate{Status: waiting(), Question: text("Ship it?")}); err != nil {
		t.Fatal(err)
	}
	wakeIs(t, store, item.ID, WorkWakeUserInput, "brain-thread:review-thread")
}

// The closure rules hold: a Work Brain left waiting on a named later step,
// after it read the result, is not judged or closed because the person wrote
// on it, even by a judge that would say done.
func TestOwnerReplyNeverOpensANamedWaitToTheJudge(t *testing.T) {
	judge := &fakeJudge{verdict: JudgeVerdict{Outcome: VerdictDone, Reason: "the last turn is done"}}
	store, service, item := endedHandlingFixture(t, judge)
	waitFor := "Brain rebuilds and restarts the live daemon, then checks reclaim"
	if _, err := service.BrainUpdateWork(item.ID, WorkUpdate{Status: func() *WorkStatus { s := WorkWaiting; return &s }(), WaitFor: &waitFor}); err != nil {
		t.Fatal(err)
	}
	wakeIs(t, store, item.ID, WorkWakeNamed, waitFor)
	ownerReplies(t, service, item.ID, "这个也看不懂。", "r1")
	wakeIs(t, store, item.ID, WorkWakeNamed, waitFor)

	later := time.Now().Add(settleDebounce + 5*time.Minute)
	service.now = func() time.Time { return later }
	runSettle(t, service)
	if got, _ := store.Work(item.ID); got.Status != WorkWaiting || closedByRuntime(t, store, item.ID) {
		t.Fatalf("the reply let the judge close a named wait: %+v", got)
	}
	if len(judge.briefs) != 0 {
		t.Fatalf("a Work waiting on a named step was judged: %d briefs", len(judge.briefs))
	}

	// And an outage fallback, once the wait is released by something else,
	// still sees the named step.
	if _, err := store.FSM().ClearWait(lifecycle.WorkID(item.ID), lifecycle.WakeNamed, waitFor, "test:released"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	if !store.namedWaitSince(item.ID, time.Time{}) {
		t.Fatal("a named wake is not counted as a named wait")
	}
}

// hostHandlingFixtureOnThread is hostHandlingFixture for a Work that came
// from a Brain thread, so a review's wait can park on it.
func hostHandlingFixtureOnThread(t *testing.T, threadID string) (*Store, *Service, Work, WorkReviewAction) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "deploy", Objective: "deploy the preview build", SourceThreadID: threadID})
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
	for _, fact := range []watcher.TurnFact{{Kind: "running", SourceID: "r1"}, {Kind: "done", SourceID: "d1", Summary: "deployed"}} {
		fact.SessionID, fact.TurnID, fact.Class, fact.At = "worker", "turn:first", watcher.EvidenceControl, time.Now()
		if _, err := service.ApplyDelegatedTurnProgress(fact); err != nil {
			t.Fatal(err)
		}
	}
	claimed, _ := claimAndDeliverTestReview(t, store, "host")
	return store, service, item, claimed
}
