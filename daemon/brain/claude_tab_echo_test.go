package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// Real Brain duplicate (thread brain_1793797903668055662, 2026-11-25 04:27Z):
// the owner pasted a line holding a tab. Claude Code's prompt editor turns
// every tab into four spaces, so the transcript carries those bytes, not the
// admitted ones, and the admission did not claim its echo. Claude auto-compacted
// right after the send; the boundary only preserves the record by uuid.
const realTabBrainInput = "refused it, since deletes must name each file in full.\t•\n\n你这个规则过于严格了吧"

const realTabRecordUUID = "66fe3325-6481-4541-b2ef-fc4295a8f0fd"

func claudeTranscriptRecords(t *testing.T, sessionID string, records ...map[string]any) work.CodexConversation {
	t.Helper()
	var lines []byte
	for _, record := range records {
		record["sessionId"] = sessionID
		line, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(append(lines, line...), '\n')
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, lines, 0o600); err != nil {
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

func threadUserRows(t *testing.T, store *Store, threadID string) []TimelineItem {
	t.Helper()
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var users []TimelineItem
	for _, item := range items {
		if item.Kind == timelineKindUserMessage {
			users = append(users, item)
		}
	}
	return users
}

func TestTabBrainInputEchoShowsOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_1793797903668055662"
	sessionID := "043fb853-1bbd-44cb-b4dd-e7588dbfe9bf"
	accepted := acceptAndProjectEchoTestAdmission(t, store, "mwvmhj4x_vcpvuu", threadID, sessionID, realTabBrainInput,
		time.Date(2026, 11, 25, 4, 27, 46, 226717129, time.UTC), time.Date(2026, 11, 25, 4, 27, 46, 607159935, time.UTC))
	// The record shapes Claude Code 2.1.330 wrote (fields Mewla reads).
	conversation := claudeTranscriptRecords(t, sessionID,
		map[string]any{
			"type": "user", "uuid": realTabRecordUUID, "timestamp": "2026-11-25T04:27:46.613Z",
			"origin": map[string]any{"kind": "human"}, "promptSource": "typed", "version": "2.1.330",
			"message": map[string]any{"role": "user", "content": "\n\n<pasted_content id=\"8faa\">\n" +
				"refused it, since deletes must name each file in full.    •\n\n你这个规则过于严格了吧" +
				"\n</pasted_content id=\"8faa\">\n"},
		},
		map[string]any{
			"type": "system", "subtype": "compact_boundary", "uuid": "600736ff-46b0-43be-a3e0-90a2d9e8d71e",
			"timestamp": "2026-11-25T04:28:56.442Z", "parentUuid": nil, "logicalParentUuid": realTabRecordUUID,
			"content":         "Conversation compacted",
			"compactMetadata": map[string]any{"trigger": "auto", "preservedSegment": map[string]any{"tailUuid": realTabRecordUUID}},
		},
	)
	echoID := sessionID + ":" + realTabRecordUUID + ":user"
	for pass := 0; pass < 2; pass++ {
		if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
			t.Fatal(err)
		}
		users := threadUserRows(t, store, threadID)
		if len(users) != 1 {
			t.Fatalf("pass %d: one sent message must leave one row, got %d: %+v", pass, len(users), users)
		}
		if users[0].ID != accepted.RequestID || users[0].Body != realTabBrainInput || users[0].AdmissionEchoEventID != echoID {
			t.Fatalf("pass %d: admission did not claim its tab-expanded echo %q: %+v", pass, echoID, users[0])
		}
	}
}

