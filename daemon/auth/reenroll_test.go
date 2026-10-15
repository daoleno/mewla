package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"
)

// A browser that already trusts this daemon and opens a fresh pairing link
// (mewla open) pairs its same device key again: one device, first AddedAt.
func TestManagerReEnrollingTheSameDeviceKeepsOneDevice(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enroll := func() *TrustedDevice {
		t.Helper()
		pairing, err := manager.IssuePairingToken(time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		device, err := manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), "browser-1", "Mewla Web", hex.EncodeToString(publicKey))
		if err != nil {
			t.Fatal(err)
		}
		return device
	}
	first := enroll()
	time.Sleep(2 * time.Millisecond)
	second := enroll()
	if devices := manager.ListDevices(); len(devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(devices))
	}
	if !second.AddedAt.Equal(first.AddedAt) {
		t.Fatalf("AddedAt = %v, want the first %v", second.AddedAt, first.AddedAt)
	}
}
