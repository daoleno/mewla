// Package ghosttyvt is the session host's terminal engine: one libghostty-vt
// terminal per Session, fed with the program's output, answering its
// queries, and read back as screen captures, program status reports and
// snapshots.
//
// The engine is cgo over the static libghostty-vt built by
// scripts/build-libghostty-desktop.sh, and is compiled only with the build
// tag ghosttyvt. Without the tag, or without cgo, Available reports false and
// New returns ErrUnavailable, so every untagged build of the daemon stays a
// pure Go build.
package ghosttyvt

import "errors"

// ErrUnavailable is returned by every constructor of a build without the
// terminal engine.
var ErrUnavailable = errors.New("terminal engine unavailable: this build has no libghostty-vt (build tag ghosttyvt)")

// Options configures a new terminal.
type Options struct {
	Cols, Rows int
	// ScrollbackLines bounds the primary screen's history; 0 keeps
	// DefaultScrollbackLines.
	ScrollbackLines int
	// XTVersion is the answer to an XTVERSION query (CSI > q); "" answers
	// with DefaultXTVersion.
	XTVersion string
	// KittyGraphics lets programs store images with the Kitty graphics
	// protocol. Off by default: a capture is text, so a program that draws
	// with images would hide what it shows from every Mewla surface.
	KittyGraphics bool
}

// DefaultScrollbackLines is the history a terminal keeps unless told
// otherwise; it matches the tmux history-limit Mewla sessions ran with.
const DefaultScrollbackLines = 10000

// DefaultXTVersion is the terminal name programs see in an XTVERSION reply.
const DefaultXTVersion = "mewla"

// StatusState is the state of an OSC 7501 program status report.
type StatusState int

const (
	StatusIdle StatusState = iota
	StatusWorking
	StatusDone
	StatusBlocked
	StatusError
	// StatusClear removes the record with the report's id and every record
	// beneath it; an empty id removes all of them.
	StatusClear
)

var statusStateNames = [...]string{"idle", "working", "done", "blocked", "error", "clear"}

func (s StatusState) String() string {
	if s >= 0 && int(s) < len(statusStateNames) {
		return statusStateNames[s]
	}
	return "unknown"
}

// StatusKind says what a blocked program waits for.
type StatusKind int

const (
	StatusKindNone StatusKind = iota
	StatusKindPermission
	StatusKindQuestion
	StatusKindAuth
)

var statusKindNames = [...]string{"", "permission", "question", "auth"}

func (k StatusKind) String() string {
	if k >= 0 && int(k) < len(statusKindNames) {
		return statusKindNames[k]
	}
	return "unknown"
}

// Status is one OSC 7501 program status report, decoded. Title and Message
// are untrusted text from the program.
type Status struct {
	State    StatusState
	Kind     StatusKind
	Progress int // 0-100, or -1 when the report carries none
	ID       string
	App      string
	Title    string
	Message  string
}

// Output is what one Write produced besides the screen change.
type Output struct {
	// Reply holds the bytes the terminal answers the program with (device
	// attributes, cursor reports, program status support, ...). The caller
	// writes them back to the program's input.
	Reply []byte
	// Status holds the program status reports, in order.
	Status []Status
	// TitleChanged reports that the program set the window title.
	TitleChanged bool
	// Bells counts BEL characters.
	Bells int
}

// Scope selects the rows a capture covers.
type Scope int

const (
	// ScopeAll is the whole history and the screen.
	ScopeAll Scope = iota
	// ScopeScreen is the screen only.
	ScopeScreen
	// ScopeRecent is the screen and up to CaptureOptions.History rows of
	// history above it.
	ScopeRecent
	// ScopeHistory is the history above the screen alone: its last
	// CaptureOptions.History rows, or all of it when History is 0.
	ScopeHistory
)

// CaptureOptions shapes a capture.
type CaptureOptions struct {
	Scope   Scope
	History int // rows of history above the screen, for ScopeRecent and ScopeHistory
	// Unwrap joins rows that a program's long line wrapped onto.
	Unwrap bool
	// Trim drops trailing whitespace on each row and trailing blank rows.
	Trim bool
	// Styled emits the text with its SGR styles, each row on its own.
	Styled bool
}

// State is a terminal's current shape and modes.
type State struct {
	Cols, Rows       int
	Alt              bool // the alternate screen is active
	CursorX, CursorY int  // zero-based, on the screen
	CursorVisible    bool
	BracketedPaste   bool // the program asked for bracketed paste (mode 2004)
	MouseTracking    bool // the program asked for mouse reports
	AppCursor        bool // application cursor keys (DECCKM, mode 1)
	AltScroll        bool // the wheel sends cursor keys on the alternate screen (mode 1007)
	// MouseFormat is how mouse reports are encoded: "sgr" (mode 1006),
	// "urxvt" (1015), "utf8" (1005) or "x10", the default.
	MouseFormat    string
	ScrollbackRows int
	TotalRows      int
	Title          string
	Pwd            string // the working directory the program reported (OSC 7), as an absolute local path (LocalDir)
	Ground         bool   // the parser is between sequences
}

// Row flags, as RowFlags reports them.
const (
	RowWrapped      = 1 << iota // soft-wrapped onto the next row
	RowContinuation             // continues the row above
	RowPrompt                   // starts a shell prompt (OSC 133)
	RowPromptMore               // continues a shell prompt
)

// mouseFormat names the mouse report encoding the modes set: the last of
// them a program sets wins in xterm, but programs set one, and SGR is checked
// first as the one they all prefer.
func mouseFormat(on func(mode uint16) bool) string {
	switch {
	case on(1006):
		return "sgr"
	case on(1015):
		return "urxvt"
	case on(1005):
		return "utf8"
	default:
		return "x10"
	}
}
