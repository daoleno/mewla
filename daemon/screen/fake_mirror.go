package screen

import (
	"context"
	"image"
	"time"
)

// FakeMirrorInput is one action sent to a fake app the way iPhone use sends
// it: a hold, a swipe, key strokes or a paste.
type FakeMirrorInput struct {
	Kind    string // hold, swipe, strokes, paste
	PID     int
	X, Y    int
	DX, DY  int
	Hold    time.Duration
	How     SwipeHow
	Strokes []KeyStroke
	Text    string
}

func (s *fakeSession) record(in FakeMirrorInput) error {
	f := s.backend
	f.mu.Lock()
	defer f.mu.Unlock()
	f.MirrorInputs = append(f.MirrorInputs, in)
	if f.AppEventsSeen {
		f.input[InputClick] = time.Now()
	}
	return nil
}

func (s *fakeSession) HoldTo(pid int, window AppWindow, x, y int, hold time.Duration) error {
	return s.record(FakeMirrorInput{Kind: "hold", PID: pid, X: x, Y: y, Hold: hold})
}

func (s *fakeSession) SwipeTo(pid int, window AppWindow, x, y, dx, dy int, how SwipeHow) error {
	return s.record(FakeMirrorInput{Kind: "swipe", PID: pid, X: x, Y: y, DX: dx, DY: dy, How: how})
}

func (s *fakeSession) StrokesTo(pid int, window AppWindow, strokes []KeyStroke) error {
	return s.record(FakeMirrorInput{Kind: "strokes", PID: pid, Strokes: strokes})
}

func (s *fakeSession) PasteTo(pid int, window AppWindow, text string) error {
	return s.record(FakeMirrorInput{Kind: "paste", PID: pid, Text: text})
}

// ReadText answers with the fake's TextLines, as if read off the picture.
func (f *FakeBackend) ReadText(_ context.Context, img *image.RGBA) ([]TextLine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.TextReads++
	return append([]TextLine(nil), f.TextLines...), nil
}

// MirrorInstalled: the fake has iPhone Mirroring unless NoMirror is set.
func (f *FakeBackend) MirrorInstalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.NoMirror
}

var (
	_ mirrorHolder = (*FakeBackend)(nil)
	_ mirrorInput  = (*fakeSession)(nil)
	_ textReader   = (*FakeBackend)(nil)
)
