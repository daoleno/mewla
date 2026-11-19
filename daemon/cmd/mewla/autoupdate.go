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
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/listenhandoff"
	"github.com/daoleno/mewla/daemon/platform"
	"github.com/daoleno/mewla/daemon/selfupdate"
	"github.com/daoleno/mewla/daemon/server"
)

// releaseBuild is "1" only in binaries built by the release script
// (-X main.releaseBuild=1). Every other build, including `go run`, the dev
// runner and a local `go build`, never updates itself and ignores update state
// it finds in a shared state directory.
var releaseBuild = ""

// An update does not wait for Workers: Sessions live in the session host and
// keep running while the daemon restarts. It waits only for a quiet point,
// a moment when a restart cuts nothing the daemon itself carries (a held
// permission decision, a model reply passing through Mewla, a message being
// typed into Brain), and installs at the cap even without one.
const (
	autoUpdateInterval = time.Hour
	autoUpdateFirst    = 2 * time.Minute
	// autoUpdateQuietPoll is how often a staged update looks for a quiet point.
	autoUpdateQuietPoll = 15 * time.Second
	// autoUpdateCap bounds the wait for a quiet point after staging.
	autoUpdateCap = time.Hour
	updateGate    = 60 * time.Second
	// A failure that is not the new version's fault (a full disk) is not
	// tried again automatically for a day.
	transientRetryAfter = 24 * time.Hour
)

// Linux and Windows. On macOS the daemon runs from inside the signed Mewla.app;
// staging next to it and swapping the binary would break the bundle's
// signature and with it the Screen Recording and Accessibility grants. macOS
// keeps `mewla update` until MAC-VERIFY.md has shown a safe way.
func selfUpdateSupported(version string) bool {
	return releaseBuild == "1" &&
		os.Getenv(selfupdate.ChildEnv) == "" &&
		os.Getenv("MEWLA_NO_AUTOUPDATE") == "" &&
		!strings.Contains(version, "-") &&
		(runtime.GOOS == "linux" || runtime.GOOS == "windows")
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

// daemonUpdater checks for a newer stable release, stages it, and installs it
// at the first quiet point, at the cap, or at once when the person asks.
type daemonUpdater struct {
	version   string
	supported bool
	stateDir  string
	addr      string
	daemonID  string
	installed string

	newClient func() (*selfupdate.Client, error)
	// host is the session host's version and whether it waits for a restart
	// (hostUpgrader); nil in tests.
	host interface {
		Status() hostStatus
		RestartNow() error
	}
	// quiet names what a restart now would cut ("a permission answer"), ""
	// when nothing would be.
	quiet     func() string
	handover  func(*updateHandover)
	now       func() time.Time
	interval  time.Duration
	first     time.Duration
	quietPoll time.Duration
	cap       time.Duration

	mu         sync.Mutex
	latest     string
	checked    time.Time
	checkErr   error
	staging    bool
	staged     *stagedUpdate
	waitingFor string
	// updating: the person asked to update, or the handover has begun.
	updating   bool
	handedOver bool
}

// stagedUpdate is a downloaded, verified release beside the installed binary.
type stagedUpdate struct {
	Version string
	Path    string
	At      time.Time
}

func newDaemonUpdater(version, stateDir, addr, daemonID string, quiet func() string, handover func(*updateHandover)) *daemonUpdater {
	installed, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(installed); err == nil {
		installed = resolved
	}
	// A Windows update moves the running executable aside; remove the
	// copies nothing runs any more.
	platform.SweepReplaced(installed)
	return &daemonUpdater{
		version: version, supported: selfUpdateSupported(version) && installDirWritable(installed), stateDir: stateDir, addr: addr, daemonID: daemonID,
		installed: installed, newClient: selfupdate.NewClient, quiet: quiet, handover: handover, now: time.Now,
		interval: autoUpdateInterval, first: autoUpdateFirst, quietPoll: autoUpdateQuietPoll, cap: autoUpdateCap,
	}
}

// installDirWritable: staging and swapping happen beside the installed binary,
// so a root-owned install cannot update itself and must not offer to.
func installDirWritable(installed string) bool {
	return accessPath(filepath.Dir(installed), accW) == nil
}

func (u *daemonUpdater) Status() server.DaemonUpdateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	status := server.DaemonUpdateStatus{Version: u.version, Supported: u.supported, Latest: u.latest, Updating: u.updating}
	if u.staged != nil && u.waitingFor != "" && !u.updating {
		status.WaitingFor = u.waitingFor
		by := u.staged.At.Add(u.cap).UTC()
		status.InstallBy = &by
	}
	if u.host != nil {
		host := u.host.Status()
		status.HostVersion, status.TerminalsRestart = host.Version, host.Restart
		if host.Restart {
			status.LiveTerminals = host.Terminals
		}
	}
	if failure, ok := selfupdate.LastFailure(u.stateDir); ok && u.supported && failure.Version != u.version {
		if failure.Permanent() {
			status.Error = fmt.Sprintf("Mewla %s did not start; still on %s", failure.Version, u.version)
		} else {
			status.Error = fmt.Sprintf("Mewla %s could not be installed; still on %s", failure.Version, u.version)
		}
	}
	return status
}

