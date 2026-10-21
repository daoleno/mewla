package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/control"
)

func TestParseMemoryTime(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in    string
		end   bool
		want  time.Time
		isErr bool
	}{
		{"", false, time.Time{}, false},
		{"2026-10-05", false, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), false},
		{"2026-10-05", true, time.Date(2026, 10, 5, 23, 59, 59, 999999999, time.UTC), false},
		{"2026-10-05T17:50:12.174Z", false, time.Date(2026, 10, 5, 17, 50, 12, 174000000, time.UTC), false},
		{"36h", false, now.Add(-36 * time.Hour), false},
		{"30d", false, now.Add(-30 * 24 * time.Hour), false},
		{"2w", false, now.Add(-14 * 24 * time.Hour), false},
		{"yesterday", false, time.Time{}, true},
		{"d", false, time.Time{}, true},
	}
	for _, tc := range cases {
		got, err := parseMemoryTime(tc.in, now, tc.end)
		if (err != nil) != tc.isErr || !got.Equal(tc.want) {
			t.Errorf("parseMemoryTime(%q, end=%v) = %v, %v; want %v (err=%v)", tc.in, tc.end, got, err, tc.want, tc.isErr)
		}
	}
}

func TestParseInterspersedAcceptsFlagsAfterQuery(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 20, "")
	from := fs.String("from", "all", "")
	positional, err := parseInterspersed(fs, []string{"two", "words", "--limit", "5", "--from", "user"})
	if err != nil || strings.Join(positional, " ") != "two words" || *limit != 5 || *from != "user" {
		t.Fatalf("positional=%v limit=%d from=%s err=%v", positional, *limit, *from, err)
	}
}

func TestHandleMemorySearchAndShow(t *testing.T) {
	root := t.TempDir()
	store, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	rows := `{"id":"u1","session_id":"s","role":"user","kind":"user_message","body":"不要用写死的规则去优化","created_at":"2026-10-05T17:55:56Z"}
{"id":"b1","session_id":"s","role":"assistant","kind":"assistant_message","body":"那条我撤回","created_at":"2026-10-05T17:56:08Z"}
`
	if err := os.WriteFile(filepath.Join(store.StateDir(), "messages.jsonl"), []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &controlApp{brainStore: store}

	resp := app.handleMemory(control.Request{Type: "memory_search", Memory: &control.MemoryRequest{Query: "写死的规则", From: "user"}})
	if !resp.OK || len(resp.MemoryHits) != 1 || resp.MemoryHits[0].ID != "u1" {
		t.Fatalf("search = %+v", resp)
	}
	resp = app.handleMemory(control.Request{Type: "memory_show", Memory: &control.MemoryRequest{ID: "u1", Around: 1}})
	if !resp.OK || len(resp.MemoryMessages) != 2 || !resp.MemoryMessages[0].Match {
		t.Fatalf("show = %+v", resp)
	}
	resp = app.handleMemory(control.Request{Type: "memory_show", Memory: &control.MemoryRequest{ID: "missing"}})
	if resp.OK || resp.Error == nil || resp.Error.Code != "memory_not_found" {
		t.Fatalf("missing id = %+v", resp)
	}
	resp = app.handleMemory(control.Request{Type: "memory_search", Memory: &control.MemoryRequest{Query: "x", From: "robot"}})
	if resp.OK || resp.Error == nil {
		t.Fatalf("bad --from = %+v", resp)
	}
	if info, err := os.Stat(filepath.Join(store.StateDir(), memoryIndexName)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("index file: %v %v", info, err)
	}
}
