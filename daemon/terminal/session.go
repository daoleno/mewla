package terminal

import "context"

// EventType classifies terminal session events.
type EventType string

const (
	EventOutput EventType = "output"
	EventExit   EventType = "exit"
	EventError  EventType = "error"
	// EventSize: the size the output that follows is drawn for (Size), the
	// Session's, which another viewer may have set: a view whose own grid
	// differs draws the Session at this size until it takes the size back.
	EventSize EventType = "size"
)

// Event is emitted by a terminal session.
type Event struct {
	Type     EventType
	Data     string
	ExitCode int
	Err      error
	Size     Size
}

// OpenOptions configures a terminal session.
type OpenOptions struct {
	Cols int
	Rows int
	// Socket is the session host that runs the target.
	Socket string
}

// Size reports the current terminal dimensions.
type Size struct {
	Cols int
	Rows int
}

// Session represents a live terminal session.
type Session interface {
	ID() string
	Start(ctx context.Context) error
	Events() <-chan Event
	Write(data string) error
	Resize(cols, rows int) error
	Close() error
	Size() Size
}

// ScrollWriter is optionally implemented by sessions that tell typing from
// scrolling: the wheel presses or cursor keys a scroll drag made are reading,
// and must not take the Session's size from another viewer the way typing does.
type ScrollWriter interface {
	WriteScroll(data string) error
}

// Backend opens terminal sessions for a given target.
type Backend interface {
	Name() string
	Open(targetID string, opts OpenOptions) (Session, error)
}
