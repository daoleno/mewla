//go:build linux || darwin

package sessionhost

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/daoleno/mewla/daemon/ghosttyvt"
)

// screen is a Session's terminal: ghosttyvt.Terminal, or a fake in tests.
type screen interface {
	Write(p []byte) ghosttyvt.Output
	Resize(cols, rows int) (ghosttyvt.Output, error)
	Capture(ghosttyvt.CaptureOptions) (string, error)
	State() ghosttyvt.State
	EncodeKey(spec string) ([]byte, error)
	Snapshot() ([]byte, error)
	Close()
}

func newEngineScreen(opts ghosttyvt.Options) (screen, error) {
	term, err := ghosttyvt.New(opts)
	if err != nil {
		return nil, err
	}
	return term, nil
}

var (
	sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	metaKeyPattern   = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
)

const (
	maxLabels      = 16
	maxLabelBytes  = 1 << 10
	maxDataKeys    = 8
	maxDataBytes   = 64 << 10
	maxInputBytes  = 8 << 20
	maxInputSleep  = 10 * time.Second
	removeGrace    = 2 * time.Second
	readerDrain    = 2 * time.Second
	outputEventGap = 100 * time.Millisecond
	defaultTerm    = "xterm-256color"
)

var (
	bracketStart = []byte("\x1b[200~")
	bracketEnd   = []byte("\x1b[201~")
)

type session struct {
	id        string
	argv      []string
	dir       string
	createdAt time.Time
	cmd       *exec.Cmd
	ptmx      *os.File
	pid       int
	host      *Server

	inputMu sync.Mutex // held for a whole input script
	writeMu sync.Mutex // one write to the PTY at a time
	replies chan []byte

	mu           sync.Mutex
	screen       screen
	outputSeq    uint64
	lastOutputAt time.Time
	alive        bool
	exitCode     *int
	exitSignal   string
	exitedAt     time.Time
	status       *statusRecords
	labels       map[string]string
	data         map[string]string
	lastOutputEv time.Time
	outputTimer  *time.Timer
	closed       bool

	exited     chan struct{} // closed when the program has exited
	readerDone chan struct{} // closed when the PTY reader has stopped
}

func startSession(host *Server, params CreateParams) (*session, error) {
	if !sessionIDPattern.MatchString(params.ID) {
		return nil, errorf(ErrCodeBadRequest, "session id %q must be 1-64 of A-Z a-z 0-9 _ . : -", params.ID)
	}
	if len(params.Argv) == 0 || params.Argv[0] == "" {
		return nil, errorf(ErrCodeBadRequest, "argv is empty")
	}
	if params.Cols < 1 || params.Rows < 1 || params.Cols > 1000 || params.Rows > 1000 {
		return nil, errorf(ErrCodeBadRequest, "size %dx%d is outside 1x1..1000x1000", params.Cols, params.Rows)
	}
	if params.Dir == "" || !filepath.IsAbs(params.Dir) {
		return nil, errorf(ErrCodeBadRequest, "dir %q must be an absolute path", params.Dir)
	}
	if len(params.Labels) > maxLabels {
		return nil, errorf(ErrCodeBadRequest, "%d labels, over %d", len(params.Labels), maxLabels)
	}
	labels := make(map[string]string, len(params.Labels))
	for key, value := range params.Labels {
		if !metaKeyPattern.MatchString(key) || len(value) > maxLabelBytes {
			return nil, errorf(ErrCodeBadRequest, "label %q: keys are a-z 0-9 _ . - (64 at most), values %d bytes at most", key, maxLabelBytes)
		}
		labels[key] = value
	}
	env := sessionEnvironment(params.Env, params.ID)
	program, err := lookPath(params.Argv[0], env)
	if err != nil {
		return nil, errorf(ErrCodeBadRequest, "%v", err)
	}
	term, err := host.newScreen(ghosttyvt.Options{Cols: params.Cols, Rows: params.Rows})
	if err != nil {
		if errors.Is(err, ghosttyvt.ErrUnavailable) {
			return nil, errorf(ErrCodeUnavailable, "%v", err)
		}
		return nil, errorf(ErrCodeInternal, "terminal: %v", err)
	}
	cmd := exec.Command(program, params.Argv[1:]...)
	cmd.Args = append([]string(nil), params.Argv...)
	cmd.Env = env
	cmd.Dir = params.Dir
	// StartWithSize makes the program a session leader with the PTY as its
	// controlling terminal, so its process group is the Session's.
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(params.Cols), Rows: uint16(params.Rows)})
	if err != nil {
		term.Close()
		return nil, errorf(ErrCodeBadRequest, "start %s: %v", params.Argv[0], err)
	}
	s := &session{
		id:         params.ID,
		argv:       append([]string(nil), params.Argv...),
		dir:        params.Dir,
		createdAt:  time.Now().UTC(),
		cmd:        cmd,
		ptmx:       ptmx,
		pid:        cmd.Process.Pid,
		host:       host,
		replies:    make(chan []byte, 64),
		screen:     term,
		alive:      true,
		status:     newStatusRecords(),
		labels:     labels,
		data:       make(map[string]string),
		exited:     make(chan struct{}),
		readerDone: make(chan struct{}),
	}
	go s.readLoop()
	go s.replyLoop()
	go s.waitLoop()
	return s, nil
}

