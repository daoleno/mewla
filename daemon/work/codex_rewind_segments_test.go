package work

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codexRewindLine(t *testing.T, kind string, payload map[string]any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"type": kind, "timestamp": "2026-10-21T01:30:00.000Z", "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	return string(data) + "\n"
}

func codexRewindTurn(t *testing.T, text, reply string) string {
	t.Helper()
	return codexRewindLine(t, "event_msg", map[string]any{"type": "task_started"}) +
		codexRewindLine(t, "event_msg", map[string]any{"type": "user_message", "message": text}) +
		codexRewindLine(t, "response_item", map[string]any{"type": "message", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": reply}}}) +
		codexRewindLine(t, "event_msg", map[string]any{"type": "task_complete"})
}

func codexRewindMeta(t *testing.T, threadID string, base map[string]any) string {
	t.Helper()
	payload := map[string]any{"id": "thread-root", "session_id": "thread-root", "cwd": "/repo/mewla", "history_mode": "paginated"}
	if base != nil {
		payload["history_base"] = base
	}
	return codexRewindLine(t, "session_meta", payload)
}

// Codex's "edit previous message" starts a new rollout file that holds only the
// turns after the rewind point; the kept turns stay in the segment it forked
// from. The conversation must read as one thread without the rewound turns.
func TestParseCodexConversation_RewoundSegmentStitchesKeptTurns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions", "2026", "10", "21")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	root := codexRewindMeta(t, "thread-root", nil) + codexRewindTurn(t, "one", "reply one") + codexRewindTurn(t, "two", "reply two")
	rootPath := filepath.Join(dir, "rollout-2026-10-21T09-30-14-thread-root.jsonl")
	// Rewound to before "two": the segment kept everything up to the end of turn one.
	keep := int64(len(codexRewindMeta(t, "thread-root", nil) + codexRewindTurn(t, "one", "reply one")))
	if err := os.WriteFile(rootPath, []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	segment := codexRewindMeta(t, "thread-segment", map[string]any{"thread_id": "thread-root", "end_ordinal_exclusive": 5, "end_byte_offset": keep}) +
		codexRewindTurn(t, "two, edited", "reply two edited")
	segmentPath := filepath.Join(dir, "rollout-2026-10-21T09-32-55-thread-root_thread-segment.jsonl")
	if err := os.WriteFile(segmentPath, []byte(segment), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := parseCodexConversation(segmentPath)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	var bodies []string
	seen := map[string]bool{}
	lastSeq := 0
	for _, event := range got.Events {
		if event.Kind != "user_message" && event.Kind != "assistant_message" {
			continue
		}
		bodies = append(bodies, event.Role+": "+event.Body)
		if seen[event.ID] {
			t.Fatalf("duplicate event id %q across segments", event.ID)
		}
		seen[event.ID] = true
		if event.Seq <= lastSeq {
			t.Fatalf("seq %d did not increase after %d", event.Seq, lastSeq)
		}
		lastSeq = event.Seq
	}
	want := "user: one\nassistant: reply one\nuser: two, edited\nassistant: reply two edited"
	if strings.Join(bodies, "\n") != want {
		t.Fatalf("stitched conversation:\n%s\nwant:\n%s", strings.Join(bodies, "\n"), want)
	}
}

func TestParseCodexConversation_RewoundSegmentChainsAcrossTwoRewinds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions", "2026", "10", "21")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	metaRoot := codexRewindMeta(t, "thread-root", nil)
	root := metaRoot + codexRewindTurn(t, "a", "ra") + codexRewindTurn(t, "b", "rb") + codexRewindTurn(t, "c", "rc")
	write("rollout-1-thread-root.jsonl", root)
	keepRoot := int64(len(metaRoot + codexRewindTurn(t, "a", "ra") + codexRewindTurn(t, "b", "rb")))
	metaTwo := codexRewindMeta(t, "s2", map[string]any{"thread_id": "thread-root", "end_byte_offset": keepRoot})
	two := metaTwo + codexRewindTurn(t, "c edited", "rc2") + codexRewindTurn(t, "d", "rd")
	write("rollout-2-thread-root_s2.jsonl", two)
	keepTwo := int64(len(metaTwo + codexRewindTurn(t, "c edited", "rc2")))
	three := codexRewindMeta(t, "s3", map[string]any{"thread_id": "s2", "end_byte_offset": keepTwo}) + codexRewindTurn(t, "d edited", "rd2")
	active := write("rollout-3-thread-root_s3.jsonl", three)

	got, err := parseCodexConversation(active)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	var users []string
	for _, event := range got.Events {
		if event.Kind == "user_message" {
			users = append(users, event.Body)
		}
	}
	if fmt.Sprint(users) != "[a b c edited d edited]" {
		t.Fatalf("user prompts across two rewinds = %v", users)
	}
}
