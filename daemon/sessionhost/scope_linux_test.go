//go:build linux

package sessionhost

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
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

// A placement that fails, for whatever reason, never keeps a Session from
// starting: its program runs unplaced and the host says why, once.
func TestASessionStartsWhenItsPlacementFails(t *testing.T) {
	unit := "mewla-worker-abc-0123456789abcdef0123456789abcdef.scope"
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	cases := []struct {
		name      string
		userUnits bool
		place     func(int, string, string) (string, error)
		cause     string
	}{
		{"no user manager", false, nil, "no systemd user manager"},
		{"systemd refuses", true, func(int, string, string) (string, error) {
			return "", errors.New("start x.scope: Unit x.scope was already loaded")
		}, "already loaded"},
		{"no answer", true, func(int, string, string) (string, error) { <-hang; return "", nil }, "did not place it within"},
		{"a panic", true, func(int, string, string) (string, error) { panic("bus gone") }, "placement panicked: bus gone"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var lines []string
			h := startHost(t, ServerOptions{
				SessionScopes: true,
				Logf: func(format string, args ...any) {
					mu.Lock()
					lines = append(lines, fmt.Sprintf(format, args...))
					mu.Unlock()
				},
				userUnits:    func() bool { return tc.userUnits },
				placeInScope: tc.place,
				placeTimeout: 200 * time.Millisecond,
			})
			c := dialHost(t, h)
			id := "placed" + strconv.Itoa(i)
			params := shellSession(id, shortTempDir(t), `printf 'running %s\n' "$MEWLA_SESSION_ID"; exec sleep 30`)
			params.Env = append(params.Env, resourceUnitEnv+"="+unit)
			info, err := c.Create(ctxT(t), params)
			if err != nil || !info.Alive {
				t.Fatalf("Create = %+v, %v: a failed placement kept the Session from starting", info, err)
			}
			waitScreen(t, c, id, "running "+id)
			mu.Lock()
			defer mu.Unlock()
			var outside []string
			for _, line := range lines {
				if strings.Contains(line, "runs outside its slice") {
					outside = append(outside, line)
				}
			}
			if len(outside) != 1 || !strings.Contains(outside[0], tc.cause) || !strings.Contains(outside[0], id) {
				t.Fatalf("log lines %q: want one naming %q", outside, tc.cause)
			}
		})
	}
}
