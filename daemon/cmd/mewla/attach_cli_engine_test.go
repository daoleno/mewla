//go:build ghosttyvt && cgo && (linux || darwin)

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
	"github.com/daoleno/mewla/daemon/sessionhost"
)

func attachTestHost(t *testing.T) *sessionhost.Client {
	t.Helper()
	_, client := attachTestHostSocket(t)
	return client
}

func attachTestHostSocket(t *testing.T) (string, *sessionhost.Client) {
	t.Helper()
	dir, err := os.MkdirTemp("", "at")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "h.sock")
	server := sessionhost.NewServer(sessionhost.ServerOptions{SocketPath: socket, HostVersion: "test", Logf: t.Logf})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = server.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	var client *sessionhost.Client
	deadline := time.Now().Add(5 * time.Second)
	for client == nil {
		client, err = sessionhost.Dial(ctx, socket, "test")
		if err != nil {
			if time.Now().After(deadline) {
				t.Fatal(err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	t.Cleanup(func() { client.Close() })
	return socket, client
}

// olderHostSocket serves a socket in front of the host at socket that acts as
// a host from before AttachResized: an attach request loses its "redraw", so
// the host neither ends the view on another viewer's resize nor says it would.
func olderHostSocket(t *testing.T, socket string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "oh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "o.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			down, err := listener.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("unix", socket)
			if err != nil {
				down.Close()
				continue
			}
			go func() { io.Copy(down, up); down.Close() }()
			go func() {
				defer up.Close()
				for {
					var header [4]byte
					if _, err := io.ReadFull(down, header[:]); err != nil {
						return
					}
					body := make([]byte, binary.BigEndian.Uint32(header[:]))
					if _, err := io.ReadFull(down, body); err != nil {
						return
					}
					body = bytes.Replace(body, []byte(`,"redraw":true`), nil, 1)
					binary.BigEndian.PutUint32(header[:], uint32(len(body)))
					if _, err := up.Write(append(header[:], body...)); err != nil {
						return
					}
				}
			}()
		}
	}()
	return path
}

func createAttachSession(t *testing.T, client *sessionhost.Client, id, script string) {
	t.Helper()
	dir := t.TempDir()
	if _, err := client.Create(context.Background(), sessionhost.CreateParams{
		ID: id, Argv: []string{"/bin/sh", "-c", script}, Env: []string{"PATH=/usr/bin:/bin", "HOME=" + dir}, Dir: dir, Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAttachShowsTypesAndDetaches(t *testing.T) {
	client := attachTestHost(t)
	createAttachSession(t, client, "mh-attach", `stty -echo; printf 'ready\r\n'; while read line; do printf 'got %s\r\n' "$line"; done`)
	in, typing := io.Pipe()
	out := &lockedBuffer{}
	resized := make(chan os.Signal, 1)
	cols := 70
	result := make(chan string, 1)
	go func() {
		outcome, err := attachLoop(context.Background(), client, "mh-attach", in, out, func() (int, int) { return cols, 20 }, resized)
		if err != nil {
			outcome = "error: " + err.Error()
		}
		result <- outcome
	}()
	waitOutput(t, out, "ready")
	if !strings.HasPrefix(out.String(), enterAttach) {
		t.Fatalf("attach did not take a screen of its own: %q", out.String())
	}
	typing.Write([]byte("hello\r"))
	waitOutput(t, out, "got hello")

	// The terminal's size becomes the Session's, now and on resize.
	info, err := client.Info(context.Background(), "mh-attach")
	if err != nil || info.Cols != 70 || info.Rows != 20 {
		t.Fatalf("info = %+v, %v; want 70x20", info, err)
	}
	cols = 90
	resized <- os.Interrupt
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, _ = client.Info(context.Background(), "mh-attach")
		if info.Cols == 90 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("resize never reached the Session: %+v", info)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Ctrl-\ detaches; what came before it in the same read is still sent.
	typing.Write([]byte("bye\r\x1cafter"))
	select {
	case outcome := <-result:
		if outcome != "[detached from mh-attach]" {
			t.Fatalf("outcome = %q", outcome)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Ctrl-\\ did not detach")
	}
	if !strings.HasSuffix(out.String(), leaveAttach) {
		t.Fatalf("detach did not give the screen back: %q", out.String())
	}
	// The Session answers what was typed before the detach a moment later.
	var capture sessionhost.Capture
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if capture, err = client.Capture(context.Background(), sessionhost.CaptureParams{ID: "mh-attach", Scope: sessionhost.ScopeScreen}); err == nil && strings.Contains(capture.Text, "got bye") {
			break
		}
	}
	if err != nil || !strings.Contains(capture.Text, "got bye") || strings.Contains(capture.Text, "after") {
		t.Fatalf("after detach the Session shows %q (%v)", capture.Text, err)
	}
	if info, err := client.Info(context.Background(), "mh-attach"); err != nil || !info.Alive {
		t.Fatalf("detaching ended the Session: %+v, %v", info, err)
	}
}

// Another viewer resizes the Session: this terminal starts its screen over at
// the new size instead of drawing output written for another width, does not
// take the size back on its own (two viewers would take it from each other
// forever), and takes it back when its person types.
func TestAttachRedrawsWhenAnotherViewerResizes(t *testing.T) {
	client := attachTestHost(t)
	createAttachSession(t, client, "mh-two", `stty -echo; printf 'ready\r\n'; while read line; do printf 'got %s\r\n' "$line"; done`)
	in, typing := io.Pipe()
	defer typing.Close()
	out := &lockedBuffer{}
	go attachLoop(context.Background(), client, "mh-two", in, out, func() (int, int) { return 70, 20 }, make(chan os.Signal))
	waitOutput(t, out, "ready")
	redraws := strings.Count(out.String(), redraw)

	phone, err := client.Attach(context.Background(), sessionhost.AttachParams{ID: "mh-two", Cols: 50, Rows: 30, Redraw: true})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	deadline := time.Now().Add(5 * time.Second)
	for strings.Count(out.String(), redraw) == redraws {
		if time.Now().After(deadline) {
			t.Fatalf("this terminal was not drawn again after another viewer resized the Session: %q", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if info, err := client.Info(context.Background(), "mh-two"); err != nil || info.Cols != 50 || info.Rows != 30 {
		t.Fatalf("info = %+v, %v; the other viewer's 50x30 should stay until this person acts", info, err)
	}

	typing.Write([]byte("hi\r"))
	waitOutput(t, out, "got hi")
	if info, err := client.Info(context.Background(), "mh-two"); err != nil || info.Cols != 70 || info.Rows != 20 {
		t.Fatalf("info = %+v, %v; typing here should take the size back to 70x20", info, err)
	}
}

// Every Session open before an upgrade runs on the older host, which never
// ends this terminal's view for another viewer's resize: the terminal asks
// the Session's size and starts its screen over just the same, without taking
// the size back until its person types.
func TestAttachOnAnOlderHostRedrawsToo(t *testing.T) {
	socket, client := attachTestHostSocket(t)
	createAttachSession(t, client, "mh-old", `stty -echo; printf 'ready\r\n'; while read line; do printf 'got %s\r\n' "$line"; done`)
	older, err := sessionhost.Dial(context.Background(), olderHostSocket(t, socket), "test")
	if err != nil {
		t.Fatal(err)
	}
	defer older.Close()
	in, typing := io.Pipe()
	defer typing.Close()
	out := &lockedBuffer{}
	go attachLoop(context.Background(), older, "mh-old", in, out, func() (int, int) { return 70, 20 }, make(chan os.Signal))
	waitOutput(t, out, "ready")
	redraws := strings.Count(out.String(), redraw)

	phone, err := client.Attach(context.Background(), sessionhost.AttachParams{ID: "mh-old", Cols: 50, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	deadline := time.Now().Add(5 * time.Second)
	for strings.Count(out.String(), redraw) == redraws {
		if time.Now().After(deadline) {
			t.Fatalf("on an older host this terminal was not drawn again after another viewer resized the Session: %q", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(3 * attachSizePoll)
	if info, err := client.Info(context.Background(), "mh-old"); err != nil || info.Cols != 50 || info.Rows != 30 {
		t.Fatalf("info = %+v, %v; the other viewer's 50x30 should stay until this person acts", info, err)
	}
	// Once over (the screen cleared, then mirrored at the other size), not again and again.
	if n := strings.Count(out.String(), redraw); n != redraws+2 {
		t.Fatalf("cleared %d times for one resize", n-redraws)
	}

	typing.Write([]byte("hi\r"))
	waitOutput(t, out, "got hi")
	if info, err := client.Info(context.Background(), "mh-old"); err != nil || info.Cols != 70 || info.Rows != 20 {
		t.Fatalf("info = %+v, %v; typing here should take the size back to 70x20", info, err)
	}
	mark := strings.Count(out.String(), redraw)
	time.Sleep(3 * attachSizePoll)
	if n := strings.Count(out.String(), redraw); n != mark {
		t.Fatalf("drawn again %d times at its own size", n-mark)
	}
}

// sizeRows is a program that draws its whole screen whenever its size
// changes: one numbered row per line, each exactly as wide as the screen,
// and ready-<cols> on the last.
const sizeRows = `last=""; while :; do s=$(stty size); if [ "$s" != "$last" ]; then last=$s; r=${s% *}; c=${s#* }
printf '\033[H\033[2J'; i=1; while [ $i -lt $r ]; do printf 'row%02d-' $i; head -c $((c-6)) /dev/zero | tr '\0' x; printf '\r\n'; i=$((i+1)); done; printf 'ready-%s ' "$c"; fi; sleep 0.05; done`

// screenRows is a terminal's screen as plain rows, trailing blanks dropped.
func screenRows(text string) []string {
	rows := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// A Session another viewer holds at another size is drawn right on this
// terminal all the same: its screen as the host has it, cut to this window
// (the end of a taller screen, the left of a wider one), never the program's
// output for the other size written over this terminal's rows.
func TestAttachMirrorsASessionOfAnotherSize(t *testing.T) {
	client := attachTestHost(t)
	createAttachSession(t, client, "mh-mirror", sizeRows)
	in, typing := io.Pipe()
	defer typing.Close()
	out := &lockedBuffer{}
	const w, h = 70, 20
	go attachLoop(context.Background(), client, "mh-mirror", in, out, func() (int, int) { return w, h }, make(chan os.Signal))
	waitOutput(t, out, "ready-70")

	// What this terminal shows: everything it was sent, drawn by a terminal of its size.
	shows := func() []string {
		t.Helper()
		term, err := ghosttyvt.New(ghosttyvt.Options{Cols: w, Rows: h})
		if err != nil {
			t.Fatal(err)
		}
		defer term.Close()
		term.Write([]byte(out.String()))
		text, err := term.Capture(ghosttyvt.CaptureOptions{Scope: ghosttyvt.ScopeScreen})
		if err != nil {
			t.Fatal(err)
		}
		return screenRows(text)
	}
	// What it should show: the host's screen, its end h rows cut to w columns.
	want := func() []string {
		t.Helper()
		c, err := client.Capture(context.Background(), sessionhost.CaptureParams{ID: "mh-mirror", Scope: sessionhost.ScopeScreen})
		if err != nil {
			t.Fatal(err)
		}
		rows := screenRows(c.Text)
		screen := gridScreen{rows: rows, cursorY: c.CursorY}
		var cut []string
		for _, row := range screen.view(h) {
			if len(row) > w {
				row = row[:w]
			}
			cut = append(cut, row)
		}
		return screenRows(strings.Join(cut, "\n"))
	}
	match := func(what string) {
		t.Helper()
		var got, expect []string
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			if got, expect = shows(), want(); strings.Join(got, "\n") == strings.Join(expect, "\n") {
				return
			}
		}
		t.Fatalf("%s: this terminal shows\n%s\nwant\n%s", what, strings.Join(got, "\n"), strings.Join(expect, "\n"))
	}
	match("its own size")

	for _, other := range []struct{ cols, rows int }{{50, 30}, {160, 45}} {
		viewer, err := client.Attach(context.Background(), sessionhost.AttachParams{ID: "mh-mirror", Cols: other.cols, Rows: other.rows, Redraw: true})
		if err != nil {
			t.Fatal(err)
		}
		waitHostScreenText(t, client, "mh-mirror", fmt.Sprintf("ready-%d", other.cols))
		match(fmt.Sprintf("another viewer at %dx%d", other.cols, other.rows))
		viewer.Close()
	}

	typing.Write([]byte("x"))
	waitHostScreenText(t, client, "mh-mirror", "ready-70")
	match("its size taken back")
}

func waitHostScreenText(t *testing.T, client *sessionhost.Client, id, want string) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if c, err := client.Capture(context.Background(), sessionhost.CaptureParams{ID: id, Scope: sessionhost.ScopeScreen}); err == nil && strings.Contains(c.Text, want) {
			return
		}
	}
	t.Fatalf("the Session never showed %q", want)
}

func TestAttachReportsTheExit(t *testing.T) {
	client := attachTestHost(t)
	createAttachSession(t, client, "mh-exit", `printf 'bye'; read x; exit 3`)
	in, typing := io.Pipe()
	out := &lockedBuffer{}
	result := make(chan string, 1)
	go func() {
		outcome, err := attachLoop(context.Background(), client, "mh-exit", in, out, func() (int, int) { return 80, 24 }, nil)
		if err != nil {
			outcome = "error: " + err.Error()
		}
		result <- outcome
	}()
	waitOutput(t, out, "bye")
	typing.Write([]byte("\r"))
	select {
	case outcome := <-result:
		if outcome != "[mh-exit exited with code 3]" {
			t.Fatalf("outcome = %q", outcome)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the exit was never reported")
	}
	if _, err := attachLoop(context.Background(), client, "mh-none", strings.NewReader(""), io.Discard, func() (int, int) { return 80, 24 }, nil); err == nil || !strings.Contains(err.Error(), "no Session mh-none") {
		t.Fatalf("attach to a missing Session: %v", err)
	}
}

// The grid shows every Session from the host's own screens, opens the one
// chosen, goes back to the grid on Ctrl-] and leaves on q, without ever
// resizing a Session it only shows.
func TestGridShowsOpensAndReturns(t *testing.T) {
	client := attachTestHost(t)
	createAttachSession(t, client, "mh-aaa", `printf '\033[32mgreen-alpha\033[0m\r\n'; sleep 30`)
	createAttachSession(t, client, "mh-bbb", `stty -echo; printf 'ready-beta\r\n'; while read line; do printf 'beta got %s\r\n' "$line"; done`)
	typing := make(chan []byte)
	out := &lockedBuffer{}
	result := make(chan string, 1)
	go func() {
		outcome, err := runGrid(context.Background(), client, 0, typing, out, func() (int, int) { return 100, 30 }, nil, termStyle{})
		if err != nil {
			outcome = "error: " + err.Error()
		}
		result <- outcome
	}()
	waitOutput(t, out, "green-alpha")
	waitOutput(t, out, "ready-beta")
	for _, id := range []string{"mh-aaa", "mh-bbb"} {
		info, err := client.Info(context.Background(), id)
		if err != nil || info.Cols != 80 || info.Rows != 24 {
			t.Fatalf("the grid resized %s: %+v, %v", id, info, err)
		}
	}
	// Two Sessions created in order: the newest, mh-bbb, is tile 1.
	typing <- []byte("1")
	waitOutput(t, out, enterAttach) // the Session takes the screen
	typing <- []byte("hello\r")
	deadline := time.Now().Add(10 * time.Second)
	for {
		capture, err := client.Capture(context.Background(), sessionhost.CaptureParams{ID: "mh-bbb", Scope: sessionhost.ScopeScreen})
		if err == nil && strings.Contains(capture.Text, "beta got hello") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("typing did not reach the opened Session: %q", capture.Text)
		}
		time.Sleep(20 * time.Millisecond)
	}
	mark := len(out.String())
	typing <- []byte("\x1d") // back to the grid
	for !strings.Contains(out.String()[mark:], gridMouseOn) {
		if time.Now().After(deadline) {
			t.Fatalf("Ctrl-] did not return to the grid: %q", out.String()[mark:])
		}
		time.Sleep(20 * time.Millisecond)
	}
	typing <- []byte("q")
	select {
	case outcome := <-result:
		if outcome != "" {
			t.Fatalf("leaving the grid reported %q", outcome)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("q did not leave the grid")
	}
	if !strings.HasSuffix(out.String(), leaveAttach) && !strings.Contains(out.String()[mark:], leaveAttach) {
		t.Fatalf("leaving did not give the screen back")
	}
	for _, id := range []string{"mh-aaa", "mh-bbb"} {
		if info, err := client.Info(context.Background(), id); err != nil || !info.Alive {
			t.Fatalf("%s did not survive the grid: %+v, %v", id, info, err)
		}
	}
}
