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
func (a *controlApp) handleBrowserControl(req control.Request) control.Response {
	if a.displays == nil || req.BrowserRequest == nil || req.WorkerID == "" {
		return control.ErrorResponse("browser_unavailable", "A managed task is required")
	}
	switch req.BrowserRequest.Action {
	case "begin":
		if err := a.displays.Begin(req.WorkerID); err != nil {
			return control.ErrorResponse("browser_denied", err.Error())
		}
		return control.Response{OK: true, Browser: &browser.Response{}}
	case "end":
		return control.Response{OK: true, Browser: &browser.Response{Notes: a.displays.End(req.WorkerID)}}
	}
	return control.ErrorResponse("browser_denied", "Agents may only begin and end a browser call")
}