// sessionEnvironment is the caller's environment with TERM defaulted and
// MEWLA_SESSION_ID set to the Session's id.
func sessionEnvironment(base []string, id string) []string {
	env := make([]string, 0, len(base)+2)
	hasTerm := false
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "MEWLA_SESSION_ID" {
			continue
		}
		if key == "TERM" {
			hasTerm = true
		}
		env = append(env, entry)
	}
	if !hasTerm {
		env = append(env, "TERM="+defaultTerm)
	}
	return append(env, "MEWLA_SESSION_ID="+id)
}

// lookPath resolves argv[0] against the Session's PATH, not the host's.
func lookPath(name string, env []string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	path := ""
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			path = value
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s: not found in the Session's PATH", name)
}

func (s *session) readLoop() {
	defer close(s.readerDone)
	buf := make([]byte, 32<<10)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.consume(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (s *session) consume(p []byte) {
	now := time.Now().UTC()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	out := s.screen.Write(p)
	s.outputSeq += uint64(len(p))
	s.lastOutputAt = now
	var events []Event
	for _, report := range out.Status {
		record := s.status.apply(report, now)
		events = append(events, Event{Type: EventStatus, Session: s.id, Status: record, At: now})
	}
	if out.TitleChanged {
		events = append(events, Event{Type: EventTitle, Session: s.id, At: now})
	}
	if out.Bells > 0 {
		events = append(events, Event{Type: EventBell, Session: s.id, At: now})
	}
	if outputEvent, ok := s.outputEventLocked(now); ok {
		events = append(events, outputEvent)
	}
	s.mu.Unlock()
	s.queueReply(out.Reply)
	for _, event := range events {
		s.host.publish(event)
	}
}

// outputEventLocked coalesces output events to one per outputEventGap: the
// first output after a quiet gap is reported at once, the rest when the gap
// ends. Callers hold mu.
func (s *session) outputEventLocked(now time.Time) (Event, bool) {
	if now.Sub(s.lastOutputEv) >= outputEventGap {
		s.lastOutputEv = now
		return Event{Type: EventOutput, Session: s.id, OutputSeq: s.outputSeq, At: now}, true
	}
	if s.outputTimer == nil {
		s.outputTimer = time.AfterFunc(outputEventGap-now.Sub(s.lastOutputEv), func() {
			at := time.Now().UTC()
			s.mu.Lock()
			s.outputTimer = nil
			s.lastOutputEv = at
			seq, closed := s.outputSeq, s.closed
			s.mu.Unlock()
			if !closed {
				s.host.publish(Event{Type: EventOutput, Session: s.id, OutputSeq: seq, At: at})
			}
		})
	}
	return Event{}, false
}

// queueReply hands the terminal's answer to the reply writer without ever
// blocking: the reader must keep draining output even when the program stops
// reading its input, so a program that floods queries it never reads loses
// the answers that do not fit.
func (s *session) queueReply(reply []byte) {
	if len(reply) == 0 {
		return
	}
	select {
	case s.replies <- reply:
	default:
		s.host.logf("session %s: dropped a %d byte terminal reply; the program is not reading its input", s.id, len(reply))
	}
}

func (s *session) replyLoop() {
	for {
		select {
		case reply := <-s.replies:
			_ = s.writePTY(reply)
		case <-s.readerDone:
			return
		}
	}
}

func (s *session) waitLoop() {
	err := s.cmd.Wait()
	now := time.Now().UTC()
	code, signal := exitStatus(s.cmd.ProcessState, err)
	s.mu.Lock()
	s.alive = false
	s.exitCode = &code
	s.exitSignal = signal
	s.exitedAt = now
	s.status.programExited()
	s.mu.Unlock()
	close(s.exited)
	exitCode := code
	s.host.publish(Event{Type: EventExited, Session: s.id, ExitCode: &exitCode, At: now})
}

func exitStatus(state *os.ProcessState, err error) (int, string) {
	if state == nil {
		return -1, ""
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal()), signalName(status.Signal())
	}
	return state.ExitCode(), ""
}

func signalName(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGHUP:
		return SignalHangup
	case syscall.SIGINT:
		return SignalInterrupt
	case syscall.SIGTERM:
		return SignalTerminate
	case syscall.SIGKILL:
		return SignalKill
	}
	return sig.String()
}

