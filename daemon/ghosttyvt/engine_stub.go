//go:build !ghosttyvt || !cgo

package ghosttyvt

// Available reports whether this build has the terminal engine.
func Available() bool { return false }

// Version is the libghostty-vt version of this build, "" without the engine.
func Version() string { return "" }

// Terminal is one terminal. A build without the engine never makes one.
type Terminal struct{}

// New makes a terminal; without the engine it is ErrUnavailable.
func New(Options) (*Terminal, error) { return nil, ErrUnavailable }

// Restore rebuilds a terminal from a snapshot; without the engine it is
// ErrUnavailable.
func Restore([]byte, Options) (*Terminal, error) { return nil, ErrUnavailable }

func (*Terminal) Write([]byte) Output                    { return Output{} }
func (*Terminal) Resize(int, int) (Output, error)        { return Output{}, ErrUnavailable }
func (*Terminal) Capture(CaptureOptions) (string, error) { return "", ErrUnavailable }
func (*Terminal) State() State                           { return State{} }
func (*Terminal) RowFlags(int, int) []uint8              { return nil }
func (*Terminal) Snapshot() ([]byte, error)              { return nil, ErrUnavailable }
func (*Terminal) Replay() ([]byte, error)                { return nil, ErrUnavailable }
func (*Terminal) Close()                                 {}
func (*Terminal) EncodeKey(string) ([]byte, error)       { return nil, ErrUnavailable }
