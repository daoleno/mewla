package work

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// claudeGrowth writes a transcript a Worker keeps appending to.
type claudeGrowth struct {
	t       *testing.T
	path    string
	fx      claudeTailFixture
	pending []string
	calls   int
	// noLate keeps every tool result next to its call: a tail parse does not
	// join a result to a call from before its window, as the full parse does.
	noLate bool
}

func newClaudeGrowth(t *testing.T) *claudeGrowth {
	t.Helper()
	g := &claudeGrowth{t: t, path: filepath.Join(t.TempDir(), "sess.jsonl")}
	if err := os.WriteFile(g.path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return g
}

// turn appends a prompt, tool calls and a reply; some tool results arrive
// turns later than their call.
func (g *claudeGrowth) turn(n int) {
	prompt := g.fx.record("user", fmt.Sprintf("%q", fmt.Sprintf("prompt %d", n)), g.fx.parent)
	parent := prompt
	for i := 0; i < 1+n%3; i++ {
		g.calls++
		call := fmt.Sprintf("toolu_%d", g.calls)
		parent = g.fx.record("assistant", fmt.Sprintf(`[{"type":"tool_use","id":%q,"name":"Bash","input":{"command":"echo %d"}}]`, call, g.calls), parent)
		if g.calls%7 == 0 && !g.noLate {
			g.pending = append(g.pending, call)
			continue
		}
		parent = g.fx.record("user", fmt.Sprintf(`[{"type":"tool_result","tool_use_id":%q,"content":"out %d"}]`, call, g.calls), parent)
	}
	if len(g.pending) > 0 && n%5 == 0 {
		call := g.pending[0]
		g.pending = g.pending[1:]
		parent = g.fx.record("user", fmt.Sprintf(`[{"type":"tool_result","tool_use_id":%q,"content":"late"}]`, call), parent)
	}
	g.fx.parent = g.fx.record("assistant", fmt.Sprintf(`[{"type":"text","text":"reply %d"}]`, n), parent)
}

// flush appends the lines written since the last flush.
func (g *claudeGrowth) flush() {
	g.t.Helper()
	g.appendRaw(strings.Join(g.fx.lines, "\n") + "\n")
	g.fx.lines = nil
}

func (g *claudeGrowth) appendRaw(text string) {
	g.t.Helper()
	file, err := os.OpenFile(g.path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		g.t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		g.t.Fatal(err)
	}
}

type claudeIncrementalLog struct {
	reads    []int64
	reseeded []bool
}

func observeClaudeIncremental(t *testing.T) *claudeIncrementalLog {
	t.Helper()
	log := &claudeIncrementalLog{}
	claudeIncrementalObserve = func(readBytes int64, reseeded bool) {
		log.reads = append(log.reads, readBytes)
		log.reseeded = append(log.reseeded, reseeded)
	}
	t.Cleanup(func() { claudeIncrementalObserve = nil })
	return log
}

func requireIncrementalMatchesFull(t *testing.T, path string) {
	t.Helper()
	incremental, err := parseClaudeConversationIncremental(path)
	if err != nil {
		t.Fatalf("incremental: %v", err)
	}
	full, err := parseClaudeConversationFull(path)
	if err != nil {
		t.Fatalf("full: %v", err)
	}
	if !reflect.DeepEqual(incremental, full) {
		if !reflect.DeepEqual(incremental.Events, full.Events) {
			for i := range incremental.Events {
				if i < len(full.Events) && !reflect.DeepEqual(incremental.Events[i], full.Events[i]) {
					t.Fatalf("event %d differs:\n incremental=%+v\n full=%+v", i, incremental.Events[i], full.Events[i])
				}
			}
			t.Fatalf("events differ: %d vs %d", len(incremental.Events), len(full.Events))
		}
		t.Fatalf("conversation differs:\n incremental=%+v\n full=%+v", incremental.Activity, full.Activity)
	}
}

func TestClaudeIncrementalMatchesFullParseWhileTheTranscriptGrows(t *testing.T) {
	for _, tail := range []bool{false, true} {
		t.Run(fmt.Sprintf("tail=%v", tail), func(t *testing.T) {
			if tail {
				withSmallClaudeTail(t)
			}
			log := observeClaudeIncremental(t)
			g := newClaudeGrowth(t)
			random := rand.New(rand.NewSource(7))
			turn := 0
			// Far past the event window and the compaction threshold, so the
			// builder drops events and late tool results land on moved indexes.
			for step := 0; step < 120; step++ {
				for n := random.Intn(4) + 1; n > 0; n-- {
					g.turn(turn)
					turn++
				}
				g.flush()
				requireIncrementalMatchesFull(t, g.path)
			}
			seeds := 0
			for _, reseeded := range log.reseeded {
				if reseeded {
					seeds++
				}
			}
			if seeds != 1 {
				t.Fatalf("the transcript was parsed from the file %d times, want once: %v", seeds, log.reseeded)
			}
			info, _ := os.Stat(g.path)
			for i, read := range log.reads[1:] {
				if read > info.Size()/10 {
					t.Fatalf("advance %d read %d of %d bytes", i, read, info.Size())
				}
			}
		})
	}
}

func TestClaudeIncrementalWaitsForTheEndOfALine(t *testing.T) {
	log := observeClaudeIncremental(t)
	g := newClaudeGrowth(t)
	for n := 0; n < 3; n++ {
		g.turn(n)
	}
	g.flush()
	requireIncrementalMatchesFull(t, g.path)
	g.turn(3)
	whole := strings.Join(g.fx.lines, "\n") + "\n"
	g.fx.lines = nil
	cut := len(whole) - 40
	g.appendRaw(whole[:cut])
	partial, err := parseClaudeConversationIncremental(g.path)
	if err != nil {
		t.Fatal(err)
	}
	g.appendRaw(whole[cut:])
	requireIncrementalMatchesFull(t, g.path)
	done, err := parseClaudeConversationIncremental(g.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(done.Events) <= len(partial.Events) {
		t.Fatalf("the finished line added no event: %d then %d", len(partial.Events), len(done.Events))
	}
	for _, reseeded := range log.reseeded[1:] {
		if reseeded {
			t.Fatalf("a line written in two parts made the state start over: %v", log.reseeded)
		}
	}
}

func TestClaudeIncrementalTakesAWholeLastLineWithoutItsNewline(t *testing.T) {
	g := newClaudeGrowth(t)
	g.turn(0)
	g.turn(1)
	g.appendRaw(strings.Join(g.fx.lines, "\n")) // no final newline, like a hand-written fixture
	g.fx.lines = nil
	requireIncrementalMatchesFull(t, g.path)
	g.appendRaw("\n")
	requireIncrementalMatchesFull(t, g.path)
}

func TestClaudeIncrementalStartsOverForARewind(t *testing.T) {
	for _, tail := range []bool{false, true} {
		t.Run(fmt.Sprintf("tail=%v", tail), func(t *testing.T) {
			if tail {
				withSmallClaudeTail(t)
			}
			log := observeClaudeIncremental(t)
			g := newClaudeGrowth(t)
			g.noLate = tail
			for n := 0; n < 300; n++ {
				g.turn(n)
			}
			g.flush()
			requireIncrementalMatchesFull(t, g.path)
			// The person rewinds to the last prompt: a second prompt under the
			// same parent, and the old one with its reply is left behind.
			fork := g.fx.parent
			old := g.fx.record("user", `"abandoned prompt"`, fork)
			g.fx.record("assistant", `[{"type":"text","text":"abandoned reply"}]`, old)
			g.flush()
			requireIncrementalMatchesFull(t, g.path)
			g.fx.parent = fork
			g.turn(9000)
			g.flush()
			requireIncrementalMatchesFull(t, g.path)
			conversation, err := parseClaudeConversationIncremental(g.path)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range conversation.Events {
				if strings.Contains(event.Body, "abandoned") {
					t.Fatalf("the rewound branch shows: %+v", event)
				}
			}
			// Growth after the rewind is incremental again, and still drops what
			// is appended under the abandoned branch.
			before := len(log.reads)
			g.fx.parent = g.fx.record("assistant", `[{"type":"text","text":"later reply"}]`, g.fx.parent)
			g.flush()
			requireIncrementalMatchesFull(t, g.path)
			for _, reseeded := range log.reseeded[before:] {
				if reseeded {
					t.Fatalf("an append after the rewind made the state start over")
				}
			}
			g.fx.record("assistant", `[{"type":"text","text":"abandoned follow-up"}]`, old)
			g.flush()
			requireIncrementalMatchesFull(t, g.path)
		})
	}
}

func TestClaudeIncrementalStartsOverForAReplacedFile(t *testing.T) {
	g := newClaudeGrowth(t)
	for n := 0; n < 20; n++ {
		g.turn(n)
	}
	g.flush()
	requireIncrementalMatchesFull(t, g.path)
	// Same path, same inode, different content, longer than before.
	other := newClaudeGrowth(t)
	for n := 100; n < 140; n++ {
		other.turn(n)
	}
	other.flush()
	content, err := os.ReadFile(other.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(g.path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	requireIncrementalMatchesFull(t, g.path)
	// Shorter than the parsed part.
	if err := os.WriteFile(g.path, content[:len(content)/3], 0o600); err != nil {
		t.Fatal(err)
	}
	requireIncrementalMatchesFull(t, g.path)
	if err := os.Remove(g.path); err != nil {
		t.Fatal(err)
	}
	if _, err := parseClaudeConversationIncremental(g.path); err == nil {
		t.Fatal("a removed transcript parsed")
	}
}
