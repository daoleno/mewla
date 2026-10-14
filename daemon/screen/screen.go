// Package screen lets paired devices view and control this computer's screen.
//
// A platform Backend captures the display and injects input; the daemon
// serves it as an RFB (VNC) session over the device-authenticated WebSocket,
// so the phone's viewer is the same on every platform. Each device needs the
// owner's one-time approval on this computer, and the operating system's own
// permissions where it has them.
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
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

const configFile = "screen.json"

var (
	// ErrDisabled is the owner's local kill switch.
	ErrDisabled = errors.New("Screen is turned off on this computer. Run `mewla screen enable` there to allow it again")
	// ErrNotApproved means this device has not been allowed on this computer.
	ErrNotApproved = errors.New("This device isn't allowed to use Screen on this computer yet")
	// ErrDenied means the person at the computer declined or didn't answer.
	ErrDenied = errors.New("Screen wasn't allowed on the computer. Someone there has to click Allow")
)

// Approval records that the owner allowed one device, at the computer.
type Approval struct {
	Name       string    `json:"name"`
	ApprovedAt time.Time `json:"approved_at"`
}

// Config is stored 0600 in the daemon state dir.
type Config struct {
	// Disabled is set by `mewla screen disable` and blocks every device.
	Disabled bool                `json:"disabled,omitempty"`
	Devices  map[string]Approval `json:"devices,omitempty"`
	// AllowSleep turns off the default keep-awake.
	AllowSleep bool `json:"allow_sleep,omitempty"`
}

// Approved reports whether a device may open the screen.
func (c Config) Approved(deviceID string) bool {
	_, ok := c.Devices[deviceID]
	return !c.Disabled && ok
}

// KeepAwake reports whether this computer should stay awake for viewers:
// once any device is allowed, unless the owner turned it off.
func (c Config) KeepAwake() bool { return !c.Disabled && len(c.Devices) > 0 && !c.AllowSleep }

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
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(stateDir, configFile), append(raw, '\n'), 0o600)
}

// Status is what a paired device sees before it opens a viewer.
type Status struct {
	Supported bool   `json:"supported"`
	Platform  string `json:"platform"`
	Reason    string `json:"reason,omitempty"`
	// Name is this computer's name.
	Name     string `json:"name,omitempty"`
	Disabled bool   `json:"disabled"`
	// Approved is about the device that asked.
	Approved    bool         `json:"approved"`
	Permissions []Permission `json:"permissions,omitempty"`
	// Ready means the device can open a viewer now.
	Ready bool `json:"ready"`
	// KeepAwake is the owner's setting; Power.KeepingAwake is what holds now.
	KeepAwake bool   `json:"keep_awake"`
	Power     *Power `json:"power,omitempty"`
}

// Service reads its config on every use, so `mewla screen enable|disable`
// takes effect without restarting the daemon.
type Service struct {
	stateDir string
	backend  Backend
	awake    keeper
	// confirm serializes approval prompts: one question on screen at a time.
	confirm sync.Mutex
	// write serializes config changes; reads go straight to disk.
	write sync.Mutex
}

func NewService(stateDir string) *Service { return NewServiceWith(stateDir, newBackend()) }

// NewServiceWith uses an explicit backend, for tests.
func NewServiceWith(stateDir string, backend Backend) *Service {
	return &Service{stateDir: stateDir, backend: backend}
}

// Run holds this computer awake while Screen is allowed and keep-awake is
// on, re-reading the config so the CLI takes effect within seconds. The hold
// ends with ctx and, on macOS, with the daemon process.
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

func (s *Service) update(change func(*Config) error) error {
	s.write.Lock()
	defer s.write.Unlock()
	cfg, err := LoadConfig(s.stateDir)
	if err != nil {
		return err
	}
	if err := change(&cfg); err != nil {
		return err
	}
	if err := SaveConfig(s.stateDir, cfg); err != nil {
		return err
	}
	s.syncAwake()
	return nil
}

