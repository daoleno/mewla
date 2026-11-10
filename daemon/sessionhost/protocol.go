// Package sessionhost is the Mewla session host: one long-lived process per
// state directory that owns every Session's PTY and terminal, so Sessions
// outlive daemon restarts and upgrades. The daemon reaches it over a local
// socket that only the same user can use.
//
// A connection starts with a hello and the host's welcome, which carries the
// protocol and versions on both sides. A daemon newer than its host is the
// normal state after an upgrade: the host answers a method it does not know
// with ErrCodeUnknownMethod, and the daemon reads the welcome to know what it
// may ask for.
package sessionhost

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// ProtocolVersion is the host protocol this build speaks.
const ProtocolVersion = 1

// SnapshotFormat is the terminal snapshot format this build writes for an
// in-place upgrade.
const SnapshotFormat = 1

// A frame is a 4-byte big-endian length of the rest, a kind byte and the
// payload. Control messages are JSON; frameBinary is reserved for terminal
// streams.
const (
	frameJSON   byte = 'J'
	frameBinary byte = 'B'

	maxFrameBytes = 16 << 20
)

// Methods.
const (
	MethodCreate    = "create"
	MethodList      = "list"
	MethodInfo      = "info"
	MethodInput     = "input"
	MethodResize    = "resize"
	MethodCapture   = "capture"
	MethodSignal    = "signal"
	MethodRemove    = "remove"
	MethodSubscribe = "subscribe"
	MethodShutdown  = "shutdown"
	MethodGetData   = "get_data"
	MethodSetData   = "set_data"
	MethodAttach    = "attach"
)

// Error codes.
const (
	ErrCodeUnknownMethod = "unknown_method"
	ErrCodeBadRequest    = "bad_request"
	ErrCodeNotFound      = "not_found"
	ErrCodeExists        = "exists"
	ErrCodeExited        = "exited"
	ErrCodeUnavailable   = "unavailable" // the host has no terminal engine
	ErrCodeInputFailed   = "input_failed"
	ErrCodeBusy          = "busy"
	ErrCodeInternal      = "internal"
)

// Error is a host error.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("session host: %s: %s", e.Code, e.Message) }

// IsCode reports whether err is a host error with the given code.
func IsCode(err error, code string) bool {
	var hostErr *Error
	return errors.As(err, &hostErr) && hostErr.Code == code
}

func errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Hello is the first message on a connection.
type Hello struct {
	Protocol int    `json:"protocol"`
	Client   string `json:"client"` // the client's version, for logs
}

// Welcome is the host's answer to Hello.
type Welcome struct {
	Protocol       int       `json:"protocol"`
	HostVersion    string    `json:"host_version"`
	Engine         bool      `json:"engine"` // the host can run Sessions
	EngineVersion  string    `json:"engine_version,omitempty"`
	SnapshotFormat int       `json:"snapshot_format"`
	PID            int       `json:"pid"`
	StartedAt      time.Time `json:"started_at"`
}

type request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// CreateParams starts a Session.
type CreateParams struct {
	ID   string   `json:"id"`
	Argv []string `json:"argv"`
	// Env is the whole environment. The host adds TERM when it is missing
	// and MEWLA_SESSION_ID always.
	Env  []string `json:"env"`
	Dir  string   `json:"dir"`
	Cols int      `json:"cols"`
	Rows int      `json:"rows"`
	// Labels are what the caller records on the Session, fixed at create
	// and reported by Info: up to maxLabels keys, maxLabelBytes each.
	Labels map[string]string `json:"labels,omitempty"`
}

// A Session's labels and data live and die with it, like tmux pane options:
// they outlast the program's exit and go with remove. An in-place upgrade
// (the snapshot) must carry both, or a re-sent input would not find the
// receipt that says it already landed.

// DataParams reads (get_data) or writes (set_data) one opaque value kept on a
// Session. Writing "" removes it.
type DataParams struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

// Data is a value read with get_data.
type Data struct {
	Value   string `json:"value"`
	Present bool   `json:"present"`
}

// IDParams names a Session.
type IDParams struct {
	ID string `json:"id"`
}

