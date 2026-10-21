package work

import (
	"path/filepath"
	"strings"
	"testing"
)

func claudeRewindUser(uuid, parent, text string) map[string]any {
	return map[string]any{
		"type": "user", "uuid": uuid, "parentUuid": parent, "sessionId": "rewind",
		"timestamp": "2026-10-21T01:00:00.000Z",
		"message":   map[string]any{"role": "user", "content": text},
	}
}

func claudeRewindAssistant(uuid, parent, text string) map[string]any {
	return map[string]any{
		"type": "assistant", "uuid": uuid, "parentUuid": parent, "sessionId": "rewind",
		"timestamp": "2026-10-21T01:00:01.000Z",
		"message": map[string]any{
			"role": "assistant", "stop_reason": "end_turn",
			"content": []map[string]any{{"type": "text", "text": text}},
		},
	}
}

func claudeConversationBodies(t *testing.T, path string) []string {
	t.Helper()
	got, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatalf("parseClaudeConversation: %v", err)
	}
	var bodies []string
	for _, event := range got.Events {
		if event.Kind == "user_message" || event.Kind == "assistant_message" {
			bodies = append(bodies, event.Role+": "+event.Body)
		}
	}
	return bodies
}

// Claude's "Restore conversation" appends the edited prompt as a second child
// of the same parent. The abandoned prompt and everything under it must leave
// the conversation, or the phone would show both timelines.
func TestParseClaudeConversation_RewindDropsAbandonedBranch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rewind.jsonl")
	writeJSONL(t, path,
		claudeRewindUser("u1", "", "reply one"),
		claudeRewindAssistant("a1", "u1", "one"),
		claudeRewindUser("u2", "a1", "create b.txt"),
		claudeRewindAssistant("a2", "u2", "made b"),
		claudeRewindUser("u3", "a2", "reply two"),
		claudeRewindAssistant("a3", "u3", "two"),
		// Rewound to before u2: the edited prompt hangs off a1.
		claudeRewindUser("u4", "a1", "create c.txt"),
		claudeRewindAssistant("a4", "u4", "made c"),
	)
	got := strings.Join(claudeConversationBodies(t, path), "\n")
	want := "user: reply one\nassistant: one\nuser: create c.txt\nassistant: made c"
	if got != want {
		t.Fatalf("conversation after rewind:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseClaudeConversation_RewindTwiceKeepsLatestBranch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rewind-twice.jsonl")
	writeJSONL(t, path,
		claudeRewindUser("u1", "", "first"),
		claudeRewindAssistant("a1", "u1", "ok"),
		claudeRewindUser("u2", "a1", "second"),
		claudeRewindAssistant("a2", "u2", "ok2"),
		claudeRewindUser("u3", "a1", "second, edited"),
		claudeRewindAssistant("a3", "u3", "ok3"),
		claudeRewindUser("u4", "a1", "second, edited again"),
		claudeRewindAssistant("a4", "u4", "ok4"),
	)
	got := strings.Join(claudeConversationBodies(t, path), "\n")
	want := "user: first\nassistant: ok\nuser: second, edited again\nassistant: ok4"
	if got != want {
		t.Fatalf("conversation after two rewinds:\n%s\nwant:\n%s", got, want)
	}
}

// Parallel tool results share a parent with each other, never with a second
// human prompt, so they are not a fork.
func TestParseClaudeConversation_ParallelToolResultsAreNotAFork(t *testing.T) {
	toolResult := func(uuid, parent, id string) map[string]any {
		return map[string]any{
			"type": "user", "uuid": uuid, "parentUuid": parent, "sessionId": "rewind",
			"timestamp": "2026-10-21T01:00:02.000Z",
			"message": map[string]any{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": id, "content": "ok"},
			}},
		}
	}
	path := filepath.Join(t.TempDir(), "parallel.jsonl")
	writeJSONL(t, path,
		claudeRewindUser("u1", "", "run both"),
		claudeRewindAssistant("a1", "u1", "running"),
		toolResult("t1", "a1", "call-1"),
		toolResult("t2", "a1", "call-2"),
		claudeRewindAssistant("a2", "t2", "done"),
	)
	got := strings.Join(claudeConversationBodies(t, path), "\n")
	want := "user: run both\nassistant: running\nassistant: done"
	if got != want {
		t.Fatalf("parallel tool results changed the conversation:\n%s\nwant:\n%s", got, want)
	}
}

// Esc is recorded as a user-shaped line. It must not show as a prompt, and the
// turn it stopped must end, or the person could neither edit nor resend.
func TestParseClaudeConversation_InterruptEndsTheTurnWithoutAPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interrupt.jsonl")
	writeJSONL(t, path,
		claudeRewindUser("u1", "", "count slowly"),
		map[string]any{
			"type": "assistant", "uuid": "a1", "parentUuid": "u1", "sessionId": "rewind",
			"timestamp": "2026-10-21T01:00:01.000Z",
			"message": map[string]any{"role": "assistant", "stop_reason": "tool_use", "content": []map[string]any{
				{"type": "tool_use", "id": "call-1", "name": "Bash", "input": map[string]any{"command": "sleep 20"}},
			}},
		},
		map[string]any{
			"type": "user", "uuid": "u2", "parentUuid": "a1", "sessionId": "rewind",
			"timestamp": "2026-10-21T01:00:02.000Z",
			"message": map[string]any{"role": "user", "content": []map[string]any{
				{"type": "text", "text": "[Request interrupted by user for tool use]"},
			}},
		},
	)
	got, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range got.Events {
		if event.Kind == "user_message" && strings.HasPrefix(event.Body, "[Request interrupted") {
			t.Fatalf("the interrupt note showed as a prompt: %+v", event)
		}
	}
	if got.Activity != nil && got.Activity.Status == ProviderActivityRunning {
		t.Fatalf("the stopped turn is still running: %+v", got.Activity)
	}
}

// Editing the very first prompt forks at the root: the edited prompt has no
// parent either. The old first prompt must leave the conversation, or the
// daemon keeps hiding the rewound turns and the new turn with them.
func TestParseClaudeConversation_RewindToFirstPromptDropsOldRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rewind-first.jsonl")
	writeJSONL(t, path,
		claudeRewindUser("u1", "", "Reply with ONE"),
		claudeRewindAssistant("a1", "u1", "ONE"),
		claudeRewindUser("u2", "", "Reply with TWO"),
		claudeRewindAssistant("a2", "u2", "TWO"),
	)
	got := strings.Join(claudeConversationBodies(t, path), "\n")
	want := "user: Reply with TWO\nassistant: TWO"
	if got != want {
		t.Fatalf("conversation after rewinding to the first prompt:\n%s\nwant:\n%s", got, want)
	}
}
