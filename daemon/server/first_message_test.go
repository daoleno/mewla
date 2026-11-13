package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

// firstMessageClient is a paired phone on a test server: send writes a frame,
// next reads frames until one matches.
type firstMessageClient struct {
	t    *testing.T
	srv  *Server
	conn *websocket.Conn
}

func newFirstMessageClient(t *testing.T) *firstMessageClient {
	t.Helper()
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-first", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(httpServer.Close)
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-first", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &firstMessageClient{t: t, srv: srv, conn: conn}
}

func (c *firstMessageClient) send(frame map[string]any) {
	c.t.Helper()
	if err := c.conn.WriteJSON(frame); err != nil {
		c.t.Fatal(err)
	}
}

func (c *firstMessageClient) next(match func(map[string]any) bool) map[string]any {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			c.t.Fatalf("no matching frame within 3s: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			c.t.Fatal(err)
		}
		if match(payload) {
			return payload
		}
	}
}

func byRequest(id string) func(map[string]any) bool {
	return func(p map[string]any) bool { return p["request_id"] == id }
}

// A first message for a command that starts no agent is refused before any
// Session exists: typed into a shell it would run as a command line.
func TestCreateSessionRefusesAFirstMessageForAShell(t *testing.T) {
	c := newFirstMessageClient(t)
	c.send(map[string]any{"type": "create_session", "request_id": "r-shell", "cwd": t.TempDir(), "command": "bash", "text": "rm -rf build"})
	reply := c.next(byRequest("r-shell"))
	if reply["type"] != "error" || reply["code"] != "first_message_unsupported" {
		t.Fatalf("reply = %#v", reply)
	}
}

// A blank first message with no attachments is no first message: the request
// is an ordinary create_session (prepareFirstMessage returns nothing).
func TestABlankFirstMessageIsNone(t *testing.T) {
	srv := &Server{}
	message, err := srv.prepareFirstMessage("claude", "  \n", nil)
	if message != nil || err != nil {
		t.Fatalf("message = %#v, err = %v", message, err)
	}
	message, err = srv.prepareFirstMessage("claude", "Fix the flaky checkout test", nil)
	if err != nil || message == nil || message.rendered != "Fix the flaky checkout test" {
		t.Fatalf("message = %#v, err = %v", message, err)
	}
}

// Only a proven non-submission reads as not sent; anything that may have
// reached the agent is unknown, never replayed.
func TestFirstMessageOutcomes(t *testing.T) {
	notSubmitted := &watcher.InputSubmissionError{Result: watcher.InputResult{Outcome: watcher.InputNotSubmitted}, Cause: watcher.ErrWorkerInputNotReady}
	ambiguous := &watcher.InputSubmissionError{Result: watcher.InputResult{Outcome: watcher.InputAmbiguous}, Cause: errors.New("pane replaced")}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, firstMessageSent},
		{notSubmitted, firstMessageNotSent},
		{errors.Join(errors.New("wrapped"), notSubmitted), firstMessageNotSent},
		{ambiguous, firstMessageUnknown},
		{errors.New("tmux went away"), firstMessageUnknown},
	} {
		if got := firstMessageOutcome(tc.err); got != tc.want {
			t.Fatalf("firstMessageOutcome(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// The held message is submitted (with the final newline that submits it) to
// the Session's launch command, and every connection hears how it went; a
// message that did not go comes back with its text.
func TestFirstMessageIsDeliveredAndBroadcast(t *testing.T) {
	c := newFirstMessageClient(t)
	// One round trip so the connection is registered before the broadcast.
	c.send(map[string]any{"type": "create_session", "request_id": "r-ready", "cwd": t.TempDir(), "command": "bash", "text": "x"})
	c.next(byRequest("r-ready"))

	var gotID, gotCommand, gotText string
	c.srv.firstMessageSend = func(workerID, command, text string) error {
		gotID, gotCommand, gotText = workerID, command, text
		return nil
	}
	c.srv.deliverFirstMessage("mewla-worker-a:@1", "claude --dangerously-skip-permissions", "r-1", &firstMessageRequest{text: "Fix it", rendered: "Fix it\n"})
	sent := c.next(byRequest("r-1"))
	if sent["type"] != "session_first_message" || sent["worker_id"] != "mewla-worker-a:@1" || sent["outcome"] != "sent" || sent["text"] != nil {
		t.Fatalf("sent = %#v", sent)
	}
	if gotID != "mewla-worker-a:@1" || gotCommand != "claude --dangerously-skip-permissions" || gotText != "Fix it\n" {
		t.Fatalf("submitted %q %q %q", gotID, gotCommand, gotText)
	}

	c.srv.firstMessageSend = func(string, string, string) error {
		return &watcher.InputSubmissionError{Result: watcher.InputResult{Outcome: watcher.InputNotSubmitted}, Cause: watcher.ErrWorkerInputNotReady}
	}
	c.srv.deliverFirstMessage("mewla-worker-b:@2", "claude", "r-2", &firstMessageRequest{text: "Fix it", rendered: "Fix it"})
	back := c.next(byRequest("r-2"))
	if back["outcome"] != "not_sent" || back["text"] != "Fix it" || back["message"] == "" {
		t.Fatalf("back = %#v", back)
	}
}
