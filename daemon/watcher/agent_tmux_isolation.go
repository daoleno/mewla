package watcher

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// The person may run their own tmux, and the daemon itself may run inside it.
// An agent that runs a plain `tmux` (`tmux kill-server` too) must never reach
// that server, so every agent Session gets the daemon's private TMUX_TMPDIR and
// no TMUX of the daemon's (workerLaunchEnvironment). Mewla itself never runs
// tmux.

// SetPrivateTmuxDir names the private TMUX_TMPDIR of agent Sessions. Call it
// once before Run or CreateSession.
func (w *Watcher) SetPrivateTmuxDir(dir string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.tmuxScratchDir = strings.TrimSpace(dir)
	w.mu.Unlock()
}

// privateTmuxDir is where a plain `tmux` run inside an agent Session keeps its
// server: the daemon's own TMUX_TMPDIR, never the person's default socket.
// tmux whose TMUX_TMPDIR does not exist falls back to /tmp, which is the
// person's default server, so the daemon creates the directory and keeps it
// while it runs. "" means none is configured.
func (w *Watcher) privateTmuxDir() (string, error) {
	if w == nil {
		return "", nil
	}
	w.mu.RLock()
	dir := w.tmuxScratchDir
	w.mu.RUnlock()
	if dir == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("private TMUX_TMPDIR: %w", err)
	}
	return dir, nil
}

// keepPrivateTmuxDir puts the private TMUX_TMPDIR back if something removed
// it while Sessions run.
func (w *Watcher) keepPrivateTmuxDir() {
	if _, err := w.privateTmuxDir(); err != nil && !w.privateTmuxDirFailed.Swap(true) {
		log.Printf("watcher: %v", err)
	} else if err == nil {
		w.privateTmuxDirFailed.Store(false)
	}
}