func (s *session) isAlive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alive
}

func (s *session) writePTY(p []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.ptmx.Write(p)
	return err
}

// input runs an input script with no other input between its steps.
func (s *session) input(steps []InputStep) (bool, error) {
	if len(steps) == 0 {
		return false, errorf(ErrCodeBadRequest, "the input script is empty")
	}
	total := 0
	for i, step := range steps {
		kinds := 0
		if len(step.Bytes) > 0 {
			kinds++
		}
		if step.Paste != "" {
			kinds++
		}
		if step.Key != "" {
			kinds++
			if err := ghosttyvt.ValidKey(step.Key); err != nil {
				return false, errorf(ErrCodeBadRequest, "input step %d: %v", i, err)
			}
		}
		if step.SleepMS > 0 {
			kinds++
		}
		if kinds != 1 || step.SleepMS < 0 {
			return false, errorf(ErrCodeBadRequest, "input step %d must have exactly one of bytes, paste, key or sleep_ms", i)
		}
		if time.Duration(step.SleepMS)*time.Millisecond > maxInputSleep {
			return false, errorf(ErrCodeBadRequest, "input step %d sleeps longer than %s", i, maxInputSleep)
		}
		total += len(step.Bytes) + len(step.Paste)
	}
	if total > maxInputBytes {
		return false, errorf(ErrCodeBadRequest, "the input script carries %d bytes, over %d", total, maxInputBytes)
	}

	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	started := false
	for i, step := range steps {
		if !s.isAlive() {
			return started, errorf(ErrCodeExited, "the program exited before input step %d", i)
		}
		if step.SleepMS > 0 {
			select {
			case <-time.After(time.Duration(step.SleepMS) * time.Millisecond):
			case <-s.exited:
			}
			continue
		}
		data := step.Bytes
		switch {
		case step.Paste != "":
			data = s.pasteBytes(step.Paste)
		case step.Key != "":
			// Encoded now, for the modes the program has set by this step.
			s.mu.Lock()
			encoded, err := s.screen.EncodeKey(step.Key)
			s.mu.Unlock()
			if err != nil {
				return started, errorf(ErrCodeInputFailed, "input step %d: %v", i, err)
			}
			data = encoded
		}
		started = true
		if err := s.writePTY(data); err != nil {
			return started, errorf(ErrCodeInputFailed, "input step %d: %v", i, err)
		}
	}
	return started, nil
}

// pasteBytes wraps text in bracketed-paste markers when the program asked
// for them. Markers inside the text are removed so the text cannot end the
// paste early and type keys of its own.
func (s *session) pasteBytes(text string) []byte {
	data := []byte(text)
	s.mu.Lock()
	bracketed := s.screen.State().BracketedPaste
	s.mu.Unlock()
	if !bracketed {
		return data
	}
	data = bytes.ReplaceAll(data, bracketStart, nil)
	data = bytes.ReplaceAll(data, bracketEnd, nil)
	out := make([]byte, 0, len(data)+len(bracketStart)+len(bracketEnd))
	out = append(out, bracketStart...)
	out = append(out, data...)
	return append(out, bracketEnd...)
}

func (s *session) resize(cols, rows int) error {
	if cols < 1 || rows < 1 || cols > 1000 || rows > 1000 {
		return errorf(ErrCodeBadRequest, "size %dx%d is outside 1x1..1000x1000", cols, rows)
	}
	s.mu.Lock()
	out, err := s.screen.Resize(cols, rows)
	s.mu.Unlock()
	if err != nil {
		return errorf(ErrCodeInternal, "resize terminal: %v", err)
	}
	if err := pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil && s.isAlive() {
		return errorf(ErrCodeInternal, "resize PTY: %v", err)
	}
	s.queueReply(out.Reply)
	return nil
}

