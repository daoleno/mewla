package main

import (
	"context"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/browser"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/vdisplay"
)

// An agent's browser tool calls are let through, and only begin and end are
// the agent's to ask. Its browser is a window on its monitor of the agent
// screen, so the person's Stop on that monitor refuses the calls, with the
// reason, and only that Worker's.
func TestBrowserGateLetsACallThroughAndOnlyBeginAndEndAreAsked(t *testing.T) {
	if err := vdisplay.Capability(); err != nil {
		t.Skip(err)
	}
	svc := vdisplay.NewService("")
	defer svc.Close()
	ctx := context.Background()
	app := &controlApp{displays: svc}
	call := func(worker, action string) control.Response {
		return app.HandleControlRequest(control.Request{Type: "browser", WorkerID: worker, BrowserRequest: &browser.Request{Action: action}})
	}
	// No window yet: the Worker's first tool call opens one, it is not refused.
	if r := call("task", "begin"); !r.OK {
		t.Fatal(r.Error)
	}
	if _, err := svc.Monitor(ctx, "task"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Monitor(ctx, "other"); err != nil {
		t.Fatal(err)
	}
	if r := call("task", "begin"); !r.OK {
		t.Fatal(r.Error)
	}
	if r := call("task", "end"); !r.OK {
		t.Fatal(r.Error)
	}
	if r := call("", "begin"); r.OK {
		t.Fatal("a call without a Worker was accepted")
	}
	if err := svc.Halt("task"); err != nil {
		t.Fatal(err)
	}
	if r := call("task", "begin"); r.OK || r.Error == nil || !strings.Contains(r.Error.Message, "stopped you") {
		t.Fatalf("a stopped Worker: %+v", r)
	}
	if r := call("other", "begin"); !r.OK {
		t.Fatal("another Worker was refused")
	}
	for _, action := range []string{"start", "stop", "delete", "grant", "create", "control", "command", "list", "settings"} {
		if call("other", action).OK {
			t.Fatalf("Agent ran %s", action)
		}
	}
}
