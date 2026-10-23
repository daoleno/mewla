package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

type heardNotifications struct {
	mu          sync.Mutex
	responses   []string
	engaged     []string
	suppressed  []string
	responseAt  time.Time
	responseSig chan struct{}
}

func (h *heardNotifications) NotificationSuppressed(kind, workerID, title, body, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.suppressed = append(h.suppressed, kind+"|"+workerID+"|"+title+"|"+body+"|"+reason)
}

func (h *heardNotifications) NotificationResponse(id, action string, at time.Time) {
	h.mu.Lock()
	h.responses = append(h.responses, id+"|"+action)
	h.responseAt = at
	h.mu.Unlock()
	h.responseSig <- struct{}{}
}

func (h *heardNotifications) NotificationEngaged(action, workerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.engaged = append(h.engaged, action+"|"+workerID)
}

func (h *heardNotifications) engagedSnapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.engaged...)
}

func TestPhoneReportsTapsForegroundAndViewingToTheRecorder(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-notify", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	heard := &heardNotifications{responseSig: make(chan struct{}, 4)}
	srv.SetNotificationRecorder(heard)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-notify", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if got := srv.NotificationPresence("w1"); !got.Connected || got.Foreground || got.ForegroundKnown || got.Viewing {
		t.Fatalf("presence before the app spoke = %#v", got)
	}
	tapped := time.Now().Add(-time.Minute).UnixMilli()
	for _, message := range []map[string]any{
		{"type": "app_state", "app_state": "active"},
		{"type": "set_active_worker", "worker_id": "w1"},
		{"type": "notification_response", "notification_id": "n_1", "occurred_at_ms": tapped},
		{"type": "notification_response", "notification_id": "  "},
	} {
		if err := conn.WriteJSON(message); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-heard.responseSig:
	case <-time.After(3 * time.Second):
		t.Fatal("the tap was not reported")
	}
	heard.mu.Lock()
	responses, at := append([]string(nil), heard.responses...), heard.responseAt
	heard.mu.Unlock()
	if len(responses) != 1 || responses[0] != "n_1|tapped" || at.UnixMilli() != tapped {
		t.Fatalf("responses = %v at %v", responses, at)
	}
	engaged := heard.engagedSnapshot()
	if len(engaged) != 2 || engaged[0] != "opened_app|" || engaged[1] != "viewed|w1" {
		t.Fatalf("engaged = %v", engaged)
	}
	if got := srv.NotificationPresence("w1"); !got.Connected || !got.Foreground || !got.ForegroundKnown || !got.Viewing {
		t.Fatalf("presence = %#v", got)
	}
	if got := srv.NotificationPresence("w2"); got.Viewing {
		t.Fatalf("w2 viewing = %#v", got)
	}
}

func TestSuppressedWorkerNotificationIsRecordedWithItsText(t *testing.T) {
	srv := New(nil, watcher.New(time.Second), nil, nil, nil, nil, nil)
	heard := &heardNotifications{responseSig: make(chan struct{}, 1)}
	srv.SetNotificationRecorder(heard)
	worker := &classifier.Worker{ID: "main:7", Name: "mewla (main:7)", Window: "mewla", Summary: "all tests pass"}
	srv.noteSuppressedNotification(watcher.SessionEvent{Type: "worker_state_change", WorkerID: "main:7", OldState: "running", NewState: "done", Worker: worker}, "person_viewing_session")
	srv.noteSuppressedNotification(watcher.SessionEvent{Type: "worker_state_change", WorkerID: "main:7", OldState: "done", NewState: "done", Worker: worker}, "person_viewing_session")
	srv.noteSuppressedNotification(watcher.SessionEvent{Type: "worker_updated", WorkerID: "main:7", OldState: "running", NewState: "done", Worker: worker}, "person_viewing_session")
	srv.noteSuppressedNotification(watcher.SessionEvent{Type: "worker_state_change", WorkerID: "main:7", OldState: "done", NewState: "running", Worker: worker}, "person_viewing_session")
	want := "worker_done|main:7|mewla finished|all tests pass|person_viewing_session"
	if len(heard.suppressed) != 1 || heard.suppressed[0] != want {
		t.Fatalf("suppressed = %v, want one %q", heard.suppressed, want)
	}
}
