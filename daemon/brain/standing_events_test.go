package brain

import (
	"testing"

	"github.com/daoleno/mewla/daemon/lifecycle"
)

func TestRaiseWorkEventOpensTheReviewThatWakesBrain(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetChatState(ChatState{ThreadID: "trigger-thread"}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
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
