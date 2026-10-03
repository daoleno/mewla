package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/daoleno/zen/daemon/browser"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	"github.com/google/uuid"
)

type brokerBackend struct{}

func (brokerBackend) Capability(context.Context) browser.Capability {
	return browser.Capability{Available: true}
}
func (brokerBackend) Start(context.Context, string) (browser.Runtime, error) {
	return brokerRuntime{}, nil
}

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
func TestBrowserBrokerRequiresExplicitResourceGrantAndActiveTask(t *testing.T) {
	m, err := browser.New(t.TempDir(), brokerBackend{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	human := browser.Owner{Kind: "human", ID: "owner"}
	ctx := context.Background()
	r, _ := m.Handle(ctx, human, browser.Request{Action: "create", Name: "Persistent"})
	id := r.Resource.ID
	m.Handle(ctx, human, browser.Request{Action: "start", ID: id})
	w := newFakeControlWatcher()
	w.workers["task"] = &classifier.Worker{ID: "task", BrowserID: id, State: classifier.StateRunning, Delegated: true}
	app := &controlApp{browsers: m, watcher: w}
	q := control.Request{Type: "browser", WorkerID: "task", Client: uuid.NewString(), BrowserRequest: &browser.Request{Action: "control", ID: id}}
	// New browsers are shared with same-server Agents; an explicit revocation still blocks.
	m.Handle(ctx, human, browser.Request{Action: "grant", ID: id, AllowAgents: false})
	if app.HandleControlRequest(q).OK {
		t.Fatal("revoked profile attached")
	}
	m.Handle(ctx, human, browser.Request{Action: "grant", ID: id, AllowAgents: true})
	w.workers["task"].BrowserID = uuid.NewString()
	if app.HandleControlRequest(q).OK {
		t.Fatal("task attached a granted but unselected resource")
	}
	w.workers["task"].BrowserID = id
	attached := app.HandleControlRequest(q)
	if !attached.OK {
		t.Fatal(attached.Error)
	}
	q.BrowserRequest = &browser.Request{Action: "command", ID: id, Lease: attached.Browser.Lease, Command: &browser.Command{Kind: "snapshot"}}
	w.workers["task"].State = classifier.StateDone
	if app.HandleControlRequest(q).OK {
		t.Fatal("completed task still read")
	}
	list, _ := m.Handle(ctx, human, browser.Request{Action: "list"})
	if list.Resources[0].State != "running" || list.Resources[0].Control != "idle" {
		t.Fatal("task completion did not preserve browser/release control")
	}
	// Quiet manual sessions must use process ownership, not terminal paint.
	w.workers["task"].Delegated = false
	q.BrowserRequest = &browser.Request{Action: "control", ID: id}
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

	q.BrowserRequest = &browser.Request{Action: "delete", ID: id}
	if app.HandleControlRequest(q).OK {
		t.Fatal("Agent deleted user profile")
	}
}
