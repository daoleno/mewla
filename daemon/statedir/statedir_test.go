//go:build linux || darwin

package statedir

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func seedLegacy(t *testing.T, home string) string {
	t.Helper()
	legacy := filepath.Join(home, LegacyDirName)
	for _, dir := range []string{"run", "brain/workspace", "worktrees/repo", "run/plugins-load-x"} {
		if err := os.MkdirAll(filepath.Join(legacy, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(legacy, "identity.json"), `{"id":"daemon"}`)
	writeFile(t, filepath.Join(legacy, "run", "daemon.lock"), "")
	writeFile(t, filepath.Join(legacy, "brain", "workspace", "note.md"), "keep me")
	return legacy
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertMigrated(t *testing.T, home string) {
	t.Helper()
	root := filepath.Join(home, DirName)
	legacy := filepath.Join(home, LegacyDirName)
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected %s to be a real directory: %v", root, err)
	}
	target, err := os.Readlink(legacy)
	if err != nil || target != DirName {
		t.Fatalf("expected %s -> %s, got %q (%v)", legacy, DirName, target, err)
	}
	if got := readFile(t, filepath.Join(root, "brain", "workspace", "note.md")); got != "keep me" {
		t.Fatalf("note moved incorrectly: %q", got)
	}
	// Old absolute paths keep resolving through the compatibility link.
	if got := readFile(t, filepath.Join(legacy, "identity.json")); got != `{"id":"daemon"}` {
		t.Fatalf("legacy path does not resolve: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "run", "plugins-load-x")); err != nil {
		t.Fatalf("private run dir lost: %v", err)
	}
	if !hasMarker(root) {
		t.Fatal("migration marker missing")
	}
	if _, err := os.Lstat(filepath.Join(home, stagingLinkName)); !os.IsNotExist(err) {
		t.Fatalf("staging link left behind: %v", err)
	}
	if got := Default(home); got != root {
		t.Fatalf("Default = %s, want %s", got, root)
	}
}

func TestMigrateFreshHome(t *testing.T) {
	home := t.TempDir()
	result, err := Migrate(home)
	if err != nil || result.Status != StatusFresh {
		t.Fatalf("Migrate = %+v, %v", result, err)
	}
	if got, want := Default(home), filepath.Join(home, DirName); got != want {
		t.Fatalf("Default = %s, want %s", got, want)
	}
	for _, name := range []string{DirName, LegacyDirName} {
		if _, err := os.Lstat(filepath.Join(home, name)); !os.IsNotExist(err) {
			t.Fatalf("fresh migrate created %s: %v", name, err)
		}
	}
}

func TestMigrateMovesLegacyRootAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	seedLegacy(t, home)
	if got, want := Default(home), filepath.Join(home, LegacyDirName); got != want {
		t.Fatalf("Default before migration = %s, want %s", got, want)
	}
	result, err := Migrate(home)
	if err != nil || result.Status != StatusMigrated {
		t.Fatalf("Migrate = %+v, %v", result, err)
	}
	assertMigrated(t, home)

	again, err := Migrate(home)
	if err != nil || again.Status != StatusCurrent {
		t.Fatalf("second Migrate = %+v, %v", again, err)
	}
	assertMigrated(t, home)
}

func TestMigrateFallbackWithoutAtomicExchange(t *testing.T) {
	previous := exchangeNames
	exchangeNames = func(string, string) error { return errExchangeUnsupported }
	t.Cleanup(func() { exchangeNames = previous })

	home := t.TempDir()
	seedLegacy(t, home)
	result, err := Migrate(home)
	if err != nil || result.Status != StatusMigrated {
		t.Fatalf("Migrate = %+v, %v", result, err)
	}
	assertMigrated(t, home)
}

func TestMigrateRestoresLinkAfterInterruptedMove(t *testing.T) {
	home := t.TempDir()
	seedLegacy(t, home)
	if _, err := Migrate(home); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash between the rename and the compatibility link.
	if err := os.Remove(filepath.Join(home, LegacyDirName)); err != nil {
		t.Fatal(err)
	}
	result, err := Migrate(home)
	if err != nil || result.Status != StatusCurrent {
		t.Fatalf("Migrate = %+v, %v", result, err)
	}
	assertMigrated(t, home)
}

func TestMigrateKeepsBothRootsWhenTheyExistIndependently(t *testing.T) {
	home := t.TempDir()
	legacy := seedLegacy(t, home)
	root := filepath.Join(home, DirName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "unrelated"), "x")

	result, err := Migrate(home)
	if err != nil || result.Status != StatusConflict || result.Root != legacy || result.Message == "" {
		t.Fatalf("Migrate = %+v, %v", result, err)
	}
	// An unmarked ~/.mewla must never hide real Zen state.
	if got := Default(home); got != legacy {
		t.Fatalf("Default = %s, want legacy %s", got, legacy)
	}
	if got := readFile(t, filepath.Join(legacy, "identity.json")); got != `{"id":"daemon"}` {
		t.Fatalf("legacy state touched: %q", got)
	}
	if got := readFile(t, filepath.Join(root, "unrelated")); got != "x" {
		t.Fatalf("new root touched: %q", got)
	}
}

func TestMigrateReportsLegacyDirRecreatedAfterMigration(t *testing.T) {
	home := t.TempDir()
	seedLegacy(t, home)
	if _, err := Migrate(home); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(home, LegacyDirName)
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(legacy, "t"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := Migrate(home)
	if err != nil || result.Status != StatusConflict || result.Root != filepath.Join(home, DirName) {
		t.Fatalf("Migrate = %+v, %v", result, err)
	}
	if got := Default(home); got != filepath.Join(home, DirName) {
		t.Fatalf("Default = %s", got)
	}
}

func TestMigrateTreatsExistingCompatibilityLinkAsCurrent(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, DirName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(DirName, filepath.Join(home, LegacyDirName)); err != nil {
		t.Fatal(err)
	}
	result, err := Migrate(home)
	if err != nil || result.Status != StatusCurrent {
		t.Fatalf("Migrate = %+v, %v", result, err)
	}
	if got := Default(home); got != root {
		t.Fatalf("Default = %s", got)
	}
}

func TestMigrateMovesUserManagedLegacySymlink(t *testing.T) {
	home := t.TempDir()
	storage := filepath.Join(t.TempDir(), "zen-storage")
	if err := os.MkdirAll(filepath.Join(storage, "brain"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(storage, "identity.json"), "id")
	if err := os.Symlink(storage, filepath.Join(home, LegacyDirName)); err != nil {
		t.Fatal(err)
	}
	result, err := Migrate(home)
	if err != nil || result.Status != StatusMigrated {
		t.Fatalf("Migrate = %+v, %v", result, err)
	}
	target, err := os.Readlink(filepath.Join(home, DirName))
	if err != nil || target != storage {
		t.Fatalf("~/.mewla -> %q (%v), want %s", target, err, storage)
	}
	if got := readFile(t, filepath.Join(home, LegacyDirName, "identity.json")); got != "id" {
		t.Fatalf("legacy link broken: %q", got)
	}
}

func TestMigrateDefersWhileDaemonHoldsLegacyLock(t *testing.T) {
	for _, lockRel := range DaemonLockPaths {
		t.Run(lockRel, func(t *testing.T) {
			home := t.TempDir()
			legacy := seedLegacy(t, home)
			lockPath := filepath.Join(legacy, lockRel)
			if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
				t.Fatal(err)
			}
			// A separate open file description, like another daemon process.
			daemonLock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer daemonLock.Close()
			if err := unix.Flock(int(daemonLock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				t.Fatal(err)
			}

			result, err := Migrate(home)
			if err != nil || result.Status != StatusDeferred || result.Root != legacy || result.Message == "" {
				t.Fatalf("Migrate = %+v, %v", result, err)
			}
			if _, err := os.Lstat(filepath.Join(home, DirName)); !os.IsNotExist(err) {
				t.Fatalf("deferred migration created new root: %v", err)
			}
			if hasMarker(legacy) {
				t.Fatal("deferred migration left a marker")
			}
			if got := Default(home); got != legacy {
				t.Fatalf("Default = %s, want %s", got, legacy)
			}

			_ = unix.Flock(int(daemonLock.Fd()), unix.LOCK_UN)
			result, err = Migrate(home)
			if err != nil || result.Status != StatusMigrated {
				t.Fatalf("Migrate after daemon stop = %+v, %v", result, err)
			}
			assertMigrated(t, home)
		})
	}
}
