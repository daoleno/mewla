package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/browser"
	"github.com/daoleno/mewla/daemon/control"
)

type brokerBackend struct{}

func (brokerBackend) Capability(context.Context) browser.Capability {
	return browser.Capability{Available: true}
}
func (brokerBackend) Start(context.Context, string) (browser.Host, error) { return brokerHost{}, nil }

type brokerHost struct{}

func (brokerHost) Alive() bool  { return true }
func (brokerHost) Close() error { return nil }
func (brokerHost) Window(context.Context, string) (browser.Runtime, error) {
	return brokerRuntime{}, nil
}

type brokerRuntime struct{}

func (brokerRuntime) Target() string                { return "tab" }
func (brokerRuntime) SetTarget(string)              {}
func (brokerRuntime) Alive() bool                   { return true }
func (brokerRuntime) Close() error                  { return nil }
func (brokerRuntime) Release(context.Context) error { return nil }
func (brokerRuntime) Page(context.Context) string   { return "" }
func (brokerRuntime) Command(context.Context, browser.Command) (json.RawMessage, error) {
	return json.RawMessage("{}"), nil
}
func (brokerRuntime) Input(context.Context, browser.Input) error {
	return nil
}
func (brokerRuntime) Stream(context.Context) (browser.Stream, error) {
	return nil, browser.ErrUnavailable
}

// An agent's browser tool calls are let through, and only begin and end are
// the agent's to ask; a window outlives its task's last call (it closes with
// the Worker's session, not with a classification of the task).
func TestBrowserGateLetsACallThroughAndOnlyBeginAndEndAreAsked(t *testing.T) {
	m, err := browser.New(t.TempDir(), brokerBackend{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx := context.Background()
	if err := m.EnsureHost(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Opened(ctx, "task", "c1", "tab"); err != nil {
		t.Fatal(err)
	}
	app := &controlApp{browsers: m}
	call := func(worker, action string) control.Response {
		return app.HandleControlRequest(control.Request{Type: "browser", WorkerID: worker, BrowserRequest: &browser.Request{Action: action}})
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
	// The person's Stop refuses the Worker, with the reason, and only that Worker.
	list, _ := m.Handle(ctx, browser.Owner{Kind: "human", ID: "owner"}, browser.Request{Action: "list"})
	if _, err := m.Handle(ctx, browser.Owner{Kind: "human", ID: "owner"}, browser.Request{Action: "halt", ID: list.Resources[0].ID}); err != nil {
		t.Fatal(err)
	}
	if r := call("task", "begin"); r.OK || r.Error == nil || !strings.Contains(r.Error.Message, "stopped you") {
		t.Fatalf("a stopped Worker: %+v", r)
	}
	if r := call("other", "begin"); !r.OK {
		t.Fatal("another Worker was refused")
	}
	for _, action := range []string{"start", "stop", "delete", "grant", "create", "control", "command", "list"} {
		if call("other", action).OK {
			t.Fatalf("Agent ran %s", action)
		}
	}
}
