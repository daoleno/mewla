package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/push"
	"github.com/daoleno/mewla/daemon/watcher"
)

func promptTransition(workerID, turnID, oldState, newState, promptID string) watcher.SessionEvent {
	event := notificationTransition(workerID, oldState, newState)
	event.TurnID = turnID
	event.Worker.Summary = "Waiting for Claude permission"
	if newState == "blocked" {
		event.Worker.Prompt = &classifier.Prompt{ID: promptID}
	}
	return event
}

func countKind(calls []recordedPush, kind string) int {
	n := 0
	for _, call := range calls {
		if call.kind == kind {
			n++
		}
	}
	return n
}

// 2026-10-30: one Claude permission prompt on mewla-friends-media flipped
// blocked and running about once a second for two minutes, and every blocked
// went to the phone: about 100 identical "needs input" pushes. The same
// question is told once, however often its state blinks, whatever the turn
// does meanwhile, and across a daemon restart; a new question, or the same
// one after the Worker really moved on, is told again.
func TestBlockedPushIsOncePerQuestion(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "worker-pushes.json")
	pusher := &recordingNotificationPusher{}
	srv, turnID := ledgerNotificationServer(t, "agent-1")
	srv.pusher = pusher
	srv.workerPushPath = statePath
	clock := time.Date(2026, 10, 30, 6, 14, 36, 0, time.UTC)
	srv.workerPushNow = func() time.Time { return clock }

	for range 25 {
		srv.maybeNotifyForSessionEvent(promptTransition("agent-1", turnID, "running", "blocked", "P"))
		clock = clock.Add(time.Second)
		srv.maybeNotifyForSessionEvent(promptTransition("agent-1", turnID, "blocked", "running", ""))
		clock = clock.Add(time.Second)
	}
	if got := countKind(pusher.snapshot(), "blocked"); got != 1 {
		t.Fatalf("a flickering prompt pushed %d times, want 1", got)
	}

	// A daemon restart rediscovers the same dialog.
	restarted, _ := ledgerNotificationServer(t, "agent-1")
	restarted.brain = srv.brain
	restarted.pusher = pusher
	restarted.workerPushPath = statePath
	restarted.workerPushNow = srv.workerPushNow
	restarted.maybeNotifyForSessionEvent(promptTransition("agent-1", turnID, "running", "blocked", "P"))
	if got := countKind(pusher.snapshot(), "blocked"); got != 1 {
		t.Fatalf("a restart repeated the push: %d", got)
	}

	// Another question is told at once.
	restarted.maybeNotifyForSessionEvent(promptTransition("agent-1", turnID, "running", "blocked", "Q"))
	if got := countKind(pusher.snapshot(), "blocked"); got != 2 {
		t.Fatalf("a new prompt was not pushed: %d", got)
	}

	// The same prompt after the Worker really moved on (answered, ran for a
	// while) is a new ask.
	restarted.maybeNotifyForSessionEvent(promptTransition("agent-1", turnID, "blocked", "running", ""))
	clock = clock.Add(workerPushMovedOn + time.Second)
	restarted.maybeNotifyForSessionEvent(promptTransition("agent-1", turnID, "running", "blocked", "Q"))
	if got := countKind(pusher.snapshot(), "blocked"); got != 3 {
		t.Fatalf("a prompt after the Worker moved on was not pushed: %d", got)
	}
}

func TestFailedPushIsOncePerFailure(t *testing.T) {
	pusher := &recordingNotificationPusher{}
	srv, turnID := ledgerNotificationServer(t, "agent-1")
	srv.pusher = pusher
	for range 10 {
		srv.maybeNotifyForSessionEvent(promptTransition("agent-1", turnID, "running", "failed", ""))
		srv.maybeNotifyForSessionEvent(promptTransition("agent-1", turnID, "failed", "running", ""))
	}
	if got := countKind(pusher.snapshot(), "failed"); got != 1 {
		t.Fatalf("a flickering failure pushed %d times, want 1", got)
	}
}

// A push that reached no phone told nobody: when a phone registers while the
// question is still open, it must still be told.
func TestUndeliveredPushDoesNotUseUpTheQuestion(t *testing.T) {
	pusher := &recordingNotificationPusher{err: push.ErrNoRegistration}
	srv, turnID := ledgerNotificationServer(t, "agent-1")
	srv.pusher = pusher
	srv.maybeNotifyForSessionEvent(promptTransition("agent-1", turnID, "running", "blocked", "P"))
	pusher.err = nil
	srv.maybeNotifyForSessionEvent(promptTransition("agent-1", turnID, "blocked", "running", ""))
	srv.maybeNotifyForSessionEvent(promptTransition("agent-1", turnID, "running", "blocked", "P"))
	if got := countKind(pusher.snapshot(), "blocked"); got != 2 {
		t.Fatalf("pushes = %d, want 2", got)
	}
}

// A prompt filed by the pane gate carries its identity in the event details
// when the worker snapshot has no structured prompt.
func TestPushKeyReadsThePromptFromEventDetails(t *testing.T) {
	event := promptTransition("agent-1", "turn:a", "running", "blocked", "")
	event.Worker.Prompt = nil
	event.Worker.DetailsJSON = `{"event":"prompt_blocked","prompt":{"id":"abc123"}}`
	if got := workerPushKey(event); got != "prompt:abc123" {
		t.Fatalf("key = %q", got)
	}
	event.Worker.DetailsJSON = ""
	if got := workerPushKey(event); got != "turn:turn:a|Waiting for Claude permission" {
		t.Fatalf("fallback key = %q", got)
	}
}
