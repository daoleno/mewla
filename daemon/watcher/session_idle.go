package watcher

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// ErrSessionNotIdle is a reclaim that waits: the Session's agent is still at
// work (or its screen has not stayed at the prompt long enough), so nothing is
// torn down now and a later pass looks again. It is never a refusal.
var ErrSessionNotIdle = errors.New("Session is not idle at its prompt")

// reclaimIdleStable is how long a Session whose turn never reported an end
// must sit unchanged at its prompt before a finished Work's cleanup removes it.
// It covers the gap between a background task ending and the agent's next
// activity showing, and an agent still writing after it marked its work done.
const reclaimIdleStable = 60 * time.Second

// SessionIdleAtPrompt reports whether a Worker Session has nothing running
// now: its agent's own record says the turn settled (when that record is
// readable), its screen sits at the input prompt, and neither the agent's
// working spinner nor background shells, agents or monitors show. These are
// the stall supervisor's own facts; the screen is the Worker's own process.
func (w *Watcher) SessionIdleAtPrompt(sessionID string) bool {
	if pending, err := w.pendingInputAdmissions(strings.TrimSpace(sessionID)); err != nil || len(pending) != 0 {
		return false // a prompt is on its way in
	}
	_, idle := w.sessionIdleScreen(sessionID)
	return idle
}

func (w *Watcher) sessionIdleScreen(sessionID string) (string, bool) {
	sessionID = strings.TrimSpace(sessionID)
	worker := w.GetWorker(sessionID)
	if worker == nil {
		return "", false
	}
	content, err := w.CapturePaneContent(sessionID)
	if err != nil {
		return "", false
	}
	var provider *ProviderActivityObservation
	w.mu.RLock()
	probe := w.providerActivityProbe
	w.mu.RUnlock()
	if probe != nil {
		observation := probe.ObserveProviderActivity(*worker, time.Now().UTC())
		provider = &observation
	}
	return content, idleAtPrompt(worker.Command, content, provider)
}

// idleAtPrompt is the idle decision on one look at a Session: a screen at the
// agent's input prompt with no working spinner and no background work, and,
// when the agent's own record is readable, a settled turn in it.
func idleAtPrompt(command, content string, provider *ProviderActivityObservation) bool {
	if strings.TrimSpace(content) == "" || classifier.AgentWorkingOnScreen(content) ||
		paneShowsBackgroundWork(content) || !isWorkerInputReady(command, content) {
		return false
	}
	return provider == nil || provider.ProbeState != ProbeStateOK || providerActivityTerminal(provider.Status)
}

// idleTracker remembers since when each Session has sat unchanged at its
// prompt, for reclaim's stability window.
type idleTracker struct {
	mu   sync.Mutex
	seen map[string]idleMark
}

type idleMark struct {
	since time.Time
	sum   uint64
}

// SessionSettledIdle is SessionIdleAtPrompt held for reclaimIdleStable with
// the screen unchanged. Each call is one observation; a busy or changed
// screen restarts the window.
func (w *Watcher) SessionSettledIdle(sessionID string, now time.Time) bool {
	content, idle := w.sessionIdleScreen(sessionID)
	return w.idle.observe(sessionID, content, idle, now)
}

// observe records one look and says whether the Session has now sat idle,
// its screen unchanged, for reclaimIdleStable.
func (t *idleTracker) observe(sessionID, content string, idle bool, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !idle {
		delete(t.seen, sessionID)
		return false
	}
	if t.seen == nil {
		t.seen = map[string]idleMark{}
	}
	sum := paneChecksum(content)
	mark, ok := t.seen[sessionID]
	if !ok || mark.sum != sum {
		t.seen[sessionID] = idleMark{since: now, sum: sum}
		return false
	}
	return now.Sub(mark.since) >= reclaimIdleStable
}

// ReclaimIdleSession removes a delegated Worker Session another owner has
// finished with (a Calendar run whose result is in) once it has settled idle
// at its prompt. ErrSessionNotIdle means look again later.
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
		if len(pending) != 0 || !w.SessionSettledIdle(sessionID, time.Now().UTC()) {
			return ErrSessionNotIdle
		}
		return w.KillSession(sessionID)
	})
}
