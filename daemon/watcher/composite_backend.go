package watcher

import (
	"errors"
	"fmt"
	"sync"

	"github.com/daoleno/mewla/daemon/classifier"
)

// compositeBackend runs every new Session in the session host and keeps
// seeing the Sessions an earlier Mewla started in tmux until each moves to the
// host (MoveToHost, keeping its id) or is left in tmux (LeaveInTmux). An id
// belongs to the host once the host runs it.
type compositeBackend struct {
	w    *Watcher
	tmux *tmuxBackend
	host *hostBackend

	// left are the tmux Sessions that stay in tmux, no longer Mewla's.
	leftMu sync.Mutex
	left   map[string]string // id -> why it stayed
}

func (c *compositeBackend) of(id string) sessionBackend {
	if c.host.validID(id) {
		return c.host
	}
	return c.tmux
}

func (c *compositeBackend) validID(id string) bool {
	return c.tmux.validID(id) || c.host.validID(id)
}

func (c *compositeBackend) observedGeneration(id string) string {
	return c.of(id).observedGeneration(id)
}

func (c *compositeBackend) terminal(id string) TerminalTarget { return c.of(id).terminal(id) }

// knows reports whether the watcher holds a Session of the given backend. It
// read-locks w.mu, so listOwned must never be called with w.mu held (poll
// releases it before listing).
func (c *compositeBackend) knows(valid func(string) bool) bool {
	c.w.mu.RLock()
	defer c.w.mu.RUnlock()
	for id := range c.w.workers {
		if valid(id) {
			return true
		}
	}
	return false
}

// listOwned merges both inventories and never returns a partial one. A
// backend with nothing running counts as empty only while the watcher holds
// none of its Sessions; once it does, a backend that vanished is unknown, as
// a vanished tmux server always was: a crash must not read as every Session
// ending.
func (c *compositeBackend) listOwned() ([]sessionPane, error) {
	if c.host.cfg.HostOnly {
		// No host running is nothing running at all, as a tmux server that
		// is not: the first inventory is empty, and a Session an earlier run
		// recorded is gone with the host that held it.
		panes, err := c.host.listOwned()
		if errors.Is(err, errSessionHostGone) {
			return nil, fmt.Errorf("%w: %w", ErrTmuxServerGone, err)
		}
		return panes, err
	}
	tmuxPanes, tmuxErr := c.tmux.listOwned()
	tmuxPanes = withoutLeft(c, tmuxPanes, func(p sessionPane) string { return p.target })
	hostPanes, hostErr := c.host.listOwned()
	// A Session that moved kept its pane id. A tmux server that restarted
	// numbers its panes from %0 again; a pane there with a moved id is never
	// that Session.
	tmuxPanes = withoutMoved(c, tmuxPanes)
	if errors.Is(hostErr, errSessionHostGone) && !c.knows(c.host.validID) {
		// No host in play: tmux's answer stands as it always did, errors
		// included, so a daemon that never used the host behaves as before.
		return tmuxPanes, tmuxErr
	}
	if hostErr != nil {
		return nil, fmt.Errorf("session host inventory: %w", hostErr)
	}
	if errors.Is(tmuxErr, ErrTmuxServerGone) && !c.knows(c.tmux.validID) {
		tmuxPanes, tmuxErr = nil, nil
	}
	if tmuxErr != nil {
		return nil, tmuxErr
	}
	return append(tmuxPanes, hostPanes...), nil
}

func (c *compositeBackend) listServices() ([]servicePane, error) {
	if c.host.cfg.HostOnly {
		return c.host.listServices()
	}
	tmuxPanes, err := c.tmux.listServices()
	if err != nil {
		return nil, err
	}
	tmuxPanes = withoutLeft(c, tmuxPanes, func(p servicePane) string { return p.paneID })
	hostPanes, err := c.host.listServices()
	if err != nil {
		return nil, err
	}
	return append(tmuxPanes, hostPanes...), nil
}

// isLeft reports a tmux Session left in tmux: it is no longer Mewla's.
func (c *compositeBackend) isLeft(id string) bool {
	if c.host.validID(id) {
		return false
	}
	c.leftMu.Lock()
	defer c.leftMu.Unlock()
	_, left := c.left[id]
	return left
}

