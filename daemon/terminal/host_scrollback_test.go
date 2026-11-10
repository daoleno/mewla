//go:build ghosttyvt && cgo && (linux || darwin)

package terminal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/sessionhost"
)

// A viewer asks for what it is missing: the whole scrollback the first time,
// the new rows after, the modes with every answer, and rows marked by what
// the program emitted.
func TestHostScrollbackDeltas(t *testing.T) {
	socket, client := startTestHost(t)
	dir := t.TempDir()
	const id = "mh-0123456789abcdef0a0b0c0d"
	script := `printf '\033]133;A\007$ first\r\n'; printf '%s\r\n' "$(printf 'w%.0s' $(seq 1 150))"; ` +
		`i=0; while [ $i -lt 40 ]; do printf 'line %d\r\n' $i; i=$((i+1)); done; printf 'ready '; read x; ` +
		`i=40; while [ $i -lt 70 ]; do printf 'line %d\r\n' $i; i=$((i+1)); done; printf 'more '; read y; ` +
		`printf '\033[?1049h\033[?1007l\033[?1h'; printf 'alt '; read z; printf '\033[?1049l'; printf 'back\r\n'; read w`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := client.Create(ctx, sessionhost.CreateParams{
		ID: id, Argv: []string{"/bin/sh", "-c", script}, Env: []string{"PATH=/usr/bin:/bin", "HOME=" + dir}, Dir: dir, Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatal(err)
	}
	waitHostScreen(t, client, id, "ready")

	sent := make(chan map[string]any, 4096)
	manager := NewManager(&HostBackend{Version: "test-daemon"})
	view, err := manager.Open("owner", HostBackendName, id, OpenOptions{Cols: 70, Rows: 20, Socket: socket}, func(v any) { sent <- v.(map[string]any) })
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range sent {
		}
	}()
	time.Sleep(300 * time.Millisecond)

	first, ok, err := manager.HistoryFrom("owner", view.ID(), HistoryCursor{})
	if err != nil || !ok {
		t.Fatalf("HistoryFrom: %v %v", ok, err)
	}
	if !first.Reset || first.From != first.First || first.First != 0 || first.Next != uint64(len(first.Lines)) || len(first.Lines) < 20 {
		t.Fatalf("first answer: reset %v first %d from %d next %d lines %d", first.Reset, first.First, first.From, first.Next, len(first.Lines))
	}
	if first.Epoch == 0 || first.Cols != 70 || first.Alternate || first.AltScroll == nil || !*first.AltScroll || first.AppCursor == nil || *first.AppCursor {
		t.Fatalf("modes: %+v", first)
	}
	if len(first.Flags) != len(first.Lines) {
		t.Fatalf("flags %d for %d lines", len(first.Flags), len(first.Lines))
	}
	if first.Flags[0]&RowPrompt == 0 || !strings.Contains(first.Lines[0], "$ first") {
		t.Fatalf("row 0 = %q flags %b, want the marked prompt", first.Lines[0], first.Flags[0])
	}
	wrapped := -1
	for i, line := range first.Lines {
		if strings.Contains(line, "wwww") && first.Flags[i]&RowWrapped != 0 {
			wrapped = i
		}
	}
	if wrapped < 0 || first.Flags[wrapped+1]&RowContinuation == 0 {
		t.Fatalf("the 150-column line is not marked wrapped then continued (row %d)", wrapped)
	}

	// Nothing new: an empty delta that keeps the cursor.
	cursor := HistoryCursor{Epoch: first.Epoch, Next: first.Next}
	idle, _, err := manager.HistoryFrom("owner", view.ID(), cursor)
	if err != nil || idle.Reset || len(idle.Lines) != 0 || idle.Next != first.Next || idle.Epoch != first.Epoch {
		t.Fatalf("idle answer: %+v %v", idle, err)
	}

	if err := manager.Input("owner", view.ID(), "go\r"); err != nil {
		t.Fatal(err)
	}
	waitHostScreen(t, client, id, "more")
	more, _, err := manager.HistoryFrom("owner", view.ID(), cursor)
	if err != nil || more.Reset || more.From != first.Next || len(more.Lines) == 0 || more.Next != more.From+uint64(len(more.Lines)) {
		t.Fatalf("delta: %+v %v", more, err)
	}
	if !strings.Contains(strings.Join(more.Lines, "\n"), "line 5") {
		t.Fatalf("delta rows: %q", more.Lines)
	}
	if len(more.Flags) != len(more.Lines) {
		t.Fatalf("delta flags %d for %d lines", len(more.Flags), len(more.Lines))
	}

	// A cursor from another epoch starts over.
	stale, _, err := manager.HistoryFrom("owner", view.ID(), HistoryCursor{Epoch: first.Epoch + 99, Next: 5})
	if err != nil || !stale.Reset || stale.From != stale.First || uint64(len(stale.Lines)) != stale.Next-stale.First {
		t.Fatalf("stale cursor answer: reset %v from %d first %d lines %d next %d", stale.Reset, stale.From, stale.First, len(stale.Lines), stale.Next)
	}

	// The alternate screen: no rows, its modes, and the viewer keeps its place.
	cursor = HistoryCursor{Epoch: more.Epoch, Next: more.Next}
	if err := manager.Input("owner", view.ID(), "go\r"); err != nil {
		t.Fatal(err)
	}
	waitHostScreen(t, client, id, "alt")
	alt, _, err := manager.HistoryFrom("owner", view.ID(), cursor)
	if err != nil || !alt.Alternate || alt.Reset || len(alt.Lines) != 0 || alt.Next != cursor.Next || alt.AltScroll == nil || *alt.AltScroll || alt.AppCursor == nil || !*alt.AppCursor {
		t.Fatalf("alternate screen answer: %+v %v", alt, err)
	}
	if err := manager.Input("owner", view.ID(), "go\r"); err != nil {
		t.Fatal(err)
	}
	waitHostScreen(t, client, id, "back")
	back, _, err := manager.HistoryFrom("owner", view.ID(), cursor)
	if err != nil || back.Alternate || back.Reset || back.From != cursor.Next || back.Next < cursor.Next {
		t.Fatalf("after the alternate screen: %+v %v", back, err)
	}
}
