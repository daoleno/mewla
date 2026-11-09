package watcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/sessionhost"
)

// Mewla 0.3 runs every Session in the session host. Sessions an earlier Mewla
// started in tmux move once: when one is idle, its program ends in tmux and
// starts again in the host, resumed into the same conversation, under the
// same Session id, so every binding to that id holds. A Session that cannot
// resume stays in tmux and is no longer Mewla's.

// TmuxMove is how a tmux Session starts again in the host.
type TmuxMove struct {
	// Command is the shell command line that resumes the program.
	Command string
	// Env is the program's whole environment.
	Env []string
	Cwd string
	// ProcessID is the agent's process in tmux. It and the pane's own process
	// must end before the agent starts again; helpers it started may outlive
	// the pane (a Codex app-server can) and are left alone.
	ProcessID int
}

// tmuxMoveExitWait bounds the wait for the program to end in tmux before it
// starts again in the host: two processes must never share a conversation.
const tmuxMoveExitWait = 10 * time.Second

func (w *Watcher) composite() *compositeBackend {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	c, _ := w.backend.(*compositeBackend)
	return c
}

// TmuxSessions are copies of the Sessions Mewla still runs in tmux.
func (w *Watcher) TmuxSessions() []classifier.Worker {
	c := w.composite()
	if c == nil {
		return nil
	}
	w.mu.RLock()
	var out []classifier.Worker
	for id, worker := range w.workers {
		if worker == nil || !c.tmux.validID(id) || c.host.validID(id) {
			continue
		}
		out = append(out, *worker)
	}
	w.mu.RUnlock()
	c.leftMu.Lock()
	kept := out[:0]
	for _, worker := range out {
		if _, left := c.left[worker.ID]; !left {
			kept = append(kept, worker)
		}
	}
	c.leftMu.Unlock()
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
	return kept
}

// SessionHostReady checks, once per call, that the session host runs and can
// start Sessions: it starts the host if needed and runs a probe Session.
func (w *Watcher) SessionHostReady() error {
	c := w.composite()
	if c == nil {
		return errors.New("no session host is configured")
	}
	client, err := c.host.conn(true)
	if err != nil {
		return fmt.Errorf("session host: %w", err)
	}
	if !client.Welcome().Engine {
		return errors.New("the session host has no terminal engine")
	}
	id, err := newHostSessionID()
	if err != nil {
		return err
	}
	probe := "probe-" + strings.TrimPrefix(id, "mh-")
	ctx, cancel := context.WithTimeout(context.Background(), hostCallTimeout)
	defer cancel()
	if _, err := client.Create(ctx, sessionhost.CreateParams{
		ID: probe, Argv: []string{"/bin/sh", "-c", "sleep 5"}, Env: []string{"PATH=/usr/bin:/bin"}, Dir: "/", Cols: 80, Rows: 24,
	}); err != nil {
		return fmt.Errorf("session host probe: %w", err)
	}
	if _, err := client.Remove(ctx, probe); err != nil {
		return fmt.Errorf("session host probe: %w", err)
	}
	return nil
}