// Run checks every hour, stages a newer release, and installs it at the first
// quiet point or once the cap has passed, Workers running or not. It returns
// when ctx ends or the daemon hands over.
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
		due := u.now().Sub(u.checked) >= u.interval
		u.mu.Unlock()
		if due {
			if err := u.check(ctx); err != nil {
				log.Printf("update check: %v", err)
			}
		}
		u.mu.Lock()
		pending := u.latest != "" && !u.heldBack()
		u.mu.Unlock()
		if !pending {
			u.dropStaged()
			wait = u.interval
			continue
		}
		staged, err := u.stage(ctx)
		switch {
		case errors.Is(err, errInProgress):
			wait = u.quietPoll
			continue
		case err != nil:
			log.Printf("update: %v", err)
			wait = u.interval
			continue
		}
		if u.installWhenQuiet(staged) {
			return
		}
		wait = u.quietPoll
	}
}

// installWhenQuiet hands over to a staged update when nothing would be cut,
// or when the cap has passed; otherwise it records (and logs, once per
// change) what the update waits for.
func (u *daemonUpdater) installWhenQuiet(staged *stagedUpdate) bool {
	reason := u.quiet()
	by := staged.At.Add(u.cap)
	if reason != "" && u.now().Before(by) {
		u.mu.Lock()
		changed := u.waitingFor != reason
		u.waitingFor = reason
		u.mu.Unlock()
		if changed {
			log.Printf("update: Mewla %s is ready and waits for %s; it installs by %s at the latest", staged.Version, reason, by.Local().Format("15:04"))
		}
		return false
	}
	if reason != "" {
		log.Printf("update: still waiting for %s after %s; installing Mewla %s now", reason, u.cap, staged.Version)
	} else {
		log.Printf("update: nothing in flight; installing Mewla %s now", staged.Version)
	}
	return u.handOver(staged) == nil
}

// heldBack: the newest release failed to install earlier for a reason that
// is not its fault and its retry time has not come. Called with u.mu held.
func (u *daemonUpdater) heldBack() bool {
	failure, ok := selfupdate.LastFailure(u.stateDir)
	return ok && failure.Version == u.latest && failure.HoldsAutomatic(time.Now())
}

var errInProgress = errors.New("an update is already in progress")

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
	u.checked, u.checkErr = u.now(), err
	if err != nil {
		return nil, err
	}
	if failure, ok := selfupdate.LastFailure(u.stateDir); ok && candidate != nil && failure.Version == candidate.Version && failure.Permanent() {
		candidate = nil
	}
	u.latest = ""
	if candidate != nil {
		u.latest = candidate.Version
	}
	return candidate, nil
}

// Updating: an update of the daemon's own is being installed.
func (u *daemonUpdater) Updating() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.updating
}

// Install is the App's Update button: it installs now, without waiting for a
// quiet point.
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
		if err := u.installNow(context.Background()); err != nil {
			u.mu.Lock()
			if !u.handedOver {
				u.updating = false
			}
			u.mu.Unlock()
			log.Printf("update: %v", err)
		}
	}()
	return nil
}

// RestartTerminals is the app's "Restart terminals" tap.
func (u *daemonUpdater) RestartTerminals() error {
	if u.host == nil {
		return server.ErrUpdateUnavailable
	}
	return u.host.RestartNow()
}

// installNow stages (or reuses what is staged) and hands over at once. A
// download the periodic path has under way is waited for, not repeated.
func (u *daemonUpdater) installNow(ctx context.Context) error {
	deadline := u.now().Add(10 * time.Minute)
	for {
		staged, err := u.stage(ctx)
		u.mu.Lock()
		handedOver := u.handedOver
		u.mu.Unlock()
		if errors.Is(err, errInProgress) && !handedOver && u.now().Before(deadline) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}
		if err != nil {
			return err
		}
		return u.handOver(staged)
	}
}

