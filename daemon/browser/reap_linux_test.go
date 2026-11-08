//go:build linux

package browser

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// A process marked with a dead daemon's pid is ended; one marked with a live
// daemon's pid is left alone.
func TestReapStraysEndsOnlyWhatADeadOwnerLeft(t *testing.T) {
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	gone := dead.Process.Pid // exited and reaped: no process has this pid now
	start := func(owner int) *exec.Cmd {
		c := exec.Command("sleep", "30")
		c.Env = append(os.Environ(), ownerEnv+"="+strconv.Itoa(owner))
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
		return c
	}
	stray, kept := start(gone), start(os.Getppid())
	done := make(chan error, 1)
	go func() { done <- stray.Wait() }()
	reapStrays()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a process left by a dead daemon survived")
	}
	if err := kept.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("a live daemon's process was ended: %v", err)
	}
}
