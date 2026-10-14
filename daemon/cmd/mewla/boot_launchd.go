package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/macapp"
	"github.com/daoleno/mewla/daemon/shellquote"
)

// On macOS `mewla boot` installs a LaunchAgent in the user's GUI session
// (gui/<uid>), the session that owns the screen, notifications and privacy
// permissions. launchd starts it at login and restarts it if it fails.
//
// Ownership: launchd reports the job's PID. The job owns the state when that
// PID runs the installed binary, holds the state lifecycle lock (lsof names
// the lock file's holders) and /health serves the state's identity. As on
// Linux, the boot CLI never kills a process it did not start.
//
// AbandonProcessGroup keeps the user's tmux server and Worker sessions alive
// when launchd stops or restarts the daemon, like KillMode=process.
//
// The job runs a copy of the binary inside ~/Applications/Mewla.app, so
// privacy settings, permission prompts and dialogs show Mewla's name and icon
// and grant permissions to com.daoleno.mewla. Install refreshes the copy from
// the installing binary and restarts the job when it changed.

const (
	bootLaunchdLabel  = "com.daoleno.mewla.daemon"
	bootLaunchdMarker = "<!-- Managed by mewla boot install -->"
	// launchctl exits 113 when the domain has no such service.
	bootLaunchdNotLoaded = 113
)

func bootLaunchdPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", bootLaunchdLabel+".plist"), nil
}

func bootLaunchdDomain() string { return "gui/" + strconv.Itoa(bootEffectiveUID()) }

func bootLaunchdTarget() string { return bootLaunchdDomain() + "/" + bootLaunchdLabel }

func bootLaunchdLogPath(config bootConfig) string {
	return filepath.Join(config.StateDir, "logs", "launchd.log")
}

const (
	bootSigningLocal = "local identity \"" + macapp.IdentityName + "\"; permissions stay granted across updates"
	bootSigningAdHoc = "ad hoc; macOS asks for the permissions again after each update"
)

// bootLaunchdSigner picks how Mewla.app is signed: the identity in
// MEWLA_CODESIGN_IDENTITY, else this Mac's local identity (created on first
// use), else ad hoc. The description goes into boot status.
func bootLaunchdSigner(home string, runner bootRunner) (macapp.Signer, string) {
	if signer, ok := macapp.SignerFromEnv(runner.run); ok {
		return signer, "MEWLA_CODESIGN_IDENTITY " + signer.Identity
	}
	signer, err := macapp.LocalSigner(home, runner.run)
	if err != nil {
		return macapp.Signer{Run: runner.run}, bootSigningAdHoc + " (local identity unavailable: " + err.Error() + ")"
	}
	return signer, bootSigningLocal
}

// bootLaunchdDaemonConfig is config with the binary launchd runs: the copy
// inside Mewla.app.
func bootLaunchdDaemonConfig(config bootConfig) bootConfig {
	config.Binary = macapp.Executable(macapp.Path(config.Home))
	return config
}

func plistString(value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return "<string>" + builder.String() + "</string>"
}

