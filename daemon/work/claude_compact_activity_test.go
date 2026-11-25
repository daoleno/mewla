package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A /compact the person sends (Claude Code 2.1.332, a Brain Host in the 0.4.29
// gate) is no turn: Claude writes the compact boundary, the summary it made
// (isCompactSummary), then the command and its output, and no reply. The
// summary used to open an Activity that nothing settled, so the phone said
// "Working" until the next message, minutes later. The records' shapes are a
// real transcript's; the words and times are made up.
func TestClaudeManualCompactLeavesNoActivityRunning(t *testing.T) {
	// Each record names the one before it; the compact boundary starts a new
	// chain and names the old one as its logical parent, as Claude writes it.
	parent := ""
	record := func(fields map[string]any) map[string]any {
		fields["sessionId"] = "compact-host"
		fields["cwd"] = "/work/brain"
		if fields["subtype"] == "compact_boundary" {
			fields["parentUuid"], fields["logicalParentUuid"] = nil, parent
		} else if parent != "" {
			fields["parentUuid"] = parent
		}
		parent = fields["uuid"].(string)
		return fields
	}
	user := func(uuid, at string, content any, extra map[string]any) map[string]any {
		fields := map[string]any{"type": "user", "uuid": uuid, "timestamp": at, "message": map[string]any{"role": "user", "content": content}}
		for key, value := range extra {
			fields[key] = value
		}
		return record(fields)
	}
	reply := func(uuid, at, text string) map[string]any {
		return record(map[string]any{"type": "assistant", "uuid": uuid, "timestamp": at, "message": map[string]any{
			"role": "assistant", "stop_reason": "end_turn", "content": []map[string]any{{"type": "text", "text": text}},
		}})
	}
	turnDuration := func(uuid, at string) map[string]any {
		return record(map[string]any{"type": "system", "subtype": "turn_duration", "uuid": uuid, "timestamp": at, "durationMs": 1800, "isMeta": false})
	}
	before := []map[string]any{
		user("prompt-1", "2026-07-20T08:00:00.000Z", "Reply only OK.", map[string]any{"promptSource": "typed"}),
		reply("reply-1", "2026-07-20T08:00:02.000Z", "OK"),
		turnDuration("duration-1", "2026-07-20T08:00:02.010Z"),
	}
	// What Claude appends once the compaction finished, in its order.
	compacted := []map[string]any{
		record(map[string]any{"type": "system", "subtype": "compact_boundary", "uuid": "boundary", "timestamp": "2026-07-20T08:01:14.010Z",
			"content": "Conversation compacted", "compactMetadata": map[string]any{"trigger": "manual", "preTokens": 40000}}),
		user("summary", "2026-07-20T08:01:14.000Z", "This session is being continued from a previous conversation. Summary: replies were OK.",
			map[string]any{"isCompactSummary": true, "isVisibleInTranscriptOnly": true}),
		user("caveat", "2026-07-20T08:01:00.000Z", "<local-command-caveat>The command below was run directly in Claude Code.</local-command-caveat>", map[string]any{"isMeta": true}),
		user("command", "2026-07-20T08:01:00.000Z", "<command-name>/compact</command-name>\n            <command-message>compact</command-message>\n            <command-args></command-args>", nil),
		user("output", "2026-07-20T08:01:14.100Z", "<local-command-stdout>Compacted (ctrl+o to see full summary)</local-command-stdout>", nil),
	}
	after := []map[string]any{
		user("prompt-2", "2026-07-20T08:05:00.000Z", "Reply only OK again.", map[string]any{"promptSource": "typed"}),
	}
	finished := []map[string]any{
		reply("reply-2", "2026-07-20T08:05:02.000Z", "OK"),
		turnDuration("duration-2", "2026-07-20T08:05:02.010Z"),
	}

	path := filepath.Join(t.TempDir(), "compact-host.jsonl")
	appendRecords := func(records []map[string]any) {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		for _, fields := range records {
			line, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.Write(append(line, '\n')); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Both readers: the full parse and the incremental one the live views use.
	read := func(step string) (CodexConversation, CodexConversation) {
		full, err := parseClaudeConversation(path)
		if err != nil {
			t.Fatalf("%s: full parse: %v", step, err)
		}
		incremental, err := parseClaudeConversationIncremental(path)
		if err != nil {
			t.Fatalf("%s: incremental parse: %v", step, err)
		}
		return full, incremental
	}

	appendRecords(before)
	read("before")
	appendRecords(compacted)
	for _, got := range func() []CodexConversation { a, b := read("compacted"); return []CodexConversation{a, b} }() {
		if got.CompactedAt == "" {
			t.Fatalf("the compaction is not recorded: %+v", got)
		}
		if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted || got.Activity.SettledAt != "2026-07-20T08:00:02Z" {
			t.Fatalf("after /compact the Activity = %+v, want the last turn, completed", got.Activity)
		}
	}
	appendRecords(after)
	for _, got := range func() []CodexConversation { a, b := read("next prompt"); return []CodexConversation{a, b} }() {
		if got.Activity == nil || got.Activity.Status != ProviderActivityRunning || got.Activity.StartedAt != "2026-07-20T08:05:00Z" {
			t.Fatalf("the next prompt's Activity = %+v, want running from its prompt", got.Activity)
		}
	}
	appendRecords(finished)
	for _, got := range func() []CodexConversation { a, b := read("next reply"); return []CodexConversation{a, b} }() {
		if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted || got.Activity.StartedAt != "2026-07-20T08:05:00Z" ||
			got.Activity.SettledAt != "2026-07-20T08:05:02Z" {
			t.Fatalf("the next turn's Activity = %+v, want completed", got.Activity)
		}
	}
}
