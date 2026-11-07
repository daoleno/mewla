package vdisplay

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"sync"
	"time"
)

// Client is a minimal RFB client: it keeps the picture of a display in memory
// so an agent can be shown it, and sends the agent's keys and pointer. It
// asks for raw pixels only; the person's viewer speaks to the display itself.
type Client struct {
	conn   net.Conn
	width  int
	height int

	mu      sync.Mutex
	pix     []byte // width*height*4, B G R X
	seq     uint64 // updates received
	changed chan struct{}
	err     error
	wmu     sync.Mutex
}

// Handshake speaks RFB 3.8 up to and including security. The display offers
// no security (it is only reachable through a private socket); what follows is
// ClientInit.
func Handshake(conn net.Conn) error {
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})
	version := make([]byte, 12)
	if _, err := io.ReadFull(conn, version); err != nil {
		return err
	}
	if !bytes.HasPrefix(version, []byte("RFB 003.")) {
		return errors.New("not an RFB server")
	}
	if _, err := conn.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	var count [1]byte
	if _, err := io.ReadFull(conn, count[:]); err != nil {
		return err
	}
	if count[0] == 0 {
		return errors.New("the display refused the connection")
	}
	types := make([]byte, count[0])
	if _, err := io.ReadFull(conn, types); err != nil {
		return err
	}
	if !bytes.Contains(types, []byte{1}) {
		return errors.New("the display asks for a password")
	}
	if _, err := conn.Write([]byte{1}); err != nil {
		return err
	}
	var result [4]byte
	if _, err := io.ReadFull(conn, result[:]); err != nil {
		return err
	}
	if binary.BigEndian.Uint32(result[:]) != 0 {
		return errors.New("the display refused the connection")
	}
	return nil
}

// Connect starts the agent's own client: handshake, a shared ClientInit (the
// person's viewer stays connected), then reads updates.
func Connect(conn net.Conn) (*Client, error) {
	if err := Handshake(conn); err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})
	if _, err := conn.Write([]byte{1}); err != nil {
		return nil, err
	}
	var init [24]byte
	if _, err := io.ReadFull(conn, init[:]); err != nil {
		return nil, err
	}
	width, height := int(binary.BigEndian.Uint16(init[0:])), int(binary.BigEndian.Uint16(init[2:]))
	name := make([]byte, binary.BigEndian.Uint32(init[20:]))
	if len(name) > 1<<16 {
		return nil, errors.New("display name too long")
	}
	if _, err := io.ReadFull(conn, name); err != nil {
		return nil, err
	}
	if width <= 0 || height <= 0 || width*height > 1<<26 {
		return nil, fmt.Errorf("display size %dx%d", width, height)
	}
	c := &Client{conn: conn, width: width, height: height, pix: make([]byte, width*height*4), changed: make(chan struct{}, 1)}
	// 32 bits, true colour, little endian, red in bits 16-23.
	pf := []byte{0, 0, 0, 0, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0}
	enc := []byte{2, 0, 0, 1, 0, 0, 0, 0} // Raw
	if err := c.write(append(pf, enc...)); err != nil {
		return nil, err
	}
	if err := c.request(false); err != nil {
		return nil, err
	}
	go c.read()
	return c, nil
}

// Size is the display's size in pixels.
func (c *Client) Size() (int, int) { return c.width, c.height }

// Close ends the connection.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) write(b []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := c.conn.Write(b)
	return err
}

