//go:build linux

package vdisplay

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func newService(t *testing.T) *Service {
	t.Helper()
	if err := Capability(); err != nil {
		t.Skip(err)
	}
	s := NewService(t.TempDir())
	t.Cleanup(s.Close)
	return s
}

// viewerInput sends the person's messages through a Watch view.
func viewerInput(t *testing.T, v *Viewer, msgs ...[]byte) {
	t.Helper()
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- v.Relay(r) }()
	if _, err := w.Write([]byte{1}); err != nil { // ClientInit
		t.Fatal(err)
	}
	for _, m := range msgs {
		if _, err := w.Write(m); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	if err := <-done; err != io.EOF {
		t.Fatalf("relay: %v", err)
	}
}

func TestAgentOpensAndSeesItsDisplay(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Act(ctx, "w1", Action{Action: "screenshot"}); err == nil {
		t.Fatal("a screenshot before open should say there is no display")
	}
	r, err := s.Act(ctx, "w1", Action{Action: "open", Command: []string{"xmessage", "-center", "hello"}})
	if err != nil || len(r.Image) < 1000 || !strings.Contains(r.Text, "xmessage") {
		t.Fatalf("open: %v %d %q", err, len(r.Image), r.Text)
	}
	list := s.List()
	if len(list) != 1 || list[0].ID != "w1" || list[0].Control != "agent" || list[0].Apps != 1 {
		t.Fatalf("list = %+v", list)
	}
	if _, err := s.Act(ctx, "w1", Action{Action: "click", X: 5000, Y: 5}); err == nil {
		t.Fatal("a click outside the display was accepted")
	}
}

func TestPersonInputIsToldNotEnforced(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Act(ctx, "w1", Action{Action: "open", Command: []string{"xmessage", "hello"}}); err != nil {
		t.Fatal(err)
	}
	v, err := s.View("w1", false)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	viewerInput(t, v, pointer(1, 40, 40), pointer(0, 40, 40), key(true, 'h'), key(true, 'i'), key(true, 0xff0d))
	r, err := s.Act(ctx, "w1", Action{Action: "screenshot"})
	if err != nil {
		t.Fatalf("the agent was stopped by the person's input: %v", err)
	}
	for _, want := range []string{"1 click", "2 characters typed (not shown to you)", "1 other key", "read the picture again"} {
		if !strings.Contains(r.Text, want) {
			t.Fatalf("notes missing %q: %s", want, r.Text)
		}
	}
	if strings.Contains(r.Text, "hi") && strings.Contains(r.Text, "typed hi") {
		t.Fatal("typed content leaked")
	}
	again, err := s.Act(ctx, "w1", Action{Action: "screenshot"})
	if err != nil || strings.Contains(again.Text, "The person") {
		t.Fatalf("told twice: %v %q", err, again.Text)
	}
}

func TestTakeOverStopAndHandBack(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Act(ctx, "w1", Action{Action: "open", Command: []string{"xmessage", "hello"}}); err != nil {
		t.Fatal(err)
	}
	v, err := s.View("w1", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Act(ctx, "w1", Action{Action: "screenshot"}); err == nil || !strings.Contains(err.Error(), "took over") {
		t.Fatalf("err = %v", err)
	}
	if got := s.List()[0].Control; got != "person" {
		t.Fatalf("control = %q", got)
	}
	time.Sleep(2100 * time.Millisecond)
	v.Close()
	r, err := s.Act(ctx, "w1", Action{Action: "screenshot"})
	if err != nil || !strings.Contains(r.Text, "handed it back") {
		t.Fatalf("hand back: %v %q", err, r.Text)
	}
	// Stop holds the agent off until the person takes over and hands back.
	if err := s.Halt("w1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Act(ctx, "w1", Action{Action: "screenshot"}); err == nil || !strings.Contains(err.Error(), "stopped you") {
		t.Fatalf("err = %v", err)
	}
	v, err = s.View("w1", true)
	if err != nil {
		t.Fatal(err)
	}
	v.Close()
	if _, err := s.Act(ctx, "w1", Action{Action: "screenshot"}); err != nil {
		t.Fatalf("after take over and hand back: %v", err)
	}
}

func TestTwoWorkersGetTwoDisplays(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	for _, w := range []string{"a", "b"} {
		if _, err := s.Act(ctx, w, Action{Action: "open", Command: []string{"xmessage", w}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.List()) != 2 {
		t.Fatalf("list = %+v", s.List())
	}
	if _, err := s.Act(ctx, "a", Action{Action: "close"}); err != nil {
		t.Fatal(err)
	}
	if l := s.List(); len(l) != 1 || l[0].ID != "b" {
		t.Fatalf("list = %+v", l)
	}
}
