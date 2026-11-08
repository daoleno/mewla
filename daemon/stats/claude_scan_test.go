//go:build linux

package stats

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// streamingLines writes assistant records the way Claude Code does: one message
// is recorded several times as its output grows, and some records call tools.
func streamingLines(from, count int) string {
	var b strings.Builder
	for i := from; i < from+count; i++ {
		id := fmt.Sprintf("msg%d", i/3) // three records per message
		tool := ""
		if i%4 == 0 {
			tool = `,{"type":"tool_use","id":"t","name":"Bash","input":{}},{"type":"tool_use","id":"s","name":"Skill","input":{"skill":"review"}}`
		}
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":"2026-04-0%dT%02d:%02d:00.000Z","cwd":"/tmp/proj%d","message":{"id":%q,"model":"claude-sonnet-4-6","content":[{"type":"text","text":"%s"}%s],"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":7,"cache_creation_input_tokens":3}}}`+"\n",
			4+i%2, (i/60)%24, i%60, i%3, id, strings.Repeat("x", 200), tool, 10+i%3, 5+i%3*5)
	}
	return b.String()
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func scanFresh(path string) map[string]*dateAgg {
	byDate := map[string]*dateAgg{}
	NewCollector().scanSessionJSONL(path, "fallback", byDate)
	return roundedCosts(byDate)
}

func scanResumed(c *Collector, path string) map[string]*dateAgg {
	byDate := map[string]*dateAgg{}
	c.scanSessionJSONL(path, "fallback", byDate)
	return roundedCosts(byDate)
}

// roundedCosts drops what the order of a sum changes: the messages of a file
// are added up in map order, so the last digits of a cost differ run to run.
func roundedCosts(byDate map[string]*dateAgg) map[string]*dateAgg {
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		v = usageField(v)
		switch v.Kind() {
		case reflect.Float32, reflect.Float64:
			v.SetFloat(math.Round(v.Float()*1e9) / 1e9)
		case reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i))
			}
		case reflect.Array, reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			for _, key := range v.MapKeys() {
				element := reflect.New(v.Type().Elem()).Elem()
				element.Set(v.MapIndex(key))
				walk(element)
				v.SetMapIndex(key, element)
			}
		}
	}
	walk(reflect.ValueOf(&byDate).Elem())
	return byDate
}

// A growing file read on from where the scan stopped must add up to what a
// scan from the first line adds up to.
func TestClaudeScanGoingOnMatchesAScanFromTheStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	c := NewCollector()
	next := 0
	for step := 0; step < 30; step++ {
		n := 1 + step%7 // a message's records are split across reads
		appendTo(t, path, streamingLines(next, n))
		next += n
		if step == 12 { // a line still being written, finished by the next read
			half := streamingLines(next, 1)
			appendTo(t, path, half[:len(half)/2])
			if !reflect.DeepEqual(scanResumed(c, path), scanFresh(path)) {
				t.Fatalf("step %d: half a line changes the totals", step)
			}
			appendTo(t, path, half[len(half)/2:])
			next++
		}
		if got, want := scanResumed(c, path), scanFresh(path); !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d: resumed scan differs from a fresh one:\n%+v\n%+v", step, got, want)
		}
	}
}

func TestClaudeScanReadsOnlyWhatTheFileGained(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, path, streamingLines(0, 9000)) // several MB
	info, _ := os.Stat(path)
	c := NewCollector()
	scanResumed(c, path)
	appendTo(t, path, streamingLines(9000, 3))
	before := selfRchar(t)
	scanResumed(c, path)
	if read := selfRchar(t) - before; read > uint64(info.Size())/50 {
		t.Fatalf("a scan of a file that gained three lines read %d of %d bytes", read, info.Size())
	}
}

func TestClaudeScanStartsOverForAFileThatWasNotAppendedTo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	c := NewCollector()
	appendTo(t, path, streamingLines(0, 300))
	scanResumed(c, path)

	// Replaced by another file under the same name, longer than before.
	other := filepath.Join(dir, "other.jsonl")
	appendTo(t, other, streamingLines(1000, 600))
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	if got, want := scanResumed(c, path), scanFresh(path); !reflect.DeepEqual(got, want) {
		t.Fatal("a replaced file was read on from the old offset")
	}
	// Rewritten in place, longer than before and different in the part read.
	if err := os.WriteFile(path, []byte(streamingLines(5000, 900)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := scanResumed(c, path), scanFresh(path); !reflect.DeepEqual(got, want) {
		t.Fatal("a rewritten file was read on from the old offset")
	}
	// Shorter than the part read.
	if err := os.WriteFile(path, []byte(streamingLines(0, 50)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := scanResumed(c, path), scanFresh(path); !reflect.DeepEqual(got, want) {
		t.Fatal("a truncated file was read on from the old offset")
	}
}

func TestClaudeScansKeptAreBoundedAndForgetGoneFiles(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-tmp-mewla")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < claudeScansKept+5; i++ {
		appendTo(t, filepath.Join(dir, fmt.Sprintf("f%d.jsonl", i)), streamingLines(i*10, 5))
	}
	c := NewCollector()
	c.usageCacheDir = filepath.Join(t.TempDir(), "usage")
	c.collectClaudeSessionStats(home)
	if len(c.claudeScans) != claudeScansKept {
		t.Fatalf("%d scans kept, want %d", len(c.claudeScans), claudeScansKept)
	}
	for path := range c.claudeScans {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	c.collectClaudeSessionStats(home)
	for path := range c.claudeScans {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("a scan of a removed file is kept: %s", path)
		}
	}
}
