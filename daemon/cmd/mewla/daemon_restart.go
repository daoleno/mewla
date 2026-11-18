package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/platform"
	"github.com/daoleno/mewla/daemon/selfupdate"
)

// A running daemon keeps the executable it started from. `mewla update`
// replaces the file, so the daemon goes on running the old version until it
// restarts; a newer CLI talking to it is then the skew a person cannot see.
// The CLI restarts the daemon through whatever started it (the logon task,
// the systemd user unit); a daemon nothing supervises (a terminal, a script,
// started detached by hand) restarts itself in place (daemon_upgrade.go).
// Either way only the daemon restarts: the session host and every Session and
// Worker keep running. The macOS LaunchAgent restarts through Mewla.app
// instead (bootLaunchdApplyUpdate).

// runningDaemon is the daemon that owns a state directory, as the CLI found
// it.
type runningDaemon struct {
	PID     int
	Version string
	Addr    string
}

// daemonHealth is what /health says about the daemon answering it.
type daemonHealth struct {
	DaemonID string `json:"daemon_id"`
	Version  string `json:"version"`
}

// daemonRuntime finds and restarts the daemon. Tests replace its functions;
// defaultDaemonRuntime is the real computer.
type daemonRuntime struct {
	goos    string
	runner  bootRunner
	self    func() (string, error)
	peerPID func(stateDir string) (int, error)
	image   func(pid int) (string, error)
	// parentImage is the file name of the process that started pid.
	parentImage func(pid int) (string, error)
	health      func(addr string) (daemonHealth, error)
	// status is the daemon's own report on the control socket
	// (daemon_status).
	status func(stateDir string) (control.DaemonStatus, error)
	// upgrade asks the daemon to restart itself onto version
	// (daemon_upgrade).
	upgrade func(stateDir, version string) error
	kill    func(pid int) error
	alive   func(pid int) bool
	// startDirect starts the daemon the logon task would, for a desktop
	// session where the task does not run (installed over SSH).
	startDirect func(config bootConfig) error
	// wait is how long a restarted daemon gets to answer as the new version;
	// poll is how often it is asked. inPlaceWait is wait for a daemon that
	// restarts itself, which includes its shutdown.
	wait        time.Duration
	inPlaceWait time.Duration
	poll        time.Duration
	// runWait is how long one run of the logon task gets before it is run
	// again.
	runWait time.Duration
}

var defaultDaemonRuntime = func() daemonRuntime {
	return daemonRuntime{
		goos:        bootGOOS,
		runner:      execBootRunner{},
		self:        os.Executable,
		peerPID:     bootControlPeerPID,
		image:       platform.ProcessImagePath,
		parentImage: platform.ParentImage,
		health:      probeDaemonHealth,
		status:      bootDaemonStatus,
		upgrade:     requestInPlaceRestart,
		kill:        killProcess,
		alive:       platform.ProcessAlive,
		startDirect: bootTaskStartDirect,
		wait:        30 * time.Second,
		// The daemon first waits for model replies (restartDrainCap).
		inPlaceWait: restartDrainCap + restartStartAllowance,
		poll:        250 * time.Millisecond,
		runWait:     3 * time.Second,
	}
}

func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

var daemonHealthClient = &http.Client{Timeout: 2 * time.Second}

func probeDaemonHealth(addr string) (daemonHealth, error) {
	response, err := daemonHealthClient.Get("http://" + bootProbeAddr(addr) + "/health")
	if err != nil {
		return daemonHealth{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return daemonHealth{}, fmt.Errorf("health returned http %d", response.StatusCode)
	}
	var health daemonHealth
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&health); err != nil {
		return daemonHealth{}, fmt.Errorf("decode health: %w", err)
	}
	return health, nil
}

// sameExecutable: the daemon's image is this executable, or what an update
// left of it. Linux shows a file replaced since the process started as
// "<path> (deleted)". Windows shows the name the running file has now: an
// update moves it aside to "<path>.old-<n>" (platform.ReplaceFile); paths
// compare without case.
func sameExecutable(image, self, goos string) bool {
	image = strings.TrimSpace(image)
	if image == "" || self == "" {
		return false
	}
	switch goos {
	case "linux":
		image = strings.TrimSuffix(image, " (deleted)")
	case "darwin":
		if resolved, err := filepath.EvalSymlinks(image); err == nil {
			image = resolved
		}
	case "windows":
		image, self = strings.ToLower(filepath.Clean(image)), strings.ToLower(filepath.Clean(self))
		return image == self || platform.IsReplacedCopy(image, self)
	}
	return filepath.Clean(image) == filepath.Clean(self)
}

