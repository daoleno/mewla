package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// Real Brain duplicate (thread brain_1790733088397305726): the user sent text
// containing tag-like markup; Claude Code recorded it inside its paste
// envelope, so the provider echo never matched the admission digest and the
// message showed twice: once as typed, once as the raw envelope.
const realPastedBrainInput = "<task-notification> <task-id>a3e33bf1f19bac0f0</task-id> <tool-use-id>toolu_017pfJ9FvTCbE6B2KQwXtm3h</tool-use-id> <output-file>/tmp/claude-1000/-home-da\n\n类似这种东西，能不能好好做个 UI 呀？"

func realPastedBrainConversation(t *testing.T, sessionID string, at time.Time) work.CodexConversation {
	t.Helper()
	wrapped := "\n\n<pasted_content id=\"4f28\">\n" + realPastedBrainInput + "\n</pasted_content id=\"4f28\">\n"
	path := filepath.Join(t.TempDir(), "session.jsonl")
	row, _ := json.Marshal(map[string]any{
		"type": "user", "sessionId": sessionID, "uuid": "4b9a6cd1-6215-4fc0-9e97-2414dbbba34e",
		"timestamp": at.Format(time.RFC3339Nano), "origin": map[string]any{"kind": "human"},
		"message": map[string]any{"role": "user", "content": wrapped},
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
	return conversation
}

func TestPastedBrainInputEchoShowsOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_1790733088397305726"
	sessionID := "07ac686b-8805-4402-87bb-8f5fed88e002"
	createdAt := time.Date(2026, 10, 7, 1, 40, 18, 300000000, time.UTC)
	accepted := acceptAndProjectEchoTestAdmission(
		t, store, "muxfxf3o_ls6w20", threadID, sessionID, realPastedBrainInput,
		createdAt, createdAt.Add(43*time.Millisecond),
	)
	conversation := realPastedBrainConversation(t, sessionID, createdAt.Add(50*time.Millisecond))
	if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("one sent message must leave one row, got %+v err=%v", items, err)
	}
	if items[0].ID != accepted.RequestID || items[0].AdmissionEchoEventID != conversation.Events[0].ID {
		t.Fatalf("admission did not claim its pasted echo: %+v", items)
	}
	suppress := ProviderUserEchoSuppressions(items, sessionID)
	if !suppress[conversation.Events[0].ID] {
		t.Fatal("live provider overlay must suppress the claimed echo")
	}
	events := TimelineItemsToConversationEvents(items)
	if len(events) != 1 || events[0].Kind != timelineKindUserMessage || events[0].Body != realPastedBrainInput {
		t.Fatalf("pasted tag-like text must show once as the user's message: %+v", events)
	}
}

func TestStartupRepairsLegacyPasteEnvelopeDuplicate(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_1790733088397305726"
	sessionID := "07ac686b-8805-4402-87bb-8f5fed88e002"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 10, 7, 1, 40, 18, 300000000, time.UTC)
	store.now = func() time.Time { return createdAt }
	prepared, created, err := store.PrepareBrainInputAdmission(BrainInputAdmission{
		RequestID: "muxfxf3o_ls6w20", ThreadID: threadID,
		HostSessionID: "brain-host:@live", SessionID: sessionID, DisplayBody: realPastedBrainInput,
	})
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	store.now = func() time.Time { return createdAt.Add(43 * time.Millisecond) }
	accepted, _, changed, err := store.AcceptBrainInputAdmission(prepared)
	if err != nil || !changed {
		t.Fatalf("accept changed=%v err=%v", changed, err)
	}
	if _, err := store.AppendTimelineItem(brainInputAdmissionTimelineItem(accepted)); err != nil {
		t.Fatal(err)
	}
	// The durable row exactly as the live store kept it before the fix.
	echoID := sessionID + ":4b9a6cd1-6215-4fc0-9e97-2414dbbba34e:user"
	if _, err := store.AppendTimelineItem(TimelineItem{
		ID: echoID, ThreadID: threadID, SessionID: sessionID, Role: "user",
		Body:      "<pasted_content id=\"4f28\">\n" + realPastedBrainInput + "\n</pasted_content id=\"4f28\">",
		CreatedAt: createdAt.Add(50 * time.Millisecond), Kind: timelineKindUserMessage,
	}); err != nil {
		t.Fatal(err)
	}
	unprojected, _, err := store.UnprojectedBrainInputAdmissions(10)
	if err != nil || len(unprojected) != 1 {
		t.Fatalf("unprojected=%+v err=%v", unprojected, err)
	}
	if err := store.ProjectBrainInputAdmission(unprojected[0]); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 || items[0].AdmissionEchoEventID != echoID {
		t.Fatalf("legacy envelope duplicate not repaired: %+v err=%v", items, err)
	}
}

