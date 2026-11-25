package work

import (
	"encoding/json"
	"testing"
	"time"
)

// Claude's answers to /model as Claude Code 2.1.332 writes them in its
// transcript (shapes from a real one; times invented).
func TestClaudeModelAnswerSinceReadsEachAnswerOnce(t *testing.T) {
	record := func(at, kind, text string) string {
		fields := map[string]any{"type": kind, "timestamp": at}
		if kind == "assistant" {
			fields["message"] = map[string]any{"role": "assistant", "model": "claude-sonnet-5-5", "content": []map[string]any{{"type": "text", "text": text}}}
		} else {
			fields["message"] = map[string]any{"role": "user", "content": text}
		}
		raw, _ := json.Marshal(fields)
		return string(raw) + "\n"
	}
	command := "<command-name>/model</command-name>\n            <command-message>model</command-message>\n            <command-args></command-args>"
	tail := []byte(
		record("2026-07-20T08:00:00.000Z", "user", command) +
			record("2026-07-20T08:00:00.000Z", "user", "<local-command-stdout>Set model to `Sonnet 5.5` for this session only with `low` effort</local-command-stdout>") +
			record("2026-07-20T08:00:10.000Z", "user", "done? The Set model to line above is yours") +
			record("2026-07-20T08:00:12.000Z", "assistant", "Set model to Haiku, said the model") +
			record("2026-07-20T08:01:00.000Z", "user", command) +
			record("2026-07-20T08:01:00.000Z", "user", "<local-command-stdout>Kept model as Sonnet 5.5</local-command-stdout>"))
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }

	got, ok := ClaudeModelAnswerSince(tail, at("2026-07-20T07:59:59Z"))
	if !ok || got.Line != "Kept model as Sonnet 5.5" || got.Named != "" {
		t.Fatalf("latest answer = %+v %v, want the kept line", got, ok)
	}
	got, ok = ClaudeModelAnswerSince(tail[:len(tail)-len(record("2026-07-20T08:01:00.000Z", "user", "<local-command-stdout>Kept model as Sonnet 5.5</local-command-stdout>"))], at("2026-07-20T07:59:59Z"))
	if !ok || got.Named != "Sonnet 5.5" || !got.SessionOnly || got.Effort != "low" ||
		got.Line != "Set model to Sonnet 5.5 for this session only with low effort" {
		t.Fatalf("switch answer = %+v %v", got, ok)
	}
	// Words in a message or an answer are not Claude's answer to /model.
	if got, ok := ClaudeModelAnswerSince(tail, at("2026-07-20T08:00:05Z")); !ok || got.Line != "Kept model as Sonnet 5.5" {
		t.Fatalf("since 08:00:05 = %+v %v", got, ok)
	}
	if _, ok := ClaudeModelAnswerSince(tail, at("2026-07-20T08:01:01Z")); ok {
		t.Fatal("an answer from before the switch was taken for its own")
	}
	// The model the Session runs on follows the same records: the answer
	// after the switch names the model, "Kept model as" changes nothing.
	if state := ClaudeTranscriptState(tail); state.Resolved != "claude-sonnet-5-5" || state.Named != "" || state.Effort != "low" {
		t.Fatalf("state = %+v", state)
	}
	// A saved default is a switch too, but not the session-only one Mewla makes.
	if saved := ParseClaudeSwitch("<local-command-stdout>Set model to \x1b[1mOpus 5.5\x1b[22m and saved as your default for new sessions</local-command-stdout>"); saved.Named != "Opus 5.5" || saved.SessionOnly {
		t.Fatalf("saved default = %+v", saved)
	}
}
