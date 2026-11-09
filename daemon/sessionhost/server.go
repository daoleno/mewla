package sessionhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
)

const (
	helloTimeout       = 10 * time.Second
	maxInFlightPerConn = 64
	subscriberBuffer   = 512
)

// socketCheckEvery is how often the host checks that its socket file is still
// there.
const socketCheckEvery = 10 * time.Second

// ServerOptions configures a host.
type ServerOptions struct {
	SocketPath  string
	HostVersion string
	Logf        func(format string, args ...any)

	// Test seams.
	newScreen   func(ghosttyvt.Options) (screen, error)
	engine      *bool
	allowPeer   func(uid uint32) bool
	socketCheck time.Duration
}

// Server is the session host.
type Server struct {
	opts      ServerOptions
	newScreen func(ghosttyvt.Options) (screen, error)
	engine    bool
	allowPeer func(uid uint32) bool
	startedAt time.Time

	mu          sync.Mutex
	sessions    map[string]*session
	subscribers map[chan Event]struct{}
	conns       map[*net.UnixConn]struct{}
	stop        context.CancelFunc
	closing     bool // set once Run ends the Sessions; no new ones start
}

// NewServer makes a host; Run serves it.
func NewServer(opts ServerOptions) *Server {
	s := &Server{
		opts:        opts,
		newScreen:   opts.newScreen,
		engine:      ghosttyvt.Available(),
		allowPeer:   opts.allowPeer,
		startedAt:   time.Now().UTC(),
		sessions:    make(map[string]*session),
		subscribers: make(map[chan Event]struct{}),
		conns:       make(map[*net.UnixConn]struct{}),
	}
	if s.newScreen == nil {
		s.newScreen = newEngineScreen
	}
	if opts.engine != nil {
		s.engine = *opts.engine
	}
	if s.allowPeer == nil {
		own := uint32(os.Getuid())
		s.allowPeer = func(uid uint32) bool { return uid == own }
	}
	return s
}

func (s *Server) logf(format string, args ...any) {
	if s.opts.Logf != nil {
		s.opts.Logf(format, args...)
		return
	}
	log.Printf("session host: "+format, args...)
}

// Run serves until ctx ends or a shutdown request; every Session's program
// is ended before it returns.
func (s *Server) Run(ctx context.Context) error {
	listener, lock, err := listen(s.opts.SocketPath)
	if err != nil {
		return err
	}
	defer lock.Close()
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.stop = cancel
	s.mu.Unlock()
	defer cancel()

	var listenerMu sync.Mutex
	current := listener
	closeListener := func() {
		listenerMu.Lock()
		current.Close()
		listenerMu.Unlock()
	}
	go func() {
		<-ctx.Done()
		closeListener()
	}()
	go s.keepSocket(ctx, &listenerMu, &current)

	s.logf("listening on %s (protocol %d, engine %v %s)", s.opts.SocketPath, ProtocolVersion, s.engine, ghosttyvt.Version())
	var wg sync.WaitGroup
	for {
		listenerMu.Lock()
		l := current
		listenerMu.Unlock()
		conn, err := l.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			listenerMu.Lock()
			replaced := current != l
			listenerMu.Unlock()
			if replaced {
				continue
			}
			s.logf("accept: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.serveConn(ctx, conn)
		}()
	}
	s.closeAll()
	s.mu.Lock()
	for ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, ch)
	}
	for conn := range s.conns {
		conn.Close()
	}
	s.mu.Unlock()
	wg.Wait()
	_ = os.Remove(s.opts.SocketPath)
	s.logf("stopped; every Session's program has ended")
	return nil
}

// keepSocket listens again when the socket file disappears, as it does when
// logind empties XDG_RUNTIME_DIR, so a daemon can still reach the Sessions.
func (s *Server) keepSocket(ctx context.Context, mu *sync.Mutex, current **net.UnixListener) {
	every := s.opts.socketCheck
	if every <= 0 {
		every = socketCheckEvery
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if _, err := os.Lstat(s.opts.SocketPath); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := ensurePrivateDir(filepath.Dir(s.opts.SocketPath), os.Getuid()); err != nil {
			s.logf("socket %s is gone and its directory cannot be made again: %v", s.opts.SocketPath, err)
			continue
		}
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: s.opts.SocketPath, Net: "unix"})
		if err != nil {
			s.logf("listen again on %s: %v", s.opts.SocketPath, err)
			continue
		}
		listener.SetUnlinkOnClose(false)
		_ = os.Chmod(s.opts.SocketPath, 0o600)
		mu.Lock()
		old := *current
		*current = listener
		mu.Unlock()
		old.Close()
		s.logf("socket %s was removed; listening on it again", s.opts.SocketPath)
	}
}

