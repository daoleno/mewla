package main

import (
	"context"
	"time"

	"github.com/daoleno/mewla/daemon/browser"
	"github.com/daoleno/mewla/daemon/control"
)

// handleBrowserControl is the daemon's say over each browser tool call a Worker
// makes (see agentuse.Gate): begin, which the person's take over or Stop can
// refuse, and end, which tells the Worker what the person did in its window.
// Local control is owner-UID access, not a sandbox against arbitrary shell.
func (a *controlApp) handleBrowserControl(req control.Request) control.Response {
	if a.browsers == nil || req.BrowserRequest == nil || req.WorkerID == "" {
		return control.ErrorResponse("browser_unavailable", "A managed task is required")
	}
	q := *req.BrowserRequest
	if q.Action != "begin" && q.Action != "end" {
		return control.ErrorResponse("browser_denied", "Agents may only begin and end a browser call")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	result, err := a.browsers.Handle(ctx, browser.Owner{Kind: "agent", ID: req.WorkerID}, q)
	if err != nil {
		return control.ErrorResponse("browser_denied", err.Error())
	}
	return control.Response{OK: true, Browser: &result}
}
