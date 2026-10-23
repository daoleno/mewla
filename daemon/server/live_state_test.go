package server

import (
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/live"
	"github.com/daoleno/mewla/daemon/watcher"
)

type liveFixture struct {
	server   *Server
	http     *httptest.Server
	conn     *websocket.Conn
	manager  *auth.Manager
	key      ed25519.PrivateKey
	deviceID string

	mu       sync.Mutex
	snapshot brain.Snapshot
	replies  []brain.WorkUserActionRequest
}

func (f *liveFixture) set(snapshot brain.Snapshot) {
	f.mu.Lock()
	f.snapshot = snapshot
	f.mu.Unlock()
}

func newLiveFixture(t *testing.T) *liveFixture {
	t.Helper()
	manager, key, deviceID := sessionFileAuthFixture(t)
	server := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	f := &liveFixture{server: server, manager: manager, key: key, deviceID: deviceID}
	server.live.snapshotOverride = func() (brain.Snapshot, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.snapshot, nil
	}
	server.live.replyOverride = func(request brain.WorkUserActionRequest) (brain.WorkUserActionResult, error) {
		f.mu.Lock()
		f.replies = append(f.replies, request)
		f.mu.Unlock()
		return brain.WorkUserActionResult{Work: brain.Work{ID: request.WorkID, Status: brain.WorkRunning}, Admission: brain.ExternalInputAccepted}, nil
	}
	f.http = httptest.NewServer(server.Handler())
	t.Cleanup(f.http.Close)
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(key, manager.DaemonID(), deviceID, "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.http.URL, "http")+"/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	f.conn = conn
	return f
}

func (f *liveFixture) replyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.replies)
}

func (f *liveFixture) send(t *testing.T, payload map[string]any) {
	t.Helper()
	if err := f.conn.WriteJSON(payload); err != nil {
		t.Fatal(err)
	}
}

func askingSnapshot() brain.Snapshot {
	since := time.Now().Add(-time.Minute)
	return brain.Snapshot{CurrentWork: []brain.CurrentWork{{
		ID: "work-1", Title: "Ship v0.2.39", Question: "Ship v0.2.39 to everyone?",
		Choices: []string{"Ship", "Wait"}, AttentionSince: &since,
	}}}
}

func TestLiveStateGetReportsCurrentState(t *testing.T) {
	f := newLiveFixture(t)
	started := time.Now().Add(-20 * time.Second)
	f.set(brain.Snapshot{
		Workers:     []brain.WorkerRef{{ID: "w1", Name: "main:7", Status: "running", Phase: "releasing", Delegated: true, StartedAt: &started}},
		CurrentWork: []brain.CurrentWork{{ID: "work-1", Title: "perpetuo-fre17-18", AttemptSessionID: "w1"}},
		Objective:   &brain.Objective{Title: "ship", Total: 5, Done: 2},
	})
	f.send(t, map[string]any{"type": "live_state_get", "request_id": "r1"})
	message := readWebSocketType(t, f.conn, "live_state")
	for message["request_id"] != "r1" { // a broadcast may arrive first
		message = readWebSocketType(t, f.conn, "live_state")
	}
	state := message["live"].(map[string]any)
	if state["kind"] != "running" || state["title"] != "perpetuo-fre17-18" || state["phase"] != "releasing" || state["done"] != float64(2) || state["total"] != float64(5) {
		t.Fatalf("live = %v", state)
	}
	if state["seq"] == nil || state["epoch"] == nil || state["run_key"] == "" {
		t.Fatalf("live = %v", state)
	}
}

func TestLiveAnswerRejectsAStaleQuestionAndRepliesThroughTheWork(t *testing.T) {
	f := newLiveFixture(t)
	asking := askingSnapshot()
	f.set(asking)
	current := live.Project(asking, time.Now()).Question
	if current == nil {
		t.Fatal("fixture has no question")
	}

	f.send(t, map[string]any{"type": "live_answer", "request_id": "live:old:0", "question_id": "not-the-current-question", "choice_index": 0})
	stale := readWebSocketType(t, f.conn, "error")
	if stale["code"] != "answer_stale" || f.replyCount() != 0 {
		t.Fatalf("stale answer: %v replies=%d", stale, f.replyCount())
	}

	request := map[string]any{"type": "live_answer", "request_id": "live:" + current.ID + ":1", "question_id": current.ID, "choice_index": 1}
	f.send(t, request)
	ok := readWebSocketType(t, f.conn, "live_answer")
	if ok["ok"] != true || ok["kind"] != "work" || ok["work_id"] != "work-1" || ok["text"] != "Wait" {
		t.Fatalf("answer = %v", ok)
	}
	if f.replyCount() != 1 || f.replies[0].Text != "Wait" || f.replies[0].Kind != brain.WorkUserReply || f.replies[0].RequestID != "live:"+current.ID+":1" {
		t.Fatalf("replies = %+v", f.replies)
	}

	// A re-delivered tap is the same answer, not a second one.
	f.send(t, request)
	again := readWebSocketType(t, f.conn, "live_answer")
	if again["duplicate"] != true || f.replyCount() != 1 {
		t.Fatalf("duplicate = %v replies=%d", again, f.replyCount())
	}

	// The question is gone: a late tap changes nothing.
	f.set(brain.Snapshot{})
	f.send(t, map[string]any{"type": "live_answer", "request_id": "live:late", "question_id": current.ID, "choice_index": 0})
	late := readWebSocketType(t, f.conn, "error")
	if late["code"] != "answer_stale" || f.replyCount() != 1 {
		t.Fatalf("late answer: %v replies=%d", late, f.replyCount())
	}
}

