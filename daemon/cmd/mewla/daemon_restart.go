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
	"github.com/daoleno/mewla/daemon/platform"
	"github.com/daoleno/mewla/daemon/selfupdate"
)

// A running daemon keeps the executable it started from. `mewla update`
// replaces the file, so on Linux and Windows the daemon goes on running the
// old version until it restarts; a newer CLI talking to it is then the
// skew a person cannot see. The CLI restarts the daemon through whatever
// started it (the logon task, the systemd user unit), which ends only the
// daemon: the session host and every Session and Worker keep running.
// macOS restarts through Mewla.app instead (bootLaunchdApplyUpdate).

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
	kill        func(pid int) error
	alive       func(pid int) bool
	// startDirect starts the daemon the logon task would, for a desktop
	// session where the task does not run (installed over SSH).
	startDirect func(config bootConfig) error
	// wait is how long a restarted daemon gets to answer as the new version;
	// poll is how often it is asked.
	wait time.Duration
	poll time.Duration
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
		kill:        killProcess,
		alive:       platform.ProcessAlive,
		startDirect: bootTaskStartDirect,
		wait:        30 * time.Second,
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
	if rt.goos != "linux" && rt.goos != "windows" {
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
	image, err := rt.image(pid)
	if err != nil || !sameExecutable(image, self, rt.goos) {
		return runningDaemon{}, false
	}
	addr := rt.daemonAddr(stateDir)
	health, err := rt.health(addr)
	if err != nil {
		return runningDaemon{}, false
	}
	if id, err := bootStateDaemonID(stateDir); err != nil || id != health.DaemonID {
		return runningDaemon{}, false
	}
	return runningDaemon{PID: pid, Version: health.Version, Addr: addr}, true
}

// errDaemonStartedByHand: the daemon was not started by the logon task or the
// systemd unit, so the CLI does not stop it; the message says what to run.
type errDaemonStartedByHand struct {
	daemon runningDaemon
	goos   string
}

func (e *errDaemonStartedByHand) Error() string {
	version := e.daemon.Version
	if version == "" {
		version = "an earlier version"
	}
	stop := fmt.Sprintf("kill %d", e.daemon.PID)
	if e.goos == "windows" {
		stop = fmt.Sprintf("Stop-Process -Id %d", e.daemon.PID)
	}
	return fmt.Sprintf("Mewla %s is running from a terminal (PID %d), so it was not restarted. Stop it with Ctrl+C in its window, or run: %s — then start it again with: mewla", version, e.daemon.PID, stop)
}

// restart ends the daemon and starts it again through its supervisor, then
// waits until it answers as version want ("" accepts any version).
func (rt daemonRuntime) restart(stateDir string, daemon runningDaemon, want string) error {
	if !rt.supervised(daemon) {
		return &errDaemonStartedByHand{daemon: daemon, goos: rt.goos}
	}
	if rt.goos == "windows" {
		return rt.restartWindows(stateDir, daemon, want)
	}
	return rt.restartLinux(stateDir, daemon, want)
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
		// started in a terminal has the shell as its parent.
		parent, err := rt.parentImage(daemon.PID)
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
	if !rt.supervised(daemon) {
		// The update is installed; only the restart is left to the person.
		fmt.Fprintf(out, "Updated Mewla %s → %s.\n%s\n", from, to, &errDaemonStartedByHand{daemon: daemon, goos: rt.goos})
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
// an update) is restarted onto it, or the person is told what to run. Inside
// a Session (an agent running mewla) nothing is restarted: the daemon is what
// keeps that agent's Session going.
func ensureCurrentDaemon(rt daemonRuntime, stateDir string, interactive bool, out io.Writer) {
	if releaseBuild != "1" || !interactive || insideSession() {
		return
	}
	daemon, ok := rt.inspect(stateDir)
	if !ok || !selfupdate.Older(daemon.Version, Version) {
		return
	}
	if !rt.supervised(daemon) {
		fmt.Fprintf(out, "This is Mewla %s. %s\n", Version, &errDaemonStartedByHand{daemon: daemon, goos: rt.goos})
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
// unit that started it.
func runRestartCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla restart", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "state directory of the Mewla to restart")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla restart [flags]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Restart Mewla running in the background, onto the installed version.")
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
	if bootGOOS == "darwin" {
		return errors.New("on macOS, quit Mewla from the menu bar and open it again")
	}
	resolved, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return err
	}
	rt := defaultDaemonRuntime()
	daemon, ok := rt.inspect(resolved)
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
