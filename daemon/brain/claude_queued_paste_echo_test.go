package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// Real Brain duplicate (thread brain_1793797903668055662, 2026-11-14 15:58:59Z):
// the owner sent a pasted crash report with a picture while Brain was busy.
// Claude Code took it mid-turn as a queued_command attachment whose prompt is
// the bare paste envelope (no "\n\n" before, no "\n" after). The daemon kept
// only the digest of the wrapped bytes, and display cleaning folded the
// report's blank-line runs, so neither digest matched the admission and the
// message showed twice: the admission row and the provider echo row.
const realQueuedPasteBrainInput = "为啥呢，一个已经发布的，生产级别的 app，打开会立马看到这种报错\n\n" +
	"Mewla 0.4.2 (40299, release) com.daoleno.mewla\nOnePlus PYR110, Android 16 (API 36), arm64-v8a\n" +
	"js at 2026-11-14T15:55:41Z\n\nError: interrupted\n    at filter (native)\n    at Fm (<null>:0:1)\n\n\n----\n\n" +
	"Mewla 0.4.2 (40299, release) com.daoleno.mewla\nOnePlus PYR110, Android 16 (API 36), arm64-v8a\n" +
	"js at 2026-11-14T15:53:55Z\n\nError: interrupted\n    at test (native)\n    at Tw (<null>:0:1)\n\n" +
	`<mewla_attachments>{"files":[{"name":"Screenshot_2026-11-14-23-58-06-61_6090dccb9b3f9d3a230313874bd173c2.jpg",` +
	`"path":"/home/daoleno/.mewla/uploads/a77204f9-f34b-495d-a261-95c3165f42d8.jpg","content_type":"image/jpeg","size":526449}]}</mewla_attachments>`

const realQueuedPasteRecordUUID = "63d48c04-1d90-4cbd-9b6e-d14d0ad1eff6"

func realQueuedPasteBrainConversation(t *testing.T, sessionID string, busySince, at time.Time) work.CodexConversation {
	t.Helper()
	// The record shape Claude Code 2.1.323 wrote (fields Mewla reads).
	prompt := "<pasted_content id=\"64a6\">\n" + realQueuedPasteBrainInput + "\n</pasted_content id=\"64a6\">"
	busy, _ := json.Marshal(map[string]any{
		"type": "user", "sessionId": sessionID, "uuid": "c0ee8ef2-21bc-4cd1-9e54-82eaa66c881b",
		"timestamp": busySince.Format(time.RFC3339Nano), "origin": map[string]any{"kind": "human"},
		"message": map[string]any{"role": "user", "content": "release it"},
	})
	queued, _ := json.Marshal(map[string]any{
		"type": "attachment", "sessionId": sessionID, "uuid": realQueuedPasteRecordUUID,
		"timestamp": at.Format(time.RFC3339Nano),
		"attachment": map[string]any{
			"type": "queued_command", "prompt": prompt, "commandMode": "prompt",
			"origin": map[string]any{"kind": "human"}, "humanTurn": true,
		},
	})
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, append(append(append(busy, '\n'), queued...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	conversation, err := work.LoadHostConversationByIdentity(work.HostTranscriptIdentity{
		Provider: work.WorkerProviderClaude, SessionID: sessionID, Path: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	return conversation
}

func TestQueuedPastedBrainInputEchoShowsOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_1793797903668055662"
	sessionID := "5ecced80-be50-44b3-aa3e-7dfa7243c74e"
	createdAt := time.Date(2026, 11, 14, 15, 58, 58, 824707755, time.UTC)
	accepted := acceptAndProjectEchoTestAdmission(
		t, store, "mwglc22u_glfv0e", threadID, sessionID, realQueuedPasteBrainInput,
		createdAt, time.Date(2026, 11, 14, 15, 58, 59, 186767407, time.UTC),
	)
	conversation := realQueuedPasteBrainConversation(t, sessionID,
		createdAt.Add(-3*time.Minute), time.Date(2026, 11, 14, 15, 58, 59, 152000000, time.UTC))
	echoID := sessionID + ":" + realQueuedPasteRecordUUID + ":user"
	if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
		t.Fatal(err)
	}
	var users []TimelineItem
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Kind == timelineKindUserMessage && item.Body != "release it" {
			users = append(users, item)
		}
	}
	if len(users) != 1 {
		t.Fatalf("one sent message must leave one row, got %d: %+v", len(users), users)
	}
	if users[0].ID != accepted.RequestID || users[0].AdmissionEchoEventID != echoID {
		t.Fatalf("admission did not claim its queued echo %q: %+v", echoID, users[0])
	}
	// Materializing again (every later poll) never brings the echo back.
	if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
		t.Fatal(err)
	}
	again, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(again) != len(items) {
		t.Fatalf("replay changed the thread: %d -> %d rows, err=%v", len(items), len(again), err)
	}
}
