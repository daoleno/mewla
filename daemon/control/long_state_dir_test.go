package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/platform"
)

// A state directory too deep for a unix socket still gets a control socket
// that the daemon binds and a CLI finds, and its lifecycle lock stays in the
// state directory, not in the socket directory every long state directory
// shares.
func TestControlSocketOfALongStateDirectory(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), strings.Repeat("a-very-long-state-directory-name/", 4))
	socketPath, err := DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(socketPath) != platform.ShortSocketDir() {
		t.Fatalf("socket %q is not in the short socket dir", socketPath)
	}
	other, _ := DefaultSocketPath(stateDir + "-2")
	if other == socketPath {
		t.Fatal("two state directories share a control socket")
	}
	server := &Server{Path: socketPath, Handler: &testHandler{response: Response{OK: true, Text: "hello"}}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	waitForSocketPath(t, socketPath)
	again, _ := DefaultSocketPath(stateDir)
	resp, err := Call(again, Request{Type: "worker_list"})
	if err != nil || !resp.OK || resp.Text != "hello" {
		t.Fatalf("Call = %#v, %v", resp, err)
	}
	lock, ok, err := TryAcquireLifecycleLock(stateDir)
	if err != nil || !ok {
		t.Fatalf("lifecycle lock: ok=%v err=%v", ok, err)
	}
	defer lock.Close()
	if _, err := os.Stat(filepath.Join(stateDir, "run", lifecycleLockName)); err != nil {
		t.Fatalf("lifecycle lock is not in the state directory: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket left behind: %v", err)
	}
}
