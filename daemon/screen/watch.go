package screen

import (
	"context"
	"errors"
	"image"
)

// ErrNoAgent means no agent is using the screen, so there is nothing to watch.
var ErrNoAgent = errors.New("No agent is using this computer's screen")

// watchedAgent is the agent of worker using the screen; without a worker, the
// longest-running one. Nil when there is none.
func (s *Service) watchedAgent(worker string) *agentSession {
	var agent *agentSession
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	for _, candidate := range s.agents {
		if candidate.ctx.Err() != nil || (worker != "" && candidate.worker != worker) {
			continue
		}
		if agent == nil || candidate.since.Before(agent.since) {
			agent = candidate
		}
	}
	return agent
}

// agentWatch is a Session over an agent's capture: the viewer sees what the
// agent sees. What the person does in it is theirs: it reaches the screen and
// is told to the agent with its next answer, like input at the computer, and
// never pauses it. A button or key the agent holds is let go when the person
// acts (see agentSession.beginAction and input), so nothing is held under them.
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

// Pointer passes the person's pointer to the screen. It is not the agent's
// own event, so it never hides itself from the agent as one.
func (w *agentWatch) Pointer(x, y int, buttons uint8) error {
	w.agent.watchInput(pointerKind(w.agent, buttons))
	return w.agent.session.Pointer(x, y, buttons)
}

// Key passes the person's key to the screen.
func (w *agentWatch) Key(keysym uint32, down bool) error {
	if down {
		w.agent.watchInput(InputKey)
	}
	return w.agent.session.Key(keysym, down)
}

func (w *agentWatch) Close() error { return nil }

// pointerKind is what a pointer event of the person's counts as: a wheel
// notch, a button press, or only a move. Releases and moves with a button down
// are not counted twice.
func pointerKind(a *agentSession, buttons uint8) InputKind {
	a.inMu.Lock()
	defer a.inMu.Unlock()
	pressed := buttons &^ a.watchedButtons
	a.watchedButtons = buttons
	switch {
	case pressed&(8|16|32|64) != 0:
		return InputScroll
	case pressed&7 != 0:
		return InputClick
	}
	return InputPointer
}