func renderLaunchdPlist(config bootConfig) (string, error) {
	if err := validateBootConfig(config); err != nil {
		return "", err
	}
	arguments := []string{config.Binary, "-state-dir", config.StateDir}
	if config.LAN {
		arguments = append(arguments, "-lan")
	} else {
		arguments = append(arguments, "-addr", config.Addr)
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(bootLaunchdMarker + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("\t<key>Label</key>\n\t" + plistString(bootLaunchdLabel) + "\n")
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, argument := range arguments {
		b.WriteString("\t\t" + plistString(argument) + "\n")
	}
	b.WriteString("\t</array>\n")
	b.WriteString("\t<key>WorkingDirectory</key>\n\t" + plistString(config.WorkDir) + "\n")
	b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
	b.WriteString("\t\t<key>HOME</key>\n\t\t" + plistString(config.Home) + "\n")
	b.WriteString("\t\t<key>PATH</key>\n\t\t" + plistString(config.PathEnv) + "\n")
	b.WriteString("\t</dict>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	// Restart after a failure, not after a clean exit.
	b.WriteString("\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n")
	b.WriteString("\t<key>ThrottleInterval</key>\n\t<integer>5</integer>\n")
	// Only the GUI session can capture the screen and show permission prompts.
	b.WriteString("\t<key>LimitLoadToSessionType</key>\n\t" + plistString("Aqua") + "\n")
	b.WriteString("\t<key>ProcessType</key>\n\t" + plistString("Interactive") + "\n")
	// Login Items settings list the job under the app instead of a bare label.
	b.WriteString("\t<key>AssociatedBundleIdentifiers</key>\n\t" + plistString(macapp.BundleID) + "\n")
	b.WriteString("\t<key>AbandonProcessGroup</key>\n\t<true/>\n")
	b.WriteString("\t<key>StandardOutPath</key>\n\t" + plistString(bootLaunchdLogPath(config)) + "\n")
	b.WriteString("\t<key>StandardErrorPath</key>\n\t" + plistString(bootLaunchdLogPath(config)) + "\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String(), nil
}

// bootLaunchdJob is launchd's view of the installed job.
type bootLaunchdJob struct {
	Loaded bool
	State  string
	PID    int
}

func (job bootLaunchdJob) running() bool { return job.Loaded && job.State == "running" && job.PID > 0 }

func (job bootLaunchdJob) describe() string {
	switch {
	case !job.Loaded:
		return "not loaded"
	case job.PID > 0:
		return fmt.Sprintf("%s (PID %d)", job.State, job.PID)
	default:
		return job.State
	}
}

func bootLaunchdInspect(runner bootRunner) (bootLaunchdJob, error) {
	output, err := runner.run("launchctl", "print", bootLaunchdTarget())
	if err != nil {
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && exit.ExitCode() == bootLaunchdNotLoaded {
			return bootLaunchdJob{}, nil
		}
		if strings.Contains(string(output), "Could not find service") {
			return bootLaunchdJob{}, nil
		}
		return bootLaunchdJob{}, fmt.Errorf("launchctl print %s: %v: %s", bootLaunchdTarget(), err, strings.TrimSpace(string(output)))
	}
	return parseLaunchdPrint(string(output)), nil
}

// parseLaunchdPrint reads the job's own top-level state and pid lines from
// `launchctl print`; nested blocks are indented further and ignored.
func parseLaunchdPrint(output string) bootLaunchdJob {
	job := bootLaunchdJob{Loaded: true}
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "\t\t") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimSpace(line), " = ")
		if !ok {
			continue
		}
		switch key {
		case "state":
			job.State = value
		case "pid":
			job.PID, _ = strconv.Atoi(value)
		}
	}
	return job
}

// bootLaunchdLockHolders lists the processes with the state lock file open.
// Only the owning daemon keeps it open; probes hold it for an instant.
func bootLaunchdLockHolders(runner bootRunner, stateDir string) ([]int, error) {
	path, err := bootLifecycleLockPath(stateDir)
	if err != nil {
		return nil, err
	}
	output, err := runner.run("lsof", "-t", "--", path)
	text := strings.TrimSpace(string(output))
	if err != nil && text == "" {
		// lsof exits 1 when nothing has the file open.
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("lsof %s: %w", path, err)
	}
	var pids []int
	for _, field := range strings.Fields(text) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("parse lsof output %q", text)
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// bootLaunchdBootout removes the job and waits until launchd no longer lists
// it: bootout returns before the job is fully torn down, and an immediate
// bootstrap of the same label fails with an I/O error.
func bootLaunchdBootout(runner bootRunner) error {
	if output, err := runner.run("launchctl", "bootout", bootLaunchdTarget()); err != nil {
		return fmt.Errorf("launchctl bootout %s: %v: %s", bootLaunchdTarget(), err, strings.TrimSpace(string(output)))
	}
	deadline := time.Now().Add(bootVerifyTimeout)
	for {
		job, err := bootLaunchdInspect(runner)
		if err != nil {
			return err
		}
		if !job.Loaded {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s is still %s after bootout", bootLaunchdLabel, job.describe())
		}
		time.Sleep(bootVerifyInterval)
	}
}

// bootLaunchdBootstrap loads the plist, retrying briefly while launchd
// finishes removing a previous instance.
func bootLaunchdBootstrap(runner bootRunner, path string) error {
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		output, err := runner.run("launchctl", "bootstrap", bootLaunchdDomain(), path)
		if err == nil {
			return nil
		}
		last = fmt.Errorf("launchctl bootstrap %s %s: %v: %s", bootLaunchdDomain(), path, err, strings.TrimSpace(string(output)))
		time.Sleep(bootVerifyInterval)
	}
	return last
}

func bootLaunchdProcessBinary(runner bootRunner, pid int) (string, error) {
	output, err := runner.run("ps", "-o", "comm=", "-p", strconv.Itoa(pid))
	if err != nil {
		return "", fmt.Errorf("ps %d: %v: %s", pid, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

type bootLaunchdSnapshot struct {
	Job        bootLaunchdJob
	InspectErr error
	Binary     string
	LockHeld   bool
	LockPIDs   []int
	LockErr    error
	ServedID   string
	ExpectedID string
	HealthErr  error
	StateIDErr error
}

func (s bootLaunchdSnapshot) jobHoldsLock() bool {
	for _, pid := range s.LockPIDs {
		if pid == s.Job.PID {
			return true
		}
	}
	return false
}

func bootLaunchdInspectOwner(config bootConfig, runner bootRunner, health bool) bootLaunchdSnapshot {
	var s bootLaunchdSnapshot
	s.Job, s.InspectErr = bootLaunchdInspect(runner)
	if s.Job.running() {
		if binary, err := bootLaunchdProcessBinary(runner, s.Job.PID); err == nil {
			s.Binary = binary
		}
	}
	if held, err := bootStateLockHeld(config.StateDir); err != nil {
		s.LockErr = err
	} else {
		s.LockHeld = held
	}
	if s.LockErr == nil && s.LockHeld {
		s.LockPIDs, s.LockErr = bootLaunchdLockHolders(runner, config.StateDir)
	}
	if !health {
		return s
	}
	if served, err := bootProbeHealth(config.Addr); err == nil {
		s.ServedID = served
	} else {
		s.HealthErr = err
	}
	if expected, err := bootStateDaemonID(config.StateDir); err == nil {
		s.ExpectedID = expected
	} else {
		s.StateIDErr = err
	}
	return s
}

func (s bootLaunchdSnapshot) verified(config bootConfig) error {
	switch {
	case s.InspectErr != nil:
		return s.InspectErr
	case !s.Job.running():
		return fmt.Errorf("%s is %s, not running", bootLaunchdLabel, s.Job.describe())
	case !bootSameFilePath(s.Binary, config.Binary):
		return fmt.Errorf("%s process %d runs %q, not the installed binary %s", bootLaunchdLabel, s.Job.PID, s.Binary, config.Binary)
	case s.LockErr != nil:
		return fmt.Errorf("inspect state ownership: %w", s.LockErr)
	case !s.LockHeld:
		return fmt.Errorf("no daemon owns state directory %s", config.StateDir)
	case !s.jobHoldsLock():
		return fmt.Errorf("state directory %s is owned by a process outside %s", config.StateDir, bootLaunchdLabel)
	case s.HealthErr != nil:
		return fmt.Errorf("health %s: %w", bootProbeAddr(config.Addr), s.HealthErr)
	case s.StateIDErr != nil:
		return fmt.Errorf("read state identity: %w", s.StateIDErr)
	case s.ServedID != s.ExpectedID:
		return fmt.Errorf("health endpoint %s serves daemon %s, installed state is %s", bootProbeAddr(config.Addr), s.ServedID, s.ExpectedID)
	}
	return nil
}

func (s bootLaunchdSnapshot) describeOwner() string {
	switch {
	case s.InspectErr != nil:
		return "unknown (" + s.InspectErr.Error() + ")"
	case s.LockErr != nil:
		return "unknown (" + s.LockErr.Error() + ")"
	case s.LockHeld && s.Job.running() && s.jobHoldsLock():
		return fmt.Sprintf("state directory is owned by %s (PID %d)", bootLaunchdLabel, s.Job.PID)
	case s.LockHeld:
		return fmt.Sprintf("state lifecycle lock is held by a process outside %s (PIDs %v); mewla boot never kills it", bootLaunchdLabel, s.LockPIDs)
	default:
		return "no running daemon owns the state directory"
	}
}

func bootLaunchdVerify(config bootConfig, runner bootRunner) error {
	deadline := time.Now().Add(bootVerifyTimeout)
	for {
		err := bootLaunchdInspectOwner(config, runner, true).verified(config)
		if err == nil {
			// Two observations, so a process that is about to exit is not
			// reported as the owner.
			time.Sleep(bootVerifyInterval)
			if err = bootLaunchdInspectOwner(config, runner, true).verified(config); err == nil {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(bootVerifyInterval)
	}
}

func bootLaunchdInstall(config bootConfig, runner bootRunner, out io.Writer) error {
	if bootEffectiveUID() == 0 {
		return errors.New("mewla boot install must run as the unprivileged user that owns the daemon state; refusing root")
	}
	if err := bootValidateLocalContext(config); err != nil {
		return err
	}
	source, bundle := config.Binary, macapp.Path(config.Home)
	config = bootLaunchdDaemonConfig(config)
	plist, err := renderLaunchdPlist(config)
	if err != nil {
		return err
	}
	path, err := bootLaunchdPlistPath()
	if err != nil {
		return err
	}
	metadataPath := bootMetadataPath(path)
	existing, readErr := os.ReadFile(path)
	if readErr == nil && !strings.Contains(string(existing), bootLaunchdMarker) {
		return fmt.Errorf("refusing to replace a LaunchAgent not managed by mewla boot: %s", path)
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("read existing LaunchAgent: %w", readErr)
	}
	if config.DryRun {
		fmt.Fprintf(out, "<!-- %s is copied to %s -->\n", source, config.Binary)
		fmt.Fprint(out, plist)
		return nil
	}
	if _, err := os.Stat(bundle); err == nil && !macapp.Managed(bundle) {
		return fmt.Errorf("refusing to replace %s, which mewla boot did not create", bundle)
	}

	// The state must be free or owned by this job; any other owner is refused,
	// never killed.
	preflight := bootLaunchdInspectOwner(config, runner, false)
	switch {
	case preflight.InspectErr != nil:
		return preflight.InspectErr
	case preflight.LockErr != nil:
		return fmt.Errorf("inspect state ownership: %w", preflight.LockErr)
	case preflight.LockHeld && !(preflight.Job.running() && preflight.jobHoldsLock()):
		return fmt.Errorf("state directory %s is owned by a running process outside %s; stop that process first (mewla boot never kills it)", config.StateDir, bootLaunchdLabel)
	}
	running := preflight.Job.running()

	previous, previousErr := readBootMetadata(metadataPath)
	writePlist := readErr != nil || string(existing) != plist
	changed := previousErr != nil || !previous.same(config)
	if !running || (previousErr == nil && previous.Addr != config.Addr) {
		if err := bootAddrFree(config.Addr); err != nil {
			return fmt.Errorf("listen address %s is not available: %v; stop the process using it first", config.Addr, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(bootLaunchdLogPath(config)), 0o700); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	// The running daemon keeps its files: a changed bundle is swapped in
	// whole and the job restarted below.
	signer, signing := bootLaunchdSigner(config.Home, runner)
	bundleChanged, err := macapp.Install(bundle, source, Version, macapp.Icon, signer)
	if errors.Is(err, macapp.ErrSign) && signer.Identity != "" {
		signing = bootSigningAdHoc + " (" + err.Error() + ")"
		bundleChanged, err = macapp.Install(bundle, source, Version, macapp.Icon, macapp.Signer{Run: runner.run})
	}
	if err != nil {
		return fmt.Errorf("install %s: %w", bundle, err)
	}
	changed = changed || bundleChanged
	if writePlist {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create LaunchAgents directory: %w", err)
		}
		if err := bootWriteFileAtomic(path, []byte(plist), 0o644); err != nil {
			return fmt.Errorf("write LaunchAgent: %w", err)
		}
	}
	if writePlist || changed || previous.Signing != signing {
		metadata := bootMetadataFor(config)
		metadata.Signing = signing
		if err := writeBootMetadata(metadataPath, metadata); err != nil {
			return fmt.Errorf("write boot metadata: %w", err)
		}
	}

	// A disabled override would stop launchd from loading the job at login.
	if output, err := runner.run("launchctl", "enable", bootLaunchdTarget()); err != nil {
		return fmt.Errorf("launchctl enable %s: %v: %s", bootLaunchdTarget(), err, strings.TrimSpace(string(output)))
	}
	bootstrap := func() error { return bootLaunchdBootstrap(runner, path) }
	action := "Installed and started"
	switch {
	case running && !changed && !writePlist:
		action = "Already installed and running"
	case preflight.Job.Loaded && (changed || writePlist):
		action = "Updated and restarted"
		if err := bootLaunchdBootout(runner); err != nil {
			return err
		}
		if err := bootstrap(); err != nil {
			return err
		}
	case preflight.Job.Loaded:
		action = "Started"
		if output, err := runner.run("launchctl", "kickstart", bootLaunchdTarget()); err != nil {
			return fmt.Errorf("launchctl kickstart %s: %v: %s", bootLaunchdTarget(), err, strings.TrimSpace(string(output)))
		}
	default:
		if err := bootstrap(); err != nil {
			return err
		}
	}

	if err := bootLaunchdVerify(config, runner); err != nil {
		if action != "Already installed and running" {
			_ = bootLaunchdBootout(runner)
		}
		return fmt.Errorf("%s did not become the verified owner (%s): %w; see %s", bootLaunchdLabel, config.StateDir, err, bootLaunchdLogPath(config))
	}

	fmt.Fprintf(out, "%s %s\n", action, path)
	fmt.Fprintf(out, "App: %s (copied from %s)\n", bundle, source)
	fmt.Fprintf(out, "Signing: %s\n", signing)
	fmt.Fprintf(out, "State directory: %s\n", config.StateDir)
	fmt.Fprintf(out, "Address: %s\n", config.Addr)
	fmt.Fprintf(out, "Working directory: %s\n", config.WorkDir)
	fmt.Fprintf(out, "Log: %s\n", bootLaunchdLogPath(config))
	fmt.Fprintln(out, "Starts when you log in to this Mac. tmux and Worker sessions are never stopped here.")
	fmt.Fprintln(out, "Stop/rollback: mewla boot uninstall")
	return nil
}

func bootLaunchdStatus(runner bootRunner, out io.Writer) error {
	path, err := bootLaunchdPlistPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Fprintf(out, "%s: not installed (%s)\n", bootLaunchdLabel, path)
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), bootLaunchdMarker) {
		fmt.Fprintf(out, "%s: present but not managed by mewla boot (%s)\n", bootLaunchdLabel, path)
		return nil
	}
	fmt.Fprintf(out, "LaunchAgent: %s\n", path)
	metadata, metadataErr := readBootMetadata(bootMetadataPath(path))
	if metadataErr != nil {
		job, err := bootLaunchdInspect(runner)
		if err != nil {
			fmt.Fprintf(out, "service: unknown (%v)\n", err)
		} else {
			fmt.Fprintf(out, "service: %s\n", job.describe())
		}
		fmt.Fprintf(out, "configuration: unavailable (%v); reinstall with `mewla boot install`\n", metadataErr)
		return nil
	}
	config := bootConfig{Binary: metadata.Binary, StateDir: metadata.StateDir, Addr: metadata.Addr}
	snapshot := bootLaunchdInspectOwner(config, runner, true)
	if snapshot.InspectErr != nil {
		fmt.Fprintf(out, "service: unknown (%v)\n", snapshot.InspectErr)
	} else {
		fmt.Fprintf(out, "service: %s\n", snapshot.Job.describe())
	}
	fmt.Fprintf(out, "binary: %s\n", metadata.Binary)
	if hash, err := bootFileHash(metadata.Binary); err == nil {
		fmt.Fprintf(out, "binary sha256: %s\n", hash)
	} else {
		fmt.Fprintf(out, "binary sha256: unavailable (%v)\n", err)
	}
	fmt.Fprintf(out, "state directory: %s\n", metadata.StateDir)
	fmt.Fprintf(out, "address: %s\n", metadata.Addr)
	fmt.Fprintf(out, "working directory: %s\n", metadata.WorkDir)
	fmt.Fprintf(out, "PATH: %s\n", metadata.PathEnv)
	fmt.Fprintf(out, "signing: %s\n", bootLaunchdDescribeSigning(metadata))
	fmt.Fprintf(out, "ownership: %s\n", snapshot.describeOwner())
	switch {
	case snapshot.HealthErr != nil:
		fmt.Fprintf(out, "health: unreachable (%s: %v)\n", bootProbeAddr(config.Addr), snapshot.HealthErr)
	case snapshot.StateIDErr != nil:
		fmt.Fprintf(out, "health: endpoint responded daemon_id=%s; state identity unavailable (%v)\n", snapshot.ServedID, snapshot.StateIDErr)
	case snapshot.ServedID != snapshot.ExpectedID:
		fmt.Fprintf(out, "health: mismatch: endpoint serves daemon_id=%s, installed state identity=%s\n", snapshot.ServedID, snapshot.ExpectedID)
	default:
		fmt.Fprintf(out, "health: ok daemon_id=%s\n", snapshot.ServedID)
	}
	return nil
}

func bootLaunchdDescribeSigning(metadata bootMetadata) string {
	switch {
	case metadata.Signing == "":
		return "unknown (installed by an older mewla; run `mewla boot install`)"
	case metadata.Signing == bootSigningLocal:
		if _, err := os.Stat(macapp.IdentityKeychain(metadata.Home)); err != nil {
			return "local identity missing (" + macapp.IdentityKeychain(metadata.Home) + "); the next `mewla boot install` creates a new one and macOS asks for the permissions once more"
		}
	}
	return metadata.Signing
}

// bootLaunchdHandOff starts the LaunchAgent instead of a second daemon when
// macOS itself opens Mewla.app: a double-click, or "Quit & Reopen" after a
// privacy permission changes. Those launches carry no arguments, so they
// would serve an unmanaged daemon on the default loopback address that the
// phone can't reach and launchd can't replace. kickstart leaves a running
// job alone. handed reports whether the launch was handed off.
func bootLaunchdHandOff(args []string, executable string, runner bootRunner) (handed bool, err error) {
	if bootGOOS != "darwin" || len(args) > 0 || os.Getenv("XPC_SERVICE_NAME") == bootLaunchdLabel {
		return false, nil
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	if executable != macapp.Executable(bundle) || !macapp.Managed(bundle) {
		return false, nil
	}
	if _, ok := bootLaunchdReinstallArgs(); !ok {
		return false, nil
	}
	if output, err := runner.run("launchctl", "kickstart", bootLaunchdTarget()); err != nil {
		return true, fmt.Errorf("launchctl kickstart %s: %v: %s", bootLaunchdTarget(), err, strings.TrimSpace(string(output)))
	}
	return true, nil
}

// bootLaunchdReinstallArgs are the `boot install` arguments that move the
// installed LaunchAgent to a new binary while keeping its state directory,
// address, working directory and PATH; plain `mewla boot install` would take
// those from the current shell. ok is false when no managed LaunchAgent
// exists.
func bootLaunchdReinstallArgs() (args []string, ok bool) {
	path, err := bootLaunchdPlistPath()
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), bootLaunchdMarker) {
		return nil, false
	}
	args = []string{"boot", "install"}
	metadata, err := readBootMetadata(bootMetadataPath(path))
	if err != nil {
		return args, true
	}
	args = append(args, "-state-dir", metadata.StateDir)
	if metadata.LAN {
		args = append(args, "-lan")
	} else {
		args = append(args, "-addr", metadata.Addr)
	}
	return append(args, "-work-dir", metadata.WorkDir, "-path-env", metadata.PathEnv), true
}

// bootUpdateRun runs the updated binary; tests replace it.
var bootUpdateRun = func(command *exec.Cmd) error { return command.Run() }

// bootLaunchdApplyUpdate moves the installed LaunchAgent to the updated
// binary by running its own `boot install`, which replaces Mewla.app and
// restarts the service. ok is false when no LaunchAgent is installed.
func bootLaunchdApplyUpdate(binary string, out, errOut io.Writer) (ok bool, err error) {
	args, ok := bootLaunchdReinstallArgs()
	if !ok {
		return false, nil
	}
	command := exec.Command(binary, args...)
	command.Stdout, command.Stderr = out, errOut
	if err := bootUpdateRun(command); err != nil {
		words := []string{shellquote.Word(binary)}
		for _, arg := range args {
			words = append(words, shellquote.Word(arg))
		}
		return true, fmt.Errorf("restart Mewla with the update: %w; run `%s` to retry", err, strings.Join(words, " "))
	}
	return true, nil
}

func bootLaunchdUninstall(runner bootRunner, out io.Writer) error {
	path, err := bootLaunchdPlistPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Fprintf(out, "%s not installed (%s)\n", bootLaunchdLabel, path)
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), bootLaunchdMarker) {
		return fmt.Errorf("refusing to remove a LaunchAgent not managed by mewla boot: %s", path)
	}
	metadataPath := bootMetadataPath(path)
	metadata, err := readBootMetadata(metadataPath)
	if err != nil {
		return fmt.Errorf("uninstall requires the installed configuration metadata: %v (LaunchAgent and configuration retained; reinstall with `mewla boot install` before uninstalling)", err)
	}

	// Stop first and keep the configuration on any failure.
	job, err := bootLaunchdInspect(runner)
	if err != nil {
		return fmt.Errorf("%w (LaunchAgent and configuration retained)", err)
	}
	if job.Loaded {
		if err := bootLaunchdBootout(runner); err != nil {
			return fmt.Errorf("%w (LaunchAgent and configuration retained; retry `mewla boot uninstall`)", err)
		}
	}

	// launchd stopped the job's own process; whatever still holds the state is
	// a separate daemon and is left alone.
	var holders []int
	if held, err := bootStateLockHeld(metadata.StateDir); err != nil {
		return fmt.Errorf("inspect state lifecycle lock after stop: %w (LaunchAgent and configuration retained)", err)
	} else if held {
		holders, _ = bootLaunchdLockHolders(runner, metadata.StateDir)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove LaunchAgent: %w", err)
	}
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove boot metadata: %w", err)
	}
	fmt.Fprintf(out, "Removed %s; daemon state and pairing are untouched\n", path)
	if bundle := macapp.Path(metadata.Home); metadata.Binary == macapp.Executable(bundle) {
		if err := macapp.Remove(bundle); err != nil {
			fmt.Fprintf(out, "Note: %s was not removed: %v\n", bundle, err)
		} else {
			fmt.Fprintf(out, "Removed %s\n", bundle)
		}
	}
	if len(holders) > 0 {
		fmt.Fprintf(out, "Note: state directory %s is owned by another process (PIDs %v); it was not stopped or modified\n", metadata.StateDir, holders)
	}
	return nil
}
