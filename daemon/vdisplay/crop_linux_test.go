//go:build linux

package vdisplay

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"testing"
	"time"
)

// A viewer is shown its monitor and nothing else: its pictures are the
// monitor's size and pixels, and its pointer is taken into the monitor.
func TestViewerSeesItsMonitor(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	for _, w := range []string{"a", "b"} {
		if _, err := s.Act(ctx, w, Action{Action: "open", Command: []string{"xmessage", "-center", w}}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(2 * time.Second)
	v, err := s.View("b")
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	p, err := v.Next(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Width != MonitorWidth || p.Height != MonitorHeight || p.Stride != MonitorWidth*4 {
		t.Fatalf("the view is %dx%d (stride %d)", p.Width, p.Height, p.Stride)
	}
	rb, _ := s.Monitor(ctx, "b")
	want := s.shared.client.Crop(rb)
	if !bytes.Equal(p.Pix, want) {
		t.Fatal("the view's pixels are not the monitor's")
	}
	if blank(p.Pix) {
		t.Fatal("the view is blank on a monitor with something on it")
	}
	// A click far outside the monitor lands at its edge, not on another monitor.
	in, out := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- v.Input(in) }()
	ptr := []byte{5, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(ptr[2:], 60000)
	binary.BigEndian.PutUint16(ptr[4:], 60000)
	_, _ = out.Write(ptr)
	_ = out.Close()
	if err := <-done; err != io.EOF {
		t.Fatalf("input: %v", err)
	}
}
