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

func TestRaiseWorkEventOpensTheReviewThatWakesBrain(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetChatState(ChatState{ThreadID: "trigger-thread"}); err != nil {
		t.Fatal(err)
	}
	service := newTestService(t, store, &fakeWatcher{}, nil)
	item, err := store.CreateWork(Work{Title: "Trigger: PR loop", Objective: "standing", SourceThreadID: "trigger-thread", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}

	// A plain actionable append opens no Review, so it would wake nobody.
	plain, _, err := service.AppendWorkEvent(WorkEvent{WorkID: item.ID, Kind: "trigger.fired", DedupeKey: "plain", Actionable: true})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := store.FSM().State(lifecycle.WorkID(item.ID)); st != nil && st.Review != nil {
		t.Fatalf("a plain append opened a Review: %+v (event %s)", st.Review, plain.ID)
	}

	first, opened, err := service.RaiseWorkEvent(WorkEvent{WorkID: item.ID, Kind: "trigger.fired", DedupeKey: "fire:1", PayloadRef: "/p/1.txt", Actionable: true})
	if err != nil || !opened {
		t.Fatalf("raise = %v opened=%v", err, opened)
	}
	st, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || st.Review == nil || st.Review.EventID != first.ID {
		t.Fatalf("review %+v err=%v", st, err)
	}
	if handled, found, err := service.WorkEventHandled(item.ID, first.ID); err != nil || !found || handled {
		t.Fatalf("an undelivered review counted as handled: %v %v %v", handled, found, err)
	}

	// While that Review is with Brain, a second one cannot open.
	second, opened, err := service.RaiseWorkEvent(WorkEvent{WorkID: item.ID, Kind: "trigger.failing", DedupeKey: "failing:1", Actionable: true})
	if err != nil || opened {
		t.Fatalf("second raise = %v opened=%v", err, opened)
	}
	if st, _ := store.FSM().State(lifecycle.WorkID(item.ID)); st.Review.EventID != first.ID {
		t.Fatalf("the second event replaced the open Review: %s", second.ID)
	}

	// Raising the same key again is the same event.
	again, opened, err := service.RaiseWorkEvent(WorkEvent{WorkID: item.ID, Kind: "trigger.fired", DedupeKey: "fire:1", Actionable: true})
	if err != nil || again.ID != first.ID || !opened {
		t.Fatalf("replay = %v %v opened=%v", again.ID, err, opened)
	}
	if _, found, _ := service.WorkEventHandled(item.ID, "missing"); found {
		t.Fatal("a missing event was found")
	}
}

// An actionable event from outside a Session (mewla brain work event
// -actionable) reaches the Host as a <mewla_work_event> turn, or fails
// saying why.
func TestDeliverWorkEventReachesTheHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, outcomes: map[string]watcher.InputOutcome{}, sessions: map[string]*classifier.Worker{
		"host": {ID: "host", Hidden: true, State: classifier.StateRunning},
	}}
	service := newTestService(t, store, fw, nil)
	item, err := store.CreateWork(Work{Title: "PRLoopWatcher", Objective: "standing PR loop"})
	if err != nil {
		t.Fatal(err)
	}
	hostTexts := func() []string {
		var texts []string
		for _, call := range fw.sentCalls {
			if call.sessionID == "host" && strings.Contains(call.text, "<mewla_work_event>") {
				texts = append(texts, call.text)
			}
		}
		return texts
	}
	event, err := service.DeliverWorkEvent(WorkEvent{WorkID: item.ID, Kind: "pr.new", DedupeKey: "new-1", PayloadRef: "worklog/new.txt", Summary: "NEW perpetuo#1@aaaa"})
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	texts := hostTexts()
	if len(texts) != 1 || !strings.Contains(texts[0], event.ID) || !strings.Contains(texts[0], "worklog/new.txt") || !strings.Contains(texts[0], "NEW perpetuo#1@aaaa") {
		t.Fatalf("the Host was sent %q, want one work event %s with its payload", texts, event.ID)
	}
	lease := requireReviewDelivered(t, store, item.ID)

	// The same dedupe key again is the same, delivered event: nothing new is sent.
	again, err := service.DeliverWorkEvent(WorkEvent{WorkID: item.ID, Kind: "pr.new", DedupeKey: "new-1"})
	if err != nil || again.ID != event.ID || len(hostTexts()) != 1 {
		t.Fatalf("redeliver: %v %s sent=%d", err, again.ID, len(hostTexts()))
	}

	// While Brain handles that one, another is refused with the reason.
	_, err = service.DeliverWorkEvent(WorkEvent{WorkID: item.ID, Kind: "pr.new", DedupeKey: "new-2"})
	if !errors.Is(err, ErrWorkEventNotDelivered) || !strings.Contains(err.Error(), "Brain still has an earlier event of this Work") {
		t.Fatalf("second event while one is with Brain: %v", err)
	}
	// Its key was used: retrying it says to pick a new one.
	_, err = service.DeliverWorkEvent(WorkEvent{WorkID: item.ID, Kind: "pr.new", DedupeKey: "new-2"})
	if !errors.Is(err, ErrWorkEventNotDelivered) || !strings.Contains(err.Error(), "new -dedupe") {
		t.Fatalf("retry of an undelivered key: %v", err)
	}

	resolveDeliveredReview(t, store, item.ID, WorkDispositionContinue)
	settleCanonicalHostTurnForTest(t, store, "host", lease.ProviderTurnID)
	third, err := service.DeliverWorkEvent(WorkEvent{WorkID: item.ID, Kind: "pr.new", DedupeKey: "new-3", Summary: "NEW perpetuo#2@bbbb"})
	if err != nil {
		t.Fatalf("after Brain handled the first: %v", err)
	}
	if texts := hostTexts(); len(texts) != 2 || !strings.Contains(texts[1], third.ID) {
		t.Fatalf("the Host was not sent the next event: %d", len(texts))
	}
}

