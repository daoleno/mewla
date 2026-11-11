package link

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveLegacyConfigRenamesOnceAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, LegacyConfigFilename)
	current := filepath.Join(dir, DefaultConfigFilename)
	if err := os.WriteFile(legacy, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := MoveLegacyConfig(dir)
	if err != nil || result != LegacyConfigMoved {
		t.Fatalf("first move = %v, %v", result, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy file still there: %v", err)
	}
	info, err := os.Stat(current)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	if raw, _ := os.ReadFile(current); string(raw) != `{"version":1}` {
		t.Fatalf("content = %q", raw)
	}

	// A second start finds nothing to move.
	if result, err := MoveLegacyConfig(dir); err != nil || result != LegacyConfigAbsent {
		t.Fatalf("second move = %v, %v", result, err)
	}
}

func TestMoveLegacyConfigNeverTouchesLegacyWhenCurrentExists(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, LegacyConfigFilename)
	current := filepath.Join(dir, DefaultConfigFilename)
	if err := os.WriteFile(legacy, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := MoveLegacyConfig(dir)
	if err != nil || result != LegacyConfigIgnored {
		t.Fatalf("move = %v, %v", result, err)
	}
	if raw, _ := os.ReadFile(legacy); string(raw) != "old" {
		t.Fatalf("legacy changed: %q", raw)
	}
	if raw, _ := os.ReadFile(current); string(raw) != "new" {
		t.Fatalf("current changed: %q", raw)
	}
}

func TestMoveLegacyConfigWithoutAnyFileIsInert(t *testing.T) {
	dir := t.TempDir()
	if result, err := MoveLegacyConfig(dir); err != nil || result != LegacyConfigAbsent {
		t.Fatalf("move = %v, %v", result, err)
	}
	if result, err := MoveLegacyConfig(""); err != nil || result != LegacyConfigAbsent {
		t.Fatalf("empty dir move = %v, %v", result, err)
	}
}
