package main

import (
	"context"
	"image"
	"testing"

	"github.com/daoleno/mewla/daemon/browser"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/screen"
	"github.com/daoleno/mewla/daemon/vdisplay"
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
	// Linux's displays and agent browser, and a Mac's agent windows.
	var displays, browsers, windows lifetimeSink
	bindAgentScreenLifetime(app, &browsers, &displays, &windows)
	for _, f := range []func(string) bool{displays.f, browsers.f, windows.f} {
		if f == nil || !f("mh-done") || f("mh-gone") {
			t.Fatal("the display, the browser or the Mac's agent windows are not bound to the Worker's lifetime")
		}
	}
}

// A Worker's browser calls answer to its window's say: a Mac's agent windows
// when the computer has them, else the agent screen's displays.
func TestBrowserCallsAnswerToTheMacsAgentWindows(t *testing.T) {
	app := &controlApp{}
	if app.browserGate() != nil {
		t.Fatal("a gate with neither")
	}
	app.displays = vdisplay.NewService("")
	defer app.displays.Close()
	if app.browserGate() != browserWindowGate(app.displays) {
		t.Fatal("the agent screen's displays are not the gate")
	}
	fake := screen.NewFakeBackend(8, 8)
	fake.AgentDisplay = true
	app.agentWindows = screen.NewServiceWith(t.TempDir(), agentDisplayBackend{fake}).AgentWindows()
	if app.agentWindows == nil || app.browserGate() != browserWindowGate(app.agentWindows) {
		t.Fatal("the Mac's agent windows are not the gate")
	}
	app.agentWindows.Opened("c1-1", "mh-signin", image.Rect(0, 0, 800, 500))
	if err := app.agentWindows.Halt("mh-signin"); err != nil {
		t.Fatal(err)
	}
	if resp := app.handleBrowserControl(browserRequest("mh-signin", "begin")); resp.OK || resp.Error == nil || resp.Error.Code != "browser_denied" {
		t.Fatalf("a stopped Worker's browser call was let through: %+v", resp)
	}
	if resp := app.handleBrowserControl(browserRequest("mh-other", "begin")); !resp.OK {
		t.Fatalf("a Worker with no window was refused: %+v", resp)
	}
}

// agentDisplayBackend is a scripted computer with an agent display (a Mac's).
type agentDisplayBackend struct{ *screen.FakeBackend }

func (agentDisplayBackend) ViewAgentDisplay(context.Context, string) (screen.Session, image.Rectangle, func(), error) {
	return nil, image.Rectangle{}, nil, context.Canceled
}
func (agentDisplayBackend) AgentWindowAt(image.Rectangle) (int, screen.AppWindow, bool) {
	return 0, screen.AppWindow{}, false
}
func (agentDisplayBackend) FocusWindow(int, screen.AppWindow) error { return nil }

func browserRequest(worker, action string) control.Request {
	return control.Request{Type: "browser", WorkerID: worker, BrowserRequest: &browser.Request{Action: action}}
}

type lifetimeSink struct{ f func(string) bool }

func (l *lifetimeSink) SetAlive(f func(string) bool)      { l.f = f }
func (l *lifetimeSink) SetWorkerLive(f func(string) bool) { l.f = f }