// Status reports support, permissions and this device's approval.
func (s *Service) Status(ctx context.Context, deviceID string) Status {
	capability := s.backend.Capability(ctx)
	status := Status{
		Supported:   capability.Supported,
		Platform:    runtime.GOOS,
		Reason:      capability.Reason,
		Name:        computerName(),
		Permissions: capability.Permissions,
	}
	cfg, err := LoadConfig(s.stateDir)
	if err != nil {
		status.Reason = err.Error()
		return status
	}
	status.Disabled = cfg.Disabled
	status.Approved = cfg.Approved(deviceID)
	status.KeepAwake = cfg.KeepAwake()
	if capability.Supported {
		status.Power = readPower()
		if status.Power != nil {
			status.Power.KeepingAwake = s.awake.holding()
		}
	}
	status.Ready = capability.Ready() && status.Approved
	return status
}

// Enable asks the person at this computer to allow a device, once, and then
// asks the operating system for any missing permission while they are there.
func (s *Service) Enable(ctx context.Context, deviceID, deviceName string) error {
	if !s.backend.Capability(ctx).Supported {
		return ErrUnsupported
	}
	cfg, err := LoadConfig(s.stateDir)
	if err != nil {
		return err
	}
	if cfg.Disabled {
		return ErrDisabled
	}
	if !cfg.Approved(deviceID) {
		s.confirm.Lock()
		allowed, err := s.backend.Confirm(ctx, deviceName)
		s.confirm.Unlock()
		if err != nil {
			return err
		}
		if !allowed {
			return ErrDenied
		}
		err = s.update(func(cfg *Config) error {
			if cfg.Disabled {
				return ErrDisabled
			}
			if cfg.Devices == nil {
				cfg.Devices = map[string]Approval{}
			}
			cfg.Devices[deviceID] = Approval{Name: deviceName, ApprovedAt: time.Now().UTC()}
			return nil
		})
		if err != nil {
			return err
		}
	}
	s.backend.RequestPermissions(ctx)
	return nil
}

// OpenSettings shows the settings page for a permission on this computer.
func (s *Service) OpenSettings(ctx context.Context, deviceID, permission string) error {
	if err := s.allowed(deviceID); err != nil {
		return err
	}
	return s.backend.OpenSettings(ctx, permission)
}

// SetKeepAwake changes keep-awake for an allowed device.
func (s *Service) SetKeepAwake(deviceID string, keep bool) error {
	if err := s.allowed(deviceID); err != nil {
		return err
	}
	return s.update(func(cfg *Config) error {
		cfg.AllowSleep = !keep
		return nil
	})
}

// Allowed reports whether a device may keep its viewer open.
func (s *Service) Allowed(deviceID string) bool { return s.allowed(deviceID) == nil }

func (s *Service) allowed(deviceID string) error {
	cfg, err := LoadConfig(s.stateDir)
	if err != nil {
		return err
	}
	if cfg.Disabled {
		return ErrDisabled
	}
	if !cfg.Approved(deviceID) {
		return ErrNotApproved
	}
	return nil
}

// Open starts a capture for an allowed device and returns the viewer's end
// of an in-process RFB session, positioned right before ClientInit: the
// caller has already done the version and security handshake.
func (s *Service) Open(ctx context.Context, deviceID string) (net.Conn, error) {
	if err := s.allowed(deviceID); err != nil {
		return nil, err
	}
	capability := s.backend.Capability(ctx)
	if !capability.Supported {
		return nil, ErrUnsupported
	}
	if !capability.Ready() {
		return nil, ErrPermissions
	}
	session, err := s.backend.Start(ctx)
	if err != nil {
		return nil, err
	}
	viewer, server := net.Pipe()
	go func() {
		defer session.Close()
		defer server.Close()
		_ = serveRFB(ctx, server, session, computerName())
	}()
	return viewer, nil
}

// Disable is the local kill switch: it blocks every device and forgets their
// approvals, so each must be allowed again at this computer.
func (s *Service) Disable() error {
	return s.update(func(cfg *Config) error {
		cfg.Disabled = true
		cfg.Devices = nil
		return nil
	})
}

// EnableLocally lifts the kill switch. Devices still need approval.
func (s *Service) EnableLocally(allowSleep bool) error {
	return s.update(func(cfg *Config) error {
		cfg.Disabled = false
		cfg.AllowSleep = allowSleep
		return nil
	})
}

// Capability exposes the backend's state for the local CLI.
func (s *Service) Capability(ctx context.Context) Capability { return s.backend.Capability(ctx) }
