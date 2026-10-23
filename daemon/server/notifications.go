package server

import (
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/daoleno/mewla/daemon/push"
	"github.com/daoleno/mewla/daemon/watcher"
)

// What the phone did with a notification, as the app and the server report it.
const (
	notificationTapped    = "tapped"
	notificationViewed    = "viewed"
	notificationOpenedApp = "opened_app"
)

// NotificationPresence is what the phone was doing at a moment: connected, in
// the foreground, and with a given Session open.
type NotificationPresence struct {
	Foreground bool
	Connected  bool
	Viewing    bool
}

// SetNotificationRecorder lets Watch hear what the phone does with
// notifications and which ones the server held back.
func (s *Server) SetNotificationRecorder(recorder notificationRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifications = recorder
}

func (s *Server) notificationRecorder() notificationRecorder {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notifications
}

// handleNotificationResponse hears that the person tapped a notification. The
// app sends only the notification's id, the action and when it happened.
func (s *Server) handleNotificationResponse(raw clientMessage) {
	recorder := s.notificationRecorder()
	id := strings.TrimSpace(raw.NotificationID)
	if recorder == nil || id == "" {
		return
	}
	action := strings.TrimSpace(raw.Action)
	if action == "" {
		action = notificationTapped
	}
	var at time.Time
	if raw.OccurredAtMS > 0 {
		at = time.UnixMilli(raw.OccurredAtMS)
	}
	recorder.NotificationResponse(id, action, at)
}

// handleAppState keeps whether this connection's app is in the foreground, and
// counts bringing the app forward as a (weak) response to recent notifications.
func (s *Server) handleAppState(conn *websocket.Conn, raw clientMessage) {
	state := strings.TrimSpace(raw.AppState)
	s.mu.Lock()
	s.appStates[conn] = state
	recorder := s.notifications
	s.mu.Unlock()
	if recorder != nil && state == "active" {
		recorder.NotificationEngaged(notificationOpenedApp, "")
	}
}

// NotificationPresence says what the phone was doing when a notification about
// workerID went out.
func (s *Server) NotificationPresence(workerID string) NotificationPresence {
	target := strings.TrimSpace(workerID)
	s.mu.Lock()
	defer s.mu.Unlock()
	presence := NotificationPresence{Connected: len(s.clients) > 0}
	for conn := range s.clients {
		if s.appStates[conn] == "active" {
			presence.Foreground = true
		}
		if target != "" && strings.TrimSpace(s.active[conn]) == target {
			presence.Viewing = true
		}
	}
	return presence
}

// noteSuppressedNotification records a Worker notification the server decided
// not to send, so Watch can tell a rightly quiet moment from a missed one.
func (s *Server) noteSuppressedNotification(ev watcher.SessionEvent, reason string) {
	recorder := s.notificationRecorder()
	if recorder == nil || ev.Worker == nil || ev.Type != "worker_state_change" || ev.OldState == ev.NewState {
		return
	}
	var kind string
	switch ev.NewState {
	case "blocked":
		kind = push.KindWorkerBlocked
	case "failed":
		kind = push.KindWorkerFailed
	case "done":
		kind = push.KindWorkerDone
	default:
		return
	}
	title, body := push.WorkerMessageText(kind, ev.WorkerID, ev.Worker.Name, ev.Worker.Summary)
	recorder.NotificationSuppressed(kind, ev.WorkerID, title, body, reason)
}
