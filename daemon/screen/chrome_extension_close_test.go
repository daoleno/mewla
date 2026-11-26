package screen

import (
	"context"
	"errors"
	"testing"
)

// closingBackend is a backend that can close a window by its number.
type closingBackend struct {
	*FakeBackend
	closed []uint32
	err    error
}

func (b *closingBackend) WindowNumber(Element) (uint32, error) { return 0, errors.New("unused") }

func (b *closingBackend) CloseWindow(_ context.Context, pid int, number uint32) error {
	if pid != 7 {
		return errors.New("another app")
	}
	b.closed = append(b.closed, number)
	return b.err
}

// The owner's Mac, 11-27: while a Worker held Chrome, every close Mewla asked of
// its own extension window was refused as "Another agent … is working in that
// app", and the window stayed. By its number, it closes with no agent at all.
func TestMewlasChromeWindowClosesByItsNumberWithoutAnAgent(t *testing.T) {
	backend := &closingBackend{FakeBackend: NewFakeBackend(100, 100)}
	s := NewServiceWith(t.TempDir(), backend)
	if err := s.closeMewlasWindow(context.Background(), 7, 4242, nil); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(backend.closed) != 1 || backend.closed[0] != 4242 {
		t.Fatalf("closed %v, want [4242]", backend.closed)
	}
	if len(s.agents) != 0 {
		t.Fatalf("an agent was started to close it: %v", s.agents)
	}

	backend.err = errors.New("its close button refused (accessibility error -25204)")
	err := s.closeMewlasWindow(context.Background(), 7, 4242, nil)
	if err == nil || err.Error() != "window 4242: its close button refused (accessibility error -25204)" {
		t.Fatalf("a close that failed says why: %v", err)
	}
}
