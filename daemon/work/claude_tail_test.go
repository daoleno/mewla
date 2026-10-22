package work

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type claudeTailFixture struct {
	lines  []string
	parent string
	next   int
}

func (f *claudeTailFixture) record(kind, content, parent string) string {
	f.next++
	uuid := fmt.Sprintf("u%06d", f.next)
	pad := strings.Repeat("x", 600)
	stopReason := ""
	if kind == "assistant" {
		stopReason = "end_turn"
	}
	f.lines = append(f.lines, fmt.Sprintf(
		`{"type":%q,"uuid":%q,"parentUuid":%q,"sessionId":"sess","cwd":"/work","timestamp":"2026-10-23T00:%02d:00Z","message":{"role":%q,"content":%s,"stop_reason":%q},"pad":%q}`,
		kind, uuid, parent, f.next%60, kind, content, stopReason, pad))
	return uuid
}

// turn appends one prompt and its reply on the live branch.
func (f *claudeTailFixture) turn(n int) (prompt string) {
	prompt = f.record("user", fmt.Sprintf("%q", fmt.Sprintf("prompt %d", n)), f.parent)
	f.parent = f.record("assistant", fmt.Sprintf(`[{"type":"text","text":"reply %d"}]`, n), prompt)
	return prompt
}

func (f *claudeTailFixture) write(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sess.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(f.lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func withSmallClaudeTail(t *testing.T) {
	t.Helper()
	minBytes, window := claudeTailMinBytes, claudeTailInitialWindow
	claudeTailMinBytes, claudeTailInitialWindow = 1<<10, 16<<10
	t.Cleanup(func() { claudeTailMinBytes, claudeTailInitialWindow = minBytes, window })
}

func requireTailMatchesFull(t *testing.T, path string) (parsedLines int) {
	t.Helper()
	claudeTailObserve = func(lines int) { parsedLines = lines }
	t.Cleanup(func() { claudeTailObserve = nil })
	tail, ok, err := parseClaudeConversationTail(path)
	if err != nil || !ok {
		t.Fatalf("tail parse ok=%v err=%v", ok, err)
	}
	full, err := parseClaudeConversationFull(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tail, full) {
		if !reflect.DeepEqual(tail.Events, full.Events) {
			t.Fatalf("tail parse events differ from full parse")
		}
		t.Fatalf("tail parse differs from full parse:\n tail=%+v\n full=%+v", tail.Activity, full.Activity)
	}
	if len(tail.Events) != maxCodexConversationEvents {
		t.Fatalf("events = %d, want %d", len(tail.Events), maxCodexConversationEvents)
	}
	return parsedLines
}

func TestClaudeTailParseMatchesFullParseAndStaysBounded(t *testing.T) {
	withSmallClaudeTail(t)
	small, large := &claudeTailFixture{}, &claudeTailFixture{}
	for n := 0; n < 400; n++ {
		small.turn(n)
	}
	for n := 0; n < 4000; n++ {
		large.turn(n)
	}
	smallLines := requireTailMatchesFull(t, small.write(t))
	largeLines := requireTailMatchesFull(t, large.write(t))
	if largeLines != smallLines {
		t.Fatalf("a 10x larger transcript parsed %d lines, want the same %d", largeLines, smallLines)
	}
	if largeLines > 2000 {
		t.Fatalf("parsed %d lines of an 8000-line transcript", largeLines)
	}
}

func TestClaudeTailParseDropsRewoundBranchInsideWindow(t *testing.T) {
	withSmallClaudeTail(t)
	f := &claudeTailFixture{}
	for n := 0; n < 600; n++ {
		f.turn(n)
	}
	// Rewind: the later prompt is a second child of the same parent.
	fork := f.parent
	old := f.record("user", `"abandoned prompt"`, fork)
	f.record("assistant", `[{"type":"text","text":"abandoned reply"}]`, old)
	f.parent = fork
	f.turn(9000)
	requireTailMatchesFull(t, f.write(t))
	conversation, _, _ := parseClaudeConversationTail(f.write(t))
	for _, event := range conversation.Events {
		if strings.Contains(event.Body, "abandoned") {
			t.Fatalf("rewound branch shows: %+v", event)
		}
	}
}

func TestClaudeTailParseNeverSplitsARewoundBranch(t *testing.T) {
	withSmallClaudeTail(t)
	f := &claudeTailFixture{}
	for n := 0; n < 100; n++ {
		f.turn(n)
	}
	// The rewound branch outlives the window's first guess: its replies run
	// long before the live prompt that replaces its first prompt arrives.
	fork := f.parent
	old := f.record("user", `"abandoned prompt"`, fork)
	branch := old
	for n := 0; n < 500; n++ {
		branch = f.record("assistant", fmt.Sprintf(`[{"type":"text","text":"abandoned reply %d"}]`, n), branch)
	}
	f.parent = fork
	for n := 0; n < 300; n++ {
		f.turn(1000 + n)
	}
	path := f.write(t)
	tail, ok, err := parseClaudeConversationTail(path)
	full, fullErr := parseClaudeConversationFull(path)
	if err != nil || fullErr != nil {
		t.Fatal(err, fullErr)
	}
	if ok && !reflect.DeepEqual(tail, full) {
		t.Fatal("tail parse answered differently from the full parse")
	}
}

func TestClaudeTailParseKeepsSeqOfBlankLineTranscripts(t *testing.T) {
	withSmallClaudeTail(t)
	f := &claudeTailFixture{}
	for n := 0; n < 400; n++ {
		f.turn(n)
		if n%7 == 0 {
			f.lines = append(f.lines, "")
		}
	}
	requireTailMatchesFull(t, f.write(t))
}

func TestClaudeTailParseSkipsSmallTranscripts(t *testing.T) {
	f := &claudeTailFixture{}
	for n := 0; n < 400; n++ {
		f.turn(n)
	}
	if _, ok, err := parseClaudeConversationTail(f.write(t)); ok || err != nil {
		t.Fatalf("small transcript ok=%v err=%v, want the full parse", ok, err)
	}
}
