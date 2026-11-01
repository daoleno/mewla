package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/selfupdate"
	"github.com/daoleno/mewla/daemon/server"
)

// releaseBuild is "1" only in binaries built by the release script
// (-X main.releaseBuild=1). Every other build, including `go run`, the dev
// runner and a local `go build`, never updates itself and ignores update state
// it finds in a shared state directory.
var releaseBuild = ""

const (
	autoUpdateInterval = 6 * time.Hour
	autoUpdateFirst    = 2 * time.Minute
	autoUpdateIdleWait = 5 * time.Minute
	updateGate         = 60 * time.Second
)

// Linux only. On macOS the daemon runs from inside the signed Mewla.app;
// staging next to it and swapping the binary would break the bundle's
// signature and with it the Screen Recording and Accessibility grants. macOS
// keeps `mewla update` until MAC-VERIFY.md has shown a safe way.
func selfUpdateSupported(version string) bool {
	return releaseBuild == "1" &&
		os.Getenv(selfupdate.ChildEnv) == "" &&
		os.Getenv("MEWLA_NO_AUTOUPDATE") == "" &&
		!strings.Contains(version, "-") &&
		runtime.GOOS == "linux"
}

// updateHandover ends the daemon so the same process can run the staged
// binary as a trial child (see selfupdate.Handover).
type updateHandover struct {
	Installed string
	Staged    string
	Version   string
	Addr      string
	DaemonID  string
	StateDir  string
}

func (h *updateHandover) Error() string { return "updating Mewla to " + h.Version }

// daemonUpdater checks for a newer stable release and installs it when asked
// or when the computer is idle.
type daemonUpdater struct {
	version   string
	supported bool
	stateDir  string
	addr      string
	daemonID  string
	installed string

	newClient func() (*selfupdate.Client, error)
	idle      func() bool
	handover  func(*updateHandover)
	interval  time.Duration
	first     time.Duration
	idleWait  time.Duration

	mu       sync.Mutex
	latest   string
	checked  time.Time
	updating bool
	checkErr error
}

func newDaemonUpdater(version, stateDir, addr, daemonID string, idle func() bool, handover func(*updateHandover)) *daemonUpdater {
	installed, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(installed); err == nil {
		installed = resolved
	}
	return &daemonUpdater{
		version: version, supported: selfUpdateSupported(version), stateDir: stateDir, addr: addr, daemonID: daemonID,
		installed: installed, newClient: selfupdate.NewClient, idle: idle, handover: handover,
		interval: autoUpdateInterval, first: autoUpdateFirst, idleWait: autoUpdateIdleWait,
	}
}

func (u *daemonUpdater) Status() server.DaemonUpdateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	status := server.DaemonUpdateStatus{Version: u.version, Supported: u.supported, Latest: u.latest, Updating: u.updating}
	if failure, ok := selfupdate.LastFailure(u.stateDir); ok && u.supported && failure.Version != u.version {
		status.Error = fmt.Sprintf("Mewla %s did not start; still on %s", failure.Version, u.version)
	}
	return status
}

// Run checks every few hours and installs a newer release once nothing is
// working. It returns when ctx ends.
func (u *daemonUpdater) Run(ctx context.Context) {
	if !u.supported {
		return
	}
	wait := u.first
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		u.mu.Lock()
		due := time.Since(u.checked) >= u.interval
		u.mu.Unlock()
		if due {
			if err := u.check(ctx); err != nil {
				log.Printf("update check: %v", err)
			}
		}
		u.mu.Lock()
		pending := u.latest != ""
		u.mu.Unlock()
		switch {
		case !pending:
			wait = u.interval
		case u.idle():
			if err := u.install(ctx); err != nil {
				log.Printf("update: %v", err)
				wait = u.interval
			} else {
				return
			}
		default:
			wait = u.idleWait
		}
	}
}

func (u *daemonUpdater) check(ctx context.Context) error {
	_, err := u.latestRelease(ctx)
	return err
}

