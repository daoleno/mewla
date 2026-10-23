package main

import (
	"context"
	"testing"

	"github.com/daoleno/mewla/daemon/agentevents"
)

func TestCodexSendLeavesAWorkerWithoutAConnectedThreadToThePane(t *testing.T) {
	events := newCodexEvents(context.Background(), agentevents.NewHub())
	handled, err := events.send("%1", "hello", "r-1")
	if handled || err != nil {
		t.Fatalf("send to an unknown Worker = (%v, %v), want it left to the pane", handled, err)
	}
	events.sessions["%1"] = &codexSession{workerID: "%1"}
	handled, err = events.send("%1", "hello", "r-1")
	if handled || err != nil {
		t.Fatalf("send to a Worker not yet connected = (%v, %v), want it left to the pane", handled, err)
	}
}