func (s *Server) welcome() Welcome {
	w := Welcome{
		Protocol:       ProtocolVersion,
		HostVersion:    s.opts.HostVersion,
		Engine:         s.engine,
		SnapshotFormat: SnapshotFormat,
		PID:            os.Getpid(),
		StartedAt:      s.startedAt,
	}
	if s.engine {
		w.EngineVersion = ghosttyvt.Version()
	}
	return w
}

func (s *Server) serveConn(ctx context.Context, conn *net.UnixConn) {
	s.mu.Lock()
	s.conns[conn] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		conn.Close()
	}()
	uid, err := peerUID(conn)
	if err != nil {
		s.logf("refused a connection: peer credentials: %v", err)
		return
	}
	if !s.allowPeer(uid) {
		// Whoever connects can type into every agent: only this user may.
		s.logf("refused a connection from uid %d", uid)
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(helloTimeout))
	var hello Hello
	if err := readJSON(conn, &hello); err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	var writeMu sync.Mutex
	if err := writeJSON(conn, s.welcome()); err != nil {
		return
	}
	inFlight := make(chan struct{}, maxInFlightPerConn)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		var req request
		if err := readJSON(conn, &req); err != nil {
			return
		}
		switch req.Method {
		case MethodSubscribe:
			s.serveEvents(ctx, conn, req, &writeMu)
			return
		case MethodAttach:
			s.serveAttach(ctx, conn, req, &writeMu)
			return
		}
		inFlight <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-inFlight }()
			resp := s.handle(req)
			writeMu.Lock()
			defer writeMu.Unlock()
			_ = writeJSON(conn, resp)
		}()
	}
}

func (s *Server) serveEvents(ctx context.Context, conn *net.UnixConn, req request, writeMu *sync.Mutex) {
	events := make(chan Event, subscriberBuffer)
	s.mu.Lock()
	s.subscribers[events] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if _, ok := s.subscribers[events]; ok {
			delete(s.subscribers, events)
			close(events)
		}
		s.mu.Unlock()
	}()
	writeMu.Lock()
	err := writeJSON(conn, response{ID: req.ID, Result: json.RawMessage(`{}`)})
	writeMu.Unlock()
	if err != nil {
		return
	}
	// The subscriber never writes again; its read ending means it is gone.
	gone := make(chan struct{})
	go func() {
		var discard request
		for readJSON(conn, &discard) == nil {
		}
		close(gone)
	}()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			writeMu.Lock()
			err := writeJSON(conn, event)
			writeMu.Unlock()
			if err != nil {
				return
			}
		case <-gone:
			return
		case <-ctx.Done():
			return
		}
	}
}

// publish sends an event to every subscriber. A subscriber too slow to keep
// up is dropped, and reconnects and lists again, rather than slowing the
// Sessions down.
func (s *Server) publish(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
			delete(s.subscribers, ch)
			close(ch)
			s.logf("dropped an event subscriber that fell behind")
		}
	}
}

func (s *Server) handle(req request) response {
	result, err := s.dispatch(req)
	return reply(req.ID, result, err)
}

// reply is the response to request id: its result, or its error.
func reply(id uint64, result any, err error) response {
	resp := response{ID: id}
	if result != nil {
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			err = errorf(ErrCodeInternal, "encode result: %v", marshalErr)
		} else {
			resp.Result = payload
		}
	}
	if err != nil {
		var hostErr *Error
		if !errors.As(err, &hostErr) {
			hostErr = errorf(ErrCodeInternal, "%v", err)
		}
		resp.Error = hostErr
	}
	return resp
}

func decode(raw json.RawMessage, value any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return errorf(ErrCodeBadRequest, "params: %v", err)
	}
	return nil
}

