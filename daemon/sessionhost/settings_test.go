package sessionhost

import (
	"os"
	"testing"
)

func TestSettingsDefaultOff(t *testing.T) {
	dir := t.TempDir()
	if LoadSettings(dir).Enabled {
		t.Fatal("a state directory without settings has the session host on")
	}
	if err := SaveSettings(dir, Settings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if !LoadSettings(dir).Enabled {
		t.Fatal("saved setting not read back")
	}
	info, err := os.Stat(SettingsPath(dir))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file mode %v, %v", info.Mode().Perm(), err)
	}
	if err := os.WriteFile(SettingsPath(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if LoadSettings(dir).Enabled {
		t.Fatal("a broken settings file reads as on")
	}
}
