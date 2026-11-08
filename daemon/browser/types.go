// Package browser runs the one browser an agent works in on a computer's
// virtual screen: one profile, one Chrome, a window for each agent, and the
// leases that decide who is typing in a window.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrUnavailable = errors.New("Browser runtime is unavailable")
	ErrLease       = errors.New("Browser control changed; request control again")
	ErrBusy        = errors.New("Browser is controlled by someone else")
	ErrUncertain   = errors.New("Browser action could not be drained; its window is opened again on the next request for control")
)

type Capability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}
type Owner struct {
	Kind string
	ID   string
}
type Lease struct {
	Epoch      uint64    `json:"epoch"`
	Generation string    `json:"generation"`
	Target     string    `json:"target"`
	Kind       string    `json:"kind"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// Resource is one agent's window, as the Screen lists it.
type Resource struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	State      string    `json:"state"`
	Generation string    `json:"generation,omitempty"`
	Control    string    `json:"control"`
	Target     string    `json:"target,omitempty"`
	// Agent is the Worker whose window it is.
	Agent string `json:"agent,omitempty"`
}
type Request struct {
	Action  string   `json:"action"`
	ID      string   `json:"id,omitempty"`
	Lease   *Lease   `json:"lease,omitempty"`
	Command *Command `json:"command,omitempty"`
	// Enabled is the new value for the "settings" action.
	Enabled *bool `json:"enabled,omitempty"`
}
type Response struct {
	Resources  []Resource      `json:"resources,omitempty"`
	Resource   *Resource       `json:"resource,omitempty"`
	Capability *Capability     `json:"capability,omitempty"`
	Lease      *Lease          `json:"lease,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	// Notes tell an agent what the person did in the browser since its last
	// action, so it can judge for itself whether to go on, wait or ask.
	Notes []string `json:"notes,omitempty"`
	// VirtualScreen is whether Workers get a browser on the virtual screen.
	VirtualScreen *bool `json:"virtual_screen,omitempty"`
}
type Command struct {
	Kind   string `json:"kind"`
	URL    string `json:"url,omitempty"`
	Target string `json:"target,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Text   string `json:"text,omitempty"`
	Input  *Input `json:"input,omitempty"`
}
type Input struct {
	Kind   string  `json:"kind"`
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	DX     float64 `json:"dx,omitempty"`
	DY     float64 `json:"dy,omitempty"`
	Button string  `json:"button,omitempty"`
	Key    string  `json:"key,omitempty"`
	Text   string  `json:"text,omitempty"`
}

// Stream frames are acknowledged by the renderer, never by the proxy on receipt.
type Stream interface {
	Read() ([]byte, error)
	Ack(uint64) error
	Close() error
}

// Runtime is one agent's window: its page, input and live view.
type Runtime interface {
	Target() string
	Command(context.Context, Command) (json.RawMessage, error)
	Release(context.Context) error
	Stream(context.Context) (Stream, error)
	Alive() bool
	Close() error
}

// Host is the computer's one browser on the one agent profile, with a private
// display. Windows are opened and closed on it without restarting it.
type Host interface {
	Window(context.Context) (Runtime, error)
	Alive() bool
	Close() error
}
type Backend interface {
	Capability(context.Context) Capability
	// Start brings the browser up on the profile directory and waits for it.
	Start(ctx context.Context, profile string) (Host, error)
}
