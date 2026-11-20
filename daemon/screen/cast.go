package screen

import (
	"context"
	"encoding/binary"
	"errors"
	"image"
	"runtime"

	"github.com/daoleno/mewla/daemon/screencast"
)

// Cast is a live capture of this computer's screen for pushed video
// (package screencast): its pictures, and the person's input as RFB client
// messages, the same bytes the RFB viewer sent.
type Cast struct {
	session Session
	release context.CancelFunc
	// cursor: the pictures carry the computer's cursor.
	cursor bool
	// mask is the RFB button mask the viewer last sent; rest the part of a
	// pixel scroll short of a wheel notch, on a computer that only has notches.
	mask byte
	rest [2]int
}

// capturesCursor says whether this computer's screen captures carry its
// cursor: macOS's do (ScreenCaptureKit), Windows' and X11's do not.
var capturesCursor = runtime.GOOS == "darwin"

type cursorKey struct{}

// withoutCursor asks the capture started with ctx to leave the cursor out of
// its pictures: the viewer draws the pointer itself.
func withoutCursor(ctx context.Context) context.Context {
	return context.WithValue(ctx, cursorKey{}, false)
}

// cursorShown says whether a capture started with ctx shows the cursor.
func cursorShown(ctx context.Context) bool {
	shown, ok := ctx.Value(cursorKey{}).(bool)
	return !ok || shown
}

// fineScroller is a session that scrolls by pixels, not only wheel notches.
type fineScroller interface {
	Scroll(x, y, dx, dy int) error
}

// OpenCast starts a capture for an allowed device. The display is held on
// while it lasts, unless the computer is locked, and obs (which may be nil)
// hears the lock. drawsPointer: the viewer draws the pointer itself (a
// phone), so the pictures leave the cursor out.
func (s *Service) OpenCast(ctx context.Context, deviceID string, obs *Observer, drawsPointer bool) (*Cast, error) {
	if err := s.allowed(deviceID); err != nil {
		return nil, err
	}
	capability := s.backend.Capability(ctx)
	if !capability.Supported {
		return nil, ErrUnsupported
	}
	if !capability.Ready() {
		return nil, ErrPermissions
	}
	start := ctx
	if drawsPointer {
		start = withoutCursor(ctx)
	}
	session, err := s.backend.Start(start)
	if err != nil {
		return nil, err
	}
	hold, release := context.WithCancel(ctx)
	s.holdWhileViewing(hold, obs)
	return &Cast{session: session, release: release, cursor: capturesCursor && !drawsPointer}, nil
}

// WatchWorkerCast lets an allowed device watch a Worker's agent using the
// screen (without a worker, the longest-running) without taking it over: the
// viewer sees the agent's own capture; what the person does reaches the
// screen and is told to the agent. It ends when the agent does.
func (s *Service) WatchWorkerCast(ctx context.Context, deviceID, worker string) (*Cast, error) {
	if err := s.allowed(deviceID); err != nil {
		return nil, err
	}
	agent := s.watchedAgent(worker)
	if agent == nil {
		return nil, ErrNoAgent
	}
	// The agent's own capture, cursor and all.
	return &Cast{session: &agentWatch{agent: agent}, release: func() {}, cursor: capturesCursor}, nil
}

// Next is the screen's next picture.
func (c *Cast) Next(ctx context.Context, after uint64) (screencast.Picture, error) {
	frame, err := c.session.Frame(ctx, after)
	if err != nil {
		return screencast.Picture{}, err
	}
	img := frame.Image
	return screencast.Picture{Pix: img.Pix, Width: img.Rect.Dx(), Height: img.Rect.Dy(), Stride: img.Stride, RGB: true, Seq: frame.Seq}, nil
}

// Input takes one RFB client message: a key, the pointer or the clipboard
// (which, as on the RFB path, does not reach the screen; Paste types).
func (c *Cast) Input(msg []byte) error {
	if len(msg) == 6 && msg[0] == 5 {
		c.mask = msg[1] &^ (8 | 16 | 32 | 64)
	}
	_, err := sessionInput{c.session}.Write(msg)
	return err
}

// Cursor says whether the pictures carry the computer's cursor.
func (c *Cast) Cursor() bool { return c.cursor }

// FineScroll says whether this screen scrolls by pixels (Scroll goes to the
// system as such), so the viewer should send pixels rather than notches.
func (c *Cast) FineScroll() bool {
	_, ok := scrollerOf(c.session)
	return ok
}