// latestRelease asks the release server and remembers the answer. A version
// that was rolled back is not offered again.
func (u *daemonUpdater) latestRelease(ctx context.Context) (*selfupdate.Candidate, error) {
	client, err := u.newClient()
	if err != nil {
		return nil, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	candidate, err := client.Latest(checkCtx, u.version)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.checked, u.checkErr = time.Now(), err
	if err != nil {
		return nil, err
	}
	if failure, ok := selfupdate.LastFailure(u.stateDir); ok && candidate != nil && failure.Version == candidate.Version {
		candidate = nil
	}
	u.latest = ""
	if candidate != nil {
		u.latest = candidate.Version
	}
	return candidate, nil
}

// Install is the App's Update button.
func (u *daemonUpdater) Install() error {
	if !u.supported {
		return server.ErrUpdateUnavailable
	}
	u.mu.Lock()
	if u.updating {
		u.mu.Unlock()
		return nil
	}
	u.updating = true
	u.mu.Unlock()
	go func() {
		if err := u.install(context.Background()); err != nil {
			log.Printf("update: %v", err)
		}
	}()
	return nil
}

// install downloads, verifies and stages the newest release, then ends the
// daemon so main can hand over to it. On any error nothing has changed.
func (u *daemonUpdater) install(ctx context.Context) (err error) {
	u.mu.Lock()
	u.updating = true
	u.mu.Unlock()
	defer func() {
		if err != nil {
			u.mu.Lock()
			u.updating = false
			u.mu.Unlock()
		}
	}()
	candidate, err := u.latestRelease(ctx)
	if err != nil {
		return fmt.Errorf("check for update: %w", err)
	}
	if candidate == nil {
		return server.ErrUpdateUnavailable
	}
	client, err := u.newClient()
	if err != nil {
		return err
	}
	downloadCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	binary, err := client.DownloadBinary(downloadCtx, *candidate)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	staged, err := selfupdate.Stage(u.installed, binary, candidate.Version)
	if err != nil {
		return fmt.Errorf("stage update: %w", err)
	}
	u.handover(&updateHandover{
		Installed: u.installed, Staged: staged, Version: candidate.Version,
		Addr: u.addr, DaemonID: u.daemonID, StateDir: u.stateDir,
	})
	return nil
}

// workersIdle: the Brain host and every Worker have stopped working.
func workersIdle(ready func() bool, workers func() []*classifier.Worker) bool {
	if !ready() {
		return false
	}
	for _, worker := range workers() {
		if worker.State == classifier.StateRunning {
			return false
		}
	}
	return true
}

// probeUpdatedDaemon is the health gate: the daemon at addr is the same
// installation (daemon_id) and runs the new version.
func probeUpdatedDaemon(addr, daemonID, version string) error {
	response, err := bootHTTPClient.Get("http://" + bootProbeAddr(addr) + "/health")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health returned http %d", response.StatusCode)
	}
	var payload struct {
		DaemonID string `json:"daemon_id"`
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&payload); err != nil {
		return err
	}
	if payload.DaemonID != daemonID || payload.Version != version {
		return fmt.Errorf("health answers daemon %q version %q", payload.DaemonID, payload.Version)
	}
	return nil
}

// runDaemonUpdating runs the daemon and, when it ended to update, hands over.
func runDaemonUpdating(args []string, stderr io.Writer) error {
	err := runDaemon(args, stderr)
	var handover *updateHandover
	if errors.As(err, &handover) {
		return runUpdateHandover(handover, os.Args[1:])
	}
	return err
}

// runUpdateHandover runs after the daemon has shut down. It never returns
// into a daemon in this process: it execs the installed binary, which is the
// new version when the handover succeeded and the old one when it did not.
func runUpdateHandover(h *updateHandover, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	brainRoot, _ := brain.DefaultRoot()
	outcome := selfupdate.Handover{
		Installed: h.Installed, Staged: h.Staged, Version: h.Version,
		Args: args, Env: os.Environ(), StateDir: h.StateDir, BrainRoot: brainRoot,
		Gate:   updateGate,
		Stdout: os.Stdout, Stderr: os.Stderr,
		Healthy: func() error { return probeUpdatedDaemon(h.Addr, h.DaemonID, h.Version) },
	}.Run(ctx)
	if outcome.Adopted {
		log.Printf("mewla: updated to %s", h.Version)
	} else {
		log.Printf("mewla: update to %s did not take, staying on the current version: %s", h.Version, outcome.Reason)
		if outcome.Bad {
			if err := selfupdate.RecordFailure(h.StateDir, selfupdate.Failure{Version: h.Version, Reason: outcome.Reason, At: time.Now()}); err != nil {
				log.Printf("mewla: remember failed update: %v", err)
			}
		}
		if ctx.Err() != nil {
			return nil // the service is being stopped
		}
	}
	return syscall.Exec(h.Installed, append([]string{os.Args[0]}, args...), os.Environ())
}
