//go:build windows

package sessionhost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Windows has no session host yet: its ConPTY backend follows once the host
// protocol settles. Until then every entry point reports the host as absent,
// so the daemon keeps the Labs switch off and nothing starts a host.

var ErrClosed = errors.New("session host connection closed")

var errNoHost = &Error{Code: ErrCodeUnavailable, Message: "the session host is not available on Windows yet"}

// Client is the Windows placeholder for a host connection; Dial never
// returns one.
type Client struct{}

func Dial(context.Context, string, string) (*Client, error) { return nil, errNoHost }

func (c *Client) Welcome() Welcome      { return Welcome{} }
func (c *Client) Done() <-chan struct{} { done := make(chan struct{}); close(done); return done }
func (c *Client) Close() error          { return nil }
func (c *Client) List(context.Context) ([]Info, error) {
	return nil, errNoHost
}
func (c *Client) Create(context.Context, CreateParams) (Info, error) { return Info{}, errNoHost }
func (c *Client) Info(context.Context, string) (Info, error)         { return Info{}, errNoHost }
func (c *Client) Input(context.Context, string, []InputStep) (bool, error) {
	return false, errNoHost
}
func (c *Client) Resize(context.Context, string, int, int) error { return errNoHost }
func (c *Client) Capture(context.Context, CaptureParams) (Capture, error) {
	return Capture{}, errNoHost
}
func (c *Client) Signal(context.Context, string, string) error          { return errNoHost }
func (c *Client) Remove(context.Context, string) (Info, error)          { return Info{}, errNoHost }
func (c *Client) GetData(context.Context, string, string) (Data, error) { return Data{}, errNoHost }
func (c *Client) SetData(context.Context, string, string, string) error { return errNoHost }
func (c *Client) Shutdown(context.Context, bool) error                  { return errNoHost }
func (c *Client) Subscribe(context.Context) (<-chan Event, error)       { return nil, errNoHost }

// StartOptions mirror the Unix ones so callers build unchanged.
type StartOptions struct {
	SocketPath    string
	Executable    string
	Args          []string
	Env           []string
	LogPath       string
	ClientVersion string
	Wait          time.Duration
}

func Connect(context.Context, StartOptions) (*Client, bool, error) { return nil, false, errNoHost }

// IsAbsent is true for the placeholder's error: no host is there.
func IsAbsent(err error) bool {
	return IsCode(err, ErrCodeUnavailable) || errors.Is(err, os.ErrNotExist)
}

// SocketPath is where the host's socket will live:
// %LOCALAPPDATA%\Mewla\run\host-<8 hex of the state dir>.sock.
func SocketPath(stateDir string) (string, error) {
	absolute, err := filepath.Abs(stateDir)
	if err != nil {
		return "", fmt.Errorf("session host socket: %w", err)
	}
	digest := sha256.Sum256([]byte(filepath.Clean(absolute)))
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "Mewla", "run", "host-"+hex.EncodeToString(digest[:4])+".sock"), nil
}

func EnsureSocketDir(socketPath string) error {
	return os.MkdirAll(filepath.Dir(socketPath), 0o700)
}

// ServerOptions and Server let `mewla host` build; Run refuses.
type ServerOptions struct {
	SocketPath  string
	HostVersion string
	Logf        func(format string, args ...any)
}

type Server struct{}

func NewServer(ServerOptions) *Server { return &Server{} }

func (s *Server) Run(context.Context) error { return errNoHost }

// Attachment mirrors the Unix viewer stream; Attach never returns one.
type Attachment struct {
	Attached
	Replay []byte
}

func (a *Attachment) Output() <-chan []byte {
	output := make(chan []byte)
	close(output)
	return output
}
func (a *Attachment) End() (AttachEnd, error) { return AttachEnd{}, errNoHost }
func (a *Attachment) Close() error            { return nil }

func (c *Client) Attach(context.Context, AttachParams) (*Attachment, error) { return nil, errNoHost }
