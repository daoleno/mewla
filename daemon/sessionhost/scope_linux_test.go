//go:build linux

package sessionhost

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A placed program is held as this binary until place has run, then becomes
// the program with the same pid: nothing it starts can run before it is
// placed.
func TestAPlacedProgramWaitsForItsPlace(t *testing.T) {
	var heldAs string
	var placedPID int
	place := func(pid int) {
		placedPID = pid
		// The parent resumes during the child's exec (vfork), before its new
		// argv is readable.
		for deadline := time.Now().Add(time.Second); heldAs == "" && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			raw, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
			heldAs = string(raw)
		}
		time.Sleep(50 * time.Millisecond) // the program must not have run meanwhile
	}
	proc, err := startProgram("/bin/sh", []string{"sh", "-c", "echo pid=$$ fd3=$(test -e /proc/self/fd/3 && echo open || echo closed)"}, []string{"PATH=/usr/bin:/bin"}, t.TempDir(), 80, 24, place)
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	var out bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&out, proc); close(done) }()
	if code, _ := proc.Wait(); code != 0 {
		t.Fatalf("program exited %d: %q", code, out.String())
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	if !strings.Contains(heldAs, ScopeExecArg) {
		t.Fatalf("at place time the child was %q, not held; output %q", heldAs, out.String())
	}
	if placedPID != proc.PID() {
		t.Fatalf("placed pid %d, program pid %d", placedPID, proc.PID())
	}
	text := out.String()
	if !strings.Contains(text, "pid="+strconv.Itoa(proc.PID())) || !strings.Contains(text, "fd3=closed") {
		t.Fatalf("program output %q: want its own pid %d and fd 3 closed", text, proc.PID())
	}
}

func TestOnlyARealHostPlacesPrograms(t *testing.T) {
	unit := "mewla-worker-abc-0123456789abcdef0123456789abcdef.scope"
	env := []string{"PATH=/bin", resourceUnitEnv + "=" + unit}
	if (&Server{}).placement("s", env) != nil {
		t.Fatal("a host without SessionScopes places programs")
	}
	host := &Server{opts: ServerOptions{SessionScopes: true}}
	if host.placement("s", []string{"PATH=/bin"}) != nil {
		t.Fatal("a Session without a resource unit is placed")
	}
	if host.placement("s", []string{resourceUnitEnv + "=mewla-session-host-1.service"}) != nil {
		t.Fatal("a Session with a malformed unit is placed")
	}
}