func TestLiveAnswerValidatesChoiceAndChatChipsAreNotSentByTheDaemon(t *testing.T) {
	f := newLiveFixture(t)
	f.set(brain.Snapshot{QuickReplies: &brain.QuickReplies{Rows: [][]string{{"Yes", "No"}}, At: time.Now()}})
	question := live.Project(f.snapshot, time.Now()).Question

	f.send(t, map[string]any{"type": "live_answer", "request_id": "bad", "question_id": question.ID, "choice_index": 7})
	if got := readWebSocketType(t, f.conn, "error"); got["code"] != "invalid_choice" {
		t.Fatalf("error = %v", got)
	}
	f.send(t, map[string]any{"type": "live_answer", "request_id": "missing", "question_id": question.ID})
	if got := readWebSocketType(t, f.conn, "error"); got["code"] != "invalid_answer" {
		t.Fatalf("error = %v", got)
	}
	f.send(t, map[string]any{"type": "live_answer", "request_id": "chat", "question_id": question.ID, "choice_index": 1})
	ok := readWebSocketType(t, f.conn, "live_answer")
	if ok["kind"] != "chat" || ok["text"] != "No" || f.replyCount() != 0 {
		t.Fatalf("chat answer = %v replies=%d (the app sends chat text itself)", ok, f.replyCount())
	}
}

func (f *liveFixture) issueToken(t *testing.T) string {
	t.Helper()
	f.send(t, map[string]any{"type": "widget_token_issue", "request_id": "t"})
	message := readWebSocketType(t, f.conn, "widget_token")
	if message["url_path"] != "/widget/snapshot" {
		t.Fatalf("message = %v", message)
	}
	return message["token"].(string)
}

func (f *liveFixture) snapshotRequest(method, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/widget/snapshot", nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(response, request)
	return response
}

