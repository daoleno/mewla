//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscardedStderrIsTheNullDeviceOnly(t *testing.T) {
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if !discarded(null) {
		t.Fatal("/dev/null is not discarded")
	}
	file, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if discarded(file) {
		t.Fatal("a file is discarded")
	}
}

// What only reaches stderr (a goroutine dump, the restart's last lines) lands
// in the capture file, which a large one is moved aside for.
func TestStderrCaptureTakesTheDescriptorAndRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", stderrCaptureName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", stderrCaptureMax+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	capture, err := openStderrCapture(path)
	if err != nil {
		t.Fatal(err)
	}
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	fd := int(null.Fd())
	if err := redirectFD(capture, fd); err != nil {
		t.Fatal(err)
	}
	capture.Close()
	if _, err := null.WriteString("goroutine 1 [running]:\n"); err != nil {
		t.Fatal(err)
	}
	null.Close()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "goroutine 1 [running]:\n" {
		t.Fatalf("capture %q (%v)", got, err)
	}
	if info, err := os.Stat(path + ".1"); err != nil || info.Size() != stderrCaptureMax+1 {
		t.Fatalf("rotated: %v %v", info, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
}
