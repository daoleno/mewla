package watcher

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrTmuxServerGone marks a failed tmux invocation whose server socket has no
// listener behind it. It is decided by dialing the socket, never by the words
// tmux prints, which differ across tmux versions and locales.
var ErrTmuxServerGone = errors.New("tmux server is not running")

// tmuxFailure wraps a failed tmux invocation. Anything that is not a tmux exit
// status (tmux missing, not executable) is returned as is. A non-zero exit
// additionally wraps ErrTmuxServerGone when the socket is unreachable.
// Permission and other dial failures stay fail-closed: not "gone".
func tmuxFailure(socket, what string, err error, output []byte) error {
	failure := fmt.Errorf("tmux %s: %w: %s", what, err, strings.TrimSpace(string(output)))
	if !isExitStatus(err) {
		return failure
	}
	if tmuxSocketUnreachable(socket) {
		return fmt.Errorf("%w: %w", ErrTmuxServerGone, failure)
	}
	return failure
}

// isExitStatus reports whether err is tmux exiting non-zero, as opposed to tmux
// not being installed or not startable.
func isExitStatus(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func isNoTmuxServerError(err error) bool { return errors.Is(err, ErrTmuxServerGone) }

// tmuxSocketUnreachable reports whether nothing listens on the socket tmux
// would use: ENOENT (no socket file) or ECONNREFUSED (stale socket file).
func tmuxSocketUnreachable(socket string) bool {
	path := strings.TrimSpace(socket)
	if path == "" {
		path = defaultTmuxSocketPath()
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err == nil {
		_ = conn.Close()
		return false
	}
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

// defaultTmuxSocketPath is where tmux puts its default server socket for this
// user: $TMUX_TMPDIR (else /tmp)/tmux-<uid>/default, from the same environment
// the tmux client is started with.
func defaultTmuxSocketPath() string {
	dir := "/tmp"
	for _, entry := range tmuxHostEnvironment() {
		if value, ok := strings.CutPrefix(entry, "TMUX_TMPDIR="); ok && value != "" {
			dir = value
		}
	}
	return filepath.Join(dir, "tmux-"+strconv.Itoa(os.Getuid()), "default")
}

// tmuxTargetGone is true only when a fresh pane listing proves the pane is
// absent, or the server is gone: the follow-up to a failed per-pane command
// that decides whether the failure meant "already gone" or a real error.
func tmuxTargetGone(socket, target string) bool {
	present, err := tmuxTargetPresentOn(socket, target, false)
	return err == nil && !present
}
