package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// A consumer that stops reading events must not stall polls, progress writes
// or session creation, and the events it later reads keep their order.
func TestEventPublicationNeverWaitsOnTheConsumer(t *testing.T) {
	w := New(time.Second)
	w.events = make(chan SessionEvent, 1)
	restore := installFakePollSeams(w, testWindows(), map[string]string{"sess-a:@1": contentA, "sess-b:@2": contentB}, liveSessionProcesses())
	defer restore()

	promptly := func(name string, run func()) {
		t.Helper()
		done := make(chan struct{})
		go func() { defer close(done); run() }()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("%s waited on an event consumer that stopped reading", name)
		}
	}
	promptly("poll", w.poll)
	promptly("UpdateWorkerProgress", func() {
		if _, err := w.UpdateWorkerProgress("sess-a:@1", classifier.WorkerProgress{Status: "running", Phase: "working", Attention: "none", Summary: "progress while stalled"}); err != nil {
			t.Error(err)
		}
	})
	promptly("registerCreatedSession", func() {
		w.registerCreatedSession("new:@9", "/new", CreateSessionOptions{Name: "new"}, time.Now().UTC())
	})
	promptly("second poll", w.poll)

	want := []struct{ kind, id string }{
		{"worker_discovered", "sess-a:@1"},
		{"worker_discovered", "sess-b:@2"},
		{"worker_state_change", "sess-a:@1"},
		{"worker_discovered", "new:@9"},
	}
	for _, expected := range want {
		select {
		case event := <-w.Events():
			if event.Type != expected.kind || event.WorkerID != expected.id {
				t.Fatalf("event=%s %s, want %s %s", event.Type, event.WorkerID, expected.kind, expected.id)
			}
		case <-time.After(time.Second):
			t.Fatalf("queued event %s %s was never delivered", expected.kind, expected.id)
		}
	}
}