func (s *Server) dispatch(req request) (any, error) {
	switch req.Method {
	case MethodCreate:
		var p CreateParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return s.create(p)
	case MethodList:
		return s.list(), nil
	case MethodInfo:
		var p IDParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		sess, err := s.session(p.ID)
		if err != nil {
			return nil, err
		}
		return sess.info(), nil
	case MethodInput:
		var p InputParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		sess, err := s.session(p.ID)
		if err != nil {
			return InputResult{}, err
		}
		started, err := sess.input(p.Steps)
		return InputResult{Started: started}, err
	case MethodResize:
		var p ResizeParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		sess, err := s.session(p.ID)
		if err != nil {
			return nil, err
		}
		return struct{}{}, sess.resize(p.Cols, p.Rows)
	case MethodCapture:
		var p CaptureParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		sess, err := s.session(p.ID)
		if err != nil {
			return nil, err
		}
		return sess.capture(p)
	case MethodSignal:
		var p SignalParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		sess, err := s.session(p.ID)
		if err != nil {
			return nil, err
		}
		return struct{}{}, sess.signal(p.Signal)
	case MethodRemove:
		var p IDParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return s.remove(p.ID)
	case MethodGetData, MethodSetData:
		var p DataParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		sess, err := s.session(p.ID)
		if err != nil {
			return nil, err
		}
		if req.Method == MethodGetData {
			return sess.getData(p.Key)
		}
		return struct{}{}, sess.setData(p.Key, p.Value)
	case MethodShutdown:
		var p ShutdownParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return struct{}{}, s.shutdown(p.Force)
	}
	return nil, errorf(ErrCodeUnknownMethod, "this host (protocol %d, %s) has no method %q", ProtocolVersion, s.opts.HostVersion, req.Method)
}

func (s *Server) create(p CreateParams) (Info, error) {
	if !s.engine {
		return Info{}, errorf(ErrCodeUnavailable, "%v", ghosttyvt.ErrUnavailable)
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return Info{}, errorf(ErrCodeUnavailable, "the session host is stopping")
	}
	if _, exists := s.sessions[p.ID]; exists {
		s.mu.Unlock()
		return Info{}, errorf(ErrCodeExists, "session %q exists", p.ID)
	}
	// Reserve the id while the program starts.
	s.sessions[p.ID] = nil
	s.mu.Unlock()
	sess, err := startSession(s, p)
	s.mu.Lock()
	if err != nil {
		delete(s.sessions, p.ID)
		s.mu.Unlock()
		return Info{}, err
	}
	if s.closing {
		// Run ended the Sessions while this one started.
		s.mu.Unlock()
		sess.close()
		return Info{}, errorf(ErrCodeUnavailable, "the session host is stopping")
	}
	s.sessions[p.ID] = sess
	s.mu.Unlock()
	info := sess.info()
	s.publish(Event{Type: EventCreated, Session: p.ID, At: info.CreatedAt})
	return info, nil
}

func (s *Server) session(id string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	if sess == nil {
		return nil, errorf(ErrCodeNotFound, "no session %q", id)
	}
	return sess, nil
}

func (s *Server) list() []Info {
	s.mu.Lock()
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if sess != nil {
			sessions = append(sessions, sess)
		}
	}
	s.mu.Unlock()
	infos := make([]Info, 0, len(sessions))
	for _, sess := range sessions {
		infos = append(infos, sess.info())
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].CreatedAt.Before(infos[j].CreatedAt) })
	return infos
}

func (s *Server) remove(id string) (Info, error) {
	sess, err := s.session(id)
	if err != nil {
		return Info{}, err
	}
	sess.close()
	info := sess.info()
	s.mu.Lock()
	if s.sessions[id] == sess {
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	s.publish(Event{Type: EventRemoved, Session: id, ExitCode: info.ExitCode, At: time.Now().UTC()})
	return info, nil
}

func (s *Server) shutdown(force bool) error {
	if !force {
		for _, info := range s.list() {
			if info.Alive {
				return errorf(ErrCodeBusy, "session %q is still running; shut down with force to end it", info.ID)
			}
		}
	}
	s.mu.Lock()
	stop := s.stop
	s.mu.Unlock()
	if stop == nil {
		return fmt.Errorf("the host is not running")
	}
	// Answer first; Run ends every Session after the listener closes.
	time.AfterFunc(50*time.Millisecond, stop)
	return nil
}

func (s *Server) closeAll() {
	s.mu.Lock()
	s.closing = true
	sessions := make([]*session, 0, len(s.sessions))
	for id, sess := range s.sessions {
		if sess != nil {
			sessions = append(sessions, sess)
		}
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, sess := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess.close()
		}()
	}
	wg.Wait()
}
