package watcher

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// SetTmuxServer installs the one caller-visible tmux server that hosts every
// Mewla-owned Brain and delegated Session, plus the private TMUX_TMPDIR used by
// provider-internal plain tmux commands. Call it once before Run or CreateSession.
func (w *Watcher) SetTmuxServer(socketPath, scratchDir string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.tmuxSocketPath = socketPath
	w.tmuxScratchDir = strings.TrimSpace(scratchDir)
	w.mu.Unlock()
}

// socketPathFor resolves every internal tmux operation to the one immutable
// host server selected at startup. Ownership is enforced separately through
// the durable @mewla_worker_created marker and the owned discovery projection.
func (w *Watcher) socketPathFor(target string) string {
	if w == nil {
		return ""
	}
	w.mu.RLock()
	socket := w.tmuxSocketPath
	w.mu.RUnlock()
	return socket
}

// applyProviderTmuxIsolation points provider-internal plain tmux at the
// daemon's private TMUX_TMPDIR. The launch shell derives its Mewla identity
// against the shared host server and then unsets TMUX, so later plain tmux
// commands cannot reach that server. It is never a Worker's own scratch: that
// goes when the Worker ends, and tmux whose TMUX_TMPDIR is gone falls back to
// the user's default server.
func applyProviderTmuxIsolation(opts *CreateSessionOptions, w *Watcher) {
	if opts == nil || w == nil || !opts.ProgressEnv {
		return
	}
	opts.Env = cloneEnvironment(opts.Env)
	scratch := strings.TrimSpace(w.tmuxScratchDir)
	if scratch == "" {
		return
	}
	opts.Env["TMUX_TMPDIR"] = scratch
}

// privateTmuxDir is where a plain `tmux` run inside a Session keeps its
// server: the daemon's own TMUX_TMPDIR, never the user's default socket. tmux
// whose TMUX_TMPDIR does not exist falls back to /tmp, which is the user's
// default server, so the daemon creates the directory and keeps it while it
// runs. "" means none is configured.
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
		return "", fmt.Errorf("private tmux directory: %w", err)
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
