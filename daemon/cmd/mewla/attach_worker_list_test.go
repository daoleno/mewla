package main

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/watcher"
)

// terminalTargetWatcher answers where each Session's terminal is, as the real
// watcher does.
type terminalTargetWatcher struct {
	*fakeControlWatcher
	targets map[string]watcher.TerminalTarget
}

func (w terminalTargetWatcher) TerminalTarget(id string) (watcher.TerminalTarget, bool) {
	target, ok := w.targets[id]
	return target, ok
}

// worker_list says which terminal backend holds each Session, and for tmux
// which server, so `mewla attach` can open a Session left in tmux.
func TestControlAppWorkerListNamesTerminalBackend(t *testing.T) {
	fw := newFakeControlWatcher()
	at := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	fw.workers["%7"] = &classifier.Worker{ID: "%7", Name: "InTmux", State: classifier.StateRunning, UpdatedAt: at}
	fw.workers["mh-aaa"] = &classifier.Worker{ID: "mh-aaa", Name: "OnHost", State: classifier.StateRunning, UpdatedAt: at.Add(-time.Minute)}
	fw.workers["x"] = &classifier.Worker{ID: "x", Name: "Unowned", State: classifier.StateRunning, UpdatedAt: at.Add(-2 * time.Minute)}
	app := &controlApp{watcher: terminalTargetWatcher{fw, map[string]watcher.TerminalTarget{
		"%7":     {Backend: "tmux", Socket: "/tmp/tmux-1000/default"},
		"mh-aaa": {Backend: "host", Socket: "/run/user/1000/mewla/host.sock"},
	}}}

	resp := app.HandleControlRequest(control.Request{Type: "worker_list"})

	if !resp.OK || len(resp.Workers) != 3 {
		t.Fatalf("response = %#v", resp)
	}
	byID := map[string]control.Worker{}
	for _, worker := range resp.Workers {
		byID[worker.ID] = worker
	}
	if w := byID["%7"]; w.Terminal != "tmux" || w.TmuxSocket != "/tmp/tmux-1000/default" {
		t.Fatalf("tmux Session = %+v", w)
	}
	if w := byID["mh-aaa"]; w.Terminal != "host" || w.TmuxSocket != "" {
		t.Fatalf("host Session = %+v", w)
	}
	if w := byID["x"]; w.Terminal != "" || w.TmuxSocket != "" {
		t.Fatalf("unowned Session = %+v", w)
	}
}
