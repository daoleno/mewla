package screen

import (
	"context"
	"errors"
	"image"
	"time"
)

// Backend is one platform's screen: what it can do, the permissions it needs,
// and live sessions that capture frames and inject input. newBackend picks
// the backend for this build; platforms without one report Unsupported.
type Backend interface {
	// Capability reports support and permission state without prompting.
	Capability(ctx context.Context) Capability
	// RequestPermissions asks the operating system for any missing
	// permission. It may show system prompts on this computer.
	RequestPermissions(ctx context.Context)
	// OpenSettings shows this computer's settings page for one permission.
	OpenSettings(ctx context.Context, permission string) error
	// Confirm asks the person at this computer whether a device may view
	// and control it. It returns false on deny, timeout or no answer.
	Confirm(ctx context.Context, device string) (bool, error)
	// Start begins capturing the main display.
	Start(ctx context.Context) (Session, error)
}

// ErrCaptureStopped means the system ended a running capture, as it does when
// the display goes to sleep.
var ErrCaptureStopped = errors.New("screen capture stopped")

// displayHolder keeps the display on, which an agent's capture needs: a
// display that sleeps takes the capture with it.
type displayHolder interface{ HoldDisplay(on bool) }

// Capability is a backend's support and permission state.
type Capability struct {
	Supported   bool         `json:"supported"`
	Reason      string       `json:"reason,omitempty"`
	Permissions []Permission `json:"permissions,omitempty"`
}

// Ready reports whether every permission is granted.
func (c Capability) Ready() bool {
	if !c.Supported {
		return false
	}
	for _, p := range c.Permissions {
		if !p.Granted {
			return false
		}
	}
	return true
}

// Permission is one operating-system grant Screen needs.
type Permission struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Granted bool   `json:"granted"`
	// Guidance tells the person at the computer how to grant it.
	Guidance string `json:"guidance,omitempty"`
	// CanOpen means OpenSettings can show the right settings page.
	CanOpen bool `json:"can_open,omitempty"`
	// RestartRequired means the grant exists but takes effect only after
	// Mewla restarts.
	RestartRequired bool `json:"restart_required,omitempty"`
}

// Presence is what an agent needs to know about the person at the computer.
type Presence struct {
	// Locked means the screen is locked, or another user has the console.
	Locked bool
	// LastInput is when the keyboard or pointer last made input, an agent's
	// own included. Zero when unknown.
	LastInput time.Time
}

// presenceReporter is a backend that can tell. Without one, the screen
// counts as unlocked and nobody as at the computer.
type presenceReporter interface {
	Presence() Presence
}

// Session is one live capture of a display.
type Session interface {
	// Frame blocks until the screen differs from the frame numbered after
	// (0 for the first frame), then returns the newest frame. The image is
	// owned by the caller until the next call.
	Frame(ctx context.Context, after uint64) (Frame, error)
	// Pointer moves the pointer to framebuffer pixel (x, y) with the RFB
	// button mask (1 left, 2 middle, 4 right, 8/16 wheel up/down, 32/64
	// wheel left/right).
	Pointer(x, y int, buttons uint8) error
	// Key presses or releases an X11 keysym.
	Key(keysym uint32, down bool) error
	Close() error
}

// Frame is one captured screen in framebuffer pixels.
type Frame struct {
	Seq   uint64
	Image *image.RGBA
}

var (
	// ErrUnsupported means this computer has no Screen backend yet.
	ErrUnsupported = errors.New("Screen works only on Macs for now")
	// ErrPermissions means a required operating-system permission is missing.
	ErrPermissions = errors.New("Screen needs permissions on this computer")
)

// unsupportedBackend is the backend of every platform without one.
type unsupportedBackend struct{ reason string }

func (b unsupportedBackend) Capability(context.Context) Capability {
	return Capability{Reason: b.reason}
}
func (unsupportedBackend) RequestPermissions(context.Context)         {}
func (unsupportedBackend) OpenSettings(context.Context, string) error { return ErrUnsupported }
func (unsupportedBackend) Confirm(context.Context, string) (bool, error) {
	return false, ErrUnsupported
}
func (unsupportedBackend) Start(context.Context) (Session, error) { return nil, ErrUnsupported }