// Scroll scrolls by (dx, dy) picture pixels at (x, y): by pixels where the
// system takes them, else as wheel notches of 120 pixels, the rest kept.
func (c *Cast) Scroll(x, y, dx, dy int) error {
	if fine, ok := scrollerOf(c.session); ok {
		if w, watched := c.session.(*agentWatch); watched {
			w.agent.watchInput(InputScroll)
		}
		return fine.Scroll(x, y, dx, dy)
	}
	const notch = 120
	c.rest[0] += dx
	c.rest[1] += dy
	for _, axis := range []struct {
		i        int
		neg, pos byte
	}{{1, 8, 16}, {0, 32, 64}} {
		for c.rest[axis.i] >= notch || c.rest[axis.i] <= -notch {
			bit := axis.pos
			if c.rest[axis.i] < 0 {
				bit = axis.neg
				c.rest[axis.i] += notch
			} else {
				c.rest[axis.i] -= notch
			}
			if err := c.session.Pointer(x, y, c.mask|bit); err != nil {
				return err
			}
			if err := c.session.Pointer(x, y, c.mask); err != nil {
				return err
			}
		}
	}
	return nil
}

// scrollerOf is the session's pixel scroll, if it has one (through an agent watch too).
func scrollerOf(session Session) (fineScroller, bool) {
	if w, ok := session.(*agentWatch); ok {
		session = w.agent.session
	}
	fine, ok := session.(fineScroller)
	return fine, ok
}

// sessionInput applies whole RFB client messages to a Session: keys and the
// pointer; ClientInit and the rest (formats, encodings, the clipboard) are
// not input to it.
type sessionInput struct{ session Session }

func (s sessionInput) Write(msg []byte) (int, error) {
	var err error
	switch {
	case len(msg) == 8 && msg[0] == 4:
		err = s.session.Key(binary.BigEndian.Uint32(msg[4:]), msg[1] != 0)
	case len(msg) == 6 && msg[0] == 5:
		err = s.session.Pointer(int(binary.BigEndian.Uint16(msg[2:])), int(binary.BigEndian.Uint16(msg[4:])), msg[1])
	case len(msg) == 0:
		err = errors.New("not an input message")
	}
	return len(msg), err
}

// Close ends the capture and lets the display go.
func (c *Cast) Close() {
	c.release()
	_ = c.session.Close()
}

// CastEncoder is this computer's own H.264 encoder for pushed video
// (VideoToolbox on a Mac), or nil where screencast uses ffmpeg.
func CastEncoder() func(kbps int) (screencast.Encoder, error) {
	if defaultVideo == nil {
		return nil
	}
	factory := defaultVideo
	return func(kbps int) (screencast.Encoder, error) {
		// The encoder starts at the first picture's size; trying one now makes
		// a computer without one (a Windows N edition) say so to the viewer.
		probe, err := factory(320, 240, kbps)
		if err != nil {
			return nil, err
		}
		probe.Close()
		return &castEncoder{factory: factory, kbps: kbps}, nil
	}
}

// castEncoder runs a videoEncoder at the picture's size, making a new one
// when the size changes.
type castEncoder struct {
	factory videoFactory
	enc     videoEncoder
	w, h    int
	kbps    int
}

func (e *castEncoder) Encode(p screencast.Picture, key bool) ([]byte, error) {
	w, h := p.Width&^1, p.Height&^1
	if e.enc == nil || e.w != w || e.h != h {
		if e.enc != nil {
			e.enc.Close()
		}
		enc, err := e.factory(w, h, e.kbps)
		if err != nil {
			e.enc = nil
			return nil, err
		}
		e.enc, e.w, e.h, key = enc, w, h, true
	}
	img := &image.RGBA{Pix: p.Pix, Stride: p.Stride, Rect: image.Rect(0, 0, w, h)}
	au, _, err := e.enc.Encode(img, key)
	if errors.Is(err, errVideoDropped) {
		return nil, nil // the stream is as it was; the next picture follows
	}
	return au, err
}

func (e *castEncoder) SetBitrate(kbps int) {
	e.kbps = kbps
	if e.enc != nil {
		e.enc.SetBitrate(kbps)
	}
}

func (e *castEncoder) Name() string { return defaultVideoName }

func (e *castEncoder) Close() {
	if e.enc != nil {
		e.enc.Close()
		e.enc = nil
	}
}
