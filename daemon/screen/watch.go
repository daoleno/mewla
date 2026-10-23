package screen

import (
	"context"
	"errors"
	"image"
	"net"
)

// ErrNoAgent means no agent is using the screen, so there is nothing to watch.
var ErrNoAgent = errors.New("No agent is using this computer's screen")

// Watch lets an allowed device watch the agent using the screen without
// taking it over: the viewer sees the agent's own capture, and its input is
// dropped. Like Open, it returns the viewer's end right before ClientInit.
// The session ends when the agent does.
func (s *Service) Watch(ctx context.Context, deviceID string, obs *Observer) (net.Conn, error) {
	if err := s.allowed(deviceID); err != nil {
		return nil, err
	}
	s.agentMu.Lock()
	agent := s.agent
	s.agentMu.Unlock()
	if agent == nil || agent.ctx.Err() != nil {
		return nil, ErrNoAgent
	}
	viewer, server := net.Pipe()
	go func() {
		defer server.Close()
		_ = serveRFB(ctx, server, &agentWatch{agent: agent}, computerName(), obs)
	}()
	return viewer, nil
}

// agentWatch is a read-only Session over an agent's capture.
type agentWatch struct {
	agent *agentSession
	image *image.RGBA
}

func (w *agentWatch) Frame(ctx context.Context, after uint64) (Frame, error) {
	a := w.agent
	for {
		a.mu.Lock()
		if a.latest != nil && a.seq > after {
			if w.image == nil || w.image.Rect != a.latest.Rect {
				w.image = image.NewRGBA(a.latest.Rect)
			}
			copy(w.image.Pix, a.latest.Pix)
			seq := a.seq
			a.mu.Unlock()
			return Frame{Seq: seq, Image: w.image}, nil
		}
		changed, frameErr := a.changed, a.frameErr
		a.mu.Unlock()
		if frameErr != nil {
			return Frame{}, frameErr
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return Frame{}, context.Cause(ctx)
		case <-a.ctx.Done():
			return Frame{}, context.Cause(a.ctx)
		}
	}
}

// Watching never moves the pointer or types.
func (w *agentWatch) Pointer(int, int, uint8) error { return nil }
func (w *agentWatch) Key(uint32, bool) error        { return nil }
func (w *agentWatch) Close() error                  { return nil }
