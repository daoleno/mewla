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
	return nil
}

// AgentBrowser is whether Workers launched now get a browser on the virtual
// screen: the switch is on and this computer can run one. It starts nothing;
// the browser comes up when an agent first asks for control.
func (m *Manager) AgentBrowser(ctx context.Context) bool {
	return m != nil && m.VirtualScreen() && m.backend.Capability(ctx).Available
}
