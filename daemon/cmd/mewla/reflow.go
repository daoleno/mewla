package main

import (
	"errors"
	"image"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/browser"
)

// windowReflower lays an agent's browser window on the agent screen out for a
// phone, through the Mewla extension in the agent browser.
type windowReflower struct {
	bridge  *agentuse.Bridge
	browser *browser.Manager
}

var errNoWindow = errors.New("the agent has no browser window")

func (r windowReflower) Reflow(worker string, at image.Point, width, height int, zoom float64) (image.Rectangle, string, error) {
	key := r.browser.WindowKey(worker)
	if key == "" {
		return image.Rectangle{}, "", errNoWindow
	}
	got, err := r.bridge.Reflow(key, agentuse.WindowBounds{Left: at.X, Top: at.Y, Width: width, Height: height}, zoom)
	if err != nil {
		return image.Rectangle{}, "", err
	}
	c := got.Content
	return image.Rect(c.Left, c.Top, c.Left+c.Width, c.Top+c.Height), got.Host, nil
}

func (r windowReflower) Restore(worker string) error {
	key := r.browser.WindowKey(worker)
	if key == "" {
		return nil // the window is gone: nothing to put back
	}
	_, err := r.bridge.Reflow(key, agentuse.WindowBounds{}, 0)
	return err
}