func TestDurableRowsProjectTaskNotificationsAndUnwrapPastes(t *testing.T) {
	at := time.Date(2026, 10, 7, 1, 39, 13, 353000000, time.UTC)
	notification := "<task-notification>\n<task-id>a3e33bf1f19bac0f0</task-id>\n<status>completed</status>\n" +
		"<summary>Agent \"Check name conflicts\" finished</summary>\n<result>**Verdict:** weak.</result>\n</task-notification>"
	items := []TimelineItem{
		{ID: "s:n:user", SessionID: "s", Role: "user", Body: notification, CreatedAt: at, Kind: timelineKindUserMessage},
		{ID: "s:p:user", SessionID: "s", Role: "user", Body: "<pasted_content id=\"4f28\">\nhello <b>\n</pasted_content id=\"4f28\">", CreatedAt: at, Kind: timelineKindUserMessage},
		{ID: "admission", SessionID: "s", Role: "user", Body: notification, CreatedAt: at, Kind: timelineKindUserMessage, BrainAdmission: true, AdmissionSHA256: AdmissionDigest(notification)},
	}
	events := TimelineItemsToConversationEvents(items)
	if len(events) != 3 {
		t.Fatalf("events=%+v", events)
	}
	if events[0].ID != "s:n:user" || events[0].Kind != "status" || events[0].Source != work.TaskNotificationConversationSource ||
		events[0].Title != `Agent "Check name conflicts" finished` || events[0].Body != "**Verdict:** weak." || events[0].Status != "done" {
		t.Fatalf("legacy notification row must project as a card: %+v", events[0])
	}
	if events[1].Kind != timelineKindUserMessage || events[1].Body != "hello <b>" {
		t.Fatalf("legacy paste row must show without envelope: %+v", events[1])
	}
	if events[2].Kind != timelineKindUserMessage || events[2].Body != notification {
		t.Fatalf("the user's own admitted words never become a card: %+v", events[2])
	}
}

// Large Mewla Work Event deliveries to the Brain Host arrive paste-wrapped. The
// display unwrap must not keep the canonical envelope from binding the turn,
// and the transport envelope must stay out of the visible conversation.
func TestPasteWrappedCanonicalWorkEventStillBindsAndStaysHidden(t *testing.T) {
	payload := work.FormatDirectWorkEventInput(work.DirectWorkEventInput{
		EventID: "e49f5d9b3c77c963", WorkID: "9042e98d-cc24-4bec-921b-0630bd09c4f7", WorkRevision: 8,
		HandlingID: "handling", ProviderTurnID: "turn", WorkTitle: "Work", Kind: "session.done",
		Source: "worker", Summary: strings.Repeat("large event 你好 ", 150), NextAction: "review",
	})
	if _, ok := work.ParseCanonicalDirectWorkEventInput(payload); !ok {
		t.Fatal("fixture must be a canonical envelope")
	}
	wrapped := "\n\n<pasted_content id=\"4f28\">\n" + payload + "\n</pasted_content id=\"4f28\">\n"
	at := time.Date(2026, 10, 7, 8, 56, 53, 983000000, time.UTC)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	row, _ := json.Marshal(map[string]any{"type": "user", "sessionId": "session", "uuid": "native-input", "timestamp": at.Format(time.RFC3339Nano), "message": map[string]any{"role": "user", "content": wrapped}})
	if err := os.WriteFile(path, append(row, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	conversation, err := work.LoadHostConversationByIdentity(work.HostTranscriptIdentity{Provider: work.WorkerProviderClaude, SessionID: "session", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	// Same filtering as HostBoundProviderConversation.
	conversation.Events = work.SuppressPrivateHostTurns(conversation.Events)
	conversation.Activity = &work.ProviderActivity{ID: "activity", Status: work.ProviderActivityRunning, StartedAt: at.Format(time.RFC3339Nano)}
	submission := watcher.InputAdmission{SessionID: "host", ProposedTurnID: "turn", Receipt: "turn", PayloadSHA256: AdmissionDigest(payload), AcceptedAt: at.Add(-time.Second)}
	if _, _, matched := boundHostConversationSubmissionResolution(conversation, submission, at.Add(time.Second)); !matched {
		t.Fatal("paste-wrapped canonical work event must still bind its pending admission")
	}
	if visible := work.SanitizeConversationProjection(conversation).Events; len(visible) != 0 {
		t.Fatalf("transport-only work event must not be a visible user row: %+v", visible)
	}
	if providerEventMaterializable(conversation.Events[0]) {
		t.Fatal("transport-only work event must not be materialized into Brain history")
	}
}
