//go:build unix

package main

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/control"
)

// The CLI's probe holds the lock for a moment; a daemon under the app's host
// gets it once the probe lets go, and a lock that stays held is still
// another daemon's.
func TestLifecycleLockRetryOutlastsAProbe(t *testing.T) {
	stateDir := t.TempDir()
	probe, acquired, err := control.TryAcquireLifecycleLock(stateDir)
	if err != nil || !acquired {
		t.Fatalf("probe: %v %v", acquired, err)
	}
	sleeps := 0
	lock, acquired, err := tryLifecycleLock(stateDir, time.Second, func(time.Duration) {
		sleeps++
		if sleeps == 2 {
			_ = probe.Close()
		}
	})
	if err != nil || !acquired {
		t.Fatalf("after the probe: %v %v", acquired, err)
	}
	_ = lock.Close()

	held, _, _ := control.TryAcquireLifecycleLock(stateDir)
	defer held.Close()
	sleeps = 0
	if _, acquired, err := tryLifecycleLock(stateDir, 300*time.Millisecond, func(time.Duration) { sleeps++ }); err != nil || acquired || sleeps != 3 {
		t.Fatalf("held: acquired %v err %v after %d waits", acquired, err, sleeps)
	}
	if _, acquired, _ := tryLifecycleLock(stateDir, 0, func(time.Duration) { t.Fatal("waited") }); acquired {
		t.Fatal("acquired a held lock")
	}
}