func TestWidgetSnapshotEndpointAndTokenRevocation(t *testing.T) {
	f := newLiveFixture(t)
	f.set(askingSnapshot())
	token := f.issueToken(t)

	response := f.snapshotRequest(http.MethodGet, "Bearer "+token)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v", response.Code, response.Header())
	}
	var body struct {
		Live        live.State `json:"live"`
		GeneratedAt int64      `json:"generated_at"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Live.Kind != "needs_you" || body.GeneratedAt == 0 {
		t.Fatalf("body = %s", response.Body.String())
	}

	for name, authorization := range map[string]string{
		"missing":   "",
		"garbage":   "Bearer garbage",
		"wrong":     "Bearer " + live.TokenPrefix + strings.Repeat("0", 16) + "." + strings.Repeat("0", 64),
		"basic":     "Basic " + token,
		"device":    calendarAuthHeader(f.key, f.manager.DaemonID(), f.deviceID, "mewla-connect"),
		"bare":      token,
		"pairing":   "Bearer " + f.deviceID,
		"extraword": "Bearer " + token + " extra",
	} {
		got := f.snapshotRequest(http.MethodGet, authorization)
		if got.Code != http.StatusUnauthorized || got.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("%s: status=%d", name, got.Code)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		got := f.snapshotRequest(method, "Bearer "+token)
		if got.Code != http.StatusMethodNotAllowed || got.Header().Get("Allow") != "GET" {
			t.Fatalf("%s: status=%d", method, got.Code)
		}
	}
	// The token opens this route only.
	other := httptest.NewRequest(http.MethodGet, "/devices", nil)
	other.Header.Set("Authorization", "Bearer "+token)
	otherResponse := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(otherResponse, other)
	if otherResponse.Code == http.StatusOK {
		t.Fatal("a widget token must not open other routes")
	}

	f.send(t, map[string]any{"type": "widget_token_revoke", "request_id": "r"})
	if revoked := readWebSocketType(t, f.conn, "widget_token_revoked"); revoked["revoked"] != float64(1) {
		t.Fatalf("revoked = %v", revoked)
	}
	if got := f.snapshotRequest(http.MethodGet, "Bearer "+token); got.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked token must stop the widget, status=%d", got.Code)
	}
}

func TestWidgetTokenDiesWithItsDevice(t *testing.T) {
	f := newLiveFixture(t)
	token := f.issueToken(t)
	if got := f.snapshotRequest(http.MethodGet, "Bearer "+token); got.Code != http.StatusOK {
		t.Fatalf("status=%d", got.Code)
	}
	if _, err := f.manager.RevokeDevice(f.deviceID); err != nil {
		t.Fatal(err)
	}
	if got := f.snapshotRequest(http.MethodGet, "Bearer "+token); got.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked device's token must stop, status=%d", got.Code)
	}
	if _, ok := f.server.live.tokens.DeviceEntry(f.deviceID); ok {
		t.Fatal("the revocation listener must delete the token")
	}
}

type fakeLivePusher struct {
	mu       sync.Mutex
	capable  bool
	sent     []map[string]any
	sentSeen atomic.Int64
}

func (p *fakeLivePusher) SetRegistration(string, string)                {}
func (p *fakeLivePusher) NotifyWorkerBlocked(_, _, _ string) error      { return nil }
func (p *fakeLivePusher) NotifyWorkerFailed(_, _, _ string) error       { return nil }
func (p *fakeLivePusher) NotifyWorkerDone(_, _, _ string) error         { return nil }
func (p *fakeLivePusher) NotifyScheduledResult(_, _, _, _ string) error { return nil }
func (p *fakeLivePusher) SetLiveCapable(capable bool) {
	p.mu.Lock()
	p.capable = capable
	p.mu.Unlock()
}
func (p *fakeLivePusher) LiveCapable() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.capable }
func (p *fakeLivePusher) SendLive(data map[string]any) error {
	p.mu.Lock()
	p.sent = append(p.sent, data)
	p.mu.Unlock()
	p.sentSeen.Add(1)
	return nil
}
func (p *fakeLivePusher) sends() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.sent) }

func waitForLive(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestLiveStateIsPushedOnlyToADeviceThatAskedForIt(t *testing.T) {
	f := newLiveFixture(t)
	pusher := &fakeLivePusher{}
	f.server.pusher = pusher
	hub := f.server.liveRuntime()
	running := func() brain.Snapshot {
		started := time.Now().Add(-time.Second)
		return brain.Snapshot{Workers: []brain.WorkerRef{{ID: "w", Name: "w", Status: "running", Phase: "working", Delegated: true, StartedAt: &started}}}
	}

	// Registered without the flag: nothing is pushed, and the flag is false.
	f.send(t, map[string]any{"type": "register_push", "push_token": "ExponentPushToken[x]", "server_ref": "srv"})
	readWebSocketType(t, f.conn, "push_registered")
	hub.projector.ObserveNow(running())
	time.Sleep(50 * time.Millisecond)
	if pusher.sends() != 0 {
		t.Fatal("a device that did not declare live_state must get no live push")
	}

	// Re-registering with the flag turns it on.
	f.send(t, map[string]any{"type": "register_push", "push_token": "ExponentPushToken[x]", "server_ref": "srv", "live_state": true})
	readWebSocketType(t, f.conn, "push_registered")
	if !pusher.LiveCapable() {
		t.Fatal("live_state capability must be recorded")
	}
	phase := running()
	phase.Workers[0].Phase = "verifying"
	hub.projector.ObserveNow(phase)
	waitForLive(t, "a live push", func() bool { return pusher.sends() == 1 })
	data := pusher.sent[0]
	var state live.State
	if err := json.Unmarshal([]byte(data["payload"].(string)), &state); err != nil {
		t.Fatal(err)
	}
	if state.Phase != "verifying" || data["seq"] != int64String(int64(state.Seq)) || data["epoch"] != int64String(state.Epoch) {
		t.Fatalf("data = %v state = %+v", data, state)
	}
	if len(data["payload"].(string)) > 2048 {
		t.Fatalf("payload is %d bytes", len(data["payload"].(string)))
	}

	// Idle after idle is not news; idle after running is (it ends the surface).
	hub.projector.ObserveNow(brain.Snapshot{})
	waitForLive(t, "the ending push", func() bool { return pusher.sends() == 2 })
	hub.projector.ObserveNow(brain.Snapshot{})
	time.Sleep(50 * time.Millisecond)
	if pusher.sends() != 2 {
		t.Fatalf("sends = %d", pusher.sends())
	}
}

func TestLiveStateBroadcastShape(t *testing.T) {
	f := newLiveFixture(t)
	f.server.liveRuntime().projector.ObserveNow(askingSnapshot())
	message := readWebSocketType(t, f.conn, "live_state")
	keys := []string{}
	for key := range message {
		keys = append(keys, key)
	}
	if len(keys) != 2 || message["live"] == nil {
		t.Fatalf("broadcast must be exactly {type, live}, got %v", keys)
	}
	_ = io.Discard
}
