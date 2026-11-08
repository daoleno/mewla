// Package browser runs the one browser an agent works in on a computer: one
// profile, one Chrome, started on the agent screen (package vdisplay) with
// Mewla's extension. The extension opens a window for each agent and puts it
// on that agent's monitor; the screen shows it, the person takes it over and
// stops it like any other program an agent runs there.
package browser

import (
	"context"
	"errors"
)

var ErrUnavailable = errors.New("Browser runtime is unavailable")

type Capability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Request is what the app and the Workers' tools ask of the manager.
type Request struct {
	// Action is "settings" (the app), or "begin" and "end" around one browser
	// tool call of a Worker.
	Action string `json:"action"`
	// Enabled is the new value for the "settings" action.
	Enabled *bool `json:"enabled,omitempty"`
}

type Response struct {
	Capability *Capability `json:"capability,omitempty"`
	// Notes tell an agent what the person did in the browser since its last
	// action, so it can judge for itself whether to go on, wait or ask.
	Notes []string `json:"notes,omitempty"`
	// VirtualScreen is whether Workers get a browser on the virtual screen.
	VirtualScreen *bool `json:"virtual_screen,omitempty"`
}

// Host is the computer's one browser on the one agent profile, running on
// the agent screen with Mewla's extension loaded.
type Host interface {
	Alive() bool
	Close() error
}

type Backend interface {
	Capability(context.Context) Capability
	// Start brings the browser up on the profile directory and waits for it.
	Start(ctx context.Context, profile string) (Host, error)
}
