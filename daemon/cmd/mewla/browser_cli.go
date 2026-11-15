package main

import (
	"github.com/daoleno/mewla/daemon/browser"
	"github.com/daoleno/mewla/daemon/control"
)

// handleBrowserControl is the daemon's say over each browser tool call a Worker
// makes (see agentuse.Gate): begin, which the person's take over or Stop can
// refuse, and end, which tells the Worker what the person did on its monitor.
// The Worker's browser is a window on its monitor of the agent screen, so the
// say is the screen's: the same Hold and Stop that govern its display.
// Local control is owner-UID access, not a sandbox against arbitrary shell.
// On a Mac the Worker's window is in the person's Chrome, on the agent display,
// and the say is its entry in Screen's list (screen.AgentWindows).
func (a *controlApp) handleBrowserControl(req control.Request) control.Response {
	gate := a.browserGate()
	if gate == nil || req.BrowserRequest == nil || req.WorkerID == "" {
		return control.ErrorResponse("browser_unavailable", "A managed task is required")
	}
	switch req.BrowserRequest.Action {
	case "begin":
		if err := gate.Begin(req.WorkerID); err != nil {
			return control.ErrorResponse("browser_denied", err.Error())
		}
		return control.Response{OK: true, Browser: &browser.Response{}}
	case "end":
		return control.Response{OK: true, Browser: &browser.Response{Notes: gate.End(req.WorkerID)}}
	}
	return control.ErrorResponse("browser_denied", "Agents may only begin and end a browser call")
}

// browserWindowGate is what has the person's say over a Worker's browser calls.
type browserWindowGate interface {
	Begin(worker string) error
	End(worker string) []string
}

// browserGate is the Mac's agent windows when there are any, else the agent
// screen's displays (Linux; elsewhere they let every call through), or nil.
func (a *controlApp) browserGate() browserWindowGate {
	if a.agentWindows != nil {
		return a.agentWindows
	}
	if a.displays != nil {
		return a.displays
	}
	return nil
}
