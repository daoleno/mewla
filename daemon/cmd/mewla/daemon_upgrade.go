package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/listenhandoff"
	"github.com/daoleno/mewla/daemon/platform"
	"github.com/daoleno/mewla/daemon/selfupdate"
)

// A daemon no supervisor restarts (one started in a terminal, by a script, or
// detached by hand) restarts itself onto an update when the CLI asks
// (control request daemon_upgrade). It checks that the executable it started
// from now runs as the requested version, answers, shuts down as it does on
// any stop (locks, ports and databases released; the session host and its
// Sessions and Workers untouched), and then runs that executable again with
// the arguments, environment and working directory it was started with.
//
// On Linux and macOS that is execve: the same PID in the same terminal, so
// its output keeps going to that window and Ctrl+C still stops it. Windows has
// no execve: the new version starts as a child in the same console, and this
// process waits for it and exits with its exit code (platform.ExecInPlace).

// startupSnapshot is how this process was started, taken before anything
// changes it: the daemon refreshes its environment from the login shell
// (platform.RefreshEnvironment), and Windows names a running executable an
// update moved aside by its new name.
type startupSnapshot struct {
	Executable string
	Args       []string
	Env        []string
	Dir        string
	Started    time.Time
}

var daemonStartup = takeStartupSnapshot()

func takeStartupSnapshot() startupSnapshot {
	snapshot := startupSnapshot{
		Args:    append([]string(nil), os.Args...),
		Env:     os.Environ(),
		Started: time.Now(),
	}
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		snapshot.Executable = executable
	}
	snapshot.Dir, _ = os.Getwd()
	return snapshot
}

// daemonReexec ends the daemon so main can run its executable again in this
// process (runInPlaceRestart).
type daemonReexec struct {
	startup  startupSnapshot
	version  string
	stateDir string
	// stops receives SIGINT and SIGTERM from the moment the restart is
	// accepted: a person pressing Ctrl+C while the daemon shuts down means
	// stop, not restart.
	stops chan os.Signal
}

func (r *daemonReexec) Error() string { return "restarting Mewla onto " + r.version }

// inPlaceRestarter answers daemon_upgrade.
type inPlaceRestarter struct {
	startup  startupSnapshot
	stateDir string
	// updating: the daemon is installing an update of its own.
	updating func() bool
	// probe runs the executable's `version --json`; tests replace it.
	probe   func(executable string) (string, error)
	restart func(*daemonReexec)

	mu       sync.Mutex
	accepted bool
}

func newInPlaceRestarter(stateDir string, updating func() bool, restart func(*daemonReexec)) *inPlaceRestarter {
	return &inPlaceRestarter{startup: daemonStartup, stateDir: stateDir, updating: updating, probe: probeExecutableVersion, restart: restart}
}

// probeExecutableVersion is the version executable reports.
func probeExecutableVersion(executable string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, executable, "version", "--json").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	var reported struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(output, &reported); err != nil || reported.Version == "" {
		return "", fmt.Errorf("it printed no version: %q", strings.TrimSpace(string(output)))
	}
	return reported.Version, nil
}

// handle answers daemon_upgrade. Every refusal names its cause; nothing has
// changed when it refuses. version "" accepts whatever is installed (mewla
// restart with nothing new).
func (r *inPlaceRestarter) handle(version string) control.Response {
	version = strings.TrimSpace(version)
	if os.Getenv(selfupdate.ChildEnv) != "" {
		return control.ErrorResponse("upgrade_on_trial", "this Mewla is an update still on trial; it restarts only when its trial ends")
	}
	if r.updating != nil && r.updating() {
		return control.ErrorResponse("upgrade_in_progress", "Mewla is installing an update of its own; try again when it has restarted")
	}
	executable := r.startup.Executable
	if executable == "" {
		return control.ErrorResponse("upgrade_no_executable", "Mewla cannot tell which file it was started from")
	}
	reported, err := r.probe(executable)
	if err != nil {
		return control.ErrorResponse("upgrade_not_runnable", fmt.Sprintf("%s does not run: %v", executable, err))
	}
	if version != "" && reported != version {
		return control.ErrorResponse("upgrade_wrong_version", fmt.Sprintf("%s is Mewla %s, not %s", executable, reported, version))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.accepted {
		return control.ErrorResponse("upgrade_in_progress", "Mewla is already restarting")
	}
	r.accepted = true
	stops := make(chan os.Signal, 1)
	signal.Notify(stops, syscall.SIGINT, syscall.SIGTERM)
	_ = os.Remove(inPlaceRestartFailurePath(r.stateDir))
	reexec := &daemonReexec{startup: r.startup, version: reported, stateDir: r.stateDir, stops: stops}
	return control.Response{OK: true, Text: reported, After: func() { r.restart(reexec) }}
}

// execInPlace runs the startup executable again in this process's place
// (platform.ExecInPlace).
func execInPlace(startup startupSnapshot, stops <-chan os.Signal) error {
	// The model relay's sockets go with it: a Worker's request made during the
	// restart waits in their queue for the new version (listenhandoff).
	return platform.ExecInPlace(startup.Executable, startup.Args, listenhandoff.ForExec(startup.Env), startup.Dir, stops)
}

// inPlaceRestartFailurePath holds why the last accepted restart did not
// start the new version, for the CLI waiting on it.
func inPlaceRestartFailurePath(stateDir string) string {
	return filepath.Join(stateDir, "run", "restart-failed.txt")
}

// errAppHostRestart ends a daemon under Mewla for Mac's host with a failure,
// so the host ends with it and launchd's KeepAlive starts both again.
var errAppHostRestart = errors.New("restarting Mewla: launchd starts it again from the installed app")

// restartsThroughAppHost: a daemon under the app's host (host, from
// MEWLA_STOP_WITH_HOST, still its parent) restarts by ending instead of
// running its executable again in place. launchd then starts the job from the
// installed app: a new host, from the app an update just put in place (an
// exec would leave the old app's host, whose permissions are not the new
// app's: app_host.go), and the session host and its Sessions run on. It also
// never meets the exec that could stall on macOS (platform/exec_darwin.go).
func restartsThroughAppHost(host, ppid int) bool {
	return host > 1 && ppid == host
}

// runInPlaceRestart runs after the daemon has shut down for an accepted
// daemon_upgrade. It returns only when the restart did not happen: stopped
// by a signal (nil), or the executable could not be started.
func runInPlaceRestart(r *daemonReexec) error {
	select {
	case received := <-r.stops:
		log.Printf("mewla: stopping on %v instead of restarting", received)
		return nil
	default:
	}
	if host := int(appHostPID.Load()); restartsThroughAppHost(host, os.Getppid()) {
		log.Printf("mewla: restarting onto %s: ending so launchd starts the installed app's host (pid %d now) and its daemon", r.version, host)
		return errAppHostRestart
	}
	log.Printf("mewla: restarting onto %s", r.version)
	err := execInPlace(r.startup, r.stops)
	if err == nil {
		return nil
	}
	cause := fmt.Sprintf("could not start %s: %v", r.startup.Executable, err)
	if writeErr := os.WriteFile(inPlaceRestartFailurePath(r.stateDir), []byte(cause+"\n"), 0o600); writeErr != nil {
		log.Printf("mewla: record restart failure: %v", writeErr)
	}
	return fmt.Errorf("restart Mewla onto %s: %s", r.version, cause)
}
