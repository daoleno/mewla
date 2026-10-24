package tmuxguard

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/daoleno/mewla/daemon/shellquote"
)

// marker is on line 2 of every shim script. RealTmux and the guard's own
// lookup skip a PATH entry that carries it, so neither can exec a guard; any
// other `tmux` on PATH (a test firewall, a wrapper) is used as found.
const marker = "# mewla-tmux-guard"

// Install writes the shim into <stateDir>/tmux-guard/bin and returns that
// directory. exe is the mewla binary that implements `tmux-guard`. The script
// is rewritten atomically on every call so a moved binary is followed.
func Install(stateDir, exe string) (string, error) {
	if strings.TrimSpace(stateDir) == "" || strings.TrimSpace(exe) == "" {
		return "", errors.New("tmux guard needs a state directory and the mewla executable")
	}
	dir := filepath.Join(stateDir, "tmux-guard", "bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	script := shimScript(exe)
	path := filepath.Join(dir, "tmux")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == script {
		return dir, nil
	}
	tmp, err := os.CreateTemp(dir, ".tmux-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.WriteString(tmp, script); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return dir, nil
}

func shimScript(exe string) string {
	quoted := shellquote.Quote(exe)
	return `#!/bin/sh
` + marker + `: written by the Mewla daemon at every start; do not edit.
# Refuses a tmux that could reach the live default server. Never falls through
# to an unguarded tmux, even when the mewla binary is being rebuilt.
if [ ! -x ` + quoted + ` ]; then
  echo "tmux: the Mewla tmux guard is unavailable (` + strings.ReplaceAll(exe, `"`, ``) + ` is missing); refusing to run an unguarded tmux. Retry in a moment." >&2
  exit 1
fi
exec ` + quoted + ` tmux-guard "$@"
`
}

// isGuard reports whether path is a Mewla guard shim.
func isGuard(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.SplitN(string(data), "\n", 3)
	return len(lines) >= 2 && strings.HasPrefix(lines[1], marker)
}

var (
	realMu     sync.Mutex
	realPath   string
	realByPath string
)

// RealTmux resolves the tmux binary the way exec.LookPath does but skipping any
// Mewla guard shim. The daemon calls it for every tmux client it spawns, so it
// is never affected by the guard even when it runs inside a guarded pane (an
// isolated dev daemon started by a Worker). It returns "tmux" when nothing is
// found, so the caller's exec error stays the ordinary one.
func RealTmux() string {
	pathEnv := os.Getenv("PATH")
	realMu.Lock()
	defer realMu.Unlock()
	if realPath != "" && realByPath == pathEnv {
		if _, err := os.Stat(realPath); err == nil {
			return realPath
		}
	}
	found := lookRealTmux(pathEnv)
	if found == "" {
		return "tmux"
	}
	realPath, realByPath = found, pathEnv
	return found
}

func lookRealTmux(pathEnv string) string {
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, "tmux")
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		if isGuard(candidate) {
			continue
		}
		if abs, err := filepath.Abs(candidate); err == nil {
			return abs
		}
		return candidate
	}
	return ""
}

// Pin points a command built for "tmux" at RealTmux, keeping Args[0] as built.
// Every tmux client the daemon spawns goes through it, so a guard shim earlier
// on PATH (inherited from a guarded pane) never sits between the daemon and its
// explicit -S socket.
func Pin(cmd *exec.Cmd) *exec.Cmd {
	real := RealTmux()
	if real == "tmux" {
		return cmd
	}
	cmd.Path = real
	if errors.Is(cmd.Err, exec.ErrNotFound) {
		cmd.Err = nil
	}
	return cmd
}

// Main is the `mewla tmux-guard` entry point: decide, then exec the real tmux
// with the caller's arguments (exec, so ttys and attach behave as before).
// It returns only when it refuses or cannot exec; the value is the exit code.
func Main(args []string, stderr io.Writer) int {
	verdict := Decide(args, SystemEnv())
	if !verdict.Allow {
		fmt.Fprintf(stderr, "tmux: refused by the Mewla tmux guard: %s.\nRule: in a Mewla pane use tmux only with an explicit -S <socket under $TMPDIR> that you created; never touch /tmp/tmux-*/default.\n", verdict.Reason)
		return 1
	}
	real := lookRealTmux(os.Getenv("PATH"))
	if real == "" {
		fmt.Fprintln(stderr, "tmux: the Mewla tmux guard found no real tmux on PATH")
		return 127
	}
	err := syscall.Exec(real, append([]string{"tmux"}, args...), os.Environ())
	fmt.Fprintf(stderr, "tmux: exec %s: %v\n", real, err)
	return 126
}

// WithoutGuard returns pathEnv minus every directory holding a guard shim.
// Tests that create and kill their own isolated tmux servers use it, so a
// Worker running them from a guarded pane exercises the real tmux; production
// code never needs it (see Pin).
func WithoutGuard(pathEnv string) string {
	var kept []string
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir != "" && isGuard(filepath.Join(dir, "tmux")) {
			continue
		}
		kept = append(kept, dir)
	}
	return strings.Join(kept, string(os.PathListSeparator))
}
