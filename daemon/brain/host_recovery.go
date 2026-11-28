package brain

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

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
// version Mewla supports. Sign-in is not part of it: Mewla never checks it. A
// host that sits at its own sign-in or first-run screen is reported, with what
// it shows, once it has been open and unready for a while.
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
	hostActiveProbe(executor.ID)
	if reason := hostUnsupported(executor.ID); reason != "" {
		return fmt.Errorf("%w: %s", ErrHostNotRunnable, reason)
	}
	return nil
}

// HostRecoveryDue reports that Brain has no observed host and its agent can run
// now: the person installed it after Mewla started, so the host should start
// without a restart. A host that is observed, or whose agent
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
		// may have finished its own screen (a sign-in, a question) since.
		return s.hostActivationStuck()
	}
	return s.hostRunnable(executor) == nil
}

// hostNotReadyGrace is how long a host's pane may stay open and unready before
// the computer says so: a slow agent start is normal for a while.
const hostNotReadyGrace = 60 * time.Second

// noteHostActivation records whether the last host reconcile left the agent open
// but never ready for input (a sign-in prompt, a first-run question), the state
// a person cannot see from the phone. An activation whose delivery is
// ambiguous reached the agent's composer, so the agent was ready: it ends the
// not-ready state as success does. Other failures (a probe to repeat) leave
// it as it was.
func (s *Service) noteHostActivation(err error) {
	s.hostProblemMu.Lock()
	defer s.hostProblemMu.Unlock()
	switch {
	case err == nil, errors.Is(err, ErrHostActivationAmbiguous):
		s.hostNotReadySince = time.Time{}
	case errors.Is(err, watcher.ErrWorkerInputNotReady) && s.hostNotReadySince.IsZero():
		s.hostNotReadySince = time.Now()
	}
}

// noteHostActivationAccepted ends the not-ready state once the Host took its
// activation, on whichever path delivered it: a client's new chat or host
// choice delivers it too, and only a reconcile used to end the state.
func (s *Service) noteHostActivationAccepted() {
	s.hostProblemMu.Lock()
	s.hostNotReadySince = time.Time{}
	s.hostProblemMu.Unlock()
}

// hostActivationRecorded reports that the current Host Session took its
// activation, as the store records it. Such a Host is past any sign-in or
// first-run screen and answers on its own, so it is not "open but not ready",
// whatever a later check of its screen says.
func (s *Service) hostActivationRecorded() bool {
	if s == nil || s.store == nil {
		return false
	}
	host, err := s.store.HostSession()
	if err != nil || strings.TrimSpace(host.ID) == "" {
		return false
	}
	activation, err := s.store.HostActivation()
	return err == nil && strings.TrimSpace(activation.SessionID) == strings.TrimSpace(host.ID)
}

func (s *Service) hostActivationStuck() bool {
	s.hostProblemMu.Lock()
	defer s.hostProblemMu.Unlock()
	return !s.hostNotReadySince.IsZero()
}

// hostNotReadyProblem names an agent whose pane is open but has not become
// ready for input for a while, with what the agent itself shows there (its
// sign-in screen, a first-run question, an error). Mewla does not guess why.
func (s *Service) hostNotReadyProblem(executor work.WorkerExecutor) *HostProblem {
	s.hostProblemMu.Lock()
	since := s.hostNotReadySince
	s.hostProblemMu.Unlock()
	if since.IsZero() {
		return nil
	}
	// Every agent takes a moment to become ready, so nothing is said before
	// the grace.
	if time.Since(since) < hostNotReadyGrace {
		return nil
	}
	if s.hostActivationRecorded() {
		return nil
	}
	return &HostProblem{
		Code:     "not_ready",
		Executor: executor.ID,
		Detail:   fmt.Sprintf("Brain's %s session is open but not ready; Brain continues once it is.", executor.ID) + s.hostScreenTail(),
	}
}

// hostScreenTailLines and hostScreenTailRunes bound the agent's own words a
// not-ready problem quotes.
const (
	hostScreenTailLines = 4
	hostScreenTailRunes = 320
)

// hostScreenTail is what the Host's agent itself shows last (its sign-in
// screen, an error it printed), as " It shows: …", or "" when nothing is known.
func (s *Service) hostScreenTail() string {
	if s == nil || s.store == nil || s.watcher == nil {
		return ""
	}
	host, err := s.store.HostSession()
	if err != nil || strings.TrimSpace(host.ID) == "" {
		return ""
	}
	worker := s.watcher.GetWorker(host.ID)
	if worker == nil {
		return ""
	}
	var lines []string
	for i := len(worker.LastLines) - 1; i >= 0 && len(lines) < hostScreenTailLines; i-- {
		line := strings.Join(strings.Fields(worker.LastLines[i]), " ")
		if line == "" || strings.Trim(line, "─━-=_│|>›▌ ") == "" {
			continue
		}
		lines = append([]string{line}, lines...)
	}
	if len(lines) == 0 {
		return ""
	}
	text := strings.Join(lines, " / ")
	if runes := []rune(text); len(runes) > hostScreenTailRunes {
		text = string(runes[len(runes)-hostScreenTailRunes:])
	}
	return " It shows: " + text
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

// hostSessionExited reports a host Session whose program has ended. The
// session host keeps such a Session, with its last screen, so it is still
// observed; the Brain in it is gone all the same.
func (s *Service) hostSessionExited(id string) bool {
	exited, ok := s.watcher.(interface{ SessionExited(string) bool })
	return ok && strings.TrimSpace(id) != "" && exited.SessionExited(id)
}

// hostRestartNoticeTitle and hostRestartNoticeBody are the Chat row a person
// sees when Brain's Session was gone and its conversation could not be resumed.
const (
	hostRestartNoticeTitle = "Brain restarted"
	hostRestartNoticeBody  = "The earlier conversation couldn't be resumed. Brain continues from its workspace notes."
)

// noteHostRestartedFresh tells Chat, once per new Host, that Brain started a
// new conversation because the one it held could not be resumed.
func (s *Service) noteHostRestartedFresh(hostID, executorID string) {
	threadID, err := s.store.ChatThreadID()
	if err != nil || strings.TrimSpace(threadID) == "" {
		log.Printf("brain host restarted fresh as %s; no Chat thread for the notice: %v", hostID, err)
		return
	}
	if _, err := s.store.AppendTimelineItem(TimelineItem{
		ID:         "brain-restarted:" + hostID,
		ThreadID:   threadID,
		SessionID:  hostID,
		ExecutorID: executorID,
		Role:       "assistant",
		Kind:       timelineKindHostNotice,
		Status:     "warning",
		Title:      hostRestartNoticeTitle,
		Body:       hostRestartNoticeBody,
	}); err != nil {
		log.Printf("brain host restarted fresh as %s; Chat notice failed: %v", hostID, err)
	}
}
