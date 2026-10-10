package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// Real Brain duplicate (thread brain_1790733088397305726, 2026-10-10): the
// pasted message has blank-line runs. Claude recorded the exact bytes inside
// its paste envelope, but display cleaning collapses the runs, and the echo row
// was materialized before the admission was projected.
const realBlankRunBrainInput = "还有这个放在中间是非常的突兀的：\n\nHeads up: several agents skip approval prompts by default.\n\nSecurity and privacy · Report a vulnerability\n\n\n还有 web 版本也不能和小猫交互，你可以自行优化下。\n\n\n还有 settings 里面的 connection 链接，不应该再写 wss:// .... ws 这种了吧？"

func claudeUserTranscript(t *testing.T, sessionID, uuid, content string, at time.Time) work.CodexConversation {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	row, _ := json.Marshal(map[string]any{
		"type": "user", "sessionId": sessionID, "uuid": uuid,
		"timestamp": at.Format(time.RFC3339Nano), "origin": map[string]any{"kind": "human"},
		"message": map[string]any{"role": "user", "content": content},
	})
	if err := os.WriteFile(path, append(row, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	conversation, err := work.LoadHostConversationByIdentity(work.HostTranscriptIdentity{
		Provider: work.WorkerProviderClaude, SessionID: sessionID, Path: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation.Events) != 1 {
		t.Fatalf("fixture must yield one provider event: %+v", conversation.Events)
	}
	return conversation
}

func acceptEchoTestAdmission(t *testing.T, store *Store, requestID, threadID, sessionID, body string, createdAt, acceptedAt time.Time) BrainInputAdmission {
	t.Helper()
	store.now = func() time.Time { return createdAt }
	candidate := BrainInputAdmission{
		RequestID: requestID, ThreadID: threadID, HostSessionID: "brain-host:@echo-test",
		SessionID: sessionID, DisplayBody: body,
	}
	if _, created, err := store.PrepareBrainInputAdmission(candidate); err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	store.now = func() time.Time { return acceptedAt }
	accepted, _, changed, err := store.AcceptBrainInputAdmission(candidate)
	if err != nil || !changed {
		t.Fatalf("accept changed=%v err=%v", changed, err)
	}
	return accepted
}

// The echo lands in the timeline between acceptance and projection, and its
// display body differs from the admitted bytes. One send must stay one row.
func TestRacedEchoWithDisplayCleanedBodyShowsOnce(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		content string
	}{
		{"paste with blank-line runs", realBlankRunBrainInput, "\n\n<pasted_content id=\"4f28\">\n" + realBlankRunBrainInput + "\n</pasted_content id=\"4f28\">\n"},
		{"typed with trailing space", "有一个维度，就是 \nSub2api 本来有的我们就不动", "有一个维度，就是 \nSub2api 本来有的我们就不动"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			threadID := "brain_1790733088397305726"
			sessionID := "07ac686b-8805-4402-87bb-8f5fed88e002"
			createdAt := time.Date(2026, 10, 10, 13, 1, 6, 913498809, time.UTC)
			accepted := acceptEchoTestAdmission(t, store, "mv2ekioj_gtqkej", threadID, sessionID, tc.body,
				createdAt, time.Date(2026, 10, 10, 13, 1, 7, 441905900, time.UTC))
			conversation := claudeUserTranscript(t, sessionID, "e2131cc3-df69-4c7e-be25-0c3d2916c438", tc.content,
				time.Date(2026, 10, 10, 13, 1, 7, 448000000, time.UTC))
			echo := conversation.Events[0]
			if AdmissionDigest(echo.Body) == accepted.BodySHA256 {
				t.Fatal("fixture must have a display body that differs from the admitted bytes")
			}
			if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
				t.Fatal(err)
			}
			if err := store.ProjectBrainInputAdmission(accepted); err != nil {
				t.Fatal(err)
			}
			for pass := 0; pass < 2; pass++ {
				if pass == 1 {
					if store, err = NewStore(root); err != nil {
						t.Fatal(err)
					}
				}
				if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
					t.Fatal(err)
				}
				items, err := store.ThreadTimeline(threadID, 0)
				if err != nil || len(items) != 1 {
					t.Fatalf("pass %d: one sent message must leave one row, got %+v err=%v", pass, items, err)
				}
				if items[0].ID != accepted.RequestID || items[0].AdmissionEchoEventID != echo.ID || items[0].Body != tc.body {
					t.Fatalf("pass %d: admission did not claim its raced echo: %+v", pass, items[0])
				}
				if events := TimelineItemsToConversationEvents(items); len(events) != 1 || events[0].Body != tc.body {
					t.Fatalf("pass %d: wire must show the message once: %+v", pass, events)
				}
			}
		})
	}
}

