//go:build windows

package platform

import (
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// A variable the person sets for their user after the daemon started (setx,
// System Properties) reaches the daemon's environment, as it reaches a new
// terminal, without logging in again.
func TestRefreshEnvironmentSeesAUserVariableSetLater(t *testing.T) {
	const name = "MEWLA_TEST_REFRESH_ENV"
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	if err := key.SetStringValue(name, "from-the-registry"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = key.DeleteValue(name); os.Unsetenv(name) })
	os.Unsetenv(name)
	refreshMu.Lock()
	lastRefresh = time.Time{}
	refreshMu.Unlock()

	RefreshEnvironment()

	if got := os.Getenv(name); got != "from-the-registry" {
		t.Fatalf("%s = %q after RefreshEnvironment, want the value set in HKCU\\Environment", name, got)
	}
	if os.Getenv("PATH") == "" {
		t.Fatal("PATH is empty after RefreshEnvironment")
	}
}