// probeOwnership never claims a Session left in tmux: it runs on there, but
// Mewla no longer owns it, so nothing of Mewla's may type into or end it.
func (c *compositeBackend) probeOwnership(id string) (bool, bool, error) {
	if c.isLeft(id) {
		return false, false, nil
	}
	return c.of(id).probeOwnership(id)
}

// resolveAbsence: a Session left in tmux has ended as far as Mewla goes.
func (c *compositeBackend) resolveAbsence(id string, sawInventory bool) (bool, error) {
	if c.isLeft(id) {
		return true, nil
	}
	return c.of(id).resolveAbsence(id, sawInventory)
}

func (c *compositeBackend) processRoot(id string) (int, error) { return c.of(id).processRoot(id) }

func (c *compositeBackend) currentPath(id string) (string, error) {
	return c.of(id).currentPath(id)
}

func (c *compositeBackend) delegatedResource(id string) (bool, string) {
	return c.of(id).delegatedResource(id)
}

func (c *compositeBackend) captureState(id string) (string, bool, int) {
	return c.of(id).captureState(id)
}

func (c *compositeBackend) captureRecent(id string) (string, error) {
	return c.of(id).captureRecent(id)
}

func (c *compositeBackend) captureVisible(id string, styled bool) (string, error) {
	return c.of(id).captureVisible(id, styled)
}

func (c *compositeBackend) captureDialog(id string) (string, classifier.CaptureInfo, error) {
	return c.of(id).captureDialog(id)
}

func (c *compositeBackend) sendKeys(id string, keys ...string) error {
	return c.of(id).sendKeys(id, keys...)
}

func (c *compositeBackend) repeatKey(id, key string, count int) error {
	return c.of(id).repeatKey(id, key, count)
}

func (c *compositeBackend) sendLiteral(id, text string) error {
	return c.of(id).sendLiteral(id, text)
}

func (c *compositeBackend) sendBytes(id string, data []byte) error {
	return c.of(id).sendBytes(id, data)
}

// place puts every new Session in the session host.
func (c *compositeBackend) place(near string, detached bool, name string) (backendPlacement, error) {
	if !c.host.validID(near) {
		// A Session still in tmux has no place in the host to stand beside.
		near = ""
	}
	return c.host.place(near, detached, name)
}

func (c *compositeBackend) create(at backendPlacement, spec backendCreateSpec) (string, error) {
	if at.backend == backendHost {
		return c.host.create(at, spec)
	}
	return c.tmux.create(at, spec)
}

func (c *compositeBackend) kill(id string) error { return c.of(id).kill(id) }

func (c *compositeBackend) inputIO() sessionInputIO {
	return compositeInputIO{c: c, tmux: c.tmux.inputIO(), host: c.host.inputIO()}
}

// compositeInputIO sends each Session's input through its own backend. The
// server it names for a host Session is the host's, so a paste or receipt
// never crosses backends.
type compositeInputIO struct {
	c    *compositeBackend
	tmux sessionInputIO
	host sessionInputIO
}

func (io compositeInputIO) of(id string) sessionInputIO {
	if io.c.host.validID(id) {
		return io.host
	}
	return io.tmux
}

func (io compositeInputIO) socket(sessionID string) string {
	return io.of(sessionID).socket(sessionID)
}

func (io compositeInputIO) pane(socket, sessionID string) sessionInputPane {
	return io.of(sessionID).pane(socket, sessionID)
}

func (io compositeInputIO) submitPaste(socket, paneID, payload string, adapter sessionInputProvider, beforeStart func() error) (bool, error) {
	return io.of(paneID).submitPaste(socket, paneID, payload, adapter, beforeStart)
}

func (io compositeInputIO) receiptLedger(socket, target string) (sessionInputReceiptLedger, error) {
	return io.of(target).receiptLedger(socket, target)
}

func (io compositeInputIO) writeReceiptLedger(socket, target string, ledger sessionInputReceiptLedger) error {
	return io.of(target).writeReceiptLedger(socket, target, ledger)
}
