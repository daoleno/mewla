//go:build linux

package userunit

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestPlaceAndStopOnTheUserManager starts real units in the person's user
// manager, so it runs only when asked: MEWLA_TEST_USER_UNITS=1.
func TestPlaceAndStopOnTheUserManager(t *testing.T) {
	if os.Getenv("MEWLA_TEST_USER_UNITS") != "1" {
		t.Skip("set MEWLA_TEST_USER_UNITS=1 to start units in the user manager")
	}
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	unit := "mewla-worker-gctest-" + hex.EncodeToString(raw[:]) + ".scope"
	slice := SessionSlice(unit, "")
	sleep := exec.Command("sleep", "60")
	if err := sleep.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sleep.Process.Kill()
		_, _ = StopSession(unit, 5*time.Second)
		// The owner's parent slice stays once made; this test's owner is its own.
		_ = exec.Command("systemctl", "--user", "stop", "mewla-worker-gctest.slice").Run()
	})
	scope, err := PlaceInScope(sleep.Process.Pid, slice, "Mewla test")
	if err != nil {
		t.Fatal(err)
	}
	cgroup, _ := os.ReadFile("/proc/" + strconv.Itoa(sleep.Process.Pid) + "/cgroup")
	if !strings.Contains(string(cgroup), "/"+slice+"/"+scope) {
		t.Fatalf("sleep is in %s, want %s/%s", cgroup, slice, scope)
	}
	stopped, err := StopSession(unit, 5*time.Second)
	if err != nil || len(stopped) != 1 || stopped[0] != slice {
		t.Fatalf("StopSession = %v, %v", stopped, err)
	}
	done := make(chan error, 1)
	go func() { done <- sleep.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sleep outlived its Session's slice")
	}
	if again, err := StopSession(unit, time.Second); err != nil || len(again) != 0 {
		t.Fatalf("second StopSession = %v, %v", again, err)
	}
}
