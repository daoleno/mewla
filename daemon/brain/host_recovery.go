package brain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/daoleno/mewla/daemon/work"
)

// ErrHostNotRunnable means Brain's agent cannot run on this computer yet: its
// command is not installed, or Mewla verified it signed out. Nothing is
// launched; the host starts once the cause is fixed (see HostRecoveryDue).
var ErrHostNotRunnable = errors.New("brain host agent cannot run yet")

// hostRunnable reports whether the executor's command is installed and not
// verified signed out. Unknown sign-in is runnable: Mewla does not guess.
func (s *Service) hostRunnable(executor work.WorkerExecutor) error {
	command := strings.TrimSpace(executor.Command)
	if command == "" {
		command = executor.ID
	}
	fields := strings.Fields(command)
	if len(fields) > 0 {
		if _, err := hostLookPath(fields[0]); err != nil {
			return fmt.Errorf("%w: %s is not installed", ErrHostNotRunnable, fields[0])
		}
	}
	if hostAuth(executor.ID) == HostAuthUnauthenticated {
		return fmt.Errorf("%w: %s is not signed in", ErrHostNotRunnable, executor.ID)
	}
	return nil
}

// HostRecoveryDue reports that Brain has no observed host and its agent can run
// now: the person installed or signed in to it after Mewla started, so the
// host should start without a restart. A host that is observed, or whose agent
// still cannot run, is not due.
func (s *Service) HostRecoveryDue() bool {
	if s == nil || s.store == nil {
		return false
	}
	executor := s.hostExecutor()
	host, err := s.projectedHostWorker(executor)
	if err != nil || s.hostObserved(host.ID) {
		return false
	}
	return s.hostRunnable(executor) == nil
}
