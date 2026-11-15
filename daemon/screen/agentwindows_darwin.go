//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"image"
)

// ViewAgentDisplay holds the agent display under key and starts a capture of
// it; release lets the hold go. Its session's input reaches one app, in the
// background (ClickTo, KeyTo, ScrollTo), at the display's own pixels.
func (b *macBackend) ViewAgentDisplay(ctx context.Context, key string) (Session, image.Rectangle, func(), error) {
	bounds, err := b.HoldAgentDisplay(key)
	if err != nil {
		return nil, image.Rectangle{}, nil, err
	}
	d := &b.agentDisp
	d.mu.Lock()
	var id uint32
	if d.vd != nil {
		id = d.vd.ID
	}
	d.mu.Unlock()
	if id == 0 {
		b.ReleaseAgentDisplay(key)
		return nil, image.Rectangle{}, nil, errors.New("the agent display is gone")
	}
	session, err := b.startOn(ctx, id)
	if err != nil {
		b.ReleaseAgentDisplay(key)
		return nil, image.Rectangle{}, nil, err
	}
	return session, bounds, func() { b.ReleaseAgentDisplay(key) }, nil
}

// AgentWindowAt is the front browser window on rect of the agent display.
func (b *macBackend) AgentWindowAt(rect image.Rectangle) (int, AppWindow, bool) {
	d := &b.agentDisp
	d.mu.Lock()
	display := d.bounds
	present := d.vd != nil
	d.mu.Unlock()
	if !present {
		return 0, AppWindow{}, false
	}
	onScreen, err := windowInfos(cgWindowListOnScreenOnly | cgWindowListExcludeDesktop)
	if err != nil {
		return 0, AppWindow{}, false
	}
	return agentWindowAt(onScreen, display, rect, func(pid int) bool { return appBundle(pid).chromium })
}

// WindowPlace is where the browser window placed at rect really is.
func (b *macBackend) WindowPlace(rect image.Rectangle) (image.Rectangle, bool) {
	d := &b.agentDisp
	d.mu.Lock()
	display := d.bounds
	present := d.vd != nil
	d.mu.Unlock()
	if !present {
		return image.Rectangle{}, false
	}
	onScreen, err := windowInfos(cgWindowListOnScreenOnly | cgWindowListExcludeDesktop)
	if err != nil {
		return image.Rectangle{}, false
	}
	return windowPlace(onScreen, display, rect, func(pid int) bool { return appBundle(pid).chromium })
}

// FocusWindow makes window its app's key window without raising it.
func (b *macBackend) FocusWindow(pid int, window AppWindow) error {
	k := loadSkyLight()
	if k.why != "" {
		return errors.New(k.why)
	}
	return k.focusWithoutRaise(pid, window.ID)
}

// ScrollTo scrolls one notch in a window of the app with pid, at framebuffer
// pixel (x, y), without the pointer: the event carries the window's routing
// fields, as a background click's do.
func (s *macSession) ScrollTo(pid int, window AppWindow, x, y, dx, dy int) error {
	k := loadSkyLight()
	if k.why != "" {
		return errors.New(k.why)
	}
	px, py := s.input.point(image.Pt(x, y))
	event := mac.CGEventCreateScrollWheelEvent2(s.source0, cgScrollUnitLine, 2, int32(-dy*3), int32(-dx*3), 0)
	if event == 0 {
		return errors.New("couldn't make the scroll event")
	}
	defer mac.CFRelease(event)
	mac.CGEventSetLocation(event, cgPoint{px, py})
	k.setField(event, fieldTargetPID, int64(pid))
	k.setField(event, fieldWindowNumber, int64(window.ID))
	k.setField(event, fieldWindowUnder, int64(window.ID))
	k.setField(event, fieldWindowUnderCan, int64(window.ID))
	k.post(int32(pid), event)
	return nil
}

var (
	_ agentDisplayViewer = (*macBackend)(nil)
	_ appScroller        = (*macSession)(nil)
)
