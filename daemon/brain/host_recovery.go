package brain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/hostdeps"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// ErrHostNotRunnable means Brain's agent cannot run on this computer yet: its
// command is not installed or of an unsupported version. Nothing is
// launched; the host starts once the cause is fixed (see HostRecoveryDue).
var ErrHostNotRunnable = errors.New("brain host agent cannot run yet")

// hostUnsupported says why an installed agent will not run on this computer
// (a version Mewla does not support), "" when nothing is known against it. The
// daemon installs doctor's verdict with SetHostUnsupportedProbe.
var hostUnsupported = func(string) string { return "" }

// SetHostUnsupportedProbe installs doctor's "installed but not runnable" verdict.
func SetHostUnsupportedProbe(probe func(executorID string) string) {
	if probe == nil {
		hostUnsupported = func(string) string { return "" }
		return
	}
	hostUnsupported = probe
}

// hostRunnable reports whether the executor's command is installed and of a
// version Mewla supports. Sign-in is not part of it: the pane starts in the
// person's login shell, whose environment can hold a sign-in (ANTHROPIC_*
// variables) that Mewla's own, narrower environment does not see, so a
// "signed out" verdict here is not proof. A host that really sits at a sign-in
// prompt is reported once it has been open and unready for a while.
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
	if reason := hostUnsupported(executor.ID); reason != "" {
		return fmt.Errorf("%w: %s", ErrHostNotRunnable, reason)
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
	if err != nil {
		return false
	}
	if s.hostObserved(host.ID) {
		if s.hostSessionExited(host.ID) {
			return true
		}
		// A pane that is open but never became ready is retried too: the person
		// may have signed in at its prompt since.
		return s.hostActivationStuck()
	}
	return s.hostRunnable(executor) == nil
}

// hostNotReadyGrace is how long a host's pane may stay open and unready before
// the computer says so: a slow agent start is normal for a while.
const hostNotReadyGrace = 60 * time.Second

// noteHostActivation records whether the last host reconcile left the agent open
// but never ready for input (a sign-in prompt, a first-run question), the state
// a person cannot see from the phone.
func (s *Service) noteHostActivation(err error) {
	s.hostProblemMu.Lock()
	defer s.hostProblemMu.Unlock()
	switch {
	case err == nil:
		s.hostNotReadySince = time.Time{}
	case errors.Is(err, watcher.ErrWorkerInputNotReady) && s.hostNotReadySince.IsZero():
		s.hostNotReadySince = time.Now()
	}
}

func (s *Service) hostActivationStuck() bool {
	s.hostProblemMu.Lock()
	defer s.hostProblemMu.Unlock()
	return !s.hostNotReadySince.IsZero()
}

// hostNotReadyProblem names an agent whose pane is open but has not become
// ready for input for a while. Agents whose sign-in Mewla cannot probe (Grok,
// Pi) end up here when they were never signed in.
func (s *Service) hostNotReadyProblem(executor work.WorkerExecutor) *HostProblem {
	s.hostProblemMu.Lock()
	since := s.hostNotReadySince
	s.hostProblemMu.Unlock()
	if since.IsZero() {
		return nil
	}
	name := executor.ID
	// Mewla's own view says signed out and the pane really is not ready: the
	// pane's login shell had no sign-in either, so say so at once instead of
	// leaving the phone waiting out the grace.
	if hostAuth(executor.ID) == HostAuthUnauthenticated {
		return &HostProblem{
			Code:            "not_signed_in",
			Executor:        executor.ID,
			Detail:          fmt.Sprintf("Brain is set to run on %s, but %s is not signed in on this computer. %s there; Brain starts by itself.", name, name, hostSignInStep(executor.ID)),
			SuggestExecutor: s.verifiedAlternativeHost(executor.ID),
		}
	}
	if time.Since(since) < hostNotReadyGrace {
		return nil
	}
	return &HostProblem{
		Code:            "not_ready",
		Executor:        executor.ID,
		Detail:          fmt.Sprintf("Brain's %s session is open but not ready. It may be waiting for you to sign in: run %s on this computer, finish its first-run steps, and Brain continues by itself.", name, name),
		SuggestExecutor: s.verifiedAlternativeHost(executor.ID),
	}
}

// hostHoldsNoConversation reports that the recorded host never held anything a
// blank replacement could lose: no provider conversation was ever identified and
// no turn was ever admitted to it. A computer rebooted before its first
// message restarts Brain; one with a conversation it cannot resume still refuses.
func (s *Service) hostHoldsNoConversation(host HostSession) bool {
	if strings.TrimSpace(host.ProviderSessionID) != "" || strings.TrimSpace(host.TranscriptPath) != "" {
		return false
	}
	_, hasTurn, err := s.store.Turn(host.ID)
	return err == nil && !hasTurn
}

// hostSignInStep is the sentence that tells a person how to sign in to an agent
// on the computer: its official login command where Mewla knows it.
func hostSignInStep(executorID string) string {
	for _, agent := range hostdeps.Agents(hostdeps.Detect()) {
		if agent.ID == executorID && agent.Login != "" {
			return "Run " + agent.Login
		}
	}
	return "Sign in to " + executorID + " by running it"
}

// hostSessionExited reports a host Session whose program has ended. The
// session host keeps such a Session, with its last screen, where tmux closed
// the pane, so it is still observed; the Brain in it is gone all the same.
func (s *Service) hostSessionExited(id string) bool {
	exited, ok := s.watcher.(interface{ SessionExited(string) bool })
	return ok && strings.TrimSpace(id) != "" && exited.SessionExited(id)
}
