package control

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/phone"
)

type testHandler struct {
	mu       sync.Mutex
	requests []Request
	response Response
}

func (h *testHandler) HandleControlRequest(req Request) Response {
	h.mu.Lock()
	h.requests = append(h.requests, req)
	h.mu.Unlock()
	return h.response
}

func TestServerCallRoundTrip(t *testing.T) {
	stateDir := shortStateDir(t)
	socketPath, err := DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatalf("DefaultSocketPath returned error: %v", err)
	}

	handler := &testHandler{
		response: Response{
			OK:     true,
			Text:   "hello",
			Worker: &Worker{ID: "main:@42", Name: "Franklin", Status: "running"},
		},
	}
	server := &Server{Path: socketPath, Handler: handler}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx)
	}()
	waitForSocketPath(t, socketPath)

	resp, err := Call(socketPath, Request{Type: "worker_capture", WorkerID: "main:@42"})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if !resp.OK || resp.Text != "hello" || resp.Worker == nil || resp.Worker.ID != "main:@42" {
		t.Fatalf("unexpected response: %#v", resp)
	}

	handler.mu.Lock()
	if len(handler.requests) != 1 {
		t.Fatalf("handler requests = %#v", handler.requests)
	}
	if handler.requests[0].Type != "worker_capture" || handler.requests[0].WorkerID != "main:@42" {
		t.Fatalf("handler request = %#v", handler.requests[0])
	}
	handler.mu.Unlock()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server exited with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server shutdown")
	}
}

func TestServerShutdownClosesAndJoinsAcceptedConnections(t *testing.T) {
	stateDir := shortStateDir(t)
	socketPath, err := DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		Path:    socketPath,
		Handler: &testHandler{response: Response{OK: true}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx)
	}()
	waitForSocketPath(t, socketPath)
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server returned before joining accepted connection")
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("accepted connection remained open after server shutdown")
	}
}

func TestRemoveStaleSocketRejectsRegularFile(t *testing.T) {
	path := filepath.Join(shortStateDir(t), "control.sock")
	if err := os.WriteFile(path, []byte("not-a-socket"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := removeStaleSocket(path); err == nil {
		t.Fatal("expected error for non-socket path")
	}
}

func waitForSocketPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// The file appears at bind, before listen; only a connection that
		// gets through says the server is accepting.
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			if conn, err := net.Dial("unix", path); err == nil {
				conn.Close()
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for control socket at %s", path)
}

// shortStateDir keeps socket paths under the 108-byte Unix socket limit, which
// t.TempDir exceeds under a long $TMPDIR and test name.
func shortStateDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mewla-ctl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// A phone action and its answer, a picture included, cross the control socket intact, and a
// request that waits longer than the usual bound is served on its own connection.
func TestPhoneActionAndPictureRoundTrip(t *testing.T) {
	stateDir := shortStateDir(t)
	socketPath, err := DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	picture := make([]byte, 300<<10)
	for i := range picture {
		picture[i] = byte(i * 7)
	}
	release := make(chan struct{})
	handler := &phoneHandler{release: release, picture: picture}
	server := &Server{Path: socketPath, Handler: handler}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	waitForSocketPath(t, socketPath)

	type answer struct {
		resp Response
		err  error
	}
	slow := make(chan answer, 1)
	go func() {
		resp, err := CallWithTimeout(socketPath, Request{Type: "phone", WorkerID: "%7", PhoneAction: &phone.AgentAction{Action: phone.ActHandoff, Reason: "Sign in"}}, 10*time.Second)
		slow <- answer{resp, err}
	}()
	// While the handoff waits for the person, other calls on the socket are still served.
	resp, err := Call(socketPath, Request{Type: "phone", WorkerID: "%7", PhoneAction: &phone.AgentAction{Action: phone.ActScreen, Image: true}})
	if err != nil || !resp.OK || resp.PhoneResult == nil || resp.PhoneResult.Text != "Screen 1080x2400" || !bytes.Equal(resp.PhoneResult.Image, picture) {
		t.Fatalf("%+v %v", resp, err)
	}
	select {
	case <-slow:
		t.Fatal("the handoff answered before the person did")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	got := <-slow
	if got.err != nil || got.resp.PhoneResult == nil || got.resp.PhoneResult.Text != "done" {
		t.Fatalf("%+v %v", got.resp, got.err)
	}
	handler.mu.Lock()
	if len(handler.seen) != 2 || handler.seen[0].PhoneAction == nil || handler.seen[0].WorkerID != "%7" {
		t.Fatalf("requests %+v", handler.seen)
	}
	handler.mu.Unlock()
	cancel()
	<-done
}

type phoneHandler struct {
	mu      sync.Mutex
	seen    []Request
	release chan struct{}
	picture []byte
}

func (h *phoneHandler) HandleControlRequest(req Request) Response {
	h.mu.Lock()
	h.seen = append(h.seen, req)
	h.mu.Unlock()
	if req.PhoneAction.Action == phone.ActHandoff {
		<-h.release
		return Response{OK: true, PhoneResult: &phone.AgentResult{Text: "done"}}
	}
	return Response{OK: true, PhoneResult: &phone.AgentResult{Text: "Screen 1080x2400", Image: h.picture}}
}
