package workerproc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLeaseRoundTripAndListing(t *testing.T) {
	dir := t.TempDir()
	resourceID := "zen-worker-abc123-0123456789abcdef0123456789abcdef.scope"
	path, err := LeasePath(dir, resourceID)
	if err != nil {
		t.Fatal(err)
	}
	lease := Lease{
		Version:    1,
		ResourceID: resourceID,
		BootID:     "boot",
		StartedAt:  time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := writeLease(path, lease); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResourceID != lease.ResourceID || got.BootID != lease.BootID {
		t.Fatalf("lease = %+v, want %+v", got, lease)
	}
	leases, err := ListLeases(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].ResourceID != resourceID {
		t.Fatalf("leases = %+v", leases)
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Fatalf("lease mode = %o, want 600", mode)
	}
}

func TestLeasePathRejectsTraversal(t *testing.T) {
	for _, resourceID := range []string{"", "../escape", "unit/name", ".."} {
		if _, err := LeasePath(t.TempDir(), resourceID); err == nil {
			t.Fatalf("LeasePath accepted %q", resourceID)
		}
	}
}

func TestStopLeaseRemovesOldBootMetadataWithoutSignaling(t *testing.T) {
	dir := t.TempDir()
	resourceID := "zen-worker-abc123-fedcba9876543210fedcba9876543210.scope"
	path, err := LeasePath(dir, resourceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLease(path, Lease{
		Version:    1,
		ResourceID: resourceID,
		BootID:     "definitely-not-the-current-boot",
	}); err != nil {
		t.Fatal(err)
	}
	if err := StopLease(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("old-boot lease still exists: %v", err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
