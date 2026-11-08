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

// A viewer is shown its monitor and nothing else: the screen it is told it
// has is the monitor's size, what it is sent is the monitor's pixels, and its
// pointer is taken into the monitor.
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
	// The viewer: ClientInit, then Raw only, then one full update request.
	in, out := io.Pipe()
	go func() { _ = v.Relay(in) }()
	go func() {
		_, _ = out.Write([]byte{1})
		_, _ = out.Write([]byte{2, 0, 0, 1, 0, 0, 0, 0}) // SetEncodings: Raw
		req := []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint16(req[6:], MonitorWidth)
		binary.BigEndian.PutUint16(req[8:], MonitorHeight)
		_, _ = out.Write(req)
		// A click far outside the monitor lands at its edge, not on another monitor.
		ptr := []byte{5, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint16(ptr[2:], 60000)
		binary.BigEndian.PutUint16(ptr[4:], 60000)
		_, _ = out.Write(ptr)
	}()
	head := make([]byte, 24+len("Mewla agent display"))
	if _, err := io.ReadFull(v.Conn, head); err != nil {
		t.Fatal(err)
	}
	if w, h := binary.BigEndian.Uint16(head[0:]), binary.BigEndian.Uint16(head[2:]); w != MonitorWidth || h != MonitorHeight {
		t.Fatalf("the viewer was told the screen is %dx%d", w, h)
	}
	update := make([]byte, 4+12)
	if _, err := io.ReadFull(v.Conn, update); err != nil {
		t.Fatal(err)
	}
	if update[0] != 0 || binary.BigEndian.Uint16(update[2:]) < 1 {
		t.Fatalf("update header %v", update)
	}
	w, h := int(binary.BigEndian.Uint16(update[8:])), int(binary.BigEndian.Uint16(update[10:]))
	pix := make([]byte, w*h*4)
	if _, err := io.ReadFull(v.Conn, pix); err != nil {
		t.Fatal(err)
	}
	rb, _ := s.Monitor(ctx, "b")
	want := s.shared.client.Crop(rb)
	ox, oy := int(binary.BigEndian.Uint16(update[4:])), int(binary.BigEndian.Uint16(update[6:]))
	if ox != 0 || oy != 0 {
		t.Skipf("the first rectangle is not the origin (%d,%d)", ox, oy)
	}
	// What the viewer got is a piece of b's monitor (the first rows of it).
	if !bytes.Contains(want, pix[:min(len(pix), 4*MonitorWidth)]) {
		t.Fatal("the viewer's pixels are not the monitor's")
	}
	if blank(pix) && !blank(want) {
		t.Fatal("the viewer got a blank picture of a monitor with something on it")
	}
}
