package watcher

import (
	"strings"

	"github.com/daoleno/mewla/daemon/tmuxguard"
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

// SetTmuxGuard installs the directory of the pane `tmux` shim. Call it once
// before Run or CreateSession.
func (w *Watcher) SetTmuxGuard(dir string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.tmuxGuardDir = strings.TrimSpace(dir)
	w.mu.Unlock()
}

// SocketPathFor returns the selected host server only for a currently known
// Mewla-owned target. Unknown targets resolve to "" so user-visible surfaces do
// not acquire access to a custom caller server without watcher ownership.
func (w *Watcher) SocketPathFor(target string) string {
	if w == nil {
		return ""
	}
	target = strings.TrimSpace(target)
	w.mu.RLock()
	defer w.mu.RUnlock()
	if _, owned := w.workers[target]; !owned {
		return ""
	}
	return w.tmuxSocketPath
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

// applyProviderTmuxIsolation points provider-internal plain tmux at per-agent
// resource scratch (or daemon scratch for the hidden Brain host). The launch
// shell derives its Mewla identity against the shared host server and then
// unsets TMUX, so later plain tmux commands cannot reach that server.
func applyProviderTmuxIsolation(opts *CreateSessionOptions, w *Watcher) {
	if opts == nil || w == nil || !opts.ProgressEnv {
		return
	}
	opts.Env = cloneEnvironment(opts.Env)
	scratch := strings.TrimSpace(w.tmuxScratchDir)
	if opts.resource != nil && strings.TrimSpace(opts.resource.TempDir) != "" {
		scratch = strings.TrimSpace(opts.resource.TempDir)
	}
	if scratch == "" {
		return
	}
	opts.Env["TMUX_TMPDIR"] = scratch
	// The pane's `tmux` shim (first on PATH, see tmuxguard) refuses a tmux that
	// would fall back to the live default server and judges "own scratch"
	// against this directory.
	if w.tmuxGuardDir != "" {
		opts.Env[tmuxguard.ScratchEnv] = scratch
		opts.Env[tmuxguard.DirEnv] = w.tmuxGuardDir
	}
}
