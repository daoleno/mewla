package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/daoleno/mewla/daemon/browser"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/google/uuid"
)

type brokerBackend struct{}

func (brokerBackend) Capability(context.Context) browser.Capability {
	return browser.Capability{Available: true}
}
func (brokerBackend) Start(context.Context, string) (browser.Host, error) { return brokerHost{}, nil }

type brokerHost struct{}

func (brokerHost) Alive() bool                                     { return true }
func (brokerHost) Close() error                                    { return nil }
func (brokerHost) Window(context.Context) (browser.Runtime, error) { return brokerRuntime{}, nil }

type brokerRuntime struct{}

func (brokerRuntime) Target() string                { return "tab" }
func (brokerRuntime) Alive() bool                   { return true }
func (brokerRuntime) Close() error                  { return nil }
func (brokerRuntime) Release(context.Context) error { return nil }
func (brokerRuntime) Stream(context.Context) (browser.Stream, error) {
	return nil, browser.ErrUnavailable
}
func (brokerRuntime) Command(context.Context, browser.Command) (json.RawMessage, error) {
	return json.RawMessage("{}"), nil
}

// An agent gets a window of its own on the first request, needs no id, and
// loses its lease when its task is no longer active.
func TestBrowserBrokerOpensAWindowForAnActiveTaskOnly(t *testing.T) {
	m, err := browser.New(t.TempDir(), brokerBackend{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	human := browser.Owner{Kind: "human", ID: "owner"}
	ctx := context.Background()
	w := newFakeControlWatcher()
	w.workers["task"] = &classifier.Worker{ID: "task", State: classifier.StateRunning, Delegated: true}
	app := &controlApp{browsers: m, watcher: w}
	q := control.Request{Type: "browser", WorkerID: "task", Client: uuid.NewString(), BrowserRequest: &browser.Request{Action: "control"}}
	// With the virtual screen off no browser starts for agents.
	if app.HandleControlRequest(q).OK {
		t.Fatal("a browser started with the virtual screen off")
	}
	on := true
	if _, err := m.Handle(ctx, human, browser.Request{Action: "settings", Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	attached := app.HandleControlRequest(q)
	if !attached.OK {
		t.Fatal(attached.Error)
	}
	q.BrowserRequest = &browser.Request{Action: "command", Lease: attached.Browser.Lease, Command: &browser.Command{Kind: "snapshot"}}
	w.workers["task"].State = classifier.StateDone
	if app.HandleControlRequest(q).OK {
		t.Fatal("completed task still read")
	}
	list, _ := m.Handle(ctx, human, browser.Request{Action: "list"})
	if len(list.Resources) != 1 || list.Resources[0].State != "running" || list.Resources[0].Control != "idle" {
		t.Fatalf("task completion did not preserve its window and release control: %+v", list.Resources)
	}
	// Quiet manual sessions must use process ownership, not terminal paint.
	w.workers["task"].Delegated = false
	q.BrowserRequest = &browser.Request{Action: "control"}
	if !app.HandleControlRequest(q).OK {
		t.Fatal("quiet live manual session denied")
	}
	w.ownershipErr = errors.New("process generation replaced")
	if app.HandleControlRequest(q).OK {
		t.Fatal("unproved process generation accepted")
	}
	list, _ = m.Handle(ctx, human, browser.Request{Action: "list"})
	if list.Resources[0].Control != "idle" {
		t.Fatal("lost process ownership retained lease")
	}
	for _, action := range []string{"start", "stop", "delete", "grant", "create"} {
		q.BrowserRequest = &browser.Request{Action: action}
		if app.HandleControlRequest(q).OK {
			t.Fatalf("Agent ran %s", action)
		}
	}
}
