package watcher

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// ErrSessionNotIdle is a reclaim that waits: the Session's agent is still at
// work (or has not been idle long enough), so nothing is torn down now and a
// later pass looks again. It is never a refusal. The wrapped text says what
// kept it.
var ErrSessionNotIdle = errors.New("Session is not idle at its prompt")

// reclaimIdleStable is the grace a Session whose turn never reported an end
// gets before a cleanup removes it: since its agent's own record says the turn
// ended, or, when that record cannot be read, with its screen unchanged at the
// prompt. It covers the gap between a background task ending and the agent's
// next activity showing, and an agent still writing after it marked its work
// done.
const reclaimIdleStable = 60 * time.Second

// sessionRest is one look at whether a Session has anything of its own
// running.
type sessionRest struct {
	idle bool
	// settledAt is when the agent's own record says its last turn ended; zero
	// when the decision rests on the screen alone.
	settledAt time.Time
	// reason says what keeps a Session that is not idle.
	reason  string
	content string
}

// SessionIdleAtPrompt reports whether a Worker Session has nothing running
// now (sessionRest) and no prompt on its way in.
func (w *Watcher) SessionIdleAtPrompt(sessionID string) bool {
	if pending, err := w.pendingInputAdmissions(strings.TrimSpace(sessionID)); err != nil || len(pending) != 0 {
		return false // a prompt is on its way in
	}
	return w.lookAtRest(sessionID).idle
}

func (w *Watcher) lookAtRest(sessionID string) sessionRest {
	sessionID = strings.TrimSpace(sessionID)
	worker := w.GetWorker(sessionID)
	if worker == nil {
		return sessionRest{reason: "the Session is not listed"}
	}
	content, err := w.CapturePaneContent(sessionID)
	if err != nil {
		return sessionRest{reason: "its screen cannot be read: " + err.Error()}
	}
	var provider *ProviderActivityObservation
	w.mu.RLock()
	probe := w.providerActivityProbe
	w.mu.RUnlock()
	if probe != nil {
		observation := probe.ObserveProviderActivity(*worker, time.Now().UTC())
		provider = &observation
	}
	rest := restOf(worker.Command, content, provider)
	rest.content = content
	return rest
}

// restOf is the idle decision on one look at a Session. The agent's own record
// (its hooks or app-server, else its transcript) is what says a turn ended:
// when it is readable, a settled last turn is idle and a running one is not,
// whatever its composer shows (Claude's "Resend your message to try again"
// after a provider error, a suggestion). The screen only vetoes: a working
// spinner or background shells, agents or monitors keep the Session. Without a
// readable record the screen must sit at the agent's empty input prompt.
func restOf(command, content string, provider *ProviderActivityObservation) sessionRest {
	switch {
	case strings.TrimSpace(content) == "":
		return sessionRest{reason: "its screen is empty"}
	case classifier.AgentWorkingOnScreen(content):
		return sessionRest{reason: "its screen shows the agent working"}
	case paneShowsBackgroundWork(content):
		return sessionRest{reason: "its own background shells, agents or monitors run"}
	}
	if provider != nil && provider.ProbeState == ProbeStateOK && strings.TrimSpace(provider.Status) != "" {
		if !providerActivityTerminal(provider.Status) {
			return sessionRest{reason: "its agent's turn is " + strings.TrimSpace(provider.Status)}
		}
		return sessionRest{idle: true, settledAt: provider.SettledAt}
	}
	if !isWorkerInputReady(command, content) {
		return sessionRest{reason: "its agent's record cannot be read and its screen is not at the prompt"}
	}
	return sessionRest{idle: true}
}

// idleTracker remembers since when each Session has sat unchanged at its
// prompt, for the stability window of a Session judged by its screen alone.
type idleTracker struct {
	mu   sync.Mutex
	seen map[string]idleMark
}

type idleMark struct {
	since time.Time
	sum   uint64
}

// sessionSettled is one observation of whether the Session has rested for
// reclaimIdleStable: since its agent's record says the turn ended, or, by the
// screen alone, unchanged at its prompt for that long. A busy look restarts
// the window. The error, when not nil, wraps ErrSessionNotIdle with what keeps
// the Session.
func (w *Watcher) sessionSettled(sessionID string, now time.Time) error {
	rest := w.lookAtRest(sessionID)
	if !w.idle.observe(sessionID, rest, now) {
		reason := rest.reason
		if rest.idle {
			reason = "it has been idle for less than " + reclaimIdleStable.String()
		}
		return fmt.Errorf("%w: %s", ErrSessionNotIdle, reason)
	}
	return nil
}

// SessionSettledIdle is sessionSettled as a yes or no.
func (w *Watcher) SessionSettledIdle(sessionID string, now time.Time) bool {
	return w.sessionSettled(sessionID, now) == nil
}

// observe records one look and says whether the Session has now rested for
// reclaimIdleStable.
func (t *idleTracker) observe(sessionID string, rest sessionRest, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !rest.idle {
		delete(t.seen, sessionID)
		return false
	}
	if !rest.settledAt.IsZero() {
		delete(t.seen, sessionID)
		return now.Sub(rest.settledAt) >= reclaimIdleStable
	}
	if t.seen == nil {
		t.seen = map[string]idleMark{}
	}
	sum := paneChecksum(rest.content)
	mark, ok := t.seen[sessionID]
	if !ok || mark.sum != sum {
		t.seen[sessionID] = idleMark{since: now, sum: sum}
		return false
	}
	return now.Sub(mark.since) >= reclaimIdleStable
}

// ReclaimIdleSession removes a delegated Worker Session another owner has
// finished with (a Calendar run whose result is in) once it has rested.
// ErrSessionNotIdle means look again later.
func (w *Watcher) ReclaimIdleSession(sessionID string) error {
	return w.sessionInputOwner().serialized(sessionID, func() error {
		worker := w.GetWorker(sessionID)
		if worker == nil {
			return nil // gone already
		}
		if worker.Hidden || !worker.Delegated {
			return ErrNotOwnedDelegatedWorker
		}
		pending, err := w.pendingInputAdmissions(sessionID)
		if err != nil {
			return err
		}
		if len(pending) != 0 {
			return fmt.Errorf("%w: a prompt is on its way in", ErrSessionNotIdle)
		}
		if err := w.sessionSettled(sessionID, time.Now().UTC()); err != nil {
			return err
		}
		return w.KillSession(sessionID)
	})
}
