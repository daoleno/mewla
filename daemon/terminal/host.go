package terminal

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/sessionhost"
)

// HostBackend opens Terminal views of Sessions that run in the session host.
// It dials the running host and never starts one: a Session it is asked to
// show already lives there.
type HostBackend struct {
	// Version names this daemon in the host's hello.
	Version string
}

// HostBackendName is the terminal backend of session host Sessions.
const HostBackendName = "host"

func (b *HostBackend) Name() string { return HostBackendName }

func (b *HostBackend) Open(targetID string, opts OpenOptions) (Session, error) {
	socket := strings.TrimSpace(opts.Socket)
	if socket == "" {
		return nil, fmt.Errorf("Session %s has no session host to open", targetID)
	}
	size := Size{Cols: 120, Rows: 36}
	if opts.Cols > 0 {
		size.Cols = opts.Cols
	}
	if opts.Rows > 0 {
		size.Rows = opts.Rows
	}
	return &hostSession{
		id:       fmt.Sprintf("%s#%d", targetID, terminalIDCounter.Add(1)),
		targetID: targetID,
		socket:   socket,
		version:  b.Version,
		size:     size,
		events:   make(chan Event, 128),
	}, nil
}

// hostCallTimeout bounds one call to the host.
const hostCallTimeout = 5 * time.Second

// fullReset starts a viewer's terminal over before a fresh replay.
const fullReset = "\x1bc"

// hostSession is one Terminal view of a host Session. The host replays the
// screen and then streams the program's output. The host keeps the history,
// so there is no copy-mode to scroll: the App reads History instead.
//
// The Session has one size, the one its program writes for, and the last
// viewer a person acted on sets it: this view sets it when it opens, when the
// App's grid changes or is shown again (Resize), and when the person types
// while another viewer has it. Any resize draws the view again, as tmux
// redraws a client: output written for another size, drawn on this view's
// grid, writes rows over rows (the host ends the view with AttachResized),
// and a replay that reached the view while its grid was another size (the
// App's grid settles while the view opens) would stay misdrawn. Attaching
// again on its own (behind, resized) never takes the size: two viewers of
// different sizes would take it from each other forever.
type hostSession struct {
	id       string
	targetID string
	socket   string
	version  string

	mu     sync.Mutex
	size   Size
	client *sessionhost.Client
	view   *sessionhost.Attachment
	cancel context.CancelFunc
	closed bool
	// redraw asks the pump for a fresh replay at size once the view ends.
	redraw bool
	// owned: the Session is this view's size (no other viewer resized it).
	owned bool
	// redraws: the host ends this view when another viewer resizes the
	// Session (AttachResized). A host from before that never does: then the
	// view takes its size back whenever it attaches again, as it always did.
	redraws bool

	events    chan Event
	closeOnce sync.Once

	// scrollback numbers the history rows this view has served.
	scrollback scrollbackTracker
}

func (s *hostSession) ID() string { return s.id }

func (s *hostSession) Events() <-chan Event { return s.events }

func (s *hostSession) Size() Size {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size
}

func (s *hostSession) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil || s.closed {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	callCtx, callCancel := context.WithTimeout(runCtx, hostCallTimeout)
	defer callCancel()
	client, err := sessionhost.Dial(callCtx, s.socket, s.version)
	if err != nil {
		cancel()
		return fmt.Errorf("open Session %s: %w", s.targetID, err)
	}
	view, err := client.Attach(callCtx, sessionhost.AttachParams{ID: s.targetID, Cols: s.size.Cols, Rows: s.size.Rows, Redraw: true})
	if err != nil {
		client.Close()
		cancel()
		return fmt.Errorf("open Session %s: %w", s.targetID, err)
	}
	s.client, s.view, s.cancel = client, view, cancel
	s.size = Size{Cols: view.Cols, Rows: view.Rows}
	s.owned, s.redraws = true, view.Redraw
	go s.pump(runCtx, view)
	return nil
}

