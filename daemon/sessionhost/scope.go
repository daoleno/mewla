package sessionhost

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/userunit"
)

// resourceUnitEnv names the Session's resource unit in its environment (set
// by the daemon for a Worker; see watcher's delegated resources).
const resourceUnitEnv = "MEWLA_WORKER_RESOURCE_UNIT"

// placeTimeout bounds how long a Session's start waits for the user manager
// to place its program.
const placeTimeout = 5 * time.Second

// placement is how a program started for Session id is put in its Session's
// slice, nil when it is not placed: the host was not asked to
// (SessionScopes), it is not Linux, or the Session has no resource unit.
//
// A placement that fails never keeps a Session from starting: with no user
// manager, a refusal, an error or no answer within placeTimeout, the program
// starts unplaced and the host logs one line naming the cause.
func (s *Server) placement(id string, env []string) func(pid int) {
	if !s.opts.SessionScopes || runtime.GOOS != "linux" {
		return nil
	}
	unit := ""
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, resourceUnitEnv+"="); ok {
			unit = value
		}
	}
	slice := userunit.SessionSlice(unit, s.enclosing)
	if slice == "" {
		return nil
	}
	available, place, timeout := userunit.Available, userunit.PlaceInScope, placeTimeout
	if s.opts.userUnits != nil {
		available = s.opts.userUnits
	}
	if s.opts.placeInScope != nil {
		place = s.opts.placeInScope
	}
	if s.opts.placeTimeout > 0 {
		timeout = s.opts.placeTimeout
	}
	if !available() {
		s.logf("Session %s runs outside its slice %s: no systemd user manager to place it", id, slice)
		return nil
	}
	return func(pid int) {
		type placed struct {
			scope string
			err   error
		}
		done := make(chan placed, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					done <- placed{err: fmt.Errorf("placement panicked: %v", r)}
				}
			}()
			scope, err := place(pid, slice, "Mewla Session "+id)
			done <- placed{scope, err}
		}()
		select {
		case result := <-done:
			if result.err != nil {
				s.logf("Session %s runs outside its slice %s: %v", id, slice, result.err)
				return
			}
			s.logf("Session %s: pid %d in %s/%s", id, pid, slice, result.scope)
		case <-time.After(timeout):
			s.logf("Session %s runs outside its slice %s: the user manager did not place it within %s", id, slice, timeout)
		}
	}
}
