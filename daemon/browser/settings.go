package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// Settings are the computer-wide Browser choices, kept beside the profiles.
type Settings struct {
	// VirtualScreen: new agent Workers get a browser on the virtual screen
	// without being told which one (Settings → Labs → Virtual screen).
	VirtualScreen bool `json:"virtual_screen,omitempty"`
}

// LoadSettings reads the computer-wide Browser choices kept under root (the
// browsers directory of the state directory), for commands that run outside
// the daemon.
func LoadSettings(root string) Settings { return loadSettings(root) }

func loadSettings(root string) Settings {
	var s Settings
	if raw, err := os.ReadFile(filepath.Join(root, "settings.json")); err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// VirtualScreen reports whether new Workers get a virtual-screen browser.
func (m *Manager) VirtualScreen() bool {
	if m == nil {
		return false
	}
	return m.virtual.Load()
}

func (m *Manager) setVirtualScreen(on bool) error {
	m.settingsMu.Lock()
	defer m.settingsMu.Unlock()
	if m.closed.Load() {
		return ErrUnavailable
	}
	raw, err := json.Marshal(Settings{VirtualScreen: on})
	if err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(m.root, "settings.json"), raw, 0o600); err != nil {
		return err
	}
	m.virtual.Store(on)
	m.mu.Lock()
	hook := m.switchHook
	m.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

// SetSwitchHook is called after the virtual screen is switched.
func (m *Manager) SetSwitchHook(f func()) {
	m.mu.Lock()
	m.switchHook = f
	m.mu.Unlock()
}

// Capability is whether this computer can run the agent browser, and why not.
func (m *Manager) Capability(ctx context.Context) Capability { return m.backend.Capability(ctx) }