// InputStep is one step of an input script. Exactly one field is set.
type InputStep struct {
	// Bytes are written as they are: keys, control characters.
	Bytes []byte `json:"bytes,omitempty"`
	// Paste is written as one paste, inside bracketed-paste markers when the
	// program asked for them, with line feeds kept as they are.
	Paste string `json:"paste,omitempty"`
	// Key is one key press in tmux's notation ("Enter", "C-u", "BTab", "Up",
	// "q"), encoded the way a terminal would for the modes the program set.
	Key string `json:"key,omitempty"`
	// SleepMS waits before the next step, with the script still holding the
	// Session's input.
	SleepMS int `json:"sleep_ms,omitempty"`
}

// InputParams runs an input script. No other input reaches the Session
// while the script runs.
type InputParams struct {
	ID    string      `json:"id"`
	Steps []InputStep `json:"steps"`
}

// InputResult says whether any byte of the script may have reached the
// program. It comes with a failed script too.
type InputResult struct {
	Started bool `json:"started"`
}

// ResizeParams resizes a Session's terminal.
type ResizeParams struct {
	ID   string `json:"id"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// Capture scopes.
const (
	ScopeAll    = "all"
	ScopeScreen = "screen"
	ScopeRecent = "recent"
	// ScopeHistory is the history above the screen alone, up to
	// CaptureParams.History of its last rows (all of it when 0).
	ScopeHistory = "history"
)

// CaptureParams reads a Session's terminal as text.
type CaptureParams struct {
	ID      string `json:"id"`
	Scope   string `json:"scope"`             // ScopeAll, ScopeScreen or ScopeRecent
	History int    `json:"history,omitempty"` // rows above the screen, for ScopeRecent and ScopeHistory
	Unwrap  bool   `json:"unwrap,omitempty"`
	Trim    bool   `json:"trim,omitempty"`
	Styled  bool   `json:"styled,omitempty"`
	// Marks asks for the flags of the captured history rows, in
	// Capture.RowFlags. A host that predates it leaves them out.
	Marks bool `json:"marks,omitempty"`
}

// Capture is a capture and the terminal state it was taken in.
type Capture struct {
	Text           string `json:"text"`
	Cols           int    `json:"cols"`
	Rows           int    `json:"rows"`
	Alt            bool   `json:"alt"`
	CursorX        int    `json:"cursor_x"`
	CursorY        int    `json:"cursor_y"`
	BracketedPaste bool   `json:"bracketed_paste"`
	MouseTracking  bool   `json:"mouse_tracking"`
	// AppCursor and AltScroll are the cursor-key mode (DECCKM) and the
	// wheel-sends-cursor-keys mode (1007). They are only ever sent when the
	// host knows them: Modes says it does, so a host that predates them reads
	// as unknown rather than as off.
	Modes          bool `json:"modes,omitempty"`
	AppCursor      bool `json:"app_cursor,omitempty"`
	AltScroll      bool `json:"alt_scroll,omitempty"`
	ScrollbackRows int  `json:"scrollback_rows"`
	// RowFlags holds one ghosttyvt.Row* byte per captured history row when
	// the capture asked for Marks and the host could read them.
	RowFlags  []byte `json:"row_flags,omitempty"`
	OutputSeq uint64 `json:"output_seq"`
}

// AttachParams opens a view of one Session, on a connection of its own as
// subscribe does. Cols and Rows, when both are set, resize the Session first:
// the viewer that attaches last sets the size, and one that only watches
// leaves them 0.
//
// The answer is Attached, then a binary frame with the replay that rebuilds
// what the Session shows on a fresh terminal of that size, then a binary frame
// for each output after it. Terminal queries are left out of the output: the
// host answers them, and a second answer from a viewer would reach the program
// as typing. The stream ends with an AttachEnd JSON frame; input goes through
// the input method as usual.
type AttachParams struct {
	ID   string `json:"id"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// Attached answers attach.
type Attached struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
	// OutputSeq is the output the replay already shows.
	OutputSeq uint64 `json:"output_seq"`
}

