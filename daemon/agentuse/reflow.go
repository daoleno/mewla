package agentuse

import (
	"errors"
	"fmt"
	"time"
)

// Reflow: while the person holds an agent's window from a phone, the window is
// laid out for the phone. The extension makes it the phone's shape (a real
// window, content bounds in the screen's pixels) and zooms its tab to the
// phone's pixel ratio, so the page lays itself out at the phone's width and is
// drawn at its sharpness; restoring puts back the window's bounds and zoom.
// The page sees what a person zooming a desktop window would make it see.

// reflowVersion is the first extension version that reflows a window.
const reflowVersion = "0.4.0"

// ErrNoReflow means the extension in Chrome predates reflow.
var ErrNoReflow = errors.New("the Mewla extension in Chrome is too old to lay a window out for the phone")

// reflowTimeout bounds one reflow: a window change and a zoom.
const reflowTimeout = 6 * time.Second

// Reflowed is the window as the extension left it.
type Reflowed struct {
	// Content is the page's area in the screen's pixels, the part the person sees.
	Content WindowBounds
	// Host is the host name of the page in the window's active tab.
	Host string
}

// Reflow lays the window with connection id out for a screen: its page's
// content takes content (the screen's pixels), zoomed by zoom. zoom 0 restores
// the window to its place and its zoom to the page's own.
func (b *Bridge) Reflow(id string, content WindowBounds, zoom float64) (Reflowed, error) {
	b.mu.Lock()
	host := b.host
	if _, agent := b.open[id]; agent {
		host = b.agentHost
	}
	if host == nil {
		b.mu.Unlock()
		return Reflowed{}, ErrNoExtensionHost
	}
	if versionOlder(host.version, reflowVersion) {
		b.mu.Unlock()
		return Reflowed{}, ErrNoReflow
	}
	b.nextID++
	req := fmt.Sprintf("r%s-%d", b.run, b.nextID)
	ch := make(chan hostMessage, 1)
	if b.replies == nil {
		b.replies = map[string]chan hostMessage{}
	}
	b.replies[req] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.replies, req)
		b.mu.Unlock()
	}()
	msg := hostMessage{Type: "reflow", ID: id, Req: req, Zoom: zoom}
	if zoom > 0 {
		bounds := content
		msg.Bounds = &bounds
	}
	if err := host.send(msg); err != nil {
		return Reflowed{}, err
	}
	select {
	case m := <-ch:
		if m.Message != "" {
			return Reflowed{}, errors.New(m.Message)
		}
		out := Reflowed{Host: m.Host}
		if m.Bounds != nil {
			out.Content = *m.Bounds
		}
		return out, nil
	case <-time.After(reflowTimeout):
		return Reflowed{}, errors.New("the Mewla extension did not lay the window out in time")
	}
}

// reflowReply hands an extension's reflowed answer to the Reflow waiting for it.
func (b *Bridge) reflowReply(m hostMessage) {
	b.mu.Lock()
	ch := b.replies[m.Req]
	b.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- m:
	default:
	}
}

// pageHost hears a reflowed window's page change host.
func (b *Bridge) pageHost(m hostMessage) {
	if b.PageHost != nil {
		b.PageHost(m.ID, m.Host)
	}
}
