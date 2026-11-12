package sessionhost

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"
)

// tapBuffer is how many output chunks a viewer may fall behind by. One that
// falls further behind is ended with AttachBehind rather than slow the
// Session down, and attaches again for a fresh replay. Tests lower it.
var tapBuffer = 256

const (
	// attachSettle bounds the wait for the terminal's parser to finish a
	// sequence the program is in the middle of writing: the output after a
	// replay must start between sequences, or the viewer reads half of one.
	attachSettle = 500 * time.Millisecond
)

// viewTap carries a Session's output to one attached viewer.
type viewTap struct {
	output chan []byte
	end    AttachEnd // set before output is closed
	// redraw: the viewer attaches again when the Session is resized.
	redraw bool
}

// attach takes the replay and starts the viewer's tap in one step under mu,
// so the tap carries exactly the output after what the replay shows. It
// reports ok=false while the parser is inside a sequence.
func (s *session) attach(redraw bool) (attached Attached, replay []byte, tap *viewTap, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Attached{}, nil, nil, false, errorf(ErrCodeNotFound, "no session %q", s.id)
	}
	state := s.screen.State()
	if !state.Ground {
		return Attached{}, nil, nil, false, nil
	}
	replay, err = s.screen.Replay()
	if err != nil {
		return Attached{}, nil, nil, false, errorf(ErrCodeInternal, "replay: %v", err)
	}
	tap = &viewTap{output: make(chan []byte, tapBuffer), redraw: redraw}
	if s.viewsEnded != nil {
		// The program has exited and its output is all in the replay.
		tap.end = *s.viewsEnded
		close(tap.output)
	} else {
		s.taps[tap] = struct{}{}
	}
	return Attached{Cols: state.Cols, Rows: state.Rows, OutputSeq: s.outputSeq, Redraw: redraw}, replay, tap, true, nil
}

func (s *session) detach(tap *viewTap) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.taps, tap)
}

// feedTapsLocked hands output to every viewer without ever blocking the
// reader. Callers hold mu.
func (s *session) feedTapsLocked(p []byte) {
	if len(s.taps) == 0 {
		return
	}
	chunk := append([]byte(nil), p...)
	for tap := range s.taps {
		select {
		case tap.output <- chunk:
		default:
			s.endTapLocked(tap, AttachEnd{Reason: AttachBehind})
		}
	}
}

func (s *session) endTapLocked(tap *viewTap, end AttachEnd) {
	delete(s.taps, tap)
	tap.end = end
	close(tap.output)
}

// endResizedLocked ends the viewers that redraw after a resize: what the
// program writes next is for the new size. Callers hold mu.
func (s *session) endResizedLocked() {
	for tap := range s.taps {
		if tap.redraw {
			s.endTapLocked(tap, AttachEnd{Reason: AttachResized})
		}
	}
}

// endViewsLocked ends every viewer, and any that attaches later. Callers hold
// mu.
func (s *session) endViewsLocked(end AttachEnd) {
	if s.viewsEnded == nil {
		s.viewsEnded = &end
	}
	for tap := range s.taps {
		s.endTapLocked(tap, end)
	}
}