// A Trigger is the producer its Work waits on: Brain's wait-for parks a typed
// wake, and a firing ends it and opens its Review (the generic CLI append is
// still refused). A Work a Worker's turn owns still takes it as history only.
// An empty wait-for alone ends the wait too.
func TestTriggerFiringEndsATypedWaitButNotAWorkersTurn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetChatState(ChatState{ThreadID: "trigger-thread"}); err != nil {
		t.Fatal(err)
	}
	service := newTestService(t, store, &fakeWatcher{}, nil)
	waitOn := func(title, waitFor string) Work {
		t.Helper()
		item, err := store.CreateWork(Work{Title: title, Objective: "standing", SourceThreadID: "trigger-thread", CompletionPolicy: CompletionBounded})
		if err != nil {
			t.Fatal(err)
		}
		waiting := WorkWaiting
		item, err = store.UpdateWork(item.ID, WorkUpdate{Status: &waiting, WaitFor: &waitFor})
		if err != nil || item.Status != WorkWaiting || item.Wake == nil || item.WaitFor != waitFor {
			t.Fatalf("wait-for did not park a wake: %+v err=%v", item, err)
		}
		return item
	}

	item := waitOn("PRLoopWatcher", "trigger.fired from Trigger 3b5d1da9")
	if _, err := service.DeliverWorkEvent(WorkEvent{WorkID: item.ID, Kind: "pr.new", DedupeKey: "cli-1"}); !errors.Is(err, ErrWorkEventNotDelivered) || !strings.Contains(err.Error(), "waits on its named wake") {
		t.Fatalf("the CLI's generic append over a typed wait: %v", err)
	}
	fired, opened, err := service.RaiseWorkEvent(WorkEvent{WorkID: item.ID, Kind: "trigger.fired", DedupeKey: "fire:2", PayloadRef: "/p/2.txt", Actionable: true})
	if err != nil || !opened {
		t.Fatalf("a firing over Brain's wait-for: opened=%v err=%v", opened, err)
	}
	st, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || st.Wake != nil || st.Review == nil || st.Review.EventID != fired.ID {
		t.Fatalf("the firing did not end the wait and open its Review: %+v err=%v", st, err)
	}
	if work, _ := store.Work(item.ID); work.WaitFor != "" || work.Wake != nil {
		t.Fatalf("the Work still shows the wait: %+v", work)
	}

	owned, err := store.CreateWork(Work{Title: "owned by a Worker", Objective: "delegated", SourceThreadID: "trigger-thread", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	bootstrapAdmittedTurnFixture(t, store, owned.ID, watcher.AdmittedTurn{SessionID: "worker", TurnID: "worker-turn", AcceptedAt: time.Now().UTC()})
	_, opened, err = service.RaiseWorkEvent(WorkEvent{WorkID: owned.ID, Kind: "trigger.fired", DedupeKey: "fire:1", Actionable: true})
	if err != nil || opened {
		t.Fatalf("a firing over a Worker's turn: opened=%v err=%v", opened, err)
	}
	if st, _ := store.FSM().State(lifecycle.WorkID(owned.ID)); st.Review != nil || st.Attempt == nil {
		t.Fatalf("a firing took a Worker's Work: %+v", st)
	}

	cleared := waitOn("Waiting", "the deploy window")
	empty := ""
	after, err := service.BrainUpdateWork(cleared.ID, WorkUpdate{WaitFor: &empty})
	if err != nil || after.WaitFor != "" || after.Wake != nil || after.Status == WorkWaiting {
		t.Fatalf("an empty wait-for did not end the wait: %+v err=%v", after, err)
	}
	if st, _ := store.FSM().State(lifecycle.WorkID(cleared.ID)); st.Wake != nil {
		t.Fatalf("the lifecycle kept the wake: %+v", st.Wake)
	}
}
