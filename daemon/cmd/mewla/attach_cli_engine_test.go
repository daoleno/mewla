//go:build ghosttyvt && cgo && (linux || darwin)

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if n := strings.Count(out.String(), redraw); n != redraws+1 {
		t.Fatalf("drawn again %d times for one resize", n-redraws)
	}

	typing.Write([]byte("hi\r"))
	waitOutput(t, out, "got hi")
	if info, err := client.Info(context.Background(), "mh-old"); err != nil || info.Cols != 70 || info.Rows != 20 {
		t.Fatalf("info = %+v, %v; typing here should take the size back to 70x20", info, err)
	}
	time.Sleep(3 * attachSizePoll)
	if n := strings.Count(out.String(), redraw); n != redraws+1 {
		t.Fatalf("taking the size back drew this terminal again (%d), though it is drawn at its own size", n-redraws-1)
	}
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
