package vdisplay

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"net"
	"sync"
	"time"
)

// A monitor's view: one rectangle of a shared display, served to a viewer as
// a display of its own. The viewer sees an RFB server whose whole screen is
// the monitor; its pointer is moved into the monitor and its keys go to the
// shared display, where the window under the pointer takes them. Nothing of
// the other monitors is ever sent to it.
//
// The updates are made here, not relayed, because RFB has no way to ask a
// server for part of its screen: the monitor's pixels are compared with what
// this viewer was last sent and the changed tiles go out as ZRLE (or Raw, for
// a viewer that does not speak it).

const cropTile = 32

// NewMonitorView serves the rectangle rect() of up's display. conn is the
// viewer's RFB stream (past the handshake, as a Viewer's Conn is); relay takes
// the viewer's messages, ClientInit first. Close ends both.
func NewMonitorView(up *Client, rect func() image.Rectangle) (conn net.Conn, relay func(src io.Reader, observe func(Input)) error, closeView func()) {
	viewer, server := net.Pipe()
	m := &monitorView{shifts: [3]int{16, 8, 0}, up: up, rect: rect, out: server, done: make(chan struct{}), kick: make(chan struct{}, 1)}
	var once sync.Once
	closeView = func() {
		once.Do(func() {
			close(m.done)
			_ = server.Close()
			_ = viewer.Close()
		})
	}
	relay = func(src io.Reader, observe func(Input)) error {
		return RelayViewer(m, src, observe)
	}
	return viewer, relay, closeView
}

type monitorView struct {
	up   *Client
	rect func() image.Rectangle
	out  net.Conn
	done chan struct{}
	kick chan struct{}

	mu      sync.Mutex
	started bool
	zrle    bool
	pending bool
	incr    bool
	req     image.Rectangle
	shadow  []byte
	shifts  [3]int // red, green, blue: where the viewer wants each byte
	zbuf    bytes.Buffer
	zw      *zlib.Writer
}

// Write receives each of the viewer's messages whole (Relay's destination).
func (m *monitorView) Write(msg []byte) (int, error) {
	if len(msg) == 0 {
		return 0, nil
	}
	r := m.rect()
	switch {
	case len(msg) == 1 && !m.started: // ClientInit
		m.started = true
		go func() {
			if err := m.serverInit(m.rect()); err != nil {
				_ = m.out.Close()
				return
			}
			m.run()
		}()
	case msg[0] == 0 && len(msg) == 20: // SetPixelFormat
		pf := msg[4:]
		rs, gs, bs := int(pf[10]), int(pf[11]), int(pf[12])
		ok := func(v int) bool { return v == 0 || v == 8 || v == 16 }
		if pf[0] != 32 || pf[2] != 0 || pf[3] == 0 || binary.BigEndian.Uint16(pf[4:]) != 255 || binary.BigEndian.Uint16(pf[6:]) != 255 || binary.BigEndian.Uint16(pf[8:]) != 255 || !ok(rs) || !ok(gs) || !ok(bs) || rs == gs || gs == bs || rs == bs {
			return 0, errors.New("the viewer asked for a pixel format this view does not serve")
		}
		m.mu.Lock()
		m.shifts = [3]int{rs, gs, bs}
		m.shadow = nil
		m.mu.Unlock()
	case msg[0] == 2 && len(msg) >= 4: // SetEncodings
		n := int(binary.BigEndian.Uint16(msg[2:]))
		zrle := false
		for i := 0; i < n && 4+4*i+4 <= len(msg); i++ {
			if int32(binary.BigEndian.Uint32(msg[4+4*i:])) == 16 {
				zrle = true
			}
		}
		m.mu.Lock()
		m.zrle = zrle
		m.mu.Unlock()
	case msg[0] == 3 && len(msg) == 10: // FramebufferUpdateRequest
		x, y := int(binary.BigEndian.Uint16(msg[2:])), int(binary.BigEndian.Uint16(msg[4:]))
		w, h := int(binary.BigEndian.Uint16(msg[6:])), int(binary.BigEndian.Uint16(msg[8:]))
		req := image.Rect(x, y, x+w, y+h).Intersect(image.Rect(0, 0, r.Dx(), r.Dy()))
		m.mu.Lock()
		m.pending, m.incr, m.req = true, msg[1] != 0, req
		if msg[1] == 0 {
			m.shadow = nil // send all of it again
		}
		m.mu.Unlock()
		select {
		case m.kick <- struct{}{}:
		default:
		}
	case msg[0] == 4: // KeyEvent
		return len(msg), m.up.Send(msg)
	case msg[0] == 5 && len(msg) == 6: // PointerEvent: into the monitor
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

func (m *monitorView) serverInit(r image.Rectangle) error {
	name := []byte("Mewla agent display")
	b := make([]byte, 24+len(name))
	binary.BigEndian.PutUint16(b[0:], uint16(r.Dx()))
	binary.BigEndian.PutUint16(b[2:], uint16(r.Dy()))
	copy(b[4:], []byte{32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0})
	binary.BigEndian.PutUint32(b[20:], uint32(len(name)))
	copy(b[24:], name)
	_, err := m.out.Write(b)
	return err
}

// run sends the monitor's changes whenever the viewer has asked for them.
func (m *monitorView) run() {
	watch, stop := m.up.Watch()
	defer stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-watch:
		case <-m.kick:
		case <-tick.C:
		}
		if m.up.Err() != nil {
			_ = m.out.Close()
			return
		}
		if err := m.flush(); err != nil {
			_ = m.out.Close()
			return
		}
		// A burst of changes goes out as one update: wait out a frame.
		select {
		case <-m.done:
			return
		case <-time.After(15 * time.Millisecond):
		}
	}
}

