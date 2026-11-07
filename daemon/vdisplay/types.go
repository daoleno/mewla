package vdisplay

import (
	"io"
	"net"
)

// Action is one thing an agent asks of its display.
type Action struct {
	// Action is open, screenshot, click, double_click, move, scroll, type, key
	// or close.
	Action  string   `json:"action"`
	Command []string `json:"command,omitempty"`
	X       int      `json:"x,omitempty"`
	Y       int      `json:"y,omitempty"`
	Button  string   `json:"button,omitempty"`
	Text    string   `json:"text,omitempty"`
	Key     string   `json:"key,omitempty"`
	// DX and DY are wheel notches: positive DY scrolls down.
	DX int `json:"dx,omitempty"`
	DY int `json:"dy,omitempty"`
}

// Result is what comes back to the agent: the picture after the action, and
// what it needs to know.
type Result struct {
	Image []byte `json:"image,omitempty"` // JPEG
	Text  string `json:"text,omitempty"`
}

// Info describes one agent's display for the list in Screen.
type Info struct {
	// ID is the Worker the display belongs to.
	ID      string `json:"id"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Control string `json:"control"` // agent or person
	Stopped bool   `json:"stopped,omitempty"`
	Apps    int    `json:"apps"`
}

// Viewer is a person's connection to a Worker's display.
type Viewer struct {
	// Conn is the display's RFB stream, past the handshake with no security:
	// the next bytes in either direction are ClientInit and then updates.
	Conn net.Conn
	// Relay passes the person's messages to the display and counts them.
	Relay func(src io.Reader) error
	// Close ends the view and, for a take over, gives the display back.
	Close func()
}
