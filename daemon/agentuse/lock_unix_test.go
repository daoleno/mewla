//go:build unix

package agentuse

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// holdLock runs this test binary as a second process that locks a byte range
// of path, the way a running Chrome holds its localStorage open.
func holdLock(t *testing.T, path string, start, length int64) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldLock$")
	cmd.Env = append(os.Environ(), "MEWLA_HOLD_LOCK="+path, "MEWLA_HOLD_START="+strconv.FormatInt(start, 10), "MEWLA_HOLD_LEN="+strconv.FormatInt(length, 10))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("helper: %q %v", line, err)
	}
}

func TestHelperHoldLock(t *testing.T) {
	path := os.Getenv("MEWLA_HOLD_LOCK")
	if path == "" {
		t.Skip("helper process only")
	}
	start, _ := strconv.ParseInt(os.Getenv("MEWLA_HOLD_START"), 10, 64)
	length, _ := strconv.ParseInt(os.Getenv("MEWLA_HOLD_LEN"), 10, 64)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	lock := syscall.Flock_t{Type: syscall.F_WRLCK, Start: start, Len: length}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("locked\n")
	time.Sleep(time.Minute)
}

func TestTheStoreChromeHasOpenWinsOverTheNewerOne(t *testing.T) {
	profile := t.TempDir()
	legacy := filepath.Join(profile, "Local Storage", "leveldb")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	copyDir(t, filepath.Join("testdata", "leveldb-newer"), legacy)
	if err := os.WriteFile(filepath.Join(legacy, "LOCK"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writeSQLiteStorage(t, profile, append([]byte{1}, validKey...))
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(profile, "LocalStorage"), old, old); err != nil {
		t.Fatal(err)
	}
	// leveldb was written last, but Chrome has SQLite open.
	holdLock(t, filepath.Join(profile, "LocalStorage"), sqliteLockOffset, sqliteLockLength)
	if token, err := readExtensionToken(profile); err != nil || token != validKey {
		t.Fatalf("open SQLite store: token %q err %v", token, err)
	}
}

func TestAnOpenStoreWithoutAKeyMeansNoKey(t *testing.T) {
	profile := t.TempDir()
	legacy := filepath.Join(profile, "Local Storage", "leveldb")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "LOCK"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writeSQLiteStorage(t, profile, append([]byte{1}, validKey...))
	// Chrome uses leveldb, where the extension has made no key: SQLite's is stale.
	holdLock(t, filepath.Join(legacy, "LOCK"), 0, 0)
	if _, err := readExtensionToken(profile); err != errNoToken {
		t.Fatalf("err %v, want errNoToken", err)
	}
}
