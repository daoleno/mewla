package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

// brainWorkFixture is a real Store, Service and Server behind one paired
// device's WebSocket.
type brainWorkFixture struct {
	store *brain.Store
	srv   *Server
	conn  *websocket.Conn
}

func newBrainWorkFixture(t *testing.T) *brainWorkFixture {
	t.Helper()
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread"}); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, &killTrackingWatcher{}, nil)
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-work", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, service)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(httpServer.Close)
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-work", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &brainWorkFixture{store: store, srv: srv, conn: conn}
}

// request sends one message and returns the reply carrying its request_id.
func (f *brainWorkFixture) request(t *testing.T, message map[string]any) map[string]any {
	t.Helper()
	if err := f.conn.WriteJSON(message); err != nil {
		t.Fatal(err)
	}
	_ = f.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, raw, err := f.conn.ReadMessage()
		if err != nil {
			t.Fatalf("no answer to %v: %v", message["type"], err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["request_id"] == message["request_id"] {
			return payload
		}
	}
}

func waitingWork(t *testing.T, store *brain.Store) brain.Work {
	t.Helper()
	item, err := store.CreateWork(brain.Work{Title: "pick a copy", Objective: "needs a call", CompletionPolicy: brain.CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	status := brain.WorkWaiting
	question := "Keep both copies, or the newest edit?"
	item, err = store.UpdateWork(item.ID, brain.WorkUpdate{Status: &status, Question: &question})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

// "Snooze 1 hour": the request carries the length; this computer's clock
// sets the end. A time still works (the shipped app's tomorrow morning).
func TestBrainWorkActionSnoozesForALength(t *testing.T) {
	f := newBrainWorkFixture(t)
	item := waitingWork(t, f.store)
	before := time.Now().UTC()
	reply := f.request(t, map[string]any{"type": "brain_work_action", "request_id": "snooze-hour", "id": item.ID, "action": "snooze", "snooze_seconds": 3600})
	if reply["type"] != "brain_work_action" {
		t.Fatalf("reply = %#v", reply)
	}
	stored, err := f.store.Work(item.ID)
	if err != nil || stored.SnoozedUntil == nil {
		t.Fatalf("snooze not kept: %+v %v", stored, err)
	}
	if got := stored.SnoozedUntil.Sub(before); got < time.Hour || got > time.Hour+time.Minute {
		t.Fatalf("snoozed for %s, want an hour", got)
	}

	until := time.Now().Add(20 * time.Hour).UTC().Truncate(time.Second)
	reply = f.request(t, map[string]any{"type": "brain_work_action", "request_id": "snooze-until", "id": item.ID, "action": "snooze", "snooze_until": until})
	if reply["type"] != "brain_work_action" {
		t.Fatalf("reply = %#v", reply)
	}
	if stored, _ = f.store.Work(item.ID); stored.SnoozedUntil == nil || !stored.SnoozedUntil.Equal(until) {
		t.Fatalf("snooze_until not kept: %+v", stored.SnoozedUntil)
	}

	for name, message := range map[string]map[string]any{
		"both":     {"snooze_seconds": 3600, "snooze_until": until},
		"negative": {"snooze_seconds": -5},
		"neither":  {},
	} {
		message["type"], message["request_id"], message["id"], message["action"] = "brain_work_action", "bad-"+name, item.ID, "snooze"
		if reply := f.request(t, message); reply["type"] != "error" || reply["code"] != "brain_work_action_failed" {
			t.Errorf("%s: reply = %#v", name, reply)
		}
	}
}

// A Work's page asks for its detail by id; an unknown id is refused.
func TestBrainWorkDetailAnswersAnyWork(t *testing.T) {
	f := newBrainWorkFixture(t)
	item := waitingWork(t, f.store)
	reply := f.request(t, map[string]any{"type": "brain_work_detail", "request_id": "detail", "id": item.ID})
	work, _ := reply["work"].(map[string]any)
	if reply["type"] != "brain_work_detail" || work["work_id"] != item.ID || work["title"] != "pick a copy" {
		t.Fatalf("reply = %#v", reply)
	}
	if sessions, ok := work["worker_sessions"].([]any); !ok || len(sessions) != 0 {
		t.Fatalf("worker_sessions = %#v, want []", work["worker_sessions"])
	}
	missing := f.request(t, map[string]any{"type": "brain_work_detail", "request_id": "missing", "id": "work-nope"})
	if missing["type"] != "error" || missing["code"] != "brain_work_detail_failed" {
		t.Fatalf("missing = %#v", missing)
	}
}