// MoveToHost ends the tmux Session id and starts move in the host under the
// same id, with the Session's labels and input receipts. Nothing else reaches
// the Session in between: its input waits, and no inventory sees it missing.
// If the program will not end in tmux, the Session stays there untouched.
func (w *Watcher) MoveToHost(id string, move TmuxMove) error {
	c := w.composite()
	if c == nil {
		return errors.New("no session host is configured")
	}
	if !c.tmux.validID(id) || c.host.validID(id) {
		return fmt.Errorf("%s is not a tmux Session", id)
	}
	return w.sessionInput.serialized(id, func() error {
		w.inventoryMu.Lock()
		defer w.inventoryMu.Unlock()
		panes, err := c.tmux.listOwned()
		if err != nil {
			return fmt.Errorf("tmux inventory: %w", err)
		}
		var pane *sessionPane
		for i := range panes {
			if panes[i].target == id {
				pane = &panes[i]
			}
		}
		if pane == nil || pane.dead {
			return fmt.Errorf("tmux no longer runs %s", id)
		}
		tmuxIO := c.tmux.inputIO()
		ledger, err := tmuxIO.receiptLedger(c.tmux.socket(), id)
		if err != nil {
			return fmt.Errorf("read the input receipts of %s: %w", id, err)
		}
		processes := []int{pane.panePID, move.ProcessID}
		if err := c.tmux.kill(id); err != nil {
			return err
		}
		if !waitProcessesGone(processes, tmuxMoveExitWait) {
			// The tmux pane is gone; a program that outlives it still holds
			// the conversation, and a second one must not start.
			return fmt.Errorf("%s did not end after its tmux pane closed; it was not started in the session host", id)
		}
		name := pane.name
		spec := backendCreateSpec{
			name:    name,
			cwd:     move.Cwd,
			command: move.Command,
			env:     move.Env,
			labels: sessionLabels{
				hidden:       pane.hidden,
				delegated:    pane.delegated,
				executorID:   pane.executorID,
				piSession:    pane.piSessionBinding,
				resourceUnit: pane.resourceUnit,
			},
		}
		if err := c.host.createAs(id, spec, map[string]string{hostLabelMovedFrom: "tmux"}); err != nil {
			return fmt.Errorf("%s ended in tmux but did not start in the session host: %w", id, err)
		}
		c.host.movedMu.Lock()
		c.host.moved[id] = true
		c.host.movedMu.Unlock()
		if len(ledger.Entries) > 0 {
			if err := c.host.inputIO().writeReceiptLedger(hostInputServer, id, ledger); err != nil {
				log.Printf("watcher: %s moved to the session host without its input receipts: %v", id, err)
			}
		}
		return nil
	})
}

func waitProcessesGone(pids []int, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		alive := false
		for _, pid := range pids {
			if pid > 0 && syscall.Kill(pid, 0) == nil {
				alive = true
				break
			}
		}
		if !alive {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// LeaveInTmux stops seeing the tmux Session id: it keeps running in tmux, no
// longer Mewla's. The choice is kept across restarts.
func (w *Watcher) LeaveInTmux(id, why string) {
	c := w.composite()
	if c == nil {
		return
	}
	c.leftMu.Lock()
	defer c.leftMu.Unlock()
	if _, ok := c.left[id]; ok {
		return
	}
	c.left[id] = why
	if err := c.saveLeftLocked(); err != nil {
		log.Printf("watcher: record that %s stays in tmux: %v", id, err)
	}
}

// LeftInTmux counts the Sessions left in tmux that still run there.
func (w *Watcher) LeftInTmux() int {
	c := w.composite()
	if c == nil {
		return 0
	}
	c.leftMu.Lock()
	left := len(c.left)
	c.leftMu.Unlock()
	if left == 0 {
		return 0
	}
	panes, err := c.tmux.listOwned()
	if err != nil {
		return 0
	}
	c.leftMu.Lock()
	defer c.leftMu.Unlock()
	count := 0
	for _, pane := range panes {
		if _, ok := c.left[pane.target]; ok && !pane.dead {
			count++
		}
	}
	return count
}

func withoutLeft[T any](c *compositeBackend, items []T, id func(T) string) []T {
	c.leftMu.Lock()
	defer c.leftMu.Unlock()
	if len(c.left) == 0 {
		return items
	}
	kept := items[:0:0]
	for _, item := range items {
		if _, left := c.left[id(item)]; !left {
			kept = append(kept, item)
		}
	}
	return kept
}

func (c *compositeBackend) leftPath() string {
	if c.host.cfg.StateDir == "" {
		return ""
	}
	return filepath.Join(c.host.cfg.StateDir, "session-host", "left-in-tmux.json")
}

func (c *compositeBackend) loadLeftInTmux() {
	c.leftMu.Lock()
	defer c.leftMu.Unlock()
	c.left = map[string]string{}
	path := c.leftPath()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(raw, &c.left); err != nil {
		log.Printf("watcher: %s: %v", path, err)
		c.left = map[string]string{}
	}
}

func (c *compositeBackend) saveLeftLocked() error {
	path := c.leftPath()
	if path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(c.left, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