// stage downloads, verifies and stages the newest release beside the
// installed binary, once per version: a staged update is reused until it
// installs or a newer release replaces it. Nothing installed changes.
func (u *daemonUpdater) stage(ctx context.Context) (*stagedUpdate, error) {
	u.mu.Lock()
	if u.staging || u.handedOver {
		u.mu.Unlock()
		return nil, errInProgress
	}
	if st := u.staged; st != nil && st.Version == u.latest && fileExists(st.Path) {
		u.mu.Unlock()
		return st, nil
	}
	u.staging = true
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.staging = false
		u.mu.Unlock()
	}()
	candidate, err := u.latestRelease(ctx)
	if err != nil {
		return nil, fmt.Errorf("check for update: %w", err)
	}
	if candidate == nil {
		return nil, server.ErrUpdateUnavailable
	}
	client, err := u.newClient()
	if err != nil {
		return nil, err
	}
	downloadCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	binary, err := client.DownloadBinary(downloadCtx, *candidate)
	if err != nil {
		return nil, fmt.Errorf("download update: %w", err)
	}
	path, err := selfupdate.Stage(u.installed, binary, candidate.Version)
	if err != nil {
		return nil, fmt.Errorf("stage update: %w", err)
	}
	staged := &stagedUpdate{Version: candidate.Version, Path: path, At: u.now()}
	u.mu.Lock()
	u.staged, u.waitingFor = staged, ""
	u.mu.Unlock()
	return staged, nil
}

// dropStaged removes a staged update nothing offers any more (it was rolled
// back, or the release was withdrawn).
func (u *daemonUpdater) dropStaged() {
	u.mu.Lock()
	staged := u.staged
	if u.handedOver || u.staging {
		staged = nil
	} else {
		u.staged, u.waitingFor = nil, ""
	}
	u.mu.Unlock()
	if staged != nil {
		_ = os.Remove(staged.Path)
	}
}

// handOver ends the daemon so main can hand over to the staged binary. Only
// one runs.
func (u *daemonUpdater) handOver(staged *stagedUpdate) error {
	u.mu.Lock()
	if u.handedOver {
		u.mu.Unlock()
		return errInProgress
	}
	u.handedOver, u.updating, u.waitingFor = true, true, ""
	u.mu.Unlock()
	u.handover(&updateHandover{
		Installed: u.installed, Staged: staged.Path, Version: staged.Version,
		Addr: u.addr, DaemonID: u.daemonID, StateDir: u.stateDir,
	})
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// updateQuietPoint names what a daemon restart now would cut, "" when
// nothing would. Running Workers are not on the list: their Sessions live in
// the session host and keep running. What lives in the daemon is: a
// permission decision a hook holds open (Brain's or the person's answer would
// find no question), a model reply streaming through Mewla's gateway or a
// Session route (cut mid-reply), and a message being typed into Brain (its
// delivery would become uncertain).
func updateQuietPoint(heldApprovals, modelRequests, brainInputs func() int) string {
	switch {
	case heldApprovals != nil && heldApprovals() > 0:
		return "a permission answer"
	case modelRequests != nil && modelRequests() > 0:
		return "a model reply"
	case brainInputs != nil && brainInputs() > 0:
		return "a message to Brain"
	}
	return ""
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
	var reexec *daemonReexec
	if errors.As(err, &reexec) {
		return runInPlaceRestart(reexec)
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
	// The relay's sockets stay open in this process throughout: the trial
	// child serves them, then the exec below takes them on.
	childEnv, childFiles := listenhandoff.ForChild(os.Environ())
	outcome := selfupdate.Handover{
		Installed: h.Installed, Staged: h.Staged, Version: h.Version,
		Args: args, Env: childEnv, ExtraFiles: childFiles, StateDir: h.StateDir, BrainRoot: brainRoot,
		Gate:   updateGate,
		Stdout: os.Stdout, Stderr: os.Stderr,
		Healthy: func() error { return probeUpdatedDaemon(h.Addr, h.DaemonID, h.Version) },
	}.Run(ctx)
	if outcome.Adopted {
		log.Printf("mewla: updated to %s", h.Version)
	} else {
		log.Printf("mewla: update to %s did not take, staying on the current version: %s", h.Version, outcome.Reason)
		if ctx.Err() != nil {
			return nil // the service is being stopped: nothing to blame, no cooldown
		}
		failure := selfupdate.Failure{Version: h.Version, Reason: outcome.Reason, At: time.Now()}
		if !outcome.Bad {
			// Not the new version's fault (a full disk, a filesystem that
			// refuses): leave it alone for a day instead of restarting the
			// daemon every few minutes.
			failure.RetryAfter = failure.At.Add(transientRetryAfter)
		}
		if err := selfupdate.RecordFailure(h.StateDir, failure); err != nil {
			log.Printf("mewla: remember failed update: %v", err)
		}
	}
	markRestarting(h.StateDir)
	return platform.ExecReplace(h.Installed, append([]string{os.Args[0]}, args...), listenhandoff.ForExec(os.Environ()))
}

// markRestarting tells commands that this process is about to answer again
// (control.MarkRestarting): the trial child cleared the mark when it listened.
func markRestarting(stateDir string) {
	socket, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		return
	}
	if err := control.MarkRestarting(socket, os.Getpid(), time.Now().Add(restartStartAllowance)); err != nil {
		log.Printf("mewla: mark the restart for waiting commands: %v", err)
	}
}
