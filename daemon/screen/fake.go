package screen

import (
	"context"
	"image"
	"image/color"
	"sync"
	"time"
)

// FakeBackend is a scripted screen for tests and the `screenfake` build. Its
// frame shows the pointer, a square at every click and a block per key, so a
// viewer can see its input arrive.
type FakeBackend struct {
	Width, Height int
	Unsupported   bool
	Missing       []string // ids of permissions not granted
	Answer        bool     // Confirm's answer
	Locked        bool
	// Tree is what Elements reads; nil reads as no accessibility.
	Tree *ElementTree

	mu        sync.Mutex
	changed   chan struct{}
	version   uint64
	pointer   image.Point
	clicks    []image.Point
	wheels    int
	keys      []uint32
	Pointers  []FakePointer
	Keys      []FakeKey
	Opened    []string
	Confirmed []string
	Requested int
	lastInput time.Time
}

// Presence reports the fake's lock and its last input, which every injected
// event updates too, the way a real system may count an agent's events.
func (f *FakeBackend) Presence() Presence {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Presence{Locked: f.Locked, LastInput: f.lastInput}
}

// Elements returns the scripted tree, as a Mac reads the focused window.
func (f *FakeBackend) Elements(context.Context, int) (ElementTree, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Tree == nil {
		return ElementTree{}, ErrNoElements
	}
	return *f.Tree, nil
}

// PersonInput is someone using the computer's own keyboard or pointer.
func (f *FakeBackend) PersonInput() {
	f.mu.Lock()
	f.lastInput = time.Now()
	f.mu.Unlock()
}

// SetLocked locks or unlocks the fake screen.
func (f *FakeBackend) SetLocked(locked bool) {
	f.mu.Lock()
	f.Locked = locked
	f.mu.Unlock()
}

type FakePointer struct {
	X, Y    int
	Buttons uint8
}

type FakeKey struct {
	Keysym uint32
	Down   bool
}

func NewFakeBackend(width, height int) *FakeBackend {
	return &FakeBackend{Width: width, Height: height, Answer: true, changed: make(chan struct{})}
}

func (f *FakeBackend) Capability(context.Context) Capability {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Unsupported {
		return Capability{Reason: ErrUnsupported.Error()}
	}
	perms := []Permission{
		{ID: "screen_recording", Name: "Screen Recording", Granted: true, CanOpen: true},
		{ID: "accessibility", Name: "Accessibility", Granted: true, CanOpen: true},
	}
	for i := range perms {
		for _, id := range f.Missing {
			if perms[i].ID == id {
				perms[i].Granted = false
				perms[i].Guidance = "Allow Mewla in " + perms[i].Name
			}
		}
	}
	return Capability{Supported: true, Permissions: perms}
}

func (f *FakeBackend) RequestPermissions(context.Context) {
	f.mu.Lock()
	f.Requested++
	f.mu.Unlock()
}

func (f *FakeBackend) OpenSettings(_ context.Context, permission string) error {
	f.mu.Lock()
	f.Opened = append(f.Opened, permission)
	f.mu.Unlock()
	return nil
}

func (f *FakeBackend) Confirm(_ context.Context, device string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Confirmed = append(f.Confirmed, device)
	return f.Answer, nil
}

func (f *FakeBackend) Start(context.Context) (Session, error) {
	return &fakeSession{backend: f, image: image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))}, nil
}

// bump marks a change; callers hold f.mu.
func (f *FakeBackend) bump() {
	f.version++
	close(f.changed)
	f.changed = make(chan struct{})
}

type fakeSession struct {
	backend *FakeBackend
	image   *image.RGBA
}

func (s *fakeSession) Frame(ctx context.Context, after uint64) (Frame, error) {
	f := s.backend
	for {
		f.mu.Lock()
		if f.version+1 > after {
			s.draw()
			frame := Frame{Seq: f.version + 1, Image: s.image}
			f.mu.Unlock()
			return frame, nil
		}
		wait := f.changed
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return Frame{}, ctx.Err()
		case <-wait:
		}
	}
}

// draw renders the scene; callers hold backend.mu.
func (s *fakeSession) draw() {
	f := s.backend
	img := s.image
	for y := 0; y < f.Height; y++ {
		for x := 0; x < f.Width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 255 / f.Width), G: 96, B: uint8(y * 255 / f.Height), A: 255})
		}
	}
	square := func(p image.Point, size int, c color.RGBA) {
		r := image.Rect(p.X-size, p.Y-size, p.X+size, p.Y+size).Intersect(img.Rect)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	for _, c := range f.clicks {
		square(c, 12, color.RGBA{R: 255, G: 220, B: 0, A: 255})
	}
	for i, k := range f.keys {
		p := image.Pt(20+(i%40)*18, 20+(i/40)*18)
		square(p, 7, color.RGBA{R: uint8(k * 37), G: 255, B: uint8(k * 91), A: 255})
	}
	for i := 0; i < f.wheels && i < 60; i++ {
		square(image.Pt(f.Width-12, 12+i*12), 5, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	}
	square(f.pointer, 4, color.RGBA{A: 255})
}

func (s *fakeSession) Pointer(x, y int, buttons uint8) error {
	f := s.backend
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Pointers = append(f.Pointers, FakePointer{X: x, Y: y, Buttons: buttons})
	f.lastInput = time.Now()
	f.pointer = image.Pt(x, y)
	if buttons&1 != 0 {
		f.clicks = append(f.clicks, f.pointer)
	}
	if buttons&(8|16|32|64) != 0 {
		f.wheels++
	}
	f.bump()
	return nil
}

func (s *fakeSession) Key(keysym uint32, down bool) error {
	f := s.backend
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Keys = append(f.Keys, FakeKey{Keysym: keysym, Down: down})
	f.lastInput = time.Now()
	if down {
		f.keys = append(f.keys, keysym)
		f.bump()
	}
	return nil
}

func (s *fakeSession) Close() error { return nil }

// Snapshot returns copies of the recorded input, for tests.
func (f *FakeBackend) Snapshot() ([]FakePointer, []FakeKey) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakePointer(nil), f.Pointers...), append([]FakeKey(nil), f.Keys...)
}