// supervisorMetadata is how `mewla boot install` registered the daemon on
// this computer, if it did.
func (rt daemonRuntime) supervisorMetadata(stateDir string) (bootMetadata, bool) {
	var path string
	switch rt.goos {
	case "windows":
		path = bootMetadataPath(bootTaskXMLPath(bootConfig{StateDir: stateDir}))
	case "linux":
		unit, err := bootUnitPath()
		if err != nil {
			return bootMetadata{}, false
		}
		path = bootMetadataPath(unit)
	default:
		return bootMetadata{}, false
	}
	metadata, err := readBootMetadata(path)
	if err != nil || !bootSameFilePath(metadata.StateDir, stateDir) {
		return bootMetadata{}, false
	}
	return metadata, true
}

func (rt daemonRuntime) daemonAddr(stateDir string) string {
	if metadata, ok := rt.supervisorMetadata(stateDir); ok && metadata.Addr != "" {
		if metadata.LAN {
			return bootLANAddr
		}
		return metadata.Addr
	}
	return bootDefaultAddr
}

// inspect finds the daemon that owns stateDir when it runs this installation's
// executable. Anything else (no daemon, a development build, another
// installation, a daemon that does not answer as this state directory's
// owner) is not ours to restart, and ok is false.
func (rt daemonRuntime) inspect(stateDir string) (runningDaemon, bool) {
	if rt.goos != "linux" && rt.goos != "windows" && rt.goos != "darwin" {
		return runningDaemon{}, false
	}
	pid, err := rt.peerPID(stateDir)
	if err != nil || pid <= 0 {
		return runningDaemon{}, false
	}
	self, err := rt.self()
	if err != nil {
		return runningDaemon{}, false
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	// The daemon says where it serves and what it runs; daemons older than
	// that are found where `mewla boot install` put them.
	status, statusErr := rt.status(stateDir)
	if statusErr == nil && status.PID != pid {
		return runningDaemon{}, false
	}
	var image string
	if rt.goos == "darwin" {
		// No /proc: the daemon's own word, checked against the kernel's PID.
		if statusErr != nil {
			return runningDaemon{}, false
		}
		image = status.Executable
	} else if image, err = rt.image(pid); err != nil {
		return runningDaemon{}, false
	}
	if !sameExecutable(image, self, rt.goos) {
		return runningDaemon{}, false
	}
	addr := rt.daemonAddr(stateDir)
	if statusErr == nil && status.Addr != "" {
		addr = status.Addr
	}
	health, err := rt.health(addr)
	if err != nil {
		return runningDaemon{}, false
	}
	if id, err := bootStateDaemonID(stateDir); err != nil || id != health.DaemonID {
		return runningDaemon{}, false
	}
	return runningDaemon{PID: pid, Version: health.Version, Addr: addr}, true
}

// restart ends the daemon and starts it again through its supervisor, or
// has it restart itself in place when nothing supervises it, then waits until
// it answers as version want ("" accepts any version).
func (rt daemonRuntime) restart(stateDir string, daemon runningDaemon, want string) error {
	if !rt.supervised(daemon) {
		return rt.restartInPlace(stateDir, daemon, want)
	}
	if rt.goos == "windows" {
		return rt.restartWindows(stateDir, daemon, want)
	}
	// Under the systemd unit too, the daemon restarts itself in place: the same
	// PID (KillMode=process keeps the session host), and it first lets every
	// Worker's model reply finish and keeps the relay's sockets open across
	// the exec. systemctl restart would end the replies still streaming after
	// the stop's short wait and refuse requests until the new daemon listens.
	// Only a daemon too old to restart itself is restarted by systemd.
	err := rt.restartInPlace(stateDir, daemon, want)
	if errors.Is(err, errInPlaceUnsupported) {
		return rt.restartLinux(stateDir, daemon, want)
	}
	return err
}

// supervised: the daemon was started by what `mewla boot install` set up
// (the logon task, the systemd user unit), which can start it again.
func (rt daemonRuntime) supervised(daemon runningDaemon) bool {
	switch rt.goos {
	case "windows":
		task, err := bootTaskFind(rt.runner)
		if err != nil || !task.Registered || !strings.Contains(task.XML, bootTaskMarker) {
			return false
		}
		// The task runs the daemon under a headless console host; one
		// started in a terminal has the shell as its parent, and ends with
		// that terminal. One whose starter is gone was started detached:
		// by `mewla boot install` or a restart over SSH, where the task does
		// not run, or by the installer.
		parent, err := rt.parentImage(daemon.PID)
		if errors.Is(err, platform.ErrParentGone) {
			return true
		}
		return err == nil && strings.EqualFold(parent, "conhost.exe")
	case "linux":
		if _, active, err := bootUnitActive(rt.runner); err != nil || !active {
			return false
		}
		mainPID, err := bootUnitMainPID(rt.runner)
		return err == nil && mainPID == daemon.PID
	}
	return false
}

func (rt daemonRuntime) restartWindows(stateDir string, daemon runningDaemon, want string) error {
	// Windows has no signal to ask a process to stop. The daemon's state is
	// crash safe (SQLite, atomic files); its Sessions live in the session
	// host, which this does not touch.
	if err := rt.kill(daemon.PID); err != nil && rt.alive(daemon.PID) {
		return fmt.Errorf("stop Mewla (PID %d): %w", daemon.PID, err)
	}
	if !rt.waitFor(func() bool { return !rt.alive(daemon.PID) }, 10*time.Second) {
		return fmt.Errorf("Mewla (PID %d) did not stop", daemon.PID)
	}
	// The task ignores a new run while its previous instance (the console
	// host that ran the daemon) is still ending, so it is asked again until
	// the daemon answers.
	task, err := bootTaskFind(rt.runner)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(rt.wait)
	startedDirect := false
	for {
		if output, err := rt.runner.run("schtasks", "/run", "/tn", task.Name); err != nil {
			return fmt.Errorf("schtasks /run: %v: %s", err, strings.TrimSpace(string(output)))
		}
		if rt.waitFor(func() bool { return rt.answersAs(stateDir, daemon.Addr, want) }, rt.runWait) {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		// The task runs only in the desktop session it was registered for;
		// from anywhere else (SSH with nobody at the desktop) it waits for
		// the next login. Start the daemon the task would, as boot install
		// does.
		if !startedDirect && !rt.alive(daemon.PID) {
			if _, err := rt.health(daemon.Addr); err != nil {
				if metadata, ok := rt.supervisorMetadata(stateDir); ok {
					startedDirect = true
					if err := rt.startDirect(bootConfigFromMetadata(metadata)); err != nil {
						return fmt.Errorf("start Mewla: %w", err)
					}
				}
			}
		}
	}
	return rt.notBack(stateDir, daemon, want)
}

func (rt daemonRuntime) restartLinux(stateDir string, daemon runningDaemon, want string) error {
	// KillMode=process: only the daemon stops; the session host and its
	// Sessions keep running.
	if output, err := rt.runner.run("systemctl", "--user", "restart", bootServiceName); err != nil {
		return fmt.Errorf("systemctl --user restart %s: %v: %s", bootServiceName, err, strings.TrimSpace(string(output)))
	}
	if rt.waitFor(func() bool { return rt.answersAs(stateDir, daemon.Addr, want) }, rt.wait) {
		return nil
	}
	return rt.notBack(stateDir, daemon, want)
}

// errInPlaceUnsupported: the running daemon predates daemon_upgrade.
var errInPlaceUnsupported = errors.New("this version of Mewla cannot restart itself; stop it (Ctrl+C in its window) and start it again: mewla")

// requestInPlaceRestart asks the daemon serving stateDir to restart itself
// onto version (control request daemon_upgrade). An error is the daemon's
// refusal, with its cause, or why it could not be asked.
func requestInPlaceRestart(stateDir, version string) error {
	socket, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		return err
	}
	// The daemon runs the new executable once before it answers.
	response, err := control.CallWithTimeout(socket, control.Request{Type: "daemon_upgrade", Version: version}, 30*time.Second)
	if err != nil {
		return err
	}
	if response.OK {
		return nil
	}
	if response.Error == nil {
		return errors.New("the request failed")
	}
	if response.Error.Code == "unknown_request" {
		return errInPlaceUnsupported
	}
	return errors.New(response.Error.Message)
}

// restartInPlace has a daemon nothing supervises restart itself onto the
// installed executable (daemon_upgrade.go). On Linux and macOS it keeps its
// PID; on Windows the old process waits for the new one in its console.
func (rt daemonRuntime) restartInPlace(stateDir string, daemon runningDaemon, want string) error {
	name := fmt.Sprintf("Mewla %s (PID %d)", daemon.Version, daemon.PID)
	if daemon.Version == "" {
		name = fmt.Sprintf("Mewla (PID %d)", daemon.PID)
	}
	onto := "the installed version"
	if want != "" {
		onto = want
	}
	// Restarted onto the same version, the old daemon answers as want until
	// it stops: only a new start time (or a new PID, on Windows) or a gap
	// in its answers shows the restart happened.
	sameVersion := want == "" || want == daemon.Version
	before, statusErr := rt.status(stateDir)
	if err := rt.upgrade(stateDir, want); err != nil {
		return fmt.Errorf("%s did not restart onto %s: %w", name, onto, err)
	}
	stopped, gap := false, false
	restarted := func() bool {
		if !sameVersion {
			return true
		}
		if statusErr == nil && !before.Started.IsZero() {
			now, err := rt.status(stateDir)
			return err == nil && (now.PID != before.PID || !now.Started.Equal(before.Started))
		}
		return gap
	}
	answered := rt.waitFor(func() bool {
		if rt.answersAs(stateDir, daemon.Addr, want) {
			return restarted()
		}
		gap = true
		stopped = !rt.alive(daemon.PID)
		return stopped
	}, rt.inPlaceWait)
	if answered && !stopped {
		return nil
	}
	if cause, err := os.ReadFile(inPlaceRestartFailurePath(stateDir)); err == nil && len(strings.TrimSpace(string(cause))) > 0 {
		return fmt.Errorf("%s stopped and did not restart onto %s: %s", name, onto, strings.TrimSpace(string(cause)))
	}
	if stopped {
		return fmt.Errorf("%s stopped and did not start again; its window says why", name)
	}
	if health, err := rt.health(daemon.Addr); err == nil && health.Version == daemon.Version {
		return fmt.Errorf("%s is still running %s after %s: it has not finished stopping", name, daemon.Version, rt.inPlaceWait)
	}
	return fmt.Errorf("%s did not answer as %s on %s within %s; its window says why", name, onto, daemon.Addr, rt.inPlaceWait)
}

func (rt daemonRuntime) notBack(stateDir string, daemon runningDaemon, want string) error {
	what := "Mewla"
	if want != "" {
		what = "Mewla " + want
	}
	return fmt.Errorf("%s did not answer on %s after the restart; see %s", what, daemon.Addr, filepath.Join(stateDir, "logs", "daemon.log"))
}

// answersAs: the daemon on addr is this state directory's owner and runs
// version want.
func (rt daemonRuntime) answersAs(stateDir, addr, want string) bool {
	health, err := rt.health(addr)
	if err != nil {
		return false
	}
	if id, err := bootStateDaemonID(stateDir); err != nil || id != health.DaemonID {
		return false
	}
	return want == "" || health.Version == want
}

func (rt daemonRuntime) waitFor(done func() bool, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		if done() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(rt.poll)
	}
}