// serveAttach streams one Session to a viewer until the stream ends, the
// viewer goes away or the host stops. The connection carries nothing else.
func (s *Server) serveAttach(ctx context.Context, conn *net.UnixConn, req request, writeMu *sync.Mutex) {
	write := func(f func() error) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return f()
	}
	attached, replay, tap, sess, err := s.attachSession(req)
	if err != nil {
		_ = write(func() error { return writeJSON(conn, reply(req.ID, nil, err)) })
		return
	}
	defer sess.detach(tap)
	if write(func() error { return writeJSON(conn, reply(req.ID, attached, nil)) }) != nil ||
		write(func() error { return writeFrame(conn, frameBinary, replay) }) != nil {
		return
	}
	// The viewer never writes again; its read ending means it is gone.
	gone := make(chan struct{})
	go func() {
		var discard request
		for readJSON(conn, &discard) == nil {
		}
		close(gone)
	}()
	var filter queryFilter
	for {
		select {
		case chunk, ok := <-tap.output:
			if !ok {
				_ = write(func() error { return writeJSON(conn, tap.end) })
				return
			}
			out := filter.write(chunk)
			if len(out) == 0 {
				continue
			}
			if write(func() error { return writeFrame(conn, frameBinary, out) }) != nil {
				return
			}
		case <-gone:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) attachSession(req request) (Attached, []byte, *viewTap, *session, error) {
	var p AttachParams
	if err := decode(req.Params, &p); err != nil {
		return Attached{}, nil, nil, nil, err
	}
	sess, err := s.session(p.ID)
	if err != nil {
		return Attached{}, nil, nil, nil, err
	}
	if p.Cols != 0 || p.Rows != 0 {
		if err := sess.resize(p.Cols, p.Rows); err != nil {
			return Attached{}, nil, nil, nil, err
		}
	}
	deadline := time.Now().Add(attachSettle)
	for {
		attached, replay, tap, ok, err := sess.attach(p.Redraw)
		if err != nil {
			return Attached{}, nil, nil, nil, err
		}
		if ok {
			return attached, replay, tap, sess, nil
		}
		if time.Now().After(deadline) {
			return Attached{}, nil, nil, nil, errorf(ErrCodeInternal, "session %q stayed in the middle of a terminal sequence", p.ID)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Attachment is a viewer's stream of one Session; see AttachParams.
type Attachment struct {
	Attached
	// Replay rebuilds what the Session showed when the stream started.
	Replay []byte

	conn   *net.UnixConn
	output chan []byte
	end    AttachEnd
	err    error
	stop   context.CancelFunc
}

// Output carries the program's output after the replay, with terminal
// queries left out. It is closed when the stream ends; End then says why.
func (a *Attachment) Output() <-chan []byte { return a.output }

// End is how the stream ended, valid once Output is closed. An error means
// the stream was cut (the host stopped, or Close was called) before the host
// ended it.
func (a *Attachment) End() (AttachEnd, error) { return a.end, a.err }

// Close ends the stream.
func (a *Attachment) Close() error {
	a.stop()
	return nil
}

// Attach opens a viewer's stream of one Session on a connection of its own.
func (c *Client) Attach(ctx context.Context, params AttachParams) (*Attachment, error) {
	conn, _, err := dialHello(ctx, c.path, c.version)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := writeJSON(conn, request{ID: 1, Method: MethodAttach, Params: raw}); err != nil {
		conn.Close()
		return nil, err
	}
	var ack response
	if err := readJSON(conn, &ack); err != nil {
		conn.Close()
		return nil, err
	}
	if ack.Error != nil {
		conn.Close()
		return nil, ack.Error
	}
	a := &Attachment{conn: conn, output: make(chan []byte, tapBuffer)}
	if err := json.Unmarshal(ack.Result, &a.Attached); err != nil {
		conn.Close()
		return nil, err
	}
	kind, replay, err := readFrame(conn)
	if err != nil || kind != frameBinary {
		conn.Close()
		if err == nil {
			err = errorf(ErrCodeInternal, "attach: expected the replay, got a %q frame", kind)
		}
		return nil, err
	}
	a.Replay = replay
	streamCtx, stop := context.WithCancel(context.Background())
	a.stop = stop
	go func() {
		<-streamCtx.Done()
		conn.Close()
	}()
	go a.read(streamCtx)
	return a, nil
}

func (a *Attachment) read(ctx context.Context) {
	defer close(a.output)
	defer a.stop()
	for {
		kind, payload, err := readFrame(a.conn)
		if err != nil {
			a.err = err
			return
		}
		if kind == frameJSON {
			if err := json.Unmarshal(payload, &a.end); err != nil {
				a.err = err
			}
			return
		}
		select {
		case a.output <- payload:
		case <-ctx.Done():
			a.err = ctx.Err()
			return
		}
	}
}
