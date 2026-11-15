package main

import (
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

// A delegated Worker that ended its turn (done, waiting for the person to sign
// in on the page it left open) may no longer act, but its display and browser
// window stay: they go only when its Session closes (main wires workerOpen to
// the displays and the browser).
func TestWorkerDisplayOutlastsItsTurn(t *testing.T) {
	w := newFakeControlWatcher()
	w.workers["mh-done"] = &classifier.Worker{ID: "mh-done", Name: "SignIn", Delegated: true, State: classifier.StateDone}
	w.workers["mh-run"] = &classifier.Worker{ID: "mh-run", Name: "Busy", Delegated: true, State: classifier.StateRunning}
	app := &controlApp{watcher: w}
	if app.liveWorker("mh-done") {
		t.Fatal("a Worker whose turn ended may still act")
	}
	if !app.workerOpen("mh-done") {
		t.Fatal("a Worker whose turn ended lost its display and browser window")
	}
	if !app.workerOpen("mh-run") || !app.liveWorker("mh-run") {
		t.Fatal("a running Worker is not open")
	}
	if app.workerOpen("mh-gone") {
		t.Fatal("a closed Worker kept its display")
	}
	var displays, browsers lifetimeSink
	bindAgentScreenLifetime(&displays, &browsers, app)
	for _, f := range []func(string) bool{displays.f, browsers.f} {
		if f == nil || !f("mh-done") || f("mh-gone") {
			t.Fatal("the display or the browser is not bound to the Worker's lifetime")
		}
	}
}

type lifetimeSink struct{ f func(string) bool }

func (l *lifetimeSink) SetAlive(f func(string) bool)      { l.f = f }
func (l *lifetimeSink) SetWorkerLive(f func(string) bool) { l.f = f }