func bootConfigFromMetadata(metadata bootMetadata) bootConfig {
	return bootConfig{
		Binary:   metadata.Binary,
		StateDir: metadata.StateDir,
		Addr:     metadata.Addr,
		LAN:      metadata.LAN,
		WorkDir:  metadata.WorkDir,
		PathEnv:  metadata.PathEnv,
		Home:     metadata.Home,
	}
}

// restartAfterUpdate is `mewla update` on Linux and Windows once the new
// executable is in place: the daemon still running the old one restarts onto
// it.
func restartAfterUpdate(rt daemonRuntime, stateDir, from, to string, out io.Writer) error {
	daemon, ok := rt.inspect(stateDir)
	if !ok || daemon.Version == to {
		fmt.Fprintf(out, "Updated Mewla %s → %s.\n", from, to)
		return nil
	}
	fmt.Fprintf(out, "Updated Mewla %s → %s. Restarting Mewla now to use it; Sessions and Workers keep running.\n", from, to)
	if err := rt.restart(stateDir, daemon, to); err != nil {
		return err
	}
	fmt.Fprintf(out, "Mewla %s is running.\n", to)
	return nil
}

// ensureCurrentDaemon runs before a command a person typed that talks to the
// daemon. A daemon of this installation older than this CLI (left running by
// an update) is restarted onto it, or the person is told what to run. Not in
// a terminal (a script, a command over SSH with its output kept) it is only
// told: nothing restarts under a script. Inside a Session (an agent running
// mewla) nothing is said or restarted: the daemon is what keeps that agent's
// Session going.
func ensureCurrentDaemon(rt daemonRuntime, stateDir string, interactive bool, out io.Writer) {
	if releaseBuild != "1" || insideSession() {
		return
	}
	daemon, ok := rt.inspect(stateDir)
	if !ok || !selfupdate.Older(daemon.Version, Version) {
		return
	}
	if !interactive {
		fmt.Fprintf(out, "Mewla %s is still running; this is %s. Restart it onto %s: mewla restart\n", daemon.Version, Version, Version)
		return
	}
	fmt.Fprintf(out, "Mewla %s is still running; this is %s. Restarting it to %s; Sessions and Workers keep running.\n", daemon.Version, Version, Version)
	if err := rt.restart(stateDir, daemon, Version); err != nil {
		fmt.Fprintln(out, err)
		return
	}
	fmt.Fprintf(out, "Mewla %s is running.\n", Version)
}

