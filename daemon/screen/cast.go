package screen

import (
	"context"
	"encoding/binary"
	"errors"
	"image"

	"github.com/daoleno/mewla/daemon/screencast"
)

// Cast is a live capture of this computer's screen for pushed video
// (package screencast): its pictures, and the person's input as RFB client
// messages, the same bytes the RFB viewer sent.
type Cast struct {
	session Session
	release context.CancelFunc
}

// OpenCast starts a capture for an allowed device, as Open does for RFB: the
// display is held on while it lasts, unless the computer is locked, and obs
// (which may be nil) hears the lock.
func (s *Service) OpenCast(ctx context.Context, deviceID string, obs *Observer) (*Cast, error) {
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
	session, err := s.backend.Start(ctx)
	if err != nil {
		return nil, err
	}
	hold, release := context.WithCancel(ctx)
	s.holdWhileViewing(hold, obs)
	return &Cast{session: session, release: release}, nil
}

// WatchWorkerCast is WatchWorker for pushed video: the agent's own capture.
func (s *Service) WatchWorkerCast(ctx context.Context, deviceID, worker string) (*Cast, error) {
	if err := s.allowed(deviceID); err != nil {
		return nil, err
	}
	agent := s.watchedAgent(worker)
	if agent == nil {
		return nil, ErrNoAgent
	}
	return &Cast{session: &agentWatch{agent: agent}, release: func() {}}, nil
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
	_, err := sessionInput{c.session}.Write(msg)
	return err
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

func (e *castEncoder) Name() string { return "hardware" }

func (e *castEncoder) Close() {
	if e.enc != nil {
		e.enc.Close()
		e.enc = nil
	}
}
