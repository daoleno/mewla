package watcher

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/daoleno/mewla/daemon/tmuxsocket"
)

// tmux server layout:
//
// Mewla-owned Brain and delegated Sessions live on the ONE caller-visible tmux
// server selected at daemon startup: the exact inherited socket when the
// daemon starts inside tmux, or the user's default server otherwise. Provider
// panes lose that host capability after deriving their target identity: TMUX
// is unset and TMUX_TMPDIR points at private scratch, so their later unscoped
// tmux commands cannot reach the shared host server.

// tmuxCommand builds a tmux invocation bound to the given server socket.
// Empty socketPath targets the user's default server.
func tmuxCommand(socketPath string, args ...string) *exec.Cmd {
	cmd := exec.Command("tmux", append(tmuxsocket.ClientArgs(socketPath), args...)...)
	cmd.Env = tmuxHostEnvironment()
	return cmd
}

// A new default server inherits its first client's environment globally. Worker
// launch context must enter only through respawn-pane -e, never server startup.
func tmuxHostEnvironment() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "MEWLA_WORKER_") || strings.HasPrefix(key, "MEWLA_BRAIN_") || key == "MEWLA_BUILD_TMPDIR" || key == "MEWLA_WORKTREE_ROOT" || key == "MEWLA_STATE_DIR" {
			continue
		}
		env = append(env, entry)
	}
	return withUTF8CType(env, runtime.GOOS)
}

// withUTF8CType gives a locale-less environment (launchd LaunchAgents and
// some service managers) a UTF-8 LC_CTYPE, so a server this client starts
// hands panes a UTF-8 character type instead of C. It follows tmux's own
// LC_ALL, LC_CTYPE, LANG precedence and never overrides an explicit value.
func withUTF8CType(env []string, goos string) []string {
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		for _, entry := range env {
			name, value, _ := strings.Cut(entry, "=")
			if name == key && value != "" {
				return env
			}
		}
	}
	ctype := "C.UTF-8"
	if goos == "darwin" {
		// macOS has no C.UTF-8; UTF-8 is its stock LC_CTYPE locale.
		ctype = "UTF-8"
	}
	return append(env, "LC_CTYPE="+ctype)
}
