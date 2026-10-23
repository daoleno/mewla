package server

import (
	"testing"

	"github.com/daoleno/mewla/daemon/brain"
)

// A Brain-scoped conversation subscription answers "unchanged" only while the
// Store's change counter holds still; a write to the host binding or chat
// state is a miss without any file being looked at.
func TestBrainConversationProjectionVersionFollowsStoreWrites(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{brain: brain.NewService(store, nil, nil)}
	const scope = "brain-thread:thread-a"

	first := srv.brainConversationProjectionVersion(scope)
	if first == "" {
		t.Fatal("a Brain scope has no version")
	}
	if again := srv.brainConversationProjectionVersion(scope); again != first {
		t.Fatalf("an unchanged store changed the version: %q then %q", first, again)
	}
	if got := srv.brainConversationProjectionVersion("session:%1"); got != "" {
		t.Fatalf("a non-Brain scope has version %q", got)
	}

	if err := store.SetHostSession("%7", "codex"); err != nil {
		t.Fatal(err)
	}
	afterHost := srv.brainConversationProjectionVersion(scope)
	if afterHost == first {
		t.Fatal("a host session write did not invalidate the cached version")
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread-a"}); err != nil {
		t.Fatal(err)
	}
	if srv.brainConversationProjectionVersion(scope) == afterHost {
		t.Fatal("a chat state write did not invalidate the cached version")
	}
}
