package sessionhost

import (
	"runtime"
	"strings"

	"github.com/daoleno/mewla/daemon/userunit"
)

// resourceUnitEnv names the Session's resource unit in its environment (set
// by the daemon for a Worker; see watcher's delegated resources).
const resourceUnitEnv = "MEWLA_WORKER_RESOURCE_UNIT"

// placement is how a program started for Session id is put in its Session's
// slice, nil when it is not: the host was not asked to (SessionScopes), the
// Session has no resource unit, or there is no user manager (Linux only).
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
	if slice == "" || !userunit.Available() {
		return nil
	}
	return func(pid int) {
		scope, err := userunit.PlaceInScope(pid, slice, "Mewla Session "+id)
		if err != nil {
			s.logf("Session %s runs outside its slice %s: %v", id, slice, err)
			return
		}
		s.logf("Session %s: pid %d in %s/%s", id, pid, slice, scope)
	}
}
