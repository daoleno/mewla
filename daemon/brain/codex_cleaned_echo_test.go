package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

func codexRolloutRecords(t *testing.T, sessionID string, records ...map[string]any) work.CodexConversation {
	t.Helper()
	var lines []byte
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(append(lines, line...), '\n')
	}
	path := filepath.Join(t.TempDir(), "rollout-"+sessionID+".jsonl")
	if err := os.WriteFile(path, lines, 0o600); err != nil {
		t.Fatal(err)
	}
	conversation, err := work.LoadHostConversationByIdentity(work.HostTranscriptIdentity{
		Provider: work.WorkerProviderCodex, SessionID: sessionID, Path: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	return conversation
}

// The record pair Codex (0.16x and 0.17x) writes for a person's input: a
// response_item user message whose input_text is the exact submitted bytes,
// then an item_completed UserMessage. No event_msg user_message follows.
func codexUserInputRecords(sessionID, turnID, at, text string) []map[string]any {
	return []map[string]any{
		{"timestamp": at, "type": "response_item", "payload": map[string]any{
			"type": "message", "id": "msg_" + turnID, "role": "user",
			"content": []map[string]any{{"type": "input_text", "text": text}},
		}},
		{"timestamp": at, "type": "event_msg", "payload": map[string]any{
			"type": "item_completed", "thread_id": sessionID, "turn_id": turnID,
			"item": map[string]any{"type": "UserMessage", "id": "item_" + turnID,
				"content": []map[string]any{{"type": "text", "text": text}}},
		}},
	}
}

// Real Brain duplicates on a Codex Host (thread brain_1793797903668055662):
// rows 28899/28900 (11-04, a send while Brain was mid-turn) and 40874/40873
// (11-22, a send that opened a turn). Codex kept the submitted bytes exactly,
// but Mewla digested the display-cleaned text (the "\n\n\n" run folded, the
// trailing space trimmed), so the admission never claimed its echo.
func TestCodexDisplayCleanedEchoShowsOnce(t *testing.T) {
	cases := []struct {
		name, sessionID, requestID, body string
		createdAt, acceptedAt            time.Time
		before                           []map[string]any
		at                               string
	}{
		{
			name: "mid-turn 11-04", sessionID: "01a1a6b2-0b67-7b43-99ef-39a7ad9d8a9f", requestID: "mw2afwqm_5mw81p",
			body:      "看下为啥 \n\n\nMewlaWebRedesign 这个 worker 现在持续在报错 502",
			createdAt: time.Date(2026, 11, 4, 15, 45, 16, 17384087, time.UTC), acceptedAt: time.Date(2026, 11, 4, 15, 45, 18, 158193102, time.UTC),
			before: []map[string]any{
				{"timestamp": "2026-11-04T15:45:18.153Z", "type": "event_msg", "payload": map[string]any{"type": "token_count"}},
			},
			at: "2026-11-04T15:45:18.155Z",
		},
		{
			// Trimmed from the 757-character report; same shape and the same
			// blank-line run before its last line.
			name: "new turn 11-22", sessionID: "01a202bb-2d32-7052-afce-3d2b073be54b", requestID: "mwrwhx36_ys2vum",
			body: "Mewla Windows 版有一个自动更新后频繁弹终端窗口、抢占焦点的问题。\n" +
				"手动 restart 能恢复，但下次自动更新可能再次复发。\n\n\n这是人家的 AI 给我们的反馈。",
			createdAt: time.Date(2026, 11, 22, 13, 56, 56, 141599173, time.UTC), acceptedAt: time.Date(2026, 11, 22, 13, 56, 58, 401204155, time.UTC),
			before: []map[string]any{
				{"timestamp": "2026-11-22T13:56:19.352Z", "type": "event_msg", "payload": map[string]any{"type": "task_complete", "turn_id": "01a2038a-2102-7c93-8c6c-629d056709ea"}},
				{"timestamp": "2026-11-22T13:56:58.263Z", "type": "event_msg", "payload": map[string]any{"type": "task_started", "turn_id": "01a2038b-b912-7b12-948c-63c7440689ce"}},
				{"timestamp": "2026-11-22T13:56:58.267Z", "type": "turn_context", "payload": map[string]any{"turn_id": "01a2038b-b912-7b12-948c-63c7440689ce", "cwd": "/home/daoleno/.mewla/brain/workspace"}},
			},
			at: "2026-11-22T13:56:58.269Z",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			threadID := "brain_1793797903668055662"
			accepted := acceptAndProjectEchoTestAdmission(t, store, tc.requestID, threadID, tc.sessionID, tc.body, tc.createdAt, tc.acceptedAt)
			records := []map[string]any{{"timestamp": "2026-11-01T00:00:00.000Z", "type": "session_meta", "payload": map[string]any{
				"id": tc.sessionID, "session_id": tc.sessionID, "cwd": "/home/daoleno/.mewla/brain/workspace", "originator": "codex_cli_rs",
			}}}
			records = append(append(records, tc.before...), codexUserInputRecords(tc.sessionID, "turn", tc.at, tc.body)...)
			conversation := codexRolloutRecords(t, tc.sessionID, records...)
			var echo work.CodexConversationEvent
			for _, event := range conversation.Events {
				if event.Kind == timelineKindUserMessage {
					echo = event
				}
			}
			if echo.AdmissionSHA256 != AdmissionDigest(tc.body) {
				t.Fatalf("echo must carry the submitted bytes' digest %s, got %+v", AdmissionDigest(tc.body), echo)
			}
			for pass := 0; pass < 2; pass++ {
				if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
					t.Fatal(err)
				}
				users := threadUserRows(t, store, threadID)
				if len(users) != 1 || users[0].ID != accepted.RequestID || users[0].AdmissionEchoEventID != echo.ID {
					t.Fatalf("pass %d: one sent message must leave one row claimed by its admission: %+v", pass, users)
				}
			}
		})
	}
}
