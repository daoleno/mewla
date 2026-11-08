package main

import (
	"errors"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/browser"
	"github.com/daoleno/mewla/daemon/control"
)

// controlGate asks the daemon about each browser tool call of a Worker. It
// lets a call through when the daemon cannot be reached: the browser needs
// the daemon anyway, and a call that cannot be told the person's say is no
// reason to fail.
type controlGate struct {
	cfg    cliConfig
	worker string
}

// newBrowserGate is the gate for a Worker, or nil for a process that is not one.
func newBrowserGate(stateDir, worker string) agentuse.Gate {
	if worker == "" {
		return nil
	}
	return controlGate{cfg: cliConfig{stateDir: stateDir}, worker: worker}
}

func (g controlGate) call(action string) (*browser.Response, error) {
	resp, err := callControl(g.cfg, control.Request{Type: "browser", WorkerID: g.worker, BrowserRequest: &browser.Request{Action: action}})
	if err != nil {
		return nil, nil
	}
	if !resp.OK {
		// Only the person's say (take over, Stop) refuses a call; a daemon that
		// cannot answer about the browser lets it through, as one that is not there.
		if resp.Error != nil && resp.Error.Code == "browser_denied" {
			return nil, errors.New(resp.Error.Message)
		}
		return nil, nil
	}
	return resp.Browser, nil
}

func (g controlGate) Begin() error {
	_, err := g.call("begin")
	return err
}

func (g controlGate) End() []string {
	r, _ := g.call("end")
	if r == nil {
		return nil
	}
	return r.Notes
}