func (m *monitorView) flush() error {
	m.mu.Lock()
	if !m.pending {
		m.mu.Unlock()
		return nil
	}
	req, incr, zrle := m.req, m.incr, m.zrle
	shadow, shifts := m.shadow, m.shifts
	m.mu.Unlock()
	r := m.rect()
	w, h := r.Dx(), r.Dy()
	pix := m.up.Crop(r)
	if shifts != [3]int{16, 8, 0} { // the display's own order is blue, green, red
		for i := 0; i+3 < len(pix); i += 4 {
			b, g, rr := uint32(pix[i]), uint32(pix[i+1]), uint32(pix[i+2])
			v := rr<<uint(shifts[0]) | g<<uint(shifts[1]) | b<<uint(shifts[2])
			pix[i], pix[i+1], pix[i+2] = byte(v), byte(v>>8), byte(v>>16)
		}
	}
	if shadow == nil || len(shadow) != len(pix) {
		shadow = make([]byte, len(pix))
		incr = false
	}
	// Tiles of the request that differ from what the viewer has, joined
	// along a row into rectangles.
	var rects []image.Rectangle
	for ty := req.Min.Y / cropTile * cropTile; ty < req.Max.Y; ty += cropTile {
		runStart := -1
		for tx := req.Min.X / cropTile * cropTile; tx <= req.Max.X; tx += cropTile {
			changed := false
			if tx < req.Max.X {
				t := image.Rect(tx, ty, tx+cropTile, ty+cropTile).Intersect(req).Intersect(image.Rect(0, 0, w, h))
				changed = !incr || tileDiffers(pix, shadow, w, t)
			}
			switch {
			case changed && runStart < 0:
				runStart = tx
			case !changed && runStart >= 0:
				rects = append(rects, image.Rect(runStart, ty, tx, ty+cropTile).Intersect(req).Intersect(image.Rect(0, 0, w, h)))
				runStart = -1
			}
		}
	}
	if len(rects) == 0 {
		return nil // nothing to say; the request stays open
	}
	var out bytes.Buffer
	out.Write([]byte{0, 0, byte(len(rects) >> 8), byte(len(rects))})
	for _, rc := range rects {
		var hdr [12]byte
		binary.BigEndian.PutUint16(hdr[0:], uint16(rc.Min.X))
		binary.BigEndian.PutUint16(hdr[2:], uint16(rc.Min.Y))
		binary.BigEndian.PutUint16(hdr[4:], uint16(rc.Dx()))
		binary.BigEndian.PutUint16(hdr[6:], uint16(rc.Dy()))
		if zrle {
			binary.BigEndian.PutUint32(hdr[8:], 16)
			out.Write(hdr[:])
			data := m.zrleRect(pix, w, rc)
			var n [4]byte
			binary.BigEndian.PutUint32(n[:], uint32(len(data)))
			out.Write(n[:])
			out.Write(data)
		} else {
			out.Write(hdr[:]) // encoding 0: Raw
			for y := rc.Min.Y; y < rc.Max.Y; y++ {
				out.Write(pix[(y*w+rc.Min.X)*4 : (y*w+rc.Max.X)*4])
			}
		}
		for y := rc.Min.Y; y < rc.Max.Y; y++ {
			copy(shadow[(y*w+rc.Min.X)*4:(y*w+rc.Max.X)*4], pix[(y*w+rc.Min.X)*4:(y*w+rc.Max.X)*4])
		}
	}
	_ = m.out.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if _, err := m.out.Write(out.Bytes()); err != nil {
		return err
	}
	m.mu.Lock()
	m.shadow = shadow
	m.pending = false
	m.mu.Unlock()
	return nil
}

func tileDiffers(a, b []byte, stride int, t image.Rectangle) bool {
	for y := t.Min.Y; y < t.Max.Y; y++ {
		if !bytes.Equal(a[(y*stride+t.Min.X)*4:(y*stride+t.Max.X)*4], b[(y*stride+t.Min.X)*4:(y*stride+t.Max.X)*4]) {
			return true
		}
	}
	return false
}

// zrleRect encodes a rectangle as ZRLE: 64x64 tiles, each a solid colour or
// raw pixels (3 bytes each), through the connection's one zlib stream.
func (m *monitorView) zrleRect(pix []byte, stride int, r image.Rectangle) []byte {
	if m.zw == nil {
		m.zw, _ = zlib.NewWriterLevel(&m.zbuf, zlib.BestSpeed)
	}
	m.zbuf.Reset()
	var raw bytes.Buffer
	for ty := r.Min.Y; ty < r.Max.Y; ty += 64 {
		for tx := r.Min.X; tx < r.Max.X; tx += 64 {
			t := image.Rect(tx, ty, min(tx+64, r.Max.X), min(ty+64, r.Max.Y))
			first := pix[(t.Min.Y*stride+t.Min.X)*4 : (t.Min.Y*stride+t.Min.X)*4+3]
			solid := true
		scan:
			for y := t.Min.Y; y < t.Max.Y; y++ {
				for x := t.Min.X; x < t.Max.X; x++ {
					if !bytes.Equal(pix[(y*stride+x)*4:(y*stride+x)*4+3], first) {
						solid = false
						break scan
					}
				}
			}
			if solid {
				raw.WriteByte(1)
				raw.Write(first)
				continue
			}
			raw.WriteByte(0)
			for y := t.Min.Y; y < t.Max.Y; y++ {
				for x := t.Min.X; x < t.Max.X; x++ {
					raw.Write(pix[(y*stride+x)*4 : (y*stride+x)*4+3])
				}
			}
		}
	}
	_, _ = m.zw.Write(raw.Bytes())
	_ = m.zw.Flush()
	return append([]byte(nil), m.zbuf.Bytes()...)
}

var _ = fmt.Sprint