// pump forwards the replay and then the output. A view that fell behind or
// was resized (by the App or by another viewer) starts over: a full reset
// and a fresh replay leave nothing stale on the viewer's terminal.
func (s *hostSession) pump(ctx context.Context, view *sessionhost.Attachment) {
	defer s.closeEvents()
	prefix := ""
	for {
		if !s.send(ctx, Event{Type: EventOutput, Data: prefix + string(view.Replay)}) {
			return
		}
		for chunk := range view.Output() {
			if !s.send(ctx, Event{Type: EventOutput, Data: string(chunk)}) {
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		end, err := view.End()
		switch {
		case s.takeRedraw():
			next, err := s.reattach(ctx)
			if err != nil {
				s.send(ctx, Event{Type: EventError, Err: err})
				s.send(ctx, Event{Type: EventExit, ExitCode: 1})
				return
			}
			view, prefix = next, fullReset
		case err != nil:
			s.send(ctx, Event{Type: EventError, Err: fmt.Errorf("the session host stopped streaming Session %s: %w", s.targetID, err)})
			s.send(ctx, Event{Type: EventExit, ExitCode: 1})
			return
		case end.Reason == sessionhost.AttachBehind || end.Reason == sessionhost.AttachResized:
			next, err := s.reattach(ctx)
			if err != nil {
				s.send(ctx, Event{Type: EventError, Err: err})
				s.send(ctx, Event{Type: EventExit, ExitCode: 1})
				return
			}
			view, prefix = next, fullReset
		default: // the program exited or the Session was removed
			code := 0
			if end.ExitCode != nil {
				code = *end.ExitCode
			}
			s.send(ctx, Event{Type: EventExit, ExitCode: code})
			return
		}
	}
}

// reattach opens the view again at the Session's size, whatever it is now:
// a size this view wants was set before (Resize), and one another viewer set
// stays until this view's person acts. A host that does not redraw viewers
// gets this view's size again, as before.
func (s *hostSession) reattach(ctx context.Context) (*sessionhost.Attachment, error) {
	s.mu.Lock()
	client, size, passive := s.client, s.size, s.redraws
	s.mu.Unlock()
	params := sessionhost.AttachParams{ID: s.targetID, Redraw: true}
	if !passive {
		params.Cols, params.Rows = size.Cols, size.Rows
	}
	callCtx, cancel := context.WithTimeout(ctx, hostCallTimeout)
	defer cancel()
	view, err := client.Attach(callCtx, params)
	if err != nil {
		return nil, fmt.Errorf("reopen Session %s: %w", s.targetID, err)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		view.Close()
		return nil, fmt.Errorf("terminal view of Session %s is closed", s.targetID)
	}
	s.view = view
	s.redraws = view.Redraw
	s.owned = view.Cols == s.size.Cols && view.Rows == s.size.Rows
	again := s.redraw // resized while this view was attaching
	s.mu.Unlock()
	if again {
		view.Close()
	}
	return view, nil
}

// takeRedraw reports, once, that a resize ended the view for a redraw.
func (s *hostSession) takeRedraw() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	redraw := s.redraw
	s.redraw = false
	return redraw
}

func (s *hostSession) send(ctx context.Context, ev Event) bool {
	select {
	case s.events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *hostSession) closeEvents() {
	s.closeOnce.Do(func() { close(s.events) })
}

func (s *hostSession) started() (*sessionhost.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("terminal view of Session %s is closed", s.targetID)
	}
	if s.client == nil {
		return nil, fmt.Errorf("terminal view of Session %s is not open", s.targetID)
	}
	return s.client, nil
}

// focusOut is the report a view sends when it loses the keyboard (DEC 1004):
// the person went elsewhere, so it is no reason to take the size.
const focusOut = "\x1b[O"

// Write types into the Session. A person typing here while another viewer
// has the Session at its size takes the size back first, so what they type
// into is drawn right.
func (s *hostSession) Write(data string) error {
	client, err := s.started()
	if err != nil {
		return err
	}
	s.mu.Lock()
	owned, size := s.owned, s.size
	s.mu.Unlock()
	if !owned && data != focusOut {
		if err := s.Resize(size.Cols, size.Rows); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostCallTimeout)
	defer cancel()
	_, err = client.Input(ctx, s.targetID, []sessionhost.InputStep{{Bytes: []byte(data)}})
	return err
}

// Resize makes cols x rows the Session's size and draws the view again. The
// App sends its grid again when the terminal is shown: at the size this view
// already has, nothing happens, unless the host would not have told it of
// another viewer's resize (then it resizes and redraws, as before).
func (s *hostSession) Resize(cols, rows int) error {
	client, err := s.started()
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.redraws && s.owned && s.size == (Size{Cols: cols, Rows: rows}) {
		s.mu.Unlock()
		return nil
	}
	// Set before the host resizes: it ends the view, and the pump must read
	// that end as this redraw, not as the host going away.
	s.size = Size{Cols: cols, Rows: rows}
	s.redraw = true
	s.owned = true
	view := s.view
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), hostCallTimeout)
	defer cancel()
	if err := client.Resize(ctx, s.targetID, cols, rows); err != nil {
		s.mu.Lock()
		s.owned = false
		s.mu.Unlock()
		return err
	}
	if view != nil {
		view.Close() // the pump attaches again at the new size (a host before AttachResized does not end it)
	}
	return nil
}

// Scroll and CancelScroll do nothing: there is no copy-mode, and the App
// scrolls the History it reads.
func (s *hostSession) Scroll(int) error    { return nil }
func (s *hostSession) CancelScroll() error { return nil }

// FocusPane does nothing: a host Session is one pane.
func (s *hostSession) FocusPane(int, int) error { return nil }

func (s *hostSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	if s.view != nil {
		s.view.Close()
	}
	if s.client != nil {
		s.client.Close()
	}
	return nil
}

// History is the Session's history above the screen, in the shape tmux's is:
// one styled row per line, the newest last, none while the alternate screen
// is active, and one pane covering the whole grid.
func (s *hostSession) History() (History, error) {
	client, err := s.started()
	if err != nil {
		return History{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	capture, err := client.Capture(ctx, sessionhost.CaptureParams{ID: s.targetID, Scope: sessionhost.ScopeHistory, History: historyLineLimit, Styled: true})
	if err != nil {
		return History{}, fmt.Errorf("capture Session history: %w", err)
	}
	ansi := ""
	if !capture.Alt && capture.Text != "" {
		ansi = strings.ReplaceAll(capture.Text, "\r\n", "\n") + "\n"
	}
	if len(ansi) > historyByteLimit {
		return History{}, fmt.Errorf("pane history exceeds 4 MiB")
	}
	return History{
		PaneID:    s.targetID,
		Cols:      capture.Cols,
		Total:     capture.ScrollbackRows,
		Alternate: capture.Alt,
		ANSI:      ansi,
		Panes: []PaneScrollMode{{
			ID:        s.targetID,
			Cols:      capture.Cols,
			Rows:      capture.Rows,
			Alternate: capture.Alt,
			Mouse:     capture.MouseTracking,
		}},
	}, nil
}
