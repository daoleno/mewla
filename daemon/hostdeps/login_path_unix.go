//go:build !windows

package hostdeps

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// A daemon started by a service manager (a systemd user unit, a desktop
// entry, launchd) runs with that manager's PATH, not the one the person's
// shell builds from its startup files. An agent installed by npm with a user
// prefix, nvm, fnm, volta, bun, pnpm or mise is on the shell's PATH only, so
// Mewla said "claude is not installed" for Brain while a Worker spawned on it
// sat silently: its Session's /bin/sh could not find it either. The daemon
// now adds the directories the person's login shell puts on PATH, and the
// usual per-user Node install directories, after its own.

const (
	loginPathTTL     = 10 * time.Minute
	loginPathTimeout = 5 * time.Second
	loginPathMarker  = "__MEWLA_LOGIN_PATH__"
)

var (
	loginPathMu      sync.Mutex
	loginPathAt      time.Time
	loginPathDirs    []string
	loginPathRunning bool
	// loginShellProbe reads the PATH a login shell sets up; tests replace it
	// (loginProbeSet), and under go test only a replaced one runs.
	loginShellProbe = probeLoginShellPath
	loginProbeSet   bool
)

// SetLoginShellProbe replaces how the login shell's PATH is read and returns
// what puts it back. A nil probe reads nothing.
func SetLoginShellProbe(probe func(shell, home string) ([]string, error)) func() {
	loginPathMu.Lock()
	defer loginPathMu.Unlock()
	previous := loginShellProbe
	if probe == nil {
		probe = func(string, string) ([]string, error) { return nil, nil }
	}
	previousSet := loginProbeSet
	loginShellProbe, loginProbeSet = probe, true
	loginPathAt, loginPathDirs = time.Time{}, nil
	return func() {
		loginPathMu.Lock()
		defer loginPathMu.Unlock()
		loginShellProbe, loginProbeSet = previous, previousSet
		loginPathAt, loginPathDirs = time.Time{}, nil
	}
}

// LoginShellPathDirs is the PATH the person's login shell sets up, read at
// most every ten minutes: the first call waits for it (bounded), later
// refreshes happen in the background. Under go test nothing is read unless a
// test installed a probe.
func LoginShellPathDirs() []string {
	loginPathMu.Lock()
	probe := loginShellProbe
	if testing.Testing() && !loginProbeSet {
		loginPathMu.Unlock()
		return nil
	}
	fresh := !loginPathAt.IsZero() && time.Since(loginPathAt) < loginPathTTL
	first := loginPathAt.IsZero()
	dirs := append([]string(nil), loginPathDirs...)
	if fresh || loginPathRunning {
		loginPathMu.Unlock()
		return dirs
	}
	loginPathRunning = true
	loginPathMu.Unlock()
	read := func() []string {
		home, _ := os.UserHomeDir()
		found, _ := probe(userShell(), home)
		loginPathMu.Lock()
		defer loginPathMu.Unlock()
		loginPathRunning = false
		loginPathAt = time.Now()
		if found != nil {
			loginPathDirs = found
		}
		return append([]string(nil), loginPathDirs...)
	}
	if first {
		return read()
	}
	go read()
	return dirs
}

// userShell is the person's login shell: $SHELL, else their /etc/passwd entry.
func userShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	raw, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	uid := strconv.Itoa(os.Getuid())
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) >= 7 && fields[2] == uid {
			return fields[6]
		}
	}
	return ""
}

// probeLoginShellPath runs the shell as an interactive login shell (that is
// where nvm and friends add themselves) with no terminal and no input, and
// reads the PATH it ends up with. A shell that hangs or fails reads nothing.
func probeLoginShellPath(shell, home string) ([]string, error) {
	shell = strings.TrimSpace(shell)
	if shell == "" || !filepath.IsAbs(shell) {
		return nil, nil
	}
	base := filepath.Base(shell)
	script := `printf '\n` + loginPathMarker + `%s\n' "$PATH"`
	args := []string{"-l", "-i", "-c", script}
	switch base {
	case "fish":
		script = `printf '\n` + loginPathMarker + `%s\n' (string join : $PATH)`
		args = []string{"-l", "-i", "-c", script}
	case "sh", "dash", "ash", "busybox":
		// Not interactive: sh -i -c still reads only $ENV.
		args = []string{"-l", "-c", script}
	case "nu", "elvish", "xonsh", "pwsh":
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginPathTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = home
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "TERM=dumb", "MEWLA_LOGIN_PATH_PROBE=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	var dirs []string
	for _, line := range strings.Split(string(out), "\n") {
		if index := strings.Index(line, loginPathMarker); index >= 0 {
			dirs = nil
			for _, dir := range filepath.SplitList(strings.TrimSpace(line[index+len(loginPathMarker):])) {
				if filepath.IsAbs(dir) {
					dirs = append(dirs, filepath.Clean(dir))
				}
			}
		}
	}
	if dirs == nil {
		return nil, err
	}
	return dirs, nil
}

// nodeInstallDirs are the per-user directories Node version managers and
// package managers install agent CLIs into, newest Node version first, for a
// computer whose shell startup files Mewla cannot read.
func nodeInstallDirs(home string) []string {
	if home == "" {
		return nil
	}
	dirs := []string{
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".npm-packages", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".volta", "bin"),
		filepath.Join(home, ".local", "share", "pnpm"),
		filepath.Join(home, ".yarn", "bin"),
		filepath.Join(home, ".local", "share", "mise", "shims"),
		filepath.Join(home, ".asdf", "shims"),
	}
	for _, pattern := range []string{
		filepath.Join(home, ".nvm", "versions", "node", "*", "bin"),
		filepath.Join(home, ".local", "share", "fnm", "node-versions", "*", "installation", "bin"),
		filepath.Join(home, ".fnm", "node-versions", "*", "installation", "bin"),
		filepath.Join(home, ".local", "share", "mise", "installs", "node", "*", "bin"),
	} {
		matches, _ := filepath.Glob(pattern)
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		dirs = append(dirs, matches...)
	}
	return dirs
}
