package main

import (
	"context"
	"errors"
	"testing"

	"github.com/daoleno/mewla/daemon/agentevents"
)

func TestCodexStatusNeedsAnAppServerConnection(t *testing.T) {
	follow := newCodexEvents(context.Background(), agentevents.NewHub())
	if _, err := follow.Status(context.Background(), "%9"); !errors.Is(err, ErrNoCodexStatus) {
		t.Fatalf("a Worker with no session: %v", err)
	}
	// A session that has not connected, or has no thread yet, has nothing to ask.
	follow.sessions["%9"] = &codexSession{workerID: "%9"}
	if _, err := follow.Status(context.Background(), "%9"); !errors.Is(err, ErrNoCodexStatus) {
		t.Fatalf("a session with no connection: %v", err)
	}
}
