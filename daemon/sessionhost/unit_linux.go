//go:build linux

package sessionhost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The host runs in a systemd unit of its own, not in the cgroup of whatever
// started the daemon (a terminal's scope, a tmux pane's scope, the daemon's
// service). When the kernel kills one process of a cgroup for lack of memory,
// systemd's default OOMPolicy=stop then stops the whole unit and ends every
// process in it: the host and every Session with it, on 2026-11-10 because
// one Chrome helper of one agent was killed. With tmux each pane was a unit
// of its own, so one pane's memory trouble ended one pane.
//
// Here the policy is "continue": the kernel's victim dies and nothing else
// does. Nothing in the unit ends the host except the host.

var userUnits struct {
	once sync.Once
	ok   bool
}

// userManagerEnv is base with the XDG_RUNTIME_DIR and DBUS_SESSION_BUS_ADDRESS
// to reach the person's systemd user manager with, whatever the daemon itself
// was given.
func userManagerEnv(base []string) ([]string, bool) {
	runtime := fmt.Sprintf("/run/user/%d", os.Getuid())
	if _, err := os.Stat(filepath.Join(runtime, "bus")); err != nil {
		if _, err := os.Stat(filepath.Join(runtime, "systemd", "private")); err != nil {
			return nil, false
		}
	}
	env := make([]string, 0, len(base)+1)
	for _, entry := range base {
		if strings.HasPrefix(entry, "XDG_RUNTIME_DIR=") || strings.HasPrefix(entry, "DBUS_SESSION_BUS_ADDRESS=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "XDG_RUNTIME_DIR="+runtime), true
}

// systemdUserUnits reports whether a transient service with OOMPolicy=continue
// can be started in the person's user manager: asked once, by starting one
// that does nothing.
func systemdUserUnits() bool {
	userUnits.once.Do(func() {
		run, err := exec.LookPath("systemd-run")
		if err != nil {
			return
		}
		env, ok := userManagerEnv(os.Environ())
		if !ok {
			return
		}
		noop, err := exec.LookPath("true")
		if err != nil {
			noop = "/bin/true"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, run, "--user", "--collect", "--wait", "--quiet", "-p", "Type=exec", "-p", "OOMPolicy=continue", "--", noop)
		cmd.Env = env
		userUnits.ok = cmd.Run() == nil
	})
	return userUnits.ok
}

// hostUnitCommand is the command that starts the host as a transient service
// of the person's user manager, or false when there is no such manager. The
// command stays until the host ends (systemd-run --wait), so a host that
// fails at start is reported as it is when started directly.
func hostUnitCommand(opts StartOptions, args []string, log *os.File) (*exec.Cmd, bool) {
	if !systemdUserUnits() {
		return nil, false
	}
	run, err := exec.LookPath("systemd-run")
	if err != nil {
		return nil, false
	}
	digest := sha256.Sum256([]byte(opts.SocketPath))
	unit := fmt.Sprintf("mewla-session-host-%s-%04x", hex.EncodeToString(digest[:4]), time.Now().UnixNano()&0xffff)
	flags := []string{
		"--user", "--collect", "--wait", "--quiet",
		"--unit=" + unit,
		"--description=Mewla session host",
		"--working-directory=/",
		"-p", "Type=exec",
		"-p", "OOMPolicy=continue",
	}
	if opts.LogPath != "" {
		flags = append(flags, "-p", "StandardOutput=append:"+opts.LogPath, "-p", "StandardError=append:"+opts.LogPath)
	}
	// The host's environment holds the daemon's secrets (API keys), and a
	// process's command line is readable by every user: it travels in the
	// environment of systemd-run, which only its owner can read, and the
	// command line names the variables (--setenv=NAME takes the value from
	// there). Only the two paths that reach the user manager, which are not
	// secrets, go by value.
	hostEnv := opts.Env
	if hostEnv == nil {
		hostEnv = os.Environ()
	}
	for _, entry := range hostEnv {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !validEnvName(name) {
			continue
		}
		if name == "XDG_RUNTIME_DIR" || name == "DBUS_SESSION_BUS_ADDRESS" {
			flags = append(flags, "--setenv="+entry)
		} else {
			flags = append(flags, "--setenv="+name)
		}
	}
	env, ok := userManagerEnv(hostEnv)
	if !ok {
		return nil, false
	}
	flags = append(flags, "--", opts.Executable)
	cmd := exec.Command(run, append(flags, args...)...)
	cmd.Env = env
	cmd.Dir = "/"
	if log != nil {
		cmd.Stdout, cmd.Stderr = log, log
	}
	return cmd, true
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}
