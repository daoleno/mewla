package work

import (
	"path/filepath"
	"testing"
)

// Trimmed from a real Brain session: Claude Code prints a thinking block's text
// as a "●" line only when the record says thinkingDisplay "updates".
func TestClaudeUpdatesThinkingBecomesUpdateEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	record := func(uuid, ts, display string, blocks ...map[string]any) map[string]any {
		content := make([]any, 0, len(blocks))
		for _, block := range blocks {
			content = append(content, block)
		}
		r := map[string]any{
			"type": "assistant", "uuid": uuid, "timestamp": ts, "sessionId": "s1",
			"message": map[string]any{"role": "assistant", "content": content},
		}
		if display != "" {
			r["thinkingDisplay"] = display
		}
		return r
	}
	user := map[string]any{
		"type": "user", "uuid": "u1", "timestamp": "2026-11-05T06:54:00.000Z", "sessionId": "s1",
		"message": map[string]any{"role": "user", "content": "go"},
	}
	writeJSONL(t, path, user,
		record("a1", "2026-11-05T06:54:30.000Z", "updates", map[string]any{"type": "thinking", "thinking": "", "signature": "sig"}),
		record("a2", "2026-11-05T06:54:31.004Z", "updates", map[string]any{"type": "thinking", "thinking": "The Singapore on-demand vCPU quota is now 256.\n\n", "signature": "sig"}),
		record("a3", "2026-11-05T06:54:32.000Z", "", map[string]any{"type": "thinking", "thinking": "hidden chain of thought", "signature": "sig"}),
		record("a4", "2026-11-05T06:54:33.000Z", "", map[string]any{"type": "text", "text": "Done."}),
	)
	conversation, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	var updates, messages, reasoning []CodexConversationEvent
	for _, event := range conversation.Events {
		switch event.Kind {
		case ConversationKindUpdate:
			updates = append(updates, event)
		case "assistant_message":
			messages = append(messages, event)
		case "commentary":
			reasoning = append(reasoning, event)
		}
	}
	if len(updates) != 1 || updates[0].Body != "The Singapore on-demand vCPU quota is now 256." || updates[0].Timestamp != "2026-11-05T06:54:31.004Z" {
		t.Fatalf("updates = %+v", updates)
	}
	if len(messages) != 1 || messages[0].Body != "Done." {
		t.Fatalf("messages = %+v", messages)
	}
	if len(reasoning) != 1 || reasoning[0].Body != "hidden chain of thought" {
		t.Fatalf("non-updates thinking must stay Reasoning: %+v", reasoning)
	}
}
