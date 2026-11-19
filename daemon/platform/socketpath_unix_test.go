//go:build unix

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShortSocketDirIsPrivateAndRefusesAnOpenOne(t *testing.T) {
	if err := EnsureSocketDir(filepath.Join(ShortSocketDir(), "x.sock")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(ShortSocketDir())
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("short socket dir: %v %v", info, err)
	}
	open := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(open, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := ensureOwnedPrivateDir(open); err == nil {
		t.Fatal("a directory open to others was accepted")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if err := ensureOwnedPrivateDir(link); err == nil {
		t.Fatal("a symlink was accepted")
	}
}