// Why an attach stream ended.
const (
	// AttachExited: the program exited and its last output came first.
	AttachExited = "exited"
	// AttachRemoved: the Session was removed.
	AttachRemoved = "removed"
	// AttachBehind: the viewer read too slowly and missed output. Attach
	// again for a fresh replay.
	AttachBehind = "behind"
)

// AttachEnd is the last frame of an attach stream.
type AttachEnd struct {
	Reason   string `json:"reason"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

// Signals.
const (
	SignalInterrupt = "interrupt"
	SignalTerminate = "terminate"
	SignalHangup    = "hangup"
	SignalKill      = "kill"
)

// SignalParams signals a Session's process group.
type SignalParams struct {
	ID     string `json:"id"`
	Signal string `json:"signal"`
}

// ShutdownParams stops the host. Without Force it refuses while a Session's
// program is running.
type ShutdownParams struct {
	Force bool `json:"force,omitempty"`
}

// ProgramStatus is one OSC 7501 record a Session's program keeps. Title and
// Message are untrusted text from the program.
type ProgramStatus struct {
	ID        string    `json:"id,omitempty"`
	State     string    `json:"state"`
	Kind      string    `json:"kind,omitempty"`
	Progress  int       `json:"progress"`
	App       string    `json:"app,omitempty"`
	Title     string    `json:"title,omitempty"`
	Message   string    `json:"message,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Info is a Session as the host sees it.
type Info struct {
	ID           string    `json:"id"`
	PID          int       `json:"pid"`
	Argv         []string  `json:"argv"`
	Dir          string    `json:"dir"`
	Alive        bool      `json:"alive"`
	ExitCode     *int      `json:"exit_code,omitempty"`   // set once the program has exited
	ExitSignal   string    `json:"exit_signal,omitempty"` // the signal that ended it, if any
	Cols         int       `json:"cols"`
	Rows         int       `json:"rows"`
	Alt          bool      `json:"alt"`
	OutputSeq    uint64    `json:"output_seq"` // bytes of output so far
	CreatedAt    time.Time `json:"created_at"`
	LastOutputAt time.Time `json:"last_output_at,omitzero"`
	ExitedAt     time.Time `json:"exited_at,omitzero"`
	Title        string    `json:"title,omitempty"`
	Cwd          string    `json:"cwd,omitempty"`
	// Command names the foreground process, as tmux's pane_current_command.
	Command string            `json:"command,omitempty"`
	Status  []ProgramStatus   `json:"status,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

// Event types.
const (
	EventCreated = "created"
	EventOutput  = "output" // coalesced: at most one per Session per outputEventInterval
	EventStatus  = "status"
	EventTitle   = "title"
	EventBell    = "bell"
	EventExited  = "exited"
	EventRemoved = "removed"
)

// Event is pushed to subscribers.
type Event struct {
	Type      string         `json:"type"`
	Session   string         `json:"session"`
	OutputSeq uint64         `json:"output_seq,omitempty"`
	Status    *ProgramStatus `json:"status,omitempty"`
	ExitCode  *int           `json:"exit_code,omitempty"`
	At        time.Time      `json:"at"`
}

func writeFrame(w io.Writer, kind byte, payload []byte) error {
	if len(payload)+1 > maxFrameBytes {
		return fmt.Errorf("frame of %d bytes is over the %d byte limit", len(payload)+1, maxFrameBytes)
	}
	header := make([]byte, 5, 5+len(payload))
	binary.BigEndian.PutUint32(header, uint32(len(payload)+1))
	header[4] = kind
	_, err := w.Write(append(header, payload...))
	return err
}

func readFrame(r io.Reader) (byte, []byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 1 || size > maxFrameBytes {
		return 0, nil, fmt.Errorf("frame of %d bytes is outside 1..%d", size, maxFrameBytes)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return body[0], body[1:], nil
}

func writeJSON(w io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeFrame(w, frameJSON, payload)
}

func readJSON(r io.Reader, value any) error {
	kind, payload, err := readFrame(r)
	if err != nil {
		return err
	}
	if kind != frameJSON {
		return fmt.Errorf("expected a JSON frame, got kind %q", kind)
	}
	return json.Unmarshal(payload, value)
}
