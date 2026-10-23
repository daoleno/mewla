package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/attachment"
	"github.com/daoleno/mewla/daemon/work"
)

// The admission digests the exact provider input, which includes the files the
// person attached; the provider's echo of that input claims the admission row
// once, and the phone receives the files as a typed field.
func TestAdmissionWithAttachmentsDigestsRenderingAndEchoMatches(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-attachment-admission"
	sessionID := "019fec64-attachment-session"
	files := []attachment.File{{Name: "photo.png", Path: "/uploads/photo.png", ContentType: "image/png"}}
	body := attachment.Message{Text: "look at this", Files: files}.Render()
	other := attachment.Message{Text: "look at this", Files: []attachment.File{{Name: "photo.png", Path: "/uploads/other.png", ContentType: "image/png"}}}.Render()
	if AdmissionDigest(body) == AdmissionDigest(other) || AdmissionDigest(body) == AdmissionDigest("look at this") {
		t.Fatal("the admission digest does not cover the attached files")
	}
	createdAt := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	acceptAndProjectEchoTestAdmission(t, store, "receipt-attachments", threadID, sessionID, body, createdAt, createdAt.Add(time.Second))

	providerID := sessionID + ":attachments"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: sessionID,
		Events: []work.CodexConversationEvent{{
			ID: providerID, Timestamp: createdAt.Add(2 * time.Second).Format(time.RFC3339Nano),
			Kind: timelineKindUserMessage, Role: "user", Body: body, AdmissionSHA256: AdmissionDigest(body),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 || !items[0].BrainAdmission || items[0].AdmissionEchoEventID != providerID {
		t.Fatalf("echo did not claim the admission row once: %+v err=%v", items, err)
	}
	events := TimelineItemsToConversationEvents(items)
	if len(events) != 1 || events[0].Body != "look at this" || len(events[0].Attachments) != 1 || events[0].Attachments[0].Path != "/uploads/photo.png" {
		t.Fatalf("typed projection = %+v", events)
	}
}
