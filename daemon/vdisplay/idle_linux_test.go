//go:build linux

package vdisplay

import (
	"context"
	"testing"
	"time"
)

// The list says how long the agent has been away from its display, so the app floats a screen
// that is in use and lets go of one that is not.
func TestListSaysHowLongTheAgentHasBeenIdle(t *testing.T) {
	s := newService(t)
	if _, err := s.Act(context.Background(), "w1", Action{Action: "open", Command: []string{"xmessage", "-center", "hello"}}); err != nil {
		t.Fatal(err)
	}
	if got := s.List()[0].IdleS; got > 2 {
		t.Fatalf("idle right after an action = %ds", got)
	}
	w := s.live("w1")
	w.mu.Lock()
	w.used = time.Now().Add(-90 * time.Second)
	w.mu.Unlock()
	if got := s.List()[0].IdleS; got < 90 || got > 92 {
		t.Fatalf("idle after 90s = %ds", got)
	}
	// An action that is running is not idle however long ago it began.
	if err := w.begin(); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	w.used = time.Now().Add(-90 * time.Second)
	w.mu.Unlock()
	if got := s.List()[0].IdleS; got != 0 {
		t.Fatalf("idle while acting = %ds", got)
	}
	w.end()
}
