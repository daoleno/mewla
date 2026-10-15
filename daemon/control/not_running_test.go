package control

import (
	"errors"
	"net"
	"path/filepath"
	"testing"
)

func TestCallSaysMewlaIsNotRunning(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.sock")
	if _, err := Call(missing, Request{Type: "computers"}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("no socket: %v", err)
	}
	// A socket left behind by a Mewla that stopped.
	stale := filepath.Join(dir, "stale.sock")
	listener, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = listener.Close()
	if _, err := Call(stale, Request{Type: "computers"}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("stale socket: %v", err)
	}
}
