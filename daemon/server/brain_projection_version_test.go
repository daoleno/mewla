package server

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/calendar"
)

// A Brain-scoped conversation subscription answers "unchanged" only while the
// counters of what the overlay reads hold still; a write to the host binding,
// the chat state or the calendar is a miss without any file being looked at.
func TestBrainProjectionKeyFollowsWhatTheOverlayReads(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cal, err := calendar.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{brain: brain.NewService(store, nil, nil), calendar: cal}
	const scope = "brain-thread:thread-a"

	if err := store.SetChatState(brain.ChatState{ThreadID: "thread-a"}); err != nil {
		t.Fatal(err)
	}
	first := srv.brainProjectionKey(scope)
	if first == (brainProjectionKey{}) {
		t.Fatal("a Brain scope has no key")
	}
	if again := srv.brainProjectionKey(scope); again != first {
		t.Fatalf("an unchanged store changed the key: %+v then %+v", first, again)
	}
	if got := srv.brainProjectionKey("session:%1"); got != (brainProjectionKey{}) {
		t.Fatalf("a non-Brain scope has key %+v", got)
	}

	if err := store.SetHostSession("%7", "codex"); err != nil {
		t.Fatal(err)
	}
	afterHost := srv.brainProjectionKey(scope)
	if afterHost == first {
		t.Fatal("a host session write did not move the key")
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread-b"}); err != nil {
		t.Fatal(err)
	}
	afterChat := srv.brainProjectionKey(scope)
	if afterChat == afterHost {
		t.Fatal("a chat state write did not move the key")
	}

	due := time.Now().Add(time.Hour).UTC()
	if _, err := cal.Create(calendar.Item{
		ID: "item-1", Title: "Job", Kind: calendar.KindScheduledAction, DueAt: &due, Timezone: "UTC",
		Recurrence: calendar.RecurrenceNone, ActionInstruction: "Run", SourceThreadID: "thread-a",
	}); err != nil {
		t.Fatal(err)
	}
	if srv.brainProjectionKey(scope) == afterChat {
		t.Fatal("a calendar write did not move the key")
	}
}
