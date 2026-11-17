//go:build linux

package vdisplay

import (
	"context"
	"encoding/binary"
	"image"
	"io"
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

	// A viewer that takes DesktopSize.
	in, out := io.Pipe()
	go func() { _ = v.Relay(in) }()
	_, _ = out.Write([]byte{1})
	enc := []byte{2, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(enc[8:], uint32(0xFFFFFF21))
	_, _ = out.Write(enc)
	head := make([]byte, 24+len("Mewla agent display"))
	if _, err := io.ReadFull(v.Conn, head); err != nil {
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
	// The viewer is told the page's size.
	req := []byte{3, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(req[6:], 1082)
	binary.BigEndian.PutUint16(req[8:], 1995)
	_, _ = out.Write(req)
	if w, h := readDesktopSize(t, v.Conn); w != 1082 || h != 1995 {
		t.Fatalf("the viewer was told %dx%d", w, h)
	}
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
	_, _ = out.Write(req)
	if w, h := readDesktopSize(t, v.Conn); w != MonitorWidth || h != MonitorHeight {
		t.Fatalf("the viewer was told %dx%d after letting go", w, h)
	}
}

// readDesktopSize reads updates until one says the screen's size.
func readDesktopSize(t *testing.T, r io.Reader) (int, int) {
	t.Helper()
	for range 50 {
		head := make([]byte, 4)
		if _, err := io.ReadFull(r, head); err != nil {
			t.Fatal(err)
		}
		if head[0] != 0 {
			t.Fatalf("message %d", head[0])
		}
		for range int(binary.BigEndian.Uint16(head[2:])) {
			rect := make([]byte, 12)
			if _, err := io.ReadFull(r, rect); err != nil {
				t.Fatal(err)
			}
			w, h := int(binary.BigEndian.Uint16(rect[4:])), int(binary.BigEndian.Uint16(rect[6:]))
			if int32(binary.BigEndian.Uint32(rect[8:])) == -223 {
				return w, h
			}
			if _, err := io.ReadFull(r, make([]byte, w*h*4)); err != nil { // Raw
				t.Fatal(err)
			}
		}
	}
	t.Fatal("no DesktopSize")
	return 0, 0
}
