// Package browser owns persistent user browsers and fences managed tool access.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrUnavailable = errors.New("Browser runtime is unavailable")
	ErrLease       = errors.New("Browser control changed; request control again")
	ErrBusy        = errors.New("Browser is controlled by someone else")
	ErrUncertain   = errors.New("Browser action could not be drained; stop and reopen this browser")
	// ErrPersonInControl and ErrHalted say why in words an agent can act on:
	// the person has the browser (wait, don't poll) or ended this agent's use.
	ErrPersonInControl = fmt.Errorf("%w: the person took over this browser and was told you are waiting. Do other work or wait a while, then request control again; do not poll", ErrBusy)
	ErrHalted          = fmt.Errorf("%w: the person stopped you using this browser. Do not request it again; tell them you stopped and what is left", ErrBusy)
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
type Resource struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	AllowAgents bool      `json:"allow_agents"`
	State       string    `json:"state"`
	Generation  string    `json:"generation,omitempty"`
	Control     string    `json:"control"`
	Target      string    `json:"target,omitempty"`
	// Agent is the Worker that holds the browser, empty when none does.
	Agent string `json:"agent,omitempty"`
}
type Request struct {
	Action      string   `json:"action"`
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name,omitempty"`
	AllowAgents bool     `json:"allow_agents,omitempty"`
	Lease       *Lease   `json:"lease,omitempty"`
	Command     *Command `json:"command,omitempty"`
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
type Runtime interface {
	Target() string
	Command(context.Context, Command) (json.RawMessage, error)
	Release(context.Context) error
	Stream(context.Context) (Stream, error)
	Alive() bool
	Close() error
}
type Backend interface {
	Capability(context.Context) Capability
	Start(context.Context, string) (Runtime, error)
}
