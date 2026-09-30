package main

import (
	"github.com/daoleno/zen/daemon/control"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrainRootOwnershipIndependentOfDaemonStateDirectory(t *testing.T) {
	// Both daemon identity locks can succeed, but the shared Brain lock must not.
	for range 2 {
		lock, ok, err := control.TryAcquireLifecycleLock(t.TempDir())
		if err != nil || !ok {
			t.Fatalf("daemon lock ok=%v err=%v", ok, err)
		}
		defer lock.Close()
	}
	root := t.TempDir()
	owner, ok, err := control.TryAcquireLifecycleLock(root)
	if err != nil || !ok {
		t.Fatalf("Brain owner ok=%v err=%v", ok, err)
	}
	defer owner.Close()
	second, ok, err := control.TryAcquireLifecycleLock(root)
	if second != nil {
		defer second.Close()
	}
	if err != nil || ok {
		t.Fatalf("second Brain owner ok=%v err=%v", ok, err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	successor, ok, err := control.TryAcquireLifecycleLock(root)
	if err != nil || !ok {
		t.Fatalf("restart owner ok=%v err=%v", ok, err)
	}
	defer successor.Close()
}

func TestDaemonRefusesSharedBrainBeforeOpeningStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	brainRoot := filepath.Join(home, ".zen", "brain")
	owner, ok, err := control.TryAcquireLifecycleLock(brainRoot)
	if err != nil || !ok {
		t.Fatalf("owner=%v err=%v", ok, err)
	}
	defer owner.Close()
	for range 2 {
		err := runDaemon([]string{"-state-dir", t.TempDir(), "-addr", "127.0.0.1:0"}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "already owned by another daemon") {
			t.Fatalf("err=%v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(brainRoot, "state")); !os.IsNotExist(err) {
		t.Fatalf("Brain store was opened: %v", err)
	}
}
