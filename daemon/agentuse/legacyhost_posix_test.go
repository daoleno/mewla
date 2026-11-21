//go:build !windows

package agentuse

import (
	"os"
	"path/filepath"
	"testing"
)

// The extension asked Chrome for com.mewla.browser until 0.4.1 and asks for ai.mewla.browser from 0.4.2. An
// extension Chrome has not reloaded still needs the old manifest, which stays until a new one has connected.
func TestTheOldNativeHostNameStaysUntilANewExtensionConnects(t *testing.T) {
	state := t.TempDir()
	profile := t.TempDir()
	s, err := New(filepath.Join(state, "state"), true)
	if err != nil {
		t.Fatal(err)
	}
	hosts := filepath.Join(profile, "NativeMessagingHosts")
	legacy := filepath.Join(hosts, LegacyNativeHostName+".json")
	if err := os.MkdirAll(hosts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"name":"com.mewla.browser"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.InstallNativeHost(profile, "/opt/mewla/bin/mewla", []string{MewlaExtensionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(hosts, NativeHostName+".json")); err != nil {
		t.Fatalf("the new name is not registered: %v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("installing the new name removed the old one: %v", err)
	}

	// An extension of 0.4.1 (or one too old to say) connected through the old name: it needs it, and is asked to reload.
	for _, version := range []string{"", "0.4.1", "0.3.9"} {
		s.retireLegacyNativeHost(version)
		if _, err := os.Stat(legacy); err != nil {
			t.Fatalf("the old name went when %q connected: %v", version, err)
		}
	}
	// One that asks for the new name connected: nothing needs the old one.
	s.retireLegacyNativeHost("0.4.2")
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("the old name remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hosts, NativeHostName+".json")); err != nil {
		t.Fatalf("the new name went with the old one: %v", err)
	}

	// A profile registered later is cleaned as it is registered.
	later := t.TempDir()
	laterHosts := filepath.Join(later, "NativeMessagingHosts")
	if err := os.MkdirAll(laterHosts, 0o755); err != nil {
		t.Fatal(err)
	}
	laterLegacy := filepath.Join(laterHosts, LegacyNativeHostName+".json")
	if err := os.WriteFile(laterLegacy, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.InstallNativeHost(later, "/opt/mewla/bin/mewla", []string{MewlaExtensionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(laterLegacy); !os.IsNotExist(err) {
		t.Fatalf("the old name remains in a profile registered after the new one connected: %v", err)
	}
}

// The bridge tells the service the version of each extension that says hello.
func TestTheBridgeTellsTheServiceWhichExtensionConnected(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state"), true)
	if err != nil {
		t.Fatal(err)
	}
	if s.Bridge().onHostVersion == nil {
		t.Fatal("hello does not reach the service")
	}
}
