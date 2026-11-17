//go:build linux

package vdisplay

import (
	"image"
	"log"
	"math"
)

// The most a reflowed window may take: its monitor's width, and a screen this
// tall (Xvnc keeps one framebuffer for the whole screen, so the height is the
// memory every monitor's column pays while one is reflowed).
const (
	maxReflowHeight = 2400
	// reflowChrome is room left above the page for the window's own tabs and toolbar.
	reflowChrome = 160
	// minReflowWidth is the narrowest window Chrome makes: a phone's page is zoomed to at least this wide.
	minReflowWidth = 500
)

// reflowSize is the window's page size in pixels and its zoom for a person's
// screen of width x height points at dpr pixels a point: the phone's own
// sharpness where the screen has room, less where it has not. ok is false for
// a screen too small to lay a page out on.
func reflowSize(width, height int, dpr float64) (w, h int, zoom float64, ok bool) {
	if width < 240 || height < 240 || dpr <= 0 || math.IsNaN(dpr) || math.IsInf(dpr, 0) {
		return 0, 0, 0, false
	}
	zoom = math.Min(dpr, 4)
	zoom = math.Min(zoom, float64(MonitorWidth)/float64(width))
	zoom = math.Min(zoom, float64(maxReflowHeight-reflowChrome)/float64(height))
	zoom = math.Max(zoom, 1)
	zoom = math.Max(zoom, math.Min(float64(minReflowWidth)/float64(width), float64(MonitorWidth)/float64(width)))
	// A screen wider than the monitor at 1x: lay the page out on the whole monitor instead.
	w = min(int(math.Round(float64(width)*zoom)), MonitorWidth)
	h = min(int(math.Round(float64(height)*zoom)), maxReflowHeight-reflowChrome)
	return w, h, zoom, true
}

// layoutHeight is the screen's height for its displays: the monitors', or more
// while a reflowed window reaches below them.
func layoutHeight(bottoms []int) int {
	h := MonitorHeight
	for _, b := range bottoms {
		h = max(h, b)
	}
	return min(h, maxReflowHeight)
}

// viewRect is the part of the screen a person's view of a display shows: the
// reflowed window's page while there is one, else the monitor.
func viewRect(monitor, reflowed image.Rectangle) image.Rectangle {
	if !reflowed.Empty() {
		return reflowed
	}
	return monitor
}

// reflow lays the display's browser window out for a person's screen of width
// x height points at dpr pixels a point, and says what the views show now.
func (s *Service) reflow(w *workerDisplay, width, height int, dpr float64) Reflow {
	s.mu.Lock()
	r := s.reflower
	s.mu.Unlock()
	w.mu.Lock()
	browser := w.windows > 0
	w.mu.Unlock()
	pw, ph, zoom, ok := reflowSize(width, height, dpr)
	if r == nil || !browser || !ok {
		return Reflow{}
	}
	s.starting.Lock()
	w.mu.Lock()
	w.reflowBottom = ph + reflowChrome
	w.mu.Unlock()
	err := s.relayout(w.screen)
	s.starting.Unlock()
	if err != nil {
		log.Printf("display: couldn't make room to lay out %s's window: %v", w.worker, err)
		s.endReflow(w)
		return Reflow{}
	}
	content, host, err := r.Reflow(w.worker, image.Pt(w.rect.Min.X, 0), pw, ph, zoom)
	if err != nil || content.Empty() {
		if err != nil {
			log.Printf("display: couldn't lay out %s's window for the phone: %v", w.worker, err)
		}
		s.endReflow(w)
		return Reflow{}
	}
	log.Printf("display: laid %s's window out for a %dx%d view at %.3fx: page %v", w.worker, width, height, zoom, content)
	w.mu.Lock()
	w.reflowed = content.Intersect(image.Rect(w.rect.Min.X, 0, w.rect.Max.X, maxReflowHeight))
	w.reflowHost = host
	w.mu.Unlock()
	state := Reflow{On: true, Host: host}
	s.tell(w, state)
	return state
}

// endReflow puts a laid-out window back on its monitor, if it was laid out.
func (s *Service) endReflow(w *workerDisplay) {
	w.mu.Lock()
	was := !w.reflowed.Empty() || w.reflowBottom > 0
	w.reflowed = image.Rectangle{}
	w.reflowHost = ""
	w.reflowBottom = 0
	w.mu.Unlock()
	if !was {
		return
	}
	s.mu.Lock()
	r := s.reflower
	s.mu.Unlock()
	if r != nil {
		if err := r.Restore(w.worker); err != nil {
			log.Printf("display: couldn't put %s's window back: %v", w.worker, err)
		}
	}
	s.starting.Lock()
	if err := s.relayout(w.screen); err != nil {
		log.Printf("display: couldn't lay the agent screen out again: %v", err)
	}
	s.starting.Unlock()
	s.tell(w, Reflow{})
}

// PageHost hears that the page in a Worker's laid-out window moved to another host.
func (s *Service) PageHost(worker, host string) {
	s.mu.Lock()
	w := s.displays[worker]
	s.mu.Unlock()
	if w == nil {
		return
	}
	w.mu.Lock()
	on := !w.reflowed.Empty()
	if on {
		w.reflowHost = host
	}
	w.mu.Unlock()
	if on {
		s.tell(w, Reflow{On: true, Host: host})
	}
}

func (s *Service) tell(w *workerDisplay, state Reflow) {
	w.mu.Lock()
	var listeners []func(Reflow)
	for _, f := range w.notify {
		listeners = append(listeners, f)
	}
	w.mu.Unlock()
	for _, f := range listeners {
		f(state)
	}
}