func (c *Client) request(incremental bool) error {
	msg := []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if incremental {
		msg[1] = 1
	}
	binary.BigEndian.PutUint16(msg[6:], uint16(c.width))
	binary.BigEndian.PutUint16(msg[8:], uint16(c.height))
	return c.write(msg)
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

func (c *Client) read() {
	var head [4]byte
	for {
		if _, err := io.ReadFull(c.conn, head[:1]); err != nil {
			c.fail(err)
			return
		}
		switch head[0] {
		case 0: // FramebufferUpdate
			if _, err := io.ReadFull(c.conn, head[1:4]); err != nil {
				c.fail(err)
				return
			}
			for n := int(binary.BigEndian.Uint16(head[2:])); n > 0; n-- {
				var rect [12]byte
				if _, err := io.ReadFull(c.conn, rect[:]); err != nil {
					c.fail(err)
					return
				}
				x, y := int(binary.BigEndian.Uint16(rect[0:])), int(binary.BigEndian.Uint16(rect[2:]))
				w, h := int(binary.BigEndian.Uint16(rect[4:])), int(binary.BigEndian.Uint16(rect[6:]))
				if binary.BigEndian.Uint32(rect[8:]) != 0 || x+w > c.width || y+h > c.height {
					c.fail(errors.New("unexpected display update"))
					return
				}
				for row := 0; row < h; row++ {
					line := make([]byte, w*4)
					if _, err := io.ReadFull(c.conn, line); err != nil {
						c.fail(err)
						return
					}
					c.mu.Lock()
					copy(c.pix[((y+row)*c.width+x)*4:], line)
					c.mu.Unlock()
				}
			}
			c.mu.Lock()
			c.seq++
			c.mu.Unlock()
			select {
			case c.changed <- struct{}{}:
			default:
			}
			if err := c.request(true); err != nil {
				c.fail(err)
				return
			}
		case 1: // SetColourMapEntries: not used with true colour
			c.fail(errors.New("unexpected colour map"))
			return
		case 2: // Bell
		case 3: // ServerCutText: the clipboard is not the agent's to read
			var cut [7]byte
			if _, err := io.ReadFull(c.conn, cut[:]); err != nil {
				c.fail(err)
				return
			}
			if _, err := io.CopyN(io.Discard, c.conn, int64(binary.BigEndian.Uint32(cut[3:]))); err != nil {
				c.fail(err)
				return
			}
		default:
			c.fail(fmt.Errorf("unknown display message %d", head[0]))
			return
		}
	}
}

// Snapshot waits for the display to answer and returns its picture.
func (c *Client) Snapshot(wait time.Duration) (*image.RGBA, error) {
	c.mu.Lock()
	before := c.seq
	c.mu.Unlock()
	if err := c.request(false); err != nil {
		return nil, err
	}
	deadline := time.After(wait)
	for {
		c.mu.Lock()
		err, now := c.err, c.seq
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if now > before {
			break
		}
		select {
		case <-c.changed:
		case <-deadline:
			return nil, errors.New("the display did not answer")
		}
	}
	// Let a burst of changes land, then take the picture.
	time.Sleep(60 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, c.width, c.height))
	for i := 0; i < c.width*c.height; i++ {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = c.pix[i*4+2], c.pix[i*4+1], c.pix[i*4], 255
	}
	return img, nil
}

// Screenshot is Snapshot as a PNG.
func (c *Client) Screenshot(wait time.Duration) ([]byte, error) {
	img, err := c.Snapshot(wait)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Pointer moves the pointer and sets the held buttons (bit 0 left, 1 middle, 2 right,
// 3 and 4 the wheel).
func (c *Client) Pointer(x, y int, buttons byte) error {
	if x < 0 || y < 0 || x >= c.width || y >= c.height {
		return fmt.Errorf("point %d,%d is outside the %dx%d display", x, y, c.width, c.height)
	}
	msg := []byte{5, buttons, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(msg[2:], uint16(x))
	binary.BigEndian.PutUint16(msg[4:], uint16(y))
	return c.write(msg)
}

// Key presses or releases a keysym.
func (c *Client) Key(sym uint32, down bool) error {
	msg := []byte{4, 0, 0, 0, 0, 0, 0, 0}
	if down {
		msg[1] = 1
	}
	binary.BigEndian.PutUint32(msg[4:], sym)
	return c.write(msg)
}
