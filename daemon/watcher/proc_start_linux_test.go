//go:build linux

package watcher

import (
	"os/exec"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

// TestProcessStartTimeFromProcRealProcess derives the precise start of a real
// spawned process and proves it is stable, lies within the same second window
// below the ps lstart rounding, and refines through refineProcessStartedAt.
func TestProcessStartTimeFromProcRealProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	pid := cmd.Process.Pid

	precise, ok := workerproc.StartTime(pid)
	if !ok {
		t.Fatalf("workerproc.StartTime(%d) failed for a live process", pid)
	}
	now := time.Now().UTC()
	if precise.After(now) || now.Sub(precise) > time.Minute {
		t.Fatalf("derived start %v is implausible for a just-spawned process (now %v)", precise, now)
	}
	// Stability: repeated reads return the identical instant.
	again, ok := workerproc.StartTime(pid)
	if !ok || !again.Equal(precise) {
		t.Fatalf("derived start not stable: %v then %v (ok=%v)", precise, again, ok)
	}

	// The ps lstart value truncates to whole seconds, so the precise start
	// of the same process lies in [rounded, rounded+1s) and the guard must
	// accept it.
	rounded := precise.Truncate(time.Second)
	if refined := refineProcessStartedAt(rounded, pid); !refined.Equal(precise) {
		t.Fatalf("refineProcessStartedAt(%v, %d) = %v, want precise %v", rounded, pid, refined, precise)
	}

	// Unknown pids and zero observations keep the observed value unchanged.
	observed := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	if got := refineProcessStartedAt(observed, 2147483647); !got.Equal(observed) {
		t.Fatalf("unknown pid must keep the observed start, got %v", got)
	}
	if got := refineProcessStartedAt(time.Time{}, pid); !got.IsZero() {
		t.Fatalf("zero observation must stay zero, got %v", got)
	}
	// A contradictory observation (wrong pid's second window) is kept.
	if got := refineProcessStartedAt(observed, pid); !got.Equal(observed) {
		t.Fatalf("inconsistent observation must be kept, got %v", got)
	}
}
