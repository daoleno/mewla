// Package screen lets paired devices view and control this computer's screen
// through the host's own RFB (VNC) server, such as macOS Screen Sharing.
//
// The daemon is the only RFB client of that server. It dials a loopback
// address, completes the server's sign-in with a password kept on this
// computer, and then offers the phone an RFB session that needs no further
// sign-in: device signatures on the WebSocket already authenticated it.
package screen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

const (
	configFile = "screen.json"
	// DefaultAddress is where macOS Screen Sharing listens.
	DefaultAddress = "127.0.0.1:5900"
	// MaxPasswordLength is the VNC password limit; longer passwords are truncated by every server.
	MaxPasswordLength = 8
)

var ErrDisabled = errors.New("Screen control is off on this computer. Run `mewla screen enable` on it to turn it on")

// Config is the owner's explicit opt-in, stored 0600 in the daemon state dir.
type Config struct {
	Enabled  bool   `json:"enabled"`
	Address  string `json:"address,omitempty"`
	Password string `json:"password,omitempty"`
	// AllowSleep turns off the default keep-awake while screen control is on.
	AllowSleep bool `json:"allow_sleep,omitempty"`
}

// KeepAwake reports whether this computer should stay awake for viewers.
func (c Config) KeepAwake() bool { return c.Enabled && !c.AllowSleep }

func (c Config) address() string {
	if strings.TrimSpace(c.Address) == "" {
		return DefaultAddress
	}
	return c.Address
}

// Status is what a paired device sees before it opens a viewer.
type Status struct {
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
	Platform  string `json:"platform"`
	Reason    string `json:"reason,omitempty"`
	Name      string `json:"name,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	// KeepAwake is the owner's setting; Power.KeepingAwake is what holds now.
	KeepAwake bool   `json:"keep_awake"`
	Power     *Power `json:"power,omitempty"`
}

// ValidateAddress admits only loopback hosts, so the daemon can never be
// turned into a relay to another machine's RFB port.
func ValidateAddress(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || port == "" {
		return fmt.Errorf("screen address %q must be host:port", address)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("screen address %q must be on this computer (127.0.0.1 or ::1)", address)
	}
	return nil
}

func LoadConfig(stateDir string) (Config, error) {
	raw, err := os.ReadFile(filepath.Join(stateDir, configFile))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("read %s: %w", configFile, err)
	}
	return cfg, nil
}

func SaveConfig(stateDir string, cfg Config) error {
	if err := ValidateAddress(cfg.address()); err != nil {
		return err
	}
	if len(cfg.Password) > MaxPasswordLength {
		return fmt.Errorf("VNC passwords are at most %d characters", MaxPasswordLength)
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(stateDir, configFile), append(raw, '\n'), 0o600)
}

// Service reads the opt-in on every use, so `mewla screen enable|disable`
// takes effect without restarting the daemon.
type Service struct {
	stateDir string
	dialer   net.Dialer
	awake    keeper
}

func NewService(stateDir string) *Service {
	return &Service{stateDir: stateDir, dialer: net.Dialer{Timeout: 5 * time.Second}}
}

// Run holds this computer awake while screen control is on and keep-awake is
// not turned off, re-reading the opt-in so the CLI takes effect within
// seconds. The hold ends with ctx and, on macOS, with the daemon process.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer s.awake.set(false)
	for {
		s.syncAwake()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) syncAwake() {
	cfg, err := LoadConfig(s.stateDir)
	s.awake.set(err == nil && cfg.KeepAwake())
}

// SetKeepAwake changes the keep-awake setting from a paired device. It never
// turns screen control itself on.
func (s *Service) SetKeepAwake(keep bool) error {
	cfg, err := s.config()
	if err != nil {
		return err
	}
	cfg.AllowSleep = !keep
	if err := SaveConfig(s.stateDir, cfg); err != nil {
		return err
	}
	s.syncAwake()
	return nil
}

func (s *Service) config() (Config, error) {
	cfg, err := LoadConfig(s.stateDir)
	if err != nil {
		return Config{}, err
	}
	if !cfg.Enabled {
		return Config{}, ErrDisabled
	}
	if err := ValidateAddress(cfg.address()); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Dial connects to the RFB server and completes its sign-in. The returned
// session is positioned right before ClientInit.
func (s *Service) Dial(ctx context.Context) (*Upstream, error) {
	cfg, err := s.config()
	if err != nil {
		return nil, err
	}
	conn, err := s.dialer.DialContext(ctx, "tcp", cfg.address())
	if err != nil {
		return nil, fmt.Errorf("%s is not reachable at %s. %s", serverName(), cfg.address(), enableHint())
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Second)
	}
	_ = conn.SetDeadline(deadline)
	if err := clientHandshake(conn, cfg.Password); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return &Upstream{Conn: conn}, nil
}

// Status signs in and reads ServerInit without keeping the session.
func (s *Service) Status(ctx context.Context) Status {
	status := Status{Platform: runtime.GOOS}
	cfg, err := LoadConfig(s.stateDir)
	if err != nil {
		status.Reason = err.Error()
		return status
	}
	status.Enabled = cfg.Enabled
	status.KeepAwake = cfg.KeepAwake()
	status.Power = readPower()
	if status.Power != nil {
		status.Power.KeepingAwake = s.awake.holding()
	}
	if !cfg.Enabled {
		status.Reason = ErrDisabled.Error()
		return status
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	upstream, err := s.Dial(ctx)
	if err != nil {
		status.Reason = err.Error()
		return status
	}
	defer upstream.Conn.Close()
	_ = upstream.Conn.SetDeadline(time.Now().Add(5 * time.Second))
	// Shared flag: never disconnect another viewer just to probe.
	if _, err := upstream.Conn.Write([]byte{1}); err != nil {
		status.Reason = err.Error()
		return status
	}
	init, err := readServerInit(upstream.Conn)
	if err != nil {
		status.Reason = err.Error()
		return status
	}
	status.Available = true
	status.Name, status.Width, status.Height = init.name, init.width, init.height
	return status
}

// Upstream is a signed-in RFB connection awaiting ClientInit.
type Upstream struct {
	Conn net.Conn
}

func serverName() string {
	if runtime.GOOS == "darwin" {
		return "Screen Sharing"
	}
	return "The screen's VNC server"
}

func enableHint() string {
	if runtime.GOOS == "darwin" {
		return "Turn on System Settings → General → Sharing → Screen Sharing on this Mac."
	}
	return "Start a VNC server for this screen on that loopback address."
}

// Enabled reports the current opt-in, so open viewers end soon after
// `mewla screen disable`.
func (s *Service) Enabled() bool {
	_, err := s.config()
	return err == nil
}
