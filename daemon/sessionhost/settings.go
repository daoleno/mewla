package sessionhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// Settings are the session host's Labs choices (Settings → Labs → Session
// host), kept in <state>/session-host/settings.json. A missing file is off.
type Settings struct {
	// Enabled: new Sessions run in the session host instead of tmux. Sessions
	// already running stay where they are until they end.
	Enabled bool `json:"enabled,omitempty"`
}

// SettingsPath is where a state directory keeps the session host's Labs
// choices.
func SettingsPath(stateDir string) string {
	return filepath.Join(stateDir, "session-host", "settings.json")
}

// LoadSettings reads the session host's Labs choices; a missing or unreadable
// file is off.
func LoadSettings(stateDir string) Settings {
	var s Settings
	if raw, err := os.ReadFile(SettingsPath(stateDir)); err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// SaveSettings writes the session host's Labs choices.
func SaveSettings(stateDir string, s Settings) error {
	path := SettingsPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("session host settings: %w", err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, raw, 0o600)
}
