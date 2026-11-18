//go:build linux

package vdisplay

import (
	"context"
	"image"
	"sync"
	"testing"
	"time"
)

func TestReflowSize(t *testing.T) {
	for _, c := range []struct {
		width, height int
		dpr           float64
		w, h          int
		zoom          float64
		ok            bool
	}{
		// A phone: its own sharpness.
		{412, 760, 2.625, 1082, 1995, 2.625, true},
		// A tall phone: as sharp as the screen's height allows.
		{412, 870, 2.625, 1061, 2240, 2240.0 / 870, true},
		// A tablet wider than the monitor's width at its ratio: the monitor's width.
		{800, 1200, 2, 1280, 1920, 1.6, true},
		// Wider than the monitor at 1x: the monitor's width, 1x.
		{1600, 900, 1, 1280, 900, 1, true},
		// A phone that says 1x: zoomed to the narrowest window Chrome makes.
		{412, 760, 1, 500, 922, 500.0 / 412, true},
		{200, 800, 2, 0, 0, 0, false},
		{412, 870, 0, 0, 0, 0, false},
	} {
		w, h, zoom, ok := reflowSize(c.width, c.height, c.dpr)
		if ok != c.ok || w != c.w || h != c.h || (ok && (zoom-c.zoom > 1e-9 || c.zoom-zoom > 1e-9)) {
			t.Errorf("reflowSize(%d, %d, %v) = %d, %d, %v, %v; want %d, %d, %v, %v", c.width, c.height, c.dpr, w, h, zoom, ok, c.w, c.h, c.zoom, c.ok)
		}
	}
	if h := layoutHeight(nil); h != MonitorHeight {
		t.Errorf("layoutHeight() = %d", h)
	}
	if h := layoutHeight([]int{0, 2155}); h != 2155 {
		t.Errorf("layoutHeight(2155) = %d", h)
	}
	if h := layoutHeight([]int{9000}); h != maxReflowHeight {
		t.Errorf("layoutHeight(9000) = %d", h)
	}
}

type fakeReflower struct {
	mu       sync.Mutex
	calls    []string
	restored int
}

func (f *fakeReflower) Reflow(worker string, at image.Point, width, height int, zoom float64) (image.Rectangle, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, worker)
	// The window's page starts below its toolbar.
	return image.Rect(at.X, at.Y+80, at.X+width, at.Y+80+height), "example.com", nil
}

func (f *fakeReflower) Restore(worker string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restored++
	return nil
}

// Taking over a browser display lays its window out for the person's phone:
// the screen grows tall enough for it, the view shows the window's page and a
// viewer that can be told a new size is; letting go puts all of it back.
func TestTakeoverLaysTheWindowOutForThePhone(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Monitor(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	f := &fakeReflower{}
	s.SetReflower(f)
	v, err := s.View("w1")
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	heard := make(chan Reflow, 8)
	v.Notify(func(r Reflow) { heard <- r })

	// No browser window: nothing to lay out.
	v.Hold(true)
	if r := v.Fit(412, 760, 2.625); r.On {
		t.Fatal("a display with no browser window was laid out")
	}
	s.Windows("w1", 1)
	// Not held: Fit does nothing.
	v.Hold(false)
	if r := v.Fit(412, 760, 2.625); r.On || len(f.calls) != 0 {
		t.Fatal("a display that was not held was laid out")
	}

	first, err := v.Next(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	v.Hold(true)
	r := v.Fit(412, 760, 2.625)
	if !r.On || r.Host != "example.com" {
		t.Fatalf("Fit = %+v", r)
	}
	if got := <-heard; !got.On {
		t.Fatalf("heard %+v", got)
	}
	// Another view that does not hold the display is told how it is and changes nothing.
	other, err := s.View("w1")
	if err != nil {
		t.Fatal(err)
	}
	if r := other.Fit(0, 0, 0); !r.On || f.restored != 0 {
		t.Fatalf("another view's Fit = %+v, restored %d", r, f.restored)
	}
	other.Close()
	if f.restored != 0 {
		t.Fatal("another view closing put the window back")
	}
	if _, h := s.shared.client.Size(); h < 80+1995 {
		t.Fatalf("the screen is %d tall, the window's page reaches %d", h, 80+1995)
	}
	// The view's pictures are the page, all of it.
	page := pictureSized(t, v, first.Seq, 1082, 1995)
	s.PageHost("w1", "accounts.example.com")
	if got := <-heard; got.Host != "accounts.example.com" {
		t.Fatalf("heard %+v after the page moved", got)
	}

	// Letting go puts it back.
	v.Hold(false)
	if got := <-heard; got.On {
		t.Fatalf("heard %+v after letting go", got)
	}
	if f.restored != 1 {
		t.Fatalf("restored %d times", f.restored)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, h := s.shared.client.Size(); h == MonitorHeight {
			break
		}
		if time.Now().After(deadline) {
			_, h := s.shared.client.Size()
			t.Fatalf("the screen stayed %d tall", h)
		}
		time.Sleep(50 * time.Millisecond)
	}
	pictureSized(t, v, page.Seq, MonitorWidth, MonitorHeight)
}

// pictureSized waits for the view's picture to be w x h.
func pictureSized(t *testing.T, v *Viewer, after uint64, w, h int) Picture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		p, err := v.Next(ctx, after)
		if err != nil {
			t.Fatalf("waiting for a %dx%d picture: %v", w, h, err)
		}
		if p.Width == w && p.Height == h {
			return p
		}
		after = p.Seq
	}
}
