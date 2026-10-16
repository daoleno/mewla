//go:build !darwin

package computers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFriendlyNameFallsBackToShortHostname(t *testing.T) {
	previous := machineInfoPath
	t.Cleanup(func() { machineInfoPath = previous })

	hostname, _ := os.Hostname()
	machineInfoPath = filepath.Join(t.TempDir(), "missing")
	if got, want := FriendlyName(), ShortHostname(hostname); got != want {
		t.Fatalf("without machine-info FriendlyName() = %q, want %q", got, want)
	}

	path := filepath.Join(t.TempDir(), "machine-info")
	if err := os.WriteFile(path, []byte("PRETTY_HOSTNAME=\"Home Server\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	machineInfoPath = path
	if got := FriendlyName(); got != "Home Server" {
		t.Fatalf("FriendlyName() = %q, want %q", got, "Home Server")
	}
}
