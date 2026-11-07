package vdisplay

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func key(down bool, sym uint32) []byte {
	b := []byte{4, 0, 0, 0, 0, 0, 0, 0}
	if down {
		b[1] = 1
	}
	binary.BigEndian.PutUint32(b[4:], sym)
	return b
}

func pointer(mask byte, x, y uint16) []byte {
	b := []byte{5, mask, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(b[2:], x)
	binary.BigEndian.PutUint16(b[4:], y)
	return b
}

func TestRelayCountsAndNeverKeepsContent(t *testing.T) {
	var in bytes.Buffer
	in.Write(key(true, 'p'))
	in.Write(key(false, 'p'))
	in.Write(key(true, 0xffe1)) // Shift: a key, not a character
	in.Write(key(true, 0xff0d)) // Enter
	in.Write(key(true, 0x20ac)) // a Unicode character (Euro)
	in.Write(key(true, 0x010020ac))
	in.Write(pointer(0, 10, 10))
	in.Write(pointer(1, 10, 10)) // press
	in.Write(pointer(1, 12, 12)) // drag: still the same press
	in.Write(pointer(0, 12, 12))
	in.Write(pointer(8, 12, 12)) // wheel up
	in.Write(pointer(0, 12, 12))
	in.Write([]byte{3, 1, 0, 0, 0, 0, 0, 1, 0, 1}) // update request, passed on, not input
	cut := []byte{6, 0, 0, 0, 0, 0, 0, 4}
	in.Write(append(cut, []byte("hunt")...))
	var out bytes.Buffer
	var total Input
	err := Relay(&out, &in, func(i Input) { total = total.Add(i) })
	if err != io.EOF {
		t.Fatalf("err = %v", err)
	}
	want := Input{Clicks: 1, Scrolls: 1, Keys: 5, Typed: 2, Pastes: 1}
	if total != want {
		t.Fatalf("counts = %+v, want %+v", total, want)
	}
	if out.Len() == 0 {
		t.Fatal("nothing reached the display")
	}
}

func TestRelayStopsOnUnknownMessage(t *testing.T) {
	in := bytes.NewReader(append(key(true, 'a'), 99, 1, 2, 3))
	var out bytes.Buffer
	if err := Relay(&out, in, nil); err == nil || err == io.EOF {
		t.Fatalf("err = %v", err)
	}
	if out.Len() != 8 {
		t.Fatalf("forwarded %d bytes; the unknown message must not pass", out.Len())
	}
}

func TestRelayPassesFenceAndEncodings(t *testing.T) {
	var in bytes.Buffer
	in.Write([]byte{2, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 7}) // SetEncodings: 2 encodings
	in.Write([]byte{248, 0, 0, 0, 0, 0, 0, 1, 2, 'x', 'y'})
	var out bytes.Buffer
	if err := Relay(&out, &in, nil); err != io.EOF {
		t.Fatalf("err = %v", err)
	}
	if out.Len() != 12+11 {
		t.Fatalf("forwarded %d", out.Len())
	}
}

func TestRelayHandlesWhatNoVNCSends(t *testing.T) {
	var in bytes.Buffer
	in.Write([]byte{150, 1, 0, 0, 0, 0, 0, 5, 0, 5})                                           // EnableContinuousUpdates
	in.Write([]byte{255, 0, 0, 1, 0, 0, 0, 'z', 0, 0, 0, 44})                                  // QEMU extended key: z down
	in.Write([]byte{255, 0, 0, 0, 0, 0, 0, 'z', 0, 0, 0, 44})                                  // and up
	in.Write([]byte{255, 1, 0, 1})                                                             // audio, dropped
	in.Write([]byte{251, 0, 0, 5, 0, 5, 1, 0, 0, 0, 0, 0, 0, 0, 0, 5, 0, 5, 0, 0, 0, 0, 0, 0}) // resize, dropped
	var out bytes.Buffer
	var total Input
	if err := Relay(&out, &in, func(i Input) { total = total.Add(i) }); err != io.EOF {
		t.Fatalf("err = %v", err)
	}
	if want := (Input{Keys: 1, Typed: 1}); total != want {
		t.Fatalf("counts = %+v, want %+v", total, want)
	}
	// Continuous updates and the two key events pass; audio and resize do not.
	if out.Len() != 10+12+12 {
		t.Fatalf("forwarded %d bytes", out.Len())
	}
}

func TestRelayExtendedClipboard(t *testing.T) {
	var in bytes.Buffer
	// A notify (flags without "provide") is not a paste: length -8, flags, one format.
	in.Write([]byte{6, 0, 0, 0, 0xff, 0xff, 0xff, 0xf8, 0, 0, 0, 1, 0, 0, 0, 1})
	// A provide is: length -8, flags with bit 28 set.
	in.Write([]byte{6, 0, 0, 0, 0xff, 0xff, 0xff, 0xf8, 0x10, 0, 0, 1, 0, 0, 0, 0})
	var out bytes.Buffer
	var total Input
	if err := Relay(&out, &in, func(i Input) { total = total.Add(i) }); err != io.EOF {
		t.Fatalf("err = %v", err)
	}
	if total.Pastes != 1 || out.Len() != 32 {
		t.Fatalf("counts = %+v, forwarded %d", total, out.Len())
	}
}
