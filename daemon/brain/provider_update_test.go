package brain

import (
	"testing"

	"github.com/daoleno/mewla/daemon/work"
)

func TestProviderUpdatesMaterializeButReasoningDoesNot(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-updates"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	events := []work.CodexConversationEvent{
		{ID: "p0", Kind: "user_message", Role: "user", Body: "go", Timestamp: "2026-11-05T06:54:00Z"},
		{ID: "m1", Kind: "assistant_message", Role: "assistant", Body: "Done.", Timestamp: "2026-11-05T06:54:33Z"},
		{ID: "u1", Kind: work.ConversationKindUpdate, Role: "assistant", Body: "Quota is now 256.", Timestamp: "2026-11-05T06:54:31Z"},
		{ID: "r1", Kind: "commentary", Title: "Reasoning", Body: "hidden", Timestamp: "2026-11-05T06:54:32Z"},
	}
	conversation := work.CodexConversation{Available: true, SessionID: "s1", Events: events}
	for range 2 { // replay stays idempotent
		if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[1].ID != "u1" || items[2].ID != "m1" {
		t.Fatalf("timeline = %+v", items)
	}
	projected := TimelineItemsToConversationEvents(items)
	if projected[1].Kind != work.ConversationKindUpdate || projected[1].Body != "Quota is now 256." || projected[2].Kind != "assistant_message" {
		t.Fatalf("projection = %+v", projected)
	}
}
