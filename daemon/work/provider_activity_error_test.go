package work

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Claude Code records a dropped stream as a synthetic assistant record whose
// stop reason looks like an ordinary end; the error record must survive.
func TestClaudeAPIErrorRecordSettlesActivityWithProviderError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-10-11T16:15:00.000Z","sessionId":"s1","message":{"role":"user","content":"Render the trailer"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-10-11T16:26:13.811Z","sessionId":"s1","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}],"stop_reason":"tool_use"}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-10-11T16:26:16.412Z","sessionId":"s1","error":"server_error","truncatedAfterOutput":true,"isApiErrorMessage":true,"message":{"model":"<synthetic>","role":"assistant","stop_reason":"stop_sequence","content":[{"type":"text","text":"API Error: Connection lost mid-response. The response above may be incomplete."}]}}`,
		`{"type":"system","subtype":"turn_duration","durationMs":638141,"timestamp":"2026-10-11T16:26:16.436Z","sessionId":"s1"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conversation, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	activity := conversation.Activity
	if activity == nil || activity.Status != ProviderActivityCompleted || activity.SettledAt == "" {
		t.Fatalf("activity = %+v, want settled", activity)
	}
	if activity.Error == nil || activity.Error.Code != "server_error" ||
		activity.Error.Message != "API Error: Connection lost mid-response. The response above may be incomplete." {
		t.Fatalf("activity error = %+v", activity.Error)
	}

	// A later ordinary turn starts without the old error.
	more := []string{
		`{"type":"user","uuid":"u2","timestamp":"2026-10-11T16:27:00.000Z","sessionId":"s1","message":{"role":"user","content":"continue"}}`,
		`{"type":"assistant","uuid":"a3","timestamp":"2026-10-11T16:28:00.000Z","sessionId":"s1","message":{"role":"assistant","content":[{"type":"text","text":"Done."}],"stop_reason":"end_turn"}}`,
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(strings.Join(more, "\n") + "\n")
	_ = f.Close()
	conversation, err = parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Activity == nil || conversation.Activity.Status != ProviderActivityCompleted || conversation.Activity.Error != nil {
		t.Fatalf("clean follow-up activity = %+v", conversation.Activity)
	}
}

// Observed in the drill: Claude flushes the partial text as end_turn, then
// writes the error record a second later.
func TestClaudeAPIErrorRecordAfterPartialEndTurnEndsSameActivity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-10-11T17:19:00.000Z","sessionId":"s1","message":{"role":"user","content":"step 1"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-10-11T17:20:01.590Z","sessionId":"s1","message":{"role":"assistant","content":[{"type":"text","text":"Step 1 done."}],"stop_reason":"end_turn"}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-10-11T17:20:02.545Z","sessionId":"s1","isApiErrorMessage":true,"error":"server_error","message":{"model":"<synthetic>","role":"assistant","stop_reason":"stop_sequence","content":[{"type":"text","text":"API Error: Connection lost mid-response. The response above may be incomplete."}]}}`,
		`{"type":"system","subtype":"turn_duration","timestamp":"2026-10-11T17:20:02.556Z","sessionId":"s1"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conversation, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	got := conversation.Activity
	if got == nil || got.Status != ProviderActivityCompleted || got.Error == nil || got.Error.Code != "server_error" ||
		got.SettledAt != "2026-10-11T17:20:02.545Z" {
		t.Fatalf("activity = %+v", got)
	}
}

func TestClaudeAPIErrorRecordKeepsUpstreamStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-10-11T16:15:00.000Z","sessionId":"s1","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-10-11T16:15:05.000Z","sessionId":"s1","error":"rate_limit","apiErrorStatus":429,"isApiErrorMessage":true,"message":{"model":"<synthetic>","role":"assistant","stop_reason":"stop_sequence","content":[{"type":"text","text":"API Error: Request rejected (429) · Upstream rate limit exceeded, please retry later"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conversation, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := conversation.Activity; got == nil || got.Error == nil || got.Error.Code != "rate_limit" || got.Error.HTTPStatus != 429 {
		t.Fatalf("activity = %+v", got)
	}
}

func TestCodexErrorEventSettlesActivityWithProviderError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	lines := []string{
		`{"timestamp":"2026-04-13T07:30:00.000Z","type":"session_meta","payload":{"id":"s1","cwd":"/repo"}}`,
		`{"timestamp":"2026-04-13T07:30:01.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1"}}`,
		`{"timestamp":"2026-04-13T07:30:02.000Z","type":"event_msg","payload":{"type":"user_message","message":"fix it"}}`,
		`{"timestamp":"2026-04-13T07:37:39.909Z","type":"event_msg","payload":{"type":"error","message":"stream disconnected before completion: stream closed before response.completed","codex_error_info":"other"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conversation, err := parseCodexConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	got := conversation.Activity
	if got == nil || got.Status != ProviderActivityFailed || got.Error == nil || got.Error.Code != "other" ||
		got.Error.Message != "stream disconnected before completion: stream closed before response.completed" {
		t.Fatalf("activity = %+v err=%+v", got, got.Error)
	}
}
