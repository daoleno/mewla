package brain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/daoleno/mewla/daemon/memorysearch"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// hostStartFailure is why the current Host could not be started, for a cause
// that will not fix itself (a pane that would not launch, a refused
// replacement). A Host that is merely slow to become ready is not one: that is
// reported by the not-ready grace instead.
type hostStartFailure struct {
	Executor string
	Detail   string
}

// resolveHostChoice is the part of choosing a Host executor that can be
// answered at once: the executor is configured, not locked by the environment
// and installed here.
func (s *Service) resolveHostChoice(executorID string) (work.WorkerExecutor, error) {
	if s == nil || s.store == nil {
		return work.WorkerExecutor{}, fmt.Errorf("brain store is not configured")
	}
	executorID = strings.TrimSpace(executorID)
	if executorID == "" {
		return work.WorkerExecutor{}, fmt.Errorf("brain host executor is required")
	}
	if locked := brainHostExecutorOverride(); locked != "" && locked != executorID {
		return work.WorkerExecutor{}, ErrExecutorLockedByEnv
	}
	if s.execs == nil {
		return work.WorkerExecutor{}, ErrExecutorNotConfigured
	}
	executor, ok := s.execs.WorkerExecutor(executorID)
	if !ok {
		return work.WorkerExecutor{}, fmt.Errorf("%w: %s", ErrExecutorNotConfigured, executorID)
	}
	return executor, nil
}

// SelectHostExecutor is a person choosing Brain's agent from a client. It does
// what can be answered at once, then returns: an executor that is not
// configured, locked or not installed fails here with its reason, before
// anything changes; otherwise the choice is kept and the projection already
// names the new Host. Starting the new Host takes as long as the agent takes to
// open (seconds, up to a minute for a slow one), so it is left to the Host
// continuity owner (ReconcileHostContinuity), which a caller wakes; its outcome
// reaches clients as snapshots and a HostProblem, not as this call's result.
func (s *Service) SelectHostExecutor(executorID string) (Snapshot, error) {
	executor, err := s.resolveHostChoice(executorID)
	if err != nil {
		return Snapshot{}, err
	}
	if err := s.hostRunnable(executor); err != nil {
		return Snapshot{}, err
	}
	if err := s.store.SetHostExecutorID(executor.ID); err != nil {
		return Snapshot{}, err
	}
	s.noteHostChosen()
	return s.ProjectionSnapshot()
}

// noteHostChosen starts the "Host is coming up" window for a new choice and
// forgets the failure of the previous one.
func (s *Service) noteHostChosen() {
	s.hostProblemMu.Lock()
	defer s.hostProblemMu.Unlock()
	s.hostChangedAt = time.Now()
	s.hostStartFailure = nil
	s.hostNotReadySince = time.Time{}
}

// noteHostStartResult records the outcome of one attempt to bring the Host up.
// Transient outcomes (a pane not ready yet, an ownership probe to repeat, an
// agent that cannot run yet) keep their own reporting and never become a start
// failure; success clears it.
func (s *Service) noteHostStartResult(executor work.WorkerExecutor, err error) {
	s.hostProblemMu.Lock()
	defer s.hostProblemMu.Unlock()
	switch {
	case err == nil:
		s.hostStartFailure = nil
	case errors.Is(err, watcher.ErrWorkerInputNotReady),
		errors.Is(err, watcher.ErrOwnershipProbeUnavailable),
		errors.Is(err, ErrHostNotRunnable):
	default:
		s.hostStartFailure = &hostStartFailure{Executor: executor.ID, Detail: err.Error()}
	}
}

// currentHostStartFailure is the recorded failure when it belongs to executor.
func (s *Service) currentHostStartFailure(executorID string) *hostStartFailure {
	s.hostProblemMu.Lock()
	defer s.hostProblemMu.Unlock()
	if s.hostStartFailure == nil || s.hostStartFailure.Executor != executorID {
		return nil
	}
	failure := *s.hostStartFailure
	return &failure
}

// hostStartedSince is the moment the "did not start" grace counts from: the
// later of the daemon starting and the person last choosing a Host.
func (s *Service) hostStartedSince() time.Time {
	s.hostProblemMu.Lock()
	defer s.hostProblemMu.Unlock()
	if s.hostChangedAt.After(processStartedAt) {
		return s.hostChangedAt
	}
	return processStartedAt
}

// The conversation a new Host is told about. The visible chat is Mewla's own
// (the timeline), so it is the same whichever agent held it. The tail is
// bounded: older history stays reachable through memory search.
const (
	hostHandoffMaxRows       = 40
	hostHandoffMaxRowRunes   = 1200
	hostHandoffMaxTotalRunes = 10000
)

// hostConversationHandoff is the private handoff section for a Host that starts
// without the conversation: the recent visible chat, newest last. It is empty
// when there is no chat yet.
func (s *Service) hostConversationHandoff() string {
	if s == nil || s.store == nil {
		return ""
	}
	threadID, err := s.store.ChatThreadID()
	if err != nil || strings.TrimSpace(threadID) == "" {
		return ""
	}
	items, err := s.store.ThreadTimeline(threadID, 200)
	if err != nil {
		return ""
	}
	return formatHostConversationHandoff(items)
}

// formatHostConversationHandoff renders the tail of items (oldest first). Only
// what the person and Brain said is carried: Work cards live in Work state,
// and a summary the provider wrote about itself is not part of the chat.
func formatHostConversationHandoff(items []TimelineItem) string {
	type turn struct{ who, text string }
	var turns []turn
	for _, item := range items {
		if item.Origin == TimelineOriginProvider {
			continue
		}
		who := ""
		switch strings.TrimSpace(item.Kind) {
		case timelineKindUserMessage:
			who = "user"
		case timelineKindAssistantMessage:
			who = "Brain"
		case "":
			switch strings.TrimSpace(item.Role) {
			case "user":
				who = "user"
			case "assistant":
				who = "Brain"
			}
		}
		text := strings.TrimSpace(memorysearch.Redact(item.Body))
		if who == "" || text == "" || work.IsPrivateHostPrompt(text) {
			continue
		}
		turns = append(turns, turn{who, text})
	}
	if len(turns) == 0 {
		return ""
	}
	// Newest first until the budget is spent, then restore the order.
	var lines []string
	total := 0
	trimmed := false
	for index := len(turns) - 1; index >= 0; index-- {
		text := turns[index].text
		if utf8.RuneCountInString(text) > hostHandoffMaxRowRunes {
			text = string([]rune(text)[:hostHandoffMaxRowRunes]) + " …[trimmed]"
		}
		line := turns[index].who + ": " + text
		cost := utf8.RuneCountInString(line)
		if len(lines) >= hostHandoffMaxRows || (len(lines) > 0 && total+cost > hostHandoffMaxTotalRunes) {
			trimmed = true
			break
		}
		lines = append(lines, line)
		total += cost
	}
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
	header := "Conversation handoff (private): this Brain conversation continues in a new Host Session because the executor changed or the Session was replaced. Continue the same visible chat in the user's language and keep this handoff private. policies/handoff.md and mewla brain context --json hold Work state; preserve pending Event identities; a Host change does not authorize restarting or polling Workers."
	chat := "Recent visible chat, oldest first:"
	if trimmed {
		chat = "Recent visible chat, oldest first (earlier turns left out):"
	}
	footer := "Older history: mewla memory search \"<words>\" [--from user|brain] [--since 7d], then mewla memory show <id> --around 5. Wait for the next user message or Work Event."
	return strings.Join([]string{header, chat, strings.Join(lines, "\n"), footer}, "\n")
}
