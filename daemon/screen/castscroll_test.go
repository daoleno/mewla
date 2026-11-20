package screen

import (
	"context"
	"image"
	"testing"
)

type recordingSession struct {
	pointers [][3]int
	scrolls  [][4]int
	fine     bool
}

func (r *recordingSession) Frame(context.Context, uint64) (Frame, error) {
	return Frame{Image: image.NewRGBA(image.Rect(0, 0, 1, 1))}, nil
}
func (r *recordingSession) Pointer(x, y int, buttons uint8) error {
	r.pointers = append(r.pointers, [3]int{x, y, int(buttons)})
	return nil
}
func (r *recordingSession) Key(uint32, bool) error { return nil }
func (r *recordingSession) Close() error           { return nil }

type fineSession struct{ recordingSession }

func (f *fineSession) Scroll(x, y, dx, dy int) error {
	f.scrolls = append(f.scrolls, [4]int{x, y, dx, dy})
	return nil
}

// A screen that scrolls by pixels gets them as they are.
func TestCastScrollByPixels(t *testing.T) {
	s := &fineSession{}
	c := &Cast{session: s}
	if !c.FineScroll() {
		t.Fatal("a pixel-scrolling session is not offered as one")
	}
	_ = c.Scroll(10, 20, 0, 7)
	if len(s.scrolls) != 1 || s.scrolls[0] != [4]int{10, 20, 0, 7} || len(s.pointers) != 0 {
		t.Fatalf("scrolls %v pointers %v", s.scrolls, s.pointers)
	}
}

// One that only has notches gets one per 120 pixels, the rest kept, with the
// held buttons kept held.
func TestCastScrollAsNotches(t *testing.T) {
	s := &recordingSession{}
	c := &Cast{session: s}
	if c.FineScroll() {
		t.Fatal("a notch-only session is offered as pixel scrolling")
	}
	_ = c.Input([]byte{5, 1, 0, 10, 0, 20})
	s.pointers = nil
	_ = c.Scroll(10, 20, 0, 100)
	if len(s.pointers) != 0 {
		t.Fatalf("a notch before 120 pixels: %v", s.pointers)
	}
	_ = c.Scroll(10, 20, 0, 150)
	if len(s.pointers) != 4 || s.pointers[0] != [3]int{10, 20, 1 | 16} || s.pointers[1] != [3]int{10, 20, 1} {
		t.Fatalf("pointers = %v", s.pointers)
	}
	s.pointers = nil
	_ = c.Scroll(10, 20, -130, 0)
	if len(s.pointers) != 2 || s.pointers[0][2] != 1|32 {
		t.Fatalf("left: %v", s.pointers)
	}
}

// A phone's capture leaves the cursor out; any other keeps it.
func TestWithoutCursor(t *testing.T) {
	if !cursorShown(context.Background()) || cursorShown(withoutCursor(context.Background())) {
		t.Fatal("cursorShown")
	}
}
