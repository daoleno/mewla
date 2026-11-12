//go:build linux || darwin

package sessionhost

import (
	"strings"
	"testing"
	"time"
)

// viewer reads an attach stream into one string.
type viewer struct {
	t   *testing.T
	a   *Attachment
	got strings.Builder
}

func attachView(t *testing.T, c *Client, params AttachParams) *viewer {
	t.Helper()
	a, err := c.Attach(ctxT(t), params)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return &viewer{t: t, a: a}
}

// until reads output until it contains want.
func (v *viewer) until(want string) {
	v.t.Helper()
	deadline := time.After(10 * time.Second)
	for !strings.Contains(v.got.String(), want) {
		select {
		case chunk, ok := <-v.a.Output():
			if !ok {
				end, err := v.a.End()
				v.t.Fatalf("stream ended (%+v, %v) before %q; got %q", end, err, want, v.got.String())
			}
			v.got.Write(chunk)
		case <-deadline:
			v.t.Fatalf("timed out waiting for %q; got %q", want, v.got.String())
		}
	}
}

// end reads the stream to its end.
func (v *viewer) end() AttachEnd {
	v.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case chunk, ok := <-v.a.Output():
			if !ok {
				end, err := v.a.End()
				if err != nil {
					v.t.Fatalf("stream was cut: %v", err)
				}
				return end
			}
			v.got.Write(chunk)
		case <-deadline:
			v.t.Fatalf("timed out waiting for the stream to end; got %q", v.got.String())
		}
	}
}

func TestAttachReplayAndOutputAddUp(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	dir := shortTempDir(t)
	script := `printf 'before-'; read x; printf 'mid\033[c\033]11;?\007after-'; read y; printf 'end'; sleep 30`
	if _, err := c.Create(ctxT(t), shellSession("view", dir, script)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "view", "before-")
	v := attachView(t, c, AttachParams{ID: "view"})
	if !strings.Contains(string(v.a.Replay), "before-") || v.a.OutputSeq != uint64(len(v.a.Replay)) {
		t.Fatalf("replay %q at output %d, want all of the output so far", v.a.Replay, v.a.OutputSeq)
	}
	if _, err := c.Input(ctxT(t), "view", []InputStep{{Bytes: []byte("go\r")}}); err != nil {
		t.Fatal(err)
	}
	v.until("after-")
	if _, err := c.Input(ctxT(t), "view", []InputStep{{Bytes: []byte("go\r")}}); err != nil {
		t.Fatal(err)
	}
	v.until("end")

	all := screenText(t, c, "view")
	want := strings.NewReplacer("\x1b[c", "", "\x1b]11;?\x07", "").Replace(all)
	if got := string(v.a.Replay) + v.got.String(); got != want {
		t.Fatalf("replay and output = %q, want %q without the queries", got, want)
	}
	if !strings.Contains(all, "\x1b[c") {
		t.Fatalf("the program's query never reached the host's terminal: %q", all)
	}
}

func TestAttachSetsTheSizeFirst(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	dir := shortTempDir(t)
	if _, err := c.Create(ctxT(t), shellSession("size", dir, `printf ready; read x; stty size; sleep 30`)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "size", "ready")
	v := attachView(t, c, AttachParams{ID: "size", Cols: 100, Rows: 30})
	if v.a.Cols != 100 || v.a.Rows != 30 {
		t.Fatalf("attached at %dx%d, want 100x30", v.a.Cols, v.a.Rows)
	}
	if _, err := c.Input(ctxT(t), "size", []InputStep{{Bytes: []byte("\r")}}); err != nil {
		t.Fatal(err)
	}
	v.until("30 100")

	// A viewer that only watches leaves the size alone.
	watcher := attachView(t, c, AttachParams{ID: "size"})
	if watcher.a.Cols != 100 || watcher.a.Rows != 30 {
		t.Fatalf("a watching viewer changed the size to %dx%d", watcher.a.Cols, watcher.a.Rows)
	}
	if _, err := c.Attach(ctxT(t), AttachParams{ID: "size", Cols: 5000, Rows: 30}); err == nil {
		t.Fatal("attach at 5000 columns succeeded")
	}
}