// Rows the live store already holds: the echo row was durable first and has
// no stored native digest. The live provider event proves the exact bytes, so
// the next materialization collapses the duplicate.
func TestDurableDisplayCleanedEchoDuplicateIsRepaired(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_1790733088397305726"
	sessionID := "07ac686b-8805-4402-87bb-8f5fed88e002"
	createdAt := time.Date(2026, 10, 10, 13, 1, 6, 913498809, time.UTC)
	accepted := acceptEchoTestAdmission(t, store, "mv2ekioj_gtqkej", threadID, sessionID, realBlankRunBrainInput,
		createdAt, time.Date(2026, 10, 10, 13, 1, 7, 441905900, time.UTC))
	conversation := claudeUserTranscript(t, sessionID, "e2131cc3-df69-4c7e-be25-0c3d2916c438",
		"\n\n<pasted_content id=\"4f28\">\n"+realBlankRunBrainInput+"\n</pasted_content id=\"4f28\">\n",
		time.Date(2026, 10, 10, 13, 1, 7, 448000000, time.UTC))
	echo := conversation.Events[0]
	// Exactly the two rows messages.jsonl kept, in file order.
	if _, err := store.AppendTimelineItem(TimelineItem{
		ID: echo.ID, ThreadID: threadID, SessionID: sessionID, Role: "user",
		Body: echo.Body, CreatedAt: time.Date(2026, 10, 10, 13, 1, 7, 448000000, time.UTC), Kind: timelineKindUserMessage,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendTimelineItem(brainInputAdmissionTimelineItem(accepted)); err != nil {
		t.Fatal(err)
	}
	if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 || items[0].ID != accepted.RequestID || items[0].AdmissionEchoEventID != echo.ID {
		t.Fatalf("durable duplicate not repaired: %+v err=%v", items, err)
	}
}

// Two genuinely identical sends stay two messages; each claims its own echo.
func TestIdenticalRacedSendsStayTwoMessages(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_1790733088397305726"
	sessionID := "07ac686b-8805-4402-87bb-8f5fed88e002"
	content := "\n\n<pasted_content id=\"4f28\">\n" + realBlankRunBrainInput + "\n</pasted_content id=\"4f28\">\n"
	base := time.Date(2026, 10, 10, 13, 1, 6, 0, time.UTC)
	var all []work.CodexConversationEvent
	for index, requestID := range []string{"first-send", "second-send"} {
		at := base.Add(time.Duration(index) * time.Minute)
		accepted := acceptEchoTestAdmission(t, store, requestID, threadID, sessionID, realBlankRunBrainInput,
			at, at.Add(500*time.Millisecond))
		conversation := claudeUserTranscript(t, sessionID, requestID+"-native", content, at.Add(510*time.Millisecond))
		all = append(all, conversation.Events...)
		conversation.Events = all
		if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
			t.Fatal(err)
		}
		if err := store.ProjectBrainInputAdmission(accepted); err != nil {
			t.Fatal(err)
		}
		if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("two sends must leave two rows, got %+v err=%v", items, err)
	}
	if items[0].ID != "first-send" || items[0].AdmissionEchoEventID != all[0].ID ||
		items[1].ID != "second-send" || items[1].AdmissionEchoEventID != all[1].ID {
		t.Fatalf("each send must claim its own echo: %+v", items)
	}
}