// The live store already holds the echo row exactly as it was written: Claude's
// envelope digest and the four-space inner digest. Its transcript record has
// left the read window, so the stored digests alone must repair it.
func TestTabEchoDuplicateAlreadyStoredIsRepaired(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_1793797903668055662"
	sessionID := "043fb853-1bbd-44cb-b4dd-e7588dbfe9bf"
	accepted := acceptAndProjectEchoTestAdmission(t, store, "mwvmhj4x_vcpvuu", threadID, sessionID, realTabBrainInput,
		time.Date(2026, 11, 25, 4, 27, 46, 226717129, time.UTC), time.Date(2026, 11, 25, 4, 27, 46, 607159935, time.UTC))
	echoID := sessionID + ":" + realTabRecordUUID + ":user"
	if _, err := store.AppendTimelineItem(TimelineItem{
		ID: echoID, ThreadID: threadID, SessionID: sessionID, Role: "user",
		Body:      "refused it, since deletes must name each file in full.    •\n\n你这个规则过于严格了吧",
		CreatedAt: time.Date(2026, 11, 25, 4, 27, 46, 613000000, time.UTC), Kind: timelineKindUserMessage,
		Origin: TimelineOriginTranscript,
		ProviderInputSHA256: []string{
			"18cd8d88448329904937fbbd9e67e7fa9d7e445dbdd26c286a83062f26c1b618",
			"d2df8441a990d7df7ff46644b835fc7fb5c651e4c37cd56624e8f10bdb35372f",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if users := threadUserRows(t, store, threadID); len(users) != 2 {
		t.Fatalf("fixture must hold the live duplicate: %+v", users)
	}
	later := claudeTranscriptRecords(t, sessionID, map[string]any{
		"type": "assistant", "uuid": "b1e8a3f0-1d4c-4c6a-9a51-6f0f3d1c2b7e", "timestamp": "2026-11-25T04:31:02.000Z",
		"message": map[string]any{"role": "assistant", "stop_reason": "end_turn",
			"content": []map[string]any{{"type": "text", "text": "规则放宽了。"}}},
	})
	if err := store.MaterializeProviderConversation(threadID, later); err != nil {
		t.Fatal(err)
	}
	users := threadUserRows(t, store, threadID)
	if len(users) != 1 || users[0].ID != accepted.RequestID || users[0].AdmissionEchoEventID != echoID {
		t.Fatalf("stored tab-echo duplicate not repaired: %+v", users)
	}
}

// Real Brain duplicate (2026-11-18 12:44Z, Claude Code 2.1.325): a send Claude
// took while busy, recorded as a user record with promptSource "queued" whose
// content is the bare paste envelope, as a queued_command prompt is. Its text
// holds a tab and a blank-line run that display cleaning folds.
func TestQueuedUserRecordPasteEchoShowsOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_1793797903668055662"
	sessionID := "e976f820-9dde-40e6-9a3e-ab6a5768effb"
	body := "方案选 A（买 RunsOn，€300/年）还是 B（免费自建）。\t1.\n在 agents 的 Chrome 里登录一次 paul-freeride 的 GitHub。\n\n\n这个是关于啥的问题"
	accepted := acceptAndProjectEchoTestAdmission(t, store, "mwm45gry_urqoln", threadID, sessionID, body,
		time.Date(2026, 11, 18, 12, 44, 34, 738609304, time.UTC), time.Date(2026, 11, 18, 12, 44, 35, 104069033, time.UTC))
	recordUUID := "e5417f48-0724-40c0-b495-fa85ce45aef7"
	conversation := claudeTranscriptRecords(t, sessionID,
		map[string]any{
			"type": "user", "uuid": "3ca70d0b-9ba1-499b-874e-94ededc0fbbf", "timestamp": "2026-11-18T12:40:00.000Z",
			"origin": map[string]any{"kind": "human"}, "promptSource": "typed",
			"message": map[string]any{"role": "user", "content": "release it"},
		},
		map[string]any{
			"type": "user", "uuid": recordUUID, "timestamp": "2026-11-18T12:44:47.525Z",
			"origin": map[string]any{"kind": "human"}, "promptSource": "queued", "version": "2.1.325",
			"message": map[string]any{"role": "user", "content": "<pasted_content id=\"3aa2\">\n" +
				"方案选 A（买 RunsOn，€300/年）还是 B（免费自建）。    1.\n在 agents 的 Chrome 里登录一次 paul-freeride 的 GitHub。\n\n\n这个是关于啥的问题" +
				"\n</pasted_content id=\"3aa2\">"},
		},
	)
	echoID := sessionID + ":" + recordUUID + ":user"
	if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
		t.Fatal(err)
	}
	var sent []TimelineItem
	for _, item := range threadUserRows(t, store, threadID) {
		if item.Body != "release it" {
			sent = append(sent, item)
		}
	}
	if len(sent) != 1 || sent[0].ID != accepted.RequestID || sent[0].AdmissionEchoEventID != echoID {
		t.Fatalf("queued user record must be claimed by its admission %q: %+v", echoID, sent)
	}
}