func insideSession() bool {
	return os.Getenv("MEWLA_SESSION_ID") != "" || os.Getenv("MEWLA_WORKER_ID") != ""
}

// daemonCommands are the commands a person types that talk to the running
// daemon; ensureCurrentDaemon runs before them and before a bare `mewla`.
var daemonCommands = map[string]bool{
	"pair": true, "web": true, "devices": true, "attach": true, "worker": true,
	"brain": true, "memory": true, "calendar": true, "service": true,
	"connections": true, "watch": true, "friends": true, "providers": true,
	"telegram": true, "agents": true, "computers": true, "screen": true,
	"resources": true, "gcloud": true, "aws": true, "gws": true,
	"agent-use": true,
}

// commandStateDir is the -state-dir a command line names, or "" for the
// default.
func commandStateDir(args []string) string {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		for _, name := range []string{"-state-dir", "--state-dir"} {
			if arg == name && index+1 < len(args) {
				return args[index+1]
			}
			if value, ok := strings.CutPrefix(arg, name+"="); ok {
				return value
			}
		}
	}
	return ""
}

// runRestartCommand is `mewla restart`: the running daemon stops and starts
// again from the installed executable, through the logon task or systemd
// unit that started it, or in place when nothing supervises it.
func runRestartCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla restart", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "state directory of the Mewla to restart")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla restart [flags]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Restart the running Mewla onto the installed version, in the terminal it runs in.")
		fmt.Fprintln(stderr, "Sessions and Workers keep running.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	resolved, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return err
	}
	rt := defaultDaemonRuntime()
	daemon, ok := rt.inspect(resolved)
	if !ok && bootGOOS == "darwin" {
		if _, managed := bootLaunchdReinstallArgs(); managed {
			return errors.New("on macOS, quit Mewla from the menu bar and open it again")
		}
	}
	if !ok {
		if held, _ := bootStateLockHeld(resolved); !held {
			return fmt.Errorf("Mewla is not running; start it with: %s", startCommand(*stateDir))
		}
		return errors.New("the Mewla running here is not this installation, so it was not restarted")
	}
	if err := rt.restart(resolved, daemon, Version); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Mewla %s is running.\n", Version)
	return nil
}

// commandStateDirResolved is commandStateDir resolved the way the command
// will resolve it.
func commandStateDirResolved(args []string) string {
	resolved, err := auth.ResolveStorageDir(commandStateDir(args))
	if err != nil {
		return ""
	}
	return resolved
}

// runningMewlaName names the daemon serving stateDir for a message: "Mewla
// 0.4.5 (PID 1234)" when it can be told, else "Mewla".
func runningMewlaName(stateDir string) string {
	name := "Mewla"
	rt := defaultDaemonRuntime()
	if health, err := rt.health(rt.daemonAddr(stateDir)); err == nil && health.Version != "" {
		if id, err := bootStateDaemonID(stateDir); err == nil && id == health.DaemonID {
			name += " " + health.Version
		}
	}
	if pid, err := rt.peerPID(stateDir); err == nil && pid > 0 {
		name += fmt.Sprintf(" (PID %d)", pid)
	}
	return name
}