func (s *session) capture(params CaptureParams) (Capture, error) {
	opts := ghosttyvt.CaptureOptions{
		History:     params.History,
		Unwrap:      params.Unwrap,
		Trim:        params.Trim,
		Styled:      params.Styled,
		Reconstruct: params.Reconstruct,
	}
	switch params.Scope {
	case ScopeAll, "":
		opts.Scope = ghosttyvt.ScopeAll
	case ScopeScreen:
		opts.Scope = ghosttyvt.ScopeScreen
	case ScopeRecent:
		opts.Scope = ghosttyvt.ScopeRecent
	default:
		return Capture{}, errorf(ErrCodeBadRequest, "unknown capture scope %q", params.Scope)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	text, err := s.screen.Capture(opts)
	if err != nil {
		return Capture{}, errorf(ErrCodeInternal, "capture: %v", err)
	}
	state := s.screen.State()
	return Capture{
		Text:           text,
		Cols:           state.Cols,
		Rows:           state.Rows,
		Alt:            state.Alt,
		CursorX:        state.CursorX,
		CursorY:        state.CursorY,
		BracketedPaste: state.BracketedPaste,
		MouseTracking:  state.MouseTracking,
		ScrollbackRows: state.ScrollbackRows,
		OutputSeq:      s.outputSeq,
	}, nil
}

func (s *session) signal(name string) error {
	sig, ok := map[string]syscall.Signal{
		SignalInterrupt: syscall.SIGINT,
		SignalTerminate: syscall.SIGTERM,
		SignalHangup:    syscall.SIGHUP,
		SignalKill:      syscall.SIGKILL,
	}[name]
	if !ok {
		return errorf(ErrCodeBadRequest, "unknown signal %q", name)
	}
	if !s.isAlive() {
		return errorf(ErrCodeExited, "the program has exited")
	}
	// The program leads its own process group: signal all of it.
	if err := syscall.Kill(-s.pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return errorf(ErrCodeInternal, "signal %s: %v", name, err)
	}
	return nil
}

func (s *session) info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.screen.State()
	info := Info{
		ID:           s.id,
		PID:          s.pid,
		Argv:         append([]string(nil), s.argv...),
		Dir:          s.dir,
		Alive:        s.alive,
		ExitSignal:   s.exitSignal,
		Cols:         state.Cols,
		Rows:         state.Rows,
		Alt:          state.Alt,
		OutputSeq:    s.outputSeq,
		CreatedAt:    s.createdAt,
		LastOutputAt: s.lastOutputAt,
		ExitedAt:     s.exitedAt,
		Title:        state.Title,
		Status:       s.status.list(),
	}
	if len(s.labels) > 0 {
		info.Labels = make(map[string]string, len(s.labels))
		for key, value := range s.labels {
			info.Labels[key] = value
		}
	}
	if s.exitCode != nil {
		code := *s.exitCode
		info.ExitCode = &code
	}
	if s.alive {
		info.Cwd = processCwd(s.ptmx, s.pid)
	}
	if info.Cwd == "" {
		info.Cwd = state.Pwd
	}
	return info
}

func (s *session) getData(key string) (Data, error) {
	if !metaKeyPattern.MatchString(key) {
		return Data{}, errorf(ErrCodeBadRequest, "data key %q: a-z 0-9 _ . - (64 at most)", key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[key]
	return Data{Value: value, Present: ok}, nil
}

func (s *session) setData(key, value string) error {
	if !metaKeyPattern.MatchString(key) {
		return errorf(ErrCodeBadRequest, "data key %q: a-z 0-9 _ . - (64 at most)", key)
	}
	if len(value) > maxDataBytes {
		return errorf(ErrCodeBadRequest, "data %q is %d bytes, over %d", key, len(value), maxDataBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if value == "" {
		delete(s.data, key)
		return nil
	}
	if _, exists := s.data[key]; !exists && len(s.data) >= maxDataKeys {
		return errorf(ErrCodeBadRequest, "session %q already keeps %d data keys", s.id, maxDataKeys)
	}
	s.data[key] = value
	return nil
}

// close ends the program if it still runs (hangup, then kill after
// removeGrace) and frees the PTY and terminal.
func (s *session) close() {
	if s.isAlive() {
		_ = syscall.Kill(-s.pid, syscall.SIGHUP)
		select {
		case <-s.exited:
		case <-time.After(removeGrace):
			_ = syscall.Kill(-s.pid, syscall.SIGKILL)
			<-s.exited
		}
	}
	_ = s.ptmx.Close()
	select {
	case <-s.readerDone:
	case <-time.After(readerDrain):
	}
	s.mu.Lock()
	s.closed = true
	if s.outputTimer != nil {
		s.outputTimer.Stop()
	}
	s.screen.Close()
	s.mu.Unlock()
}
