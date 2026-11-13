package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture is the tool-call records of a real Claude Code 2.1.322 Session
// (default permission mode, three `curl` calls it asked about): the first was
// allowed, the second refused by a permission hook (toolDenialKind
// "permission-rule"), the third refused at the terminal prompt ("user-rejected").
func claudePermissionFixture(t *testing.T, lines int) CodexConversation {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "claude_permission_tool_calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	kept := strings.Join(strings.Split(strings.TrimSpace(string(raw)), "\n")[:lines], "\n") + "\n"
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(kept), 0o600); err != nil {
		t.Fatal(err)
	}
	conversation, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	return conversation
}

func TestPendingConversationToolCallsNameTheCallAPermissionPromptAsksAbout(t *testing.T) {
	// The tool call is written before the prompt opens and has no result yet.
	pending := PendingConversationToolCalls(claudePermissionFixture(t, 5))
	if len(pending) != 1 {
		t.Fatalf("pending = %+v", pending)
	}
	got := pending[0]
	if got.CallID != "toolu_01BP98uTFK22xVax9XTQWn7q" || got.Tool != "Bash" || got.Summary != "curl -sI https://example.net" || got.Done {
		t.Fatalf("pending call = %+v", got)
	}
	if none := PendingConversationToolCalls(claudePermissionFixture(t, 6)); len(none) != 0 {
		t.Fatalf("a settled conversation has no pending call: %+v", none)
	}
}

func TestConversationToolCallSaysWhetherItRanOrWasRefused(t *testing.T) {
	conversation := claudePermissionFixture(t, 6)
	for callID, want := range map[string]ConversationToolCall{
		"toolu_01KviKsG2eJQVeNbYw7XocDL": {Tool: "Bash", Summary: "curl -sI https://example.org", Done: true},
		"toolu_018zqGCHUbvbRHizfABqEYMx": {Tool: "Bash", Summary: "curl -sI https://example.com", Done: true, Rejected: true, Error: false},
		"toolu_01BP98uTFK22xVax9XTQWn7q": {Tool: "Bash", Summary: "curl -sI https://example.net", Done: true, Rejected: true},
	} {
		got, ok := ConversationToolCallByID(conversation, callID)
		want.CallID = callID
		if !ok || got != want {
			t.Fatalf("%s: got %+v ok=%v, want %+v", callID, got, ok, want)
		}
	}
	byLine, ok := ConversationToolCallByLine(conversation, "Bash", "curl  -sI https://example.net")
	if !ok || byLine.CallID != "toolu_01BP98uTFK22xVax9XTQWn7q" || !byLine.Rejected {
		t.Fatalf("by line = %+v ok=%v", byLine, ok)
	}
}

func TestClaudeToolSummaryIsTheLineAnApprovalShows(t *testing.T) {
	for _, tc := range []struct{ tool, input, want string }{
		{"Bash", `{"command":"git push origin main","description":"Push"}`, "git push origin main"},
		{"Edit", `{"file_path":"/repo/a.go","old_string":"x"}`, "/repo/a.go"},
		{"WebFetch", `{"description":"Fetch the page"}`, "Fetch the page"},
		{"Task", `{"prompt":"go"}`, "Task"},
	} {
		if got := ClaudeToolSummary(tc.tool, json.RawMessage(tc.input)); got != tc.want {
			t.Fatalf("%s: %q, want %q", tc.tool, got, tc.want)
		}
	}
}
