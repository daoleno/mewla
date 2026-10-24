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
	// ForegroundKnown is false when an app is connected but has never said
	// whether it is in the foreground (an older app version).
	ForegroundKnown bool
	Connected       bool
	Viewing         bool
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
	// With no app connected, it is certainly not in the foreground.
	presence.ForegroundKnown = !presence.Connected
	for conn := range s.clients {
		if state := s.appStates[conn]; state != "" {
			presence.ForegroundKnown = true
			if state == "active" {
				presence.Foreground = true
			}
		}
		if target != "" && strings.TrimSpace(s.active[conn]) == target {
			presence.Viewing = true
		}
	}
	return presence
}

// doneSuppression says why a worker_done push should not go out, or "" when it
// should. The first done of a Work in a run is pushed (the Work's later
// versions and progress stay in the app); blocked or failed on the Work starts
// a new run, since the person then acted on it.
func (s *Server) doneSuppression(ev watcher.SessionEvent, kind string) string {
	workID := ""
	if s.brain != nil {
		workID, _ = s.brain.TurnWorkID(ev.WorkerID, ev.TurnID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind != push.KindWorkerDone {
		if workID != "" {
			delete(s.doneNotified, workID)
		}
		return ""
	}
	foreground := false
	for conn := range s.clients {
		if s.appStates[conn] == "active" {
			foreground = true
			break
		}
	}
	if foreground {
		return "app_in_foreground"
	}
	if workID == "" {
		return ""
	}
	if s.doneNotified[workID] {
		return "work_already_notified"
	}
	if s.doneNotified == nil {
		s.doneNotified = make(map[string]bool)
	}
	s.doneNotified[workID] = true
	return ""
}

// noteSuppressedNotification records a Worker notification the server decided
// not to send, so Watch can tell a rightly quiet moment from a missed one.
func (s *Server) noteSuppressedNotification(ev watcher.SessionEvent, reason string) {
	recorder := s.notificationRecorder()
	if recorder == nil || ev.Worker == nil || ev.Type != "worker_state_change" || ev.OldState == ev.NewState {
		return
	}
	kind, ok := workerNotificationKind(ev.NewState)
	if !ok {
		return
	}
	title, body := push.WorkerMessageText(kind, workerNotice(ev))
	recorder.NotificationSuppressed(kind, ev.WorkerID, title, body, reason)
}

// workerNotificationKind is the notification a Worker's new state earns, if any.
func workerNotificationKind(state string) (string, bool) {
	switch state {
	case "blocked":
		return push.KindWorkerBlocked, true
	case "failed":
		return push.KindWorkerFailed, true
	case "done":
		return push.KindWorkerDone, true
	}
	return "", false
}

// workerNotice is what a Worker notification says about the Worker of a
// session event.
func workerNotice(ev watcher.SessionEvent) push.WorkerNotice {
	return push.WorkerNotice{WorkerID: ev.WorkerID, Window: ev.Worker.Window, Summary: ev.Worker.Summary}
}
