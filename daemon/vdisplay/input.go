package vdisplay

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Input is what the person did through a viewer, as counts. It never holds
// what was typed: only how many keys went down, and which of them were
// printable characters.
type Input struct {
	Clicks  int // pointer button presses (not the wheel)
	Scrolls int // wheel notches
	Keys    int // key presses
	Typed   int // of those, printable characters
	Pastes  int // clipboard sends
}

// Empty reports whether nothing happened.
func (i Input) Empty() bool { return i == Input{} }

// Add sums two counts.
func (i Input) Add(o Input) Input {
	return Input{i.Clicks + o.Clicks, i.Scrolls + o.Scrolls, i.Keys + o.Keys, i.Typed + o.Typed, i.Pastes + o.Pastes}
}

// RelayViewer passes the viewer's ClientInit on and then relays its messages.
func RelayViewer(dst io.Writer, src io.Reader, observe func(Input)) error {
	var init [1]byte
	if _, err := io.ReadFull(src, init[:]); err != nil {
		return err
	}
	if _, err := dst.Write([]byte{1}); err != nil { // always shared: the agent's own client is connected too
		return err
	}
	return Relay(dst, src, observe)
}

// Relay copies a viewer's RFB messages (everything after ClientInit) to the
// display and reports what the person did. Messages it does not know end the
// copy: it cannot tell where the next one starts, and passing on unseen bytes
// would let input through uncounted.
func Relay(dst io.Writer, src io.Reader, observe func(Input)) error {
	var buttons byte
	var head [1]byte
	for {
		if _, err := io.ReadFull(src, head[:]); err != nil {
			return err
		}
		msg := append([]byte(nil), head[0])
		read := func(n int) ([]byte, error) {
			b := make([]byte, n)
			_, err := io.ReadFull(src, b)
			msg = append(msg, b...)
			return b, err
		}
		var in Input
		switch head[0] {
		case 0: // SetPixelFormat
			if _, err := read(19); err != nil {
				return err
			}
		case 2: // SetEncodings
			b, err := read(3)
			if err != nil {
				return err
			}
			if _, err := read(4 * int(binary.BigEndian.Uint16(b[1:]))); err != nil {
				return err
			}
		case 3: // FramebufferUpdateRequest
			if _, err := read(9); err != nil {
				return err
			}
		case 4: // KeyEvent
			b, err := read(7)
			if err != nil {
				return err
			}
			if b[0] != 0 {
				in.Keys = 1
				if printable(binary.BigEndian.Uint32(b[3:])) {
					in.Typed = 1
				}
			}
		case 5: // PointerEvent
			b, err := read(5)
			if err != nil {
				return err
			}
			pressed := b[0] &^ buttons
			buttons = b[0]
			if pressed&0b111 != 0 {
				in.Clicks = 1
			}
			if pressed&0b11000 != 0 {
				in.Scrolls = 1
			}
		case 6: // ClientCutText
			b, err := read(7)
			if err != nil {
				return err
			}
			n := binary.BigEndian.Uint32(b[3:])
			extended := int32(n) < 0
			if extended {
				// The extended clipboard: a negative length is minus the size of
				// what follows, which starts with flags saying what the message is.
				n = uint32(-int32(n))
			}
			if n > 1<<20 {
				return fmt.Errorf("clipboard of %d bytes", n)
			}
			body, err := read(int(n))
			if err != nil {
				return err
			}
			// Only sending the person's clipboard counts; asking for or announcing
			// it does not.
			// A viewer's capability announcement lists "provide" among the
			// actions it can do; it is not a paste.
			const caps, provide = 1 << 24, 1 << 28
			if !extended || (len(body) >= 4 && binary.BigEndian.Uint32(body)&provide != 0 && binary.BigEndian.Uint32(body)&caps == 0) {
				in.Pastes = 1
			}
		case 150: // EnableContinuousUpdates: enable flag and a rectangle
			if _, err := read(9); err != nil {
				return err
			}
		case 251: // SetDesktopSize: the display keeps its size; read the message, pass nothing on
			b, err := read(7)
			if err != nil {
				return err
			}
			if _, err := read(16 * int(b[5])); err != nil {
				return err
			}
			continue
		case 255: // QEMU client message
			b, err := read(1)
			if err != nil {
				return err
			}
			switch b[0] {
			case 0: // extended key event: down flag, keysym, keycode
				e, err := read(10)
				if err != nil {
					return err
				}
				if binary.BigEndian.Uint16(e[0:]) != 0 {
					in.Keys = 1
					if printable(binary.BigEndian.Uint32(e[2:])) {
						in.Typed = 1
					}
				}
			case 1: // audio: not for an agent's display
				if _, err := read(2); err != nil {
					return err
				}
				continue
			default:
				return fmt.Errorf("unknown QEMU message %d", b[0])
			}
		case 248: // ClientFence: 3 pad, flags, length, payload
			b, err := read(8)
			if err != nil {
				return err
			}
			if _, err := read(int(b[7])); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown viewer message %d", head[0])
		}
		if _, err := dst.Write(msg); err != nil {
			return err
		}
		if !in.Empty() && observe != nil {
			observe(in)
		}
	}
}

// AcceptViewer answers a viewer's RFB handshake as a 3.8 server offering only
// security None. The viewer is already authenticated by its device signature;
// the next bytes it sends are ClientInit.
func AcceptViewer(rw io.ReadWriter) error {
	if _, err := io.WriteString(rw, "RFB 003.008\n"); err != nil {
		return err
	}
	var version [12]byte
	if _, err := io.ReadFull(rw, version[:]); err != nil {
		return err
	}
	if string(version[:]) != "RFB 003.008\n" {
		return fmt.Errorf("viewer asked for unsupported RFB version %q", string(version[:11]))
	}
	if _, err := rw.Write([]byte{1, 1}); err != nil {
		return err
	}
	var chosen [1]byte
	if _, err := io.ReadFull(rw, chosen[:]); err != nil {
		return err
	}
	if chosen[0] != 1 {
		return fmt.Errorf("viewer chose unsupported security type %d", chosen[0])
	}
	_, err := rw.Write([]byte{0, 0, 0, 0})
	return err
}

// printable says whether a keysym types a character rather than acting as a
// modifier or a named key.
func printable(sym uint32) bool {
	return (sym >= 0x20 && sym < 0x7f) || (sym >= 0xa0 && sym <= 0xff) || sym >= 0x01000100
}
