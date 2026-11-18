package vdisplay

import (
	"image"
	"io"
	"net"
	"time"
)

// IdleClose is how long a monitor may go unused (by the agent, or by the
// person, who also keeps it by having it open in Screen) before it closes.
const IdleClose = time.Hour

// WhyIdle is why Mewla closed a window nobody used for IdleClose, in the words
// the person and the agent are told.
const WhyIdle = "nobody used it for an hour"

// WindowNote is what an agent hears once its browser window is open: who sees
// it, and how long it lasts.
const WindowNote = "The person sees this browser window in Mewla's Screen, under your name, and can take it over there. It stays open after your turn ends: until your Session closes, the person closes it, or nobody uses it for an hour. To hand the person a page (a sign-in, a code, a captcha, a payment), leave it open and tell them; your Work card in Mewla has a Screen button that opens it."

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
	ID string `json:"id"`
	// Name is the Worker's name, when the daemon knows it.
	Name    string `json:"name,omitempty"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Control string `json:"control"` // agent or person
	Stopped bool   `json:"stopped,omitempty"`
	Apps    int    `json:"apps"`
	// Browser is how many of the agent's browser windows are on the display.
	Browser int `json:"browser,omitempty"`
	// IdleS is the seconds since the agent last used the display (0 while it acts).
	IdleS int `json:"idle_s"`
}

// Reflow is how an agent's browser window is shown to the person who holds it:
// laid out for their screen (On), with the page's host, or as the agent left it.
type Reflow struct {
	On   bool   `json:"on"`
	Host string `json:"host,omitempty"`
}

// Reflower lays a Worker's browser window out for the person's screen and
// back: the browser's side of a reflow (the extension in the agent browser).
// Reflow puts the window's top left at at, its page's content width x height
// pixels, zoomed by zoom, and answers where the page's content is on the
// screen and its host. Restore puts the window back on its monitor.
type Reflower interface {
	Reflow(worker string, at image.Point, width, height int, zoom float64) (content image.Rectangle, host string, err error)
	Restore(worker string) error
}

// Viewer is a person's connection to a Worker's display.
type Viewer struct {
	// Conn is the display's RFB stream, past the handshake with no security:
	// the next bytes in either direction are ClientInit and then updates.
	Conn net.Conn
	// Picture is what the view shows now, as B G R X rows, and its size; Watch
	// is told when it may have changed. Nil where the display has no picture
	// of its own to give (its view is RFB only).
	Picture func() ([]byte, int, int)
	Watch   func() (<-chan struct{}, func())
	// Relay passes the person's messages to the display and counts them.
	Relay func(src io.Reader) error
	// Hold holds the agent off (true) or lets it go on (false). A held view
	// that closes lets go by itself.
	Hold func(on bool)
	// Close ends the view and lets go of a hold.
	Close func()
	// Fit lays the agent's browser window out for the person's screen, width x
	// height points at dpr pixels a point, while this view holds the display;
	// width 0 puts it back. It answers what the view now shows; Notify hears
	// later changes (the page's host, the window put back).
	Fit func(width, height int, dpr float64) Reflow
	// Notify, when set, hears every change of how the display is shown.
	Notify func(func(Reflow))
}
