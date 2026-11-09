//go:build ghosttyvt && cgo && (linux || darwin)

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/sessionhost"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func attachTestHost(t *testing.T) *sessionhost.Client {
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
	return client
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

func waitOutput(t *testing.T, out *lockedBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q in %q", want, out.String())
		}
		time.Sleep(20 * time.Millisecond)
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
	capture, err := client.Capture(context.Background(), sessionhost.CaptureParams{ID: "mh-attach", Scope: sessionhost.ScopeScreen})
	if err != nil || !strings.Contains(capture.Text, "got bye") || strings.Contains(capture.Text, "after") {
		t.Fatalf("after detach the Session shows %q (%v)", capture.Text, err)
	}
	if info, err := client.Info(context.Background(), "mh-attach"); err != nil || !info.Alive {
		t.Fatalf("detaching ended the Session: %+v, %v", info, err)
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
