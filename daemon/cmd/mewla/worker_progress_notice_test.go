package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
)

type noticingProgressWatcher struct {
	*fakeControlWatcher
	notice string
	err    error
}

func (w *noticingProgressWatcher) ReportWorkerProgress(id string, progress classifier.WorkerProgress) (*classifier.Worker, string, error) {
	if w.err != nil {
		return nil, "", w.err
	}
	worker, err := w.fakeControlWatcher.UpdateWorkerProgress(id, progress)
	return worker, w.notice, err
}

// A Worker must see why a report was refused and which turn to use, and must be
// told when its report was recorded on a different turn than the one it named.
func TestControlAppWorkerProgressTellsTheWorkerWhyAndWhatToUse(t *testing.T) {
	const id = "mewla-worker-worker:@1"
	fw := newFakeControlWatcher()
	fw.workers[id] = &classifier.Worker{ID: id, Name: "Worker", State: classifier.StateRunning, Delegated: true}
	request := control.Request{
		Type: "worker_progress", WorkerID: id, TurnID: "turn:one",
		Status: "blocked", Phase: "working", Attention: "user_input", Summary: "ask",
	}

	refusal := "progress report refused: turn turn:one was superseded by a newer prompt. The Session's current delegated turn is turn:two: report with --turn-id turn:two."
	app := &controlApp{watcher: &noticingProgressWatcher{fakeControlWatcher: fw, err: errors.New(refusal)}}
	resp := app.HandleControlRequest(request)
	if resp.OK || resp.Error == nil || resp.Error.Code != "progress_failed" || resp.Error.Message != refusal {
		t.Fatalf("refused response = %#v", resp)
	}

	notice := "Turn turn:one was superseded by turn turn:two; use --turn-id turn:two."
	app = &controlApp{watcher: &noticingProgressWatcher{fakeControlWatcher: fw, notice: notice}}
	resp = app.HandleControlRequest(request)
	if !resp.OK || !strings.Contains(resp.Confirmation, "--turn-id turn:two") {
		t.Fatalf("redirected response = %#v", resp)
	}
}
