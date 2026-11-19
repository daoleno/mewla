package vdisplay

import (
	"encoding/binary"
	"image"
)

// monitorInput takes a person's input for one monitor of a shared display,
// as whole RFB client messages (Relay's destination): the pointer is moved
// into the monitor, and keys and the clipboard go to the display, where the
// window under the pointer takes them.
type monitorInput struct {
	up   *Client
	rect func() image.Rectangle
}

func (m monitorInput) Write(msg []byte) (int, error) {
	if len(msg) == 0 {
		return 0, nil
	}
	switch {
	case msg[0] == 4: // KeyEvent
		return len(msg), m.up.Send(msg)
	case msg[0] == 5 && len(msg) == 6: // PointerEvent: into the monitor
		r := m.rect()
		x := clamp(int(binary.BigEndian.Uint16(msg[2:])), 0, r.Dx()-1) + r.Min.X
		y := clamp(int(binary.BigEndian.Uint16(msg[4:])), 0, r.Dy()-1) + r.Min.Y
		return len(msg), m.up.Pointer(x, y, msg[1])
	case msg[0] == 6: // ClientCutText: the person's own clipboard
		return len(msg), m.up.Send(msg)
	}
	return len(msg), nil
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return min(max(v, lo), hi)
}
