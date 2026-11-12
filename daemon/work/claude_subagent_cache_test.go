package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func appendRaw(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func jsonLine(t *testing.T, record any) string {
	t.Helper()
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

func subagentToolCall(n int) map[string]any {
	return claudeRecord("assistant", "sa-call-"+strings.Repeat("x", n%3)+string(rune('a'+n%26))+string(rune('a'+n/26)), "2026-10-17T10:03:00.000Z", true, map[string]any{
		"type": "tool_use", "id": "call-" + string(rune('a'+n%26)) + string(rune('a'+n/26)), "name": "Bash",
		"input": map[string]any{"command": "ls " + strings.Repeat("x", n%40)},
	})
}

func subagentToolResult(n int) map[string]any {
	return claudeRecord("user", "sa-res-"+string(rune('a'+n%26))+string(rune('a'+n/26)), "2026-10-17T10:03:01.000Z", true, map[string]any{
		"type": "tool_result", "tool_use_id": "call-" + string(rune('a'+n%26)) + string(rune('a'+n/26)), "content": "output " + strings.Repeat("y", n%30),
	})
}

// A subagent transcript is read once, and after that for the bytes it grew by;
// every answer is the one a read of the whole file gives.
func TestSubagentCacheReadsOnlyWhatGrewAndAnswersAsAFullRead(t *testing.T) {
	dir := t.TempDir()
	writeSubagent(t, dir, "a1", map[string]any{"agentType": "Explore", "description": "audit", "toolUseId": "task-1"},
		claudeRecord("user", "sa-0", "2026-10-17T10:02:59.000Z", true, map[string]any{"type": "text", "text": "look"}))
	path := filepath.Join(dir, "main-session", "subagents", "agent-a1.jsonl")
	var readBytes int64
	claudeSubagentObserve = func(_ string, n int64) { readBytes = n }
	t.Cleanup(func() { claudeSubagentObserve = nil })
	claudeSubagentCache.forget(path)

	same := func(step string) {
		t.Helper()
		got, ok := readClaudeSubagent(path)
		want, wantOK := readClaudeSubagentFull(path)
		if ok != wantOK || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: cached answer differs from a full read\ncached: %+v\nfull:   %+v", step, got, want)
		}
	}

	readBytes = -1
	same("first read")
	first, _ := os.Stat(path)
	if readBytes != first.Size() {
		t.Fatalf("first read took %d bytes, want the file's %d", readBytes, first.Size())
	}

	readBytes = -1
	same("unchanged")
	if readBytes != -1 {
		t.Fatalf("an unchanged subagent was read again (%d bytes)", readBytes)
	}

	for n := 1; n <= 300; n++ {
		added := jsonLine(t, subagentToolCall(n))
		if n%2 == 0 {
			added += jsonLine(t, subagentToolResult(n))
		}
		appendRaw(t, path, added)
		readBytes = -1
		same("append")
		if readBytes > int64(len(added)) || readBytes < 0 {
			t.Fatalf("append %d: read %d bytes for %d appended", n, readBytes, len(added))
		}
	}

	// The tool results that came late settle calls the first read already held.
	appendRaw(t, path, jsonLine(t, subagentToolResult(1)))
	same("late result")

	// A last line still being written waits for its newline.
	whole := jsonLine(t, claudeRecord("assistant", "sa-end", "2026-10-17T10:09:00.000Z", true, map[string]any{"type": "text", "text": "All done."}))
	appendRaw(t, path, whole[:len(whole)/2])
	same("half a line")
	appendRaw(t, path, whole[len(whole)/2:])
	same("the rest of the line")

	// The meta says finished: the answer changes without the transcript changing.
	metaPath := strings.TrimSuffix(path, ".jsonl") + ".meta.json"
	raw, _ := json.Marshal(map[string]any{"agentType": "Explore", "description": "audit again", "toolUseId": "task-1", "finished": true})
	if err := os.WriteFile(metaPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	readBytes = -1
	same("meta changed")
	if readBytes != -1 {
		t.Fatalf("a meta change read the transcript (%d bytes)", readBytes)
	}

	// Rewritten with other content (same file, not an append): read again.
	writeJSONL(t, path, claudeRecord("user", "sa-new", "2026-10-17T11:00:00.000Z", true, map[string]any{"type": "text", "text": "different"}), subagentToolCall(7))
	same("rewritten")
	// Shorter than before: read again.
	writeJSONL(t, path, claudeRecord("user", "sa-new2", "2026-10-17T11:00:00.000Z", true, map[string]any{"type": "text", "text": "x"}))
	same("truncated")
}

// Appending to the conversation reads none of its subagents' files again.
func TestAConversationAppendDoesNotReadItsSubagentsAgain(t *testing.T) {
	dir := t.TempDir()
	main := writeMainWithTask(t, dir, "task-1", false)
	for _, id := range []string{"a1", "a2", "a3"} {
		writeSubagent(t, dir, id, map[string]any{"agentType": "Explore", "description": id, "toolUseId": "task-" + id},
			claudeRecord("user", "sa-"+id, "2026-10-17T10:02:59.000Z", true, map[string]any{"type": "text", "text": "look"}), subagentToolCall(1))
	}
	reads := 0
	claudeSubagentObserve = func(string, int64) { reads++ }
	t.Cleanup(func() { claudeSubagentObserve = nil })
	if _, err := parseClaudeConversation(main); err != nil {
		t.Fatal(err)
	}
	if reads != 3 {
		t.Fatalf("first parse read %d subagent files, want 3", reads)
	}
	reads = 0
	for n := 0; n < 20; n++ {
		appendRaw(t, main, jsonLine(t, claudeRecord("assistant", "m-"+string(rune('a'+n)), "2026-10-17T10:05:00.000Z", false, map[string]any{"type": "text", "text": "more"})))
		if _, err := parseClaudeConversation(main); err != nil {
			t.Fatal(err)
		}
	}
	if reads != 0 {
		t.Fatalf("%d subagent files were read again after the conversation grew", reads)
	}
}