// A viewer whose terminal stays one size while another viewer resizes the
// Session draws the program's output for the other size wrongly (rows written
// over rows) until it starts over: one that asked to redraw is ended, and its
// passive attach again replays at the new size without taking the size back.
func TestResizeEndsTheViewersThatRedraw(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	dir := shortTempDir(t)
	if _, err := c.Create(ctxT(t), shellSession("two", dir, `printf ready; while read x; do printf "line-$x "; done`)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "two", "ready")
	say := func(x string) {
		t.Helper()
		if _, err := c.Input(ctxT(t), "two", []InputStep{{Bytes: []byte(x + "\r")}}); err != nil {
			t.Fatal(err)
		}
	}
	phone := attachView(t, c, AttachParams{ID: "two", Cols: 80, Rows: 24, Redraw: true})
	older := attachView(t, c, AttachParams{ID: "two"}) // a viewer from before Redraw

	// The same size again changes nothing.
	if err := c.Resize(ctxT(t), "two", 80, 24); err != nil {
		t.Fatal(err)
	}
	say("a")
	phone.until("line-a")
	older.until("line-a")

	// Another viewer, another size: the phone is ended; the older viewer keeps its stream.
	desk := attachView(t, c, AttachParams{ID: "two", Cols: 160, Rows: 45, Redraw: true})
	if end := phone.end(); end.Reason != AttachResized {
		t.Fatalf("phone ended with %+v, want %q", end, AttachResized)
	}
	say("b")
	older.until("line-b")
	desk.until("line-b")

	// Attaching again without a size replays at the Session's size and leaves it there.
	again := attachView(t, c, AttachParams{ID: "two", Redraw: true})
	if again.a.Cols != 160 || again.a.Rows != 45 || !strings.Contains(string(again.a.Replay), "line-b") {
		t.Fatalf("passive attach at %dx%d with replay %q, want 160x45 with the output so far", again.a.Cols, again.a.Rows, again.a.Replay)
	}
	say("c")
	desk.until("line-c")
	again.until("line-c")

	// A resize that is no viewer's own (the Resize method) ends every viewer that redraws.
	if err := c.Resize(ctxT(t), "two", 80, 24); err != nil {
		t.Fatal(err)
	}
	if end := desk.end(); end.Reason != AttachResized {
		t.Fatalf("desk ended with %+v, want %q", end, AttachResized)
	}
	if end := again.end(); end.Reason != AttachResized {
		t.Fatalf("passive viewer ended with %+v, want %q", end, AttachResized)
	}
	say("d")
	older.until("line-d")
}

func TestAttachEndsWhenTheProgramExits(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	dir := shortTempDir(t)
	if _, err := c.Create(ctxT(t), shellSession("bye", dir, `printf 'bye-'; read x; printf 'last'; exit 3`)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "bye", "bye-")
	v := attachView(t, c, AttachParams{ID: "bye"})
	if _, err := c.Input(ctxT(t), "bye", []InputStep{{Bytes: []byte("\r")}}); err != nil {
		t.Fatal(err)
	}
	end := v.end()
	if end.Reason != AttachExited || end.ExitCode == nil || *end.ExitCode != 3 {
		t.Fatalf("end = %+v, want exited with 3", end)
	}
	if !strings.Contains(v.got.String(), "last") {
		t.Fatalf("the last output %q did not come before the end", v.got.String())
	}

	// A viewer of an exited program gets the final screen and the end.
	late := attachView(t, c, AttachParams{ID: "bye"})
	if !strings.Contains(string(late.a.Replay), "last") {
		t.Fatalf("late replay = %q", late.a.Replay)
	}
	if end := late.end(); end.Reason != AttachExited || late.got.Len() != 0 {
		t.Fatalf("late viewer: end %+v, output %q", end, late.got.String())
	}
}

func TestAttachEndsWhenRemoved(t *testing.T) {
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	dir := shortTempDir(t)
	if _, err := c.Create(ctxT(t), shellSession("gone", dir, `printf here; sleep 30`)); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, c, "gone", "here")
	v := attachView(t, c, AttachParams{ID: "gone"})
	if _, err := c.Remove(ctxT(t), "gone"); err != nil {
		t.Fatal(err)
	}
	if end := v.end(); end.Reason != AttachRemoved {
		t.Fatalf("end = %+v, want removed", end)
	}
	if _, err := c.Attach(ctxT(t), AttachParams{ID: "gone"}); !isHostError(err) {
		t.Fatalf("attach to a removed session: %v", err)
	}
}

func TestSlowViewerIsEndedNotWaitedFor(t *testing.T) {
	defer func(n int) { tapBuffer = n }(tapBuffer)
	tapBuffer = 4
	h := startHost(t, ServerOptions{})
	c := dialHost(t, h)
	dir := shortTempDir(t)
	script := `read x; head -c 8388608 /dev/zero | tr '\0' 'x'; printf FLOOD-DONE; sleep 30`
	if _, err := c.Create(ctxT(t), shellSession("flood", dir, script)); err != nil {
		t.Fatal(err)
	}
	v := attachView(t, c, AttachParams{ID: "flood"})
	if _, err := c.Input(ctxT(t), "flood", []InputStep{{Bytes: []byte("\r")}}); err != nil {
		t.Fatal(err)
	}
	// Nothing reads the viewer's stream, and the program still finishes.
	waitScreen(t, c, "flood", "FLOOD-DONE")
	if end := v.end(); end.Reason != AttachBehind {
		t.Fatalf("end = %+v, want behind", end)
	}
	again := attachView(t, c, AttachParams{ID: "flood"})
	if !strings.HasSuffix(string(again.a.Replay), "FLOOD-DONE") {
		t.Fatalf("a fresh replay does not show the end of the output")
	}
}
