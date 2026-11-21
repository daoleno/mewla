package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/platform"
)

// fakeDaemon is one daemon process as daemonRuntime sees it: killed, it stops
// answering; the logon task or unit starts its successor at the new version.
type fakeDaemon struct {
	mu       sync.Mutex
	daemonID string
	pid      int
	version  string
	running  bool
	killed   []int
	// upgrades are the versions daemon_upgrade was asked for; refuse is the
	// daemon's refusal, and exitOnUpgrade makes it stop instead of coming back.
	upgrades      []string
	refuse        error
	exitOnUpgrade bool
	addr          string
	// started is when the daemon process image started (daemon_status);
	// oldAnswers is how many health answers the old image still gives after
	// accepting a restart before it starts again.
	started    time.Time
	oldAnswers int
	pending    string
	healthAsks int
	// ignoreRuns is how many logon task runs are dropped (the previous
	// instance is still ending) before one starts the daemon.
	ignoreRuns int
}

func (d *fakeDaemon) start(version string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ignoreRuns > 0 {
		d.ignoreRuns--
		return
	}
	d.pid++
	d.version, d.running = version, true
}

func (d *fakeDaemon) runtime(t *testing.T, goos string, runner *fakeBootRunner, self string) daemonRuntime {
	t.Helper()
	return daemonRuntime{
		goos:   goos,
		runner: runner,
		self:   func() (string, error) { return self, nil },
		peerPID: func(string) (int, error) {
			d.mu.Lock()
			defer d.mu.Unlock()
			if !d.running {
				return 0, control.ErrNotRunning
			}
			return d.pid, nil
		},
		image: func(int) (string, error) { return self, nil },
		status: func(string) (control.DaemonStatus, error) {
			d.mu.Lock()
			defer d.mu.Unlock()
			if !d.running {
				return control.DaemonStatus{}, control.ErrNotRunning
			}
			return control.DaemonStatus{PID: d.pid, Executable: self, Version: d.version, Addr: d.addr, Started: d.started}, nil
		},
		upgrade: func(_ string, version string) error {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.upgrades = append(d.upgrades, version)
			if d.refuse != nil {
				return d.refuse
			}
			if d.exitOnUpgrade {
				d.running = false
				return nil
			}
			// execve: the same PID answers as the new version, once the old
			// image has finished answering.
			if d.oldAnswers > 0 {
				d.pending = version
				return nil
			}
			d.version, d.started = version, d.started.Add(time.Second)
			return nil
		},
		parentImage: func(int) (string, error) { return "conhost.exe", nil },
		health: func(string) (daemonHealth, error) {
			d.mu.Lock()
			defer d.mu.Unlock()
			if !d.running {
				return daemonHealth{}, errors.New("connection refused")
			}
			d.healthAsks++
			if d.pending != "" {
				if d.oldAnswers > 0 {
					d.oldAnswers--
				} else {
					d.version, d.started, d.pending = d.pending, d.started.Add(time.Second), ""
				}
			}
			return daemonHealth{DaemonID: d.daemonID, Version: d.version}, nil
		},
		kill: func(pid int) error {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.killed = append(d.killed, pid)
			if pid == d.pid {
				d.running = false
			}
			return nil
		},
		alive: func(pid int) bool {
			d.mu.Lock()
			defer d.mu.Unlock()
			return d.running && pid == d.pid
		},
		startDirect: func(bootConfig) error {
			t.Fatal("started directly")
			return nil
		},
		wait:        2 * time.Second,
		inPlaceWait: 2 * time.Second,
		poll:        5 * time.Millisecond,
		runWait:     50 * time.Millisecond,
	}
}

func restartStateDir(t *testing.T) (string, string) {
	t.Helper()
	stateDir := t.TempDir()
	manager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	daemonID, err := bootStateDaemonID(stateDir)
	if err != nil || daemonID != manager.DaemonID() {
		t.Fatalf("state daemon id %q (%v), manager %q", daemonID, err, manager.DaemonID())
	}
	return stateDir, daemonID
}

func windowsTaskRunner(t *testing.T, daemon *fakeDaemon, version string) *fakeBootRunner {
	withTaskUser(t)
	runner := &fakeBootRunner{
		responses: map[string]string{
			"schtasks /query /tn Mewla (ana) /xml": "<Task><Description>Starts Mewla when you log in. " + bootTaskMarker + ".</Description></Task>",
		},
		handlers: map[string]func(){
			"schtasks /run /tn Mewla (ana)": func() { daemon.start(version) },
		},
	}
	return runner
}

func TestUpdateRestartsTheOldDaemonThroughTheLogonTask(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 4242, version: "0.4.5", running: true}
	runner := windowsTaskRunner(t, daemon, "0.4.6")
	rt := daemon.runtime(t, "windows", runner, `C:\Users\p\AppData\Local\Programs\Mewla\mewla.exe`)
	// Windows names a running executable by where it is now: the update
	// moved it aside.
	rt.image = func(int) (string, error) {
		return `C:\Users\p\AppData\Local\Programs\Mewla\mewla.exe.old-1763200000000000000`, nil
	}

	var out bytes.Buffer
	if err := restartAfterUpdate(rt, stateDir, "0.4.5", "0.4.6", &out); err != nil {
		t.Fatalf("restart after update: %v\n%s", err, out.String())
	}
	if len(daemon.killed) != 1 || daemon.killed[0] != 4242 {
		t.Fatalf("killed %v, want the old daemon 4242", daemon.killed)
	}
	if runner.count("schtasks /run /tn Mewla (ana)") != 1 || daemon.version != "0.4.6" {
		t.Fatalf("task runs %d, daemon version %s", runner.count("schtasks /run /tn Mewla (ana)"), daemon.version)
	}
	if strings.Contains(out.String(), "Stop and start") ||
		!strings.Contains(out.String(), "Restarting Mewla now") ||
		!strings.Contains(out.String(), "Mewla 0.4.6 is running.") {
		t.Fatalf("output %q", out.String())
	}
}

func TestRestartRunsTheLogonTaskAgainWhileItsLastInstanceEnds(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 10, version: "0.4.5", running: true, ignoreRuns: 2}
	runner := windowsTaskRunner(t, daemon, "0.4.6")
	rt := daemon.runtime(t, "windows", runner, `C:\Mewla\mewla.exe`)

	found, ok := rt.inspect(stateDir)
	if !ok {
		t.Fatal("old daemon not found")
	}
	if err := rt.restart(stateDir, found, "0.4.6"); err != nil {
		t.Fatal(err)
	}
	if got := runner.count("schtasks /run /tn Mewla (ana)"); got != 3 {
		t.Fatalf("task runs %d, want 3 (two dropped)", got)
	}
}

// The owner's report: a Mewla started in a terminal was left on the old
// version with instructions to kill it. It restarts itself in place instead.
func TestUpdateRestartsADaemonStartedInATerminalInPlace(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 4054216, version: "0.4.8", running: true}
	runner := &fakeBootRunner{responses: map[string]string{}, failures: map[string]bool{"systemctl --user is-active mewla.service": true}}
	rt := daemon.runtime(t, "linux", runner, "/home/p/.local/bin/mewla")
	rt.image = func(int) (string, error) { return "/home/p/.local/bin/mewla (deleted)", nil }

	var out bytes.Buffer
	if err := restartAfterUpdate(rt, stateDir, "0.4.8", "0.4.9", &out); err != nil {
		t.Fatalf("restart after update: %v\n%s", err, out.String())
	}
	if len(daemon.upgrades) != 1 || daemon.upgrades[0] != "0.4.9" || len(daemon.killed) != 0 {
		t.Fatalf("upgrades %v, killed %v", daemon.upgrades, daemon.killed)
	}
	if daemon.pid != 4054216 || daemon.version != "0.4.9" {
		t.Fatalf("daemon PID %d version %s", daemon.pid, daemon.version)
	}
	for _, unwanted := range []string{"kill", "was not restarted", "start it again"} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("output %q says %q", out.String(), unwanted)
		}
	}
	if !strings.Contains(out.String(), "Restarting Mewla now") || !strings.Contains(out.String(), "Mewla 0.4.9 is running.") {
		t.Fatalf("output %q", out.String())
	}
}

// A default Windows install always has the logon task; a daemon started in
// a terminal on top of it is still the person's: it restarts in its own
// console, not through the task.
func TestRestartLeavesADaemonStartedInATerminalBesideTheTask(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 88, version: "0.4.5", running: true}
	runner := windowsTaskRunner(t, daemon, "0.4.6")
	rt := daemon.runtime(t, "windows", runner, `C:\Mewla\mewla.exe`)
	rt.parentImage = func(int) (string, error) { return "powershell.exe", nil }

	var out bytes.Buffer
	if err := restartAfterUpdate(rt, stateDir, "0.4.5", "0.4.6", &out); err != nil {
		t.Fatal(err)
	}
	if len(daemon.killed) != 0 || runner.count("schtasks /run /tn Mewla (ana)") != 0 {
		t.Fatalf("killed %v, task runs %d", daemon.killed, runner.count("schtasks /run /tn Mewla (ana)"))
	}
	if len(daemon.upgrades) != 1 || daemon.version != "0.4.6" || strings.Contains(out.String(), "Stop-Process") {
		t.Fatalf("upgrades %v, version %s, output %q", daemon.upgrades, daemon.version, out.String())
	}
}

// mewla restart with nothing new: the old daemon answers as the wanted
// version until it stops, so only its new start time says it restarted.
func TestInPlaceRestartOntoTheSameVersionWaitsForTheNewStart(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	started := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 40, version: "0.4.9", running: true, started: started, oldAnswers: 5}
	rt := daemon.runtime(t, "linux", &fakeBootRunner{failures: map[string]bool{"systemctl --user is-active mewla.service": true}}, "/home/p/.local/bin/mewla")
	found, ok := rt.inspect(stateDir)
	if !ok {
		t.Fatal("daemon not found")
	}
	asksBefore := daemon.healthAsks
	if err := rt.restart(stateDir, found, "0.4.9"); err != nil {
		t.Fatal(err)
	}
	if daemon.started.Equal(started) || daemon.pending != "" {
		t.Fatalf("returned while the old daemon still answered (started %v, pending %q)", daemon.started, daemon.pending)
	}
	if asked := daemon.healthAsks - asksBefore; asked < 6 {
		t.Fatalf("health asked %d times; the old daemon answered 5", asked)
	}
}

// Each refusal reaches the person with the daemon's own cause, and the
// update stays installed.
func TestInPlaceRestartRefusalSaysWhy(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 77, version: "0.4.5", running: true,
		refuse: errors.New("/home/p/.local/bin/mewla is Mewla 0.4.5, not 0.4.6")}
	rt := daemon.runtime(t, "linux", &fakeBootRunner{failures: map[string]bool{"systemctl --user is-active mewla.service": true}}, "/home/p/.local/bin/mewla")

	var out bytes.Buffer
	err := restartAfterUpdate(rt, stateDir, "0.4.5", "0.4.6", &out)
	if err == nil || err.Error() != "Mewla 0.4.5 (PID 77) did not restart onto 0.4.6: /home/p/.local/bin/mewla is Mewla 0.4.5, not 0.4.6" {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(out.String(), "Updated Mewla 0.4.5 → 0.4.6.") || strings.Contains(out.String(), "is running.") {
		t.Fatalf("output %q", out.String())
	}
}

// A daemon that accepted but stopped without coming back: the cause it
// recorded is what the person reads.
func TestInPlaceRestartThatDiedReportsTheRecordedCause(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 77, version: "0.4.5", running: true, exitOnUpgrade: true}
	rt := daemon.runtime(t, "linux", &fakeBootRunner{failures: map[string]bool{"systemctl --user is-active mewla.service": true}}, "/home/p/.local/bin/mewla")
	found, ok := rt.inspect(stateDir)
	if !ok {
		t.Fatal("daemon not found")
	}

	err := rt.restart(stateDir, found, "0.4.6")
	if err == nil || !strings.Contains(err.Error(), "Mewla 0.4.5 (PID 77) stopped and did not start again") {
		t.Fatalf("without a recorded cause: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "run"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inPlaceRestartFailurePath(stateDir), []byte("could not start /home/p/.local/bin/mewla: exec format error\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = rt.restart(stateDir, found, "0.4.6")
	if err == nil || err.Error() != "Mewla 0.4.5 (PID 77) stopped and did not restart onto 0.4.6: could not start /home/p/.local/bin/mewla: exec format error" {
		t.Fatalf("with a recorded cause: %v", err)
	}
}

// The daemon says where it serves: one started with its own -addr is found
// there, not at the default address.
func TestInspectFindsADaemonOnItsOwnAddress(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 5, version: "0.4.8", running: true, addr: "127.0.0.1:9931"}
	rt := daemon.runtime(t, "linux", &fakeBootRunner{}, "/home/p/.local/bin/mewla")
	health := rt.health
	var asked []string
	rt.health = func(addr string) (daemonHealth, error) {
		asked = append(asked, addr)
		return health(addr)
	}
	found, ok := rt.inspect(stateDir)
	if !ok || found.Addr != "127.0.0.1:9931" || len(asked) != 1 || asked[0] != "127.0.0.1:9931" {
		t.Fatalf("found %+v (%t), health asked %v", found, ok, asked)
	}
}

// macOS has no /proc: a terminal daemon is known by its own daemon_status,
// for the PID the kernel gives for the control socket.
func TestInspectOnMacOSUsesTheDaemonsStatus(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 6, version: "0.4.8", running: true}
	rt := daemon.runtime(t, "darwin", &fakeBootRunner{}, "/Users/p/.local/bin/mewla")
	rt.image = func(int) (string, error) { return "", errors.New("no /proc") }
	if _, ok := rt.inspect(stateDir); !ok {
		t.Fatal("terminal daemon on macOS not found")
	}
	status := rt.status
	rt.status = func(dir string) (control.DaemonStatus, error) {
		got, err := status(dir)
		got.Executable = "/Users/p/Applications/Mewla.app/Contents/MacOS/mewla"
		return got, err
	}
	if _, ok := rt.inspect(stateDir); ok {
		t.Fatal("Mewla.app's daemon was taken for this executable's")
	}
	rt.status = func(string) (control.DaemonStatus, error) {
		return control.DaemonStatus{PID: 999, Executable: "/Users/p/.local/bin/mewla"}, nil
	}
	if _, ok := rt.inspect(stateDir); ok {
		t.Fatal("a status for another PID than the socket's was believed")
	}
}

// Started detached (by boot install or a restart over SSH, where the task
// does not run, or by the installer), the daemon's starter has ended: it is
// Mewla's, and restarts like one the task started.
func TestRestartTakesADaemonStartedDetached(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 89, version: "0.4.7", running: true}
	runner := windowsTaskRunner(t, daemon, "0.4.8")
	rt := daemon.runtime(t, "windows", runner, `C:\Mewla\mewla.exe`)
	rt.parentImage = func(int) (string, error) { return "", fmt.Errorf("parent 7 of 89: %w", platform.ErrParentGone) }

	var out bytes.Buffer
	if err := restartAfterUpdate(rt, stateDir, "0.4.7", "0.4.8", &out); err != nil {
		t.Fatal(err)
	}
	if daemon.version != "0.4.8" || len(daemon.killed) != 1 || !strings.Contains(out.String(), "Mewla 0.4.8 is running.") {
		t.Fatalf("daemon %s, killed %v, output %q", daemon.version, daemon.killed, out.String())
	}
}

// Mewla for Mac runs `mewla restart -if-older` when it starts: a daemon left on the version an update replaced
// restarts onto the app's, one that is current is left alone.
func TestRestartIfOlderLeavesACurrentDaemonAlone(t *testing.T) {
	previousVersion := Version
	Version = "0.4.8"
	t.Cleanup(func() { Version = previousVersion })
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 90, version: "0.4.7", running: true}
	runner := windowsTaskRunner(t, daemon, "0.4.8")
	rt := daemon.runtime(t, "windows", runner, `C:\Mewla\mewla.exe`)
	rt.parentImage = func(int) (string, error) { return "", fmt.Errorf("parent 7 of 90: %w", platform.ErrParentGone) }

	var out bytes.Buffer
	if err := restartOntoInstalled(rt, stateDir, "", true, &out); err != nil {
		t.Fatal(err)
	}
	if daemon.version != "0.4.8" || len(daemon.killed) != 1 || !strings.Contains(out.String(), "Mewla 0.4.8 is running.") {
		t.Fatalf("an older daemon: version %s, killed %v, output %q", daemon.version, daemon.killed, out.String())
	}
	out.Reset()
	if err := restartOntoInstalled(rt, stateDir, "", true, &out); err != nil {
		t.Fatal(err)
	}
	if len(daemon.killed) != 1 || !strings.Contains(out.String(), "Mewla 0.4.8 is running.") {
		t.Fatalf("a current daemon was restarted: killed %v, output %q", daemon.killed, out.String())
	}
	// Without the flag it restarts all the same.
	if err := restartOntoInstalled(rt, stateDir, "", false, &out); err != nil {
		t.Fatal(err)
	}
	if len(daemon.killed) != 2 {
		t.Fatalf("plain restart killed %v", daemon.killed)
	}
}

// Under the systemd unit the daemon restarts itself in place too (it drains
// Workers' model replies and keeps the relay's sockets open across the exec);
// systemd restarts only a daemon too old to do that.
func TestRestartUsesTheSystemdUnitOnlyForADaemonThatCannotRestartItself(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	for _, test := range []struct {
		name    string
		mainPID string
		refuse  error
		systemd bool
	}{
		{"unit runs it", "500", nil, false},
		{"unit runs an old one", "500", errInPlaceUnsupported, true},
		{"started by hand", "0", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			daemon := &fakeDaemon{daemonID: daemonID, pid: 500, version: "0.4.5", running: true, refuse: test.refuse}
			runner := &fakeBootRunner{
				responses: map[string]string{
					"systemctl --user is-active mewla.service":               "active",
					"systemctl --user show mewla.service -p MainPID --value": test.mainPID,
				},
				handlers: map[string]func(){
					"systemctl --user restart mewla.service": func() {
						daemon.mu.Lock()
						daemon.running = false
						daemon.mu.Unlock()
						daemon.start("0.4.6")
					},
				},
			}
			rt := daemon.runtime(t, "linux", runner, "/home/p/.local/bin/mewla")
			rt.image = func(int) (string, error) { return "/home/p/.local/bin/mewla (deleted)", nil }
			var out bytes.Buffer
			err := restartAfterUpdate(rt, stateDir, "0.4.5", "0.4.6", &out)
			systemd := runner.count("systemctl --user restart mewla.service") == 1
			if systemd != test.systemd || err != nil {
				t.Fatalf("systemd restart %t, want %t (err %v)", systemd, test.systemd, err)
			}
			if daemon.version != "0.4.6" {
				t.Fatalf("daemon runs %s after the restart", daemon.version)
			}
			if !test.systemd && len(daemon.upgrades) != 1 {
				t.Fatalf("in-place restarts %v", daemon.upgrades)
			}
		})
	}
}

func TestOnlyThisInstallationsDaemonIsRestarted(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 9, version: "0.4.5", running: true}
	runner := windowsTaskRunner(t, daemon, "0.4.6")
	rt := daemon.runtime(t, "linux", runner, "/home/p/.local/bin/mewla")
	// The owner's development daemon runs another executable.
	rt.image = func(int) (string, error) { return "/home/p/workspace/mewla/daemon/tmp/mewla-dev", nil }
	if _, ok := rt.inspect(stateDir); ok {
		t.Fatal("a daemon of another executable was taken for this installation's")
	}
	// A daemon answering for another state directory is not this one's owner.
	rt = daemon.runtime(t, "linux", runner, "/home/p/.local/bin/mewla")
	daemon.daemonID = "other"
	if _, ok := rt.inspect(stateDir); ok {
		t.Fatal("a daemon of another state directory was taken for this one's owner")
	}
}

func TestSameExecutable(t *testing.T) {
	for _, test := range []struct {
		image, self, goos string
		same              bool
	}{
		{"/u/.local/bin/mewla (deleted)", "/u/.local/bin/mewla", "linux", true},
		{"/u/.local/bin/mewla", "/u/.local/bin/mewla", "linux", true},
		{"/u/dev/mewla", "/u/.local/bin/mewla", "linux", false},
		{`C:\Users\P\AppData\Local\Programs\Mewla\mewla.exe`, `c:\users\p\appdata\local\programs\mewla\mewla.exe`, "windows", true},
		{`C:\Users\P\AppData\Local\Programs\Mewla\mewla.exe.old-639303505113571771`, `C:\Users\P\AppData\Local\Programs\Mewla\mewla.exe`, "windows", true},
		{`C:\Other\mewla.exe.old-1`, `C:\Mewla\mewla.exe`, "windows", false},
		{`C:\Mewla\mewla-dev.exe`, `C:\Mewla\mewla.exe`, "windows", false},
		{"", "/u/.local/bin/mewla", "linux", false},
	} {
		if got := sameExecutable(test.image, test.self, test.goos); got != test.same {
			t.Errorf("sameExecutable(%q, %q, %s) = %t", test.image, test.self, test.goos, got)
		}
	}
}

func TestOlderDaemonRestartsBeforeACommandButNotInsideASession(t *testing.T) {
	previousBuild, previousVersion := releaseBuild, Version
	releaseBuild, Version = "1", "0.4.7"
	t.Cleanup(func() { releaseBuild, Version = previousBuild, previousVersion })
	t.Setenv("MEWLA_SESSION_ID", "")
	t.Setenv("MEWLA_WORKER_ID", "")

	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 30, version: "0.4.6", running: true}
	runner := windowsTaskRunner(t, daemon, "0.4.7")
	rt := daemon.runtime(t, "windows", runner, `C:\Mewla\mewla.exe`)

	t.Setenv("MEWLA_WORKER_ID", "w1")
	ensureCurrentDaemon(rt, stateDir, true, &bytes.Buffer{})
	if len(daemon.killed) != 0 {
		t.Fatal("a Worker's mewla restarted the daemon")
	}
	t.Setenv("MEWLA_WORKER_ID", "")
	// Not in a terminal it is said, not done.
	var said bytes.Buffer
	ensureCurrentDaemon(rt, stateDir, false, &said)
	if len(daemon.killed) != 0 {
		t.Fatal("a non-interactive mewla restarted the daemon")
	}
	if !strings.Contains(said.String(), "Mewla 0.4.6 is still running; this is 0.4.7. Restart it onto 0.4.7: mewla restart") {
		t.Fatalf("non-interactive mewla said %q", said.String())
	}

	var out bytes.Buffer
	ensureCurrentDaemon(rt, stateDir, true, &out)
	if daemon.version != "0.4.7" || !strings.Contains(out.String(), "Mewla 0.4.6 is still running") {
		t.Fatalf("daemon %s, output %q", daemon.version, out.String())
	}
	// Current now: nothing more happens.
	ensureCurrentDaemon(rt, stateDir, true, &out)
	if len(daemon.killed) != 1 {
		t.Fatalf("killed %v", daemon.killed)
	}
}

func TestCommandStateDir(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"web"}, ""},
		{[]string{"web", "-state-dir", "/s"}, "/s"},
		{[]string{"devices", "list", "--state-dir=/t"}, "/t"},
	} {
		if got := commandStateDir(test.args); got != test.want {
			t.Errorf("commandStateDir(%q) = %q, want %q", test.args, got, test.want)
		}
	}
}

// slowPairHandler answers pair after delay, as a daemon whose durable write
// is held up (Windows antivirus scanning a fresh file) does.
type slowPairHandler struct {
	app   *controlApp
	delay time.Duration
}

func (h slowPairHandler) HandleControlRequest(request control.Request) control.Response {
	if request.Type == "pair" {
		time.Sleep(h.delay)
	}
	return h.app.HandleControlRequest(request)
}

// The reported failure: `mewla web` gave up on a running daemon whose pair
// answer took over a second ("read control response: i/o timeout").
func TestPairWaitsForADaemonSlowerThanASecond(t *testing.T) {
	stateDir := shortControlStateDir(t)
	lifecycle, manager, err := acquireDaemonAuthOwner(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle.Close()
	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- (&control.Server{
			Path:    socketPath,
			Handler: slowPairHandler{app: &controlApp{auth: manager}, delay: 1500 * time.Millisecond},
		}).Run(ctx)
	}()
	defer func() {
		stop()
		<-done
	}()
	waitForDeviceControlReady(t, socketPath)

	pairing, err := requestPairingToken(stateDir)
	if err != nil {
		t.Fatalf("pair against a daemon answering in 1.5 s: %v", err)
	}
	if pairing.Token == "" {
		t.Fatal("no token")
	}
}

func TestPairTimeoutSaysWhatToRun(t *testing.T) {
	previous := deviceControlRequestTimeout
	deviceControlRequestTimeout = 200 * time.Millisecond
	t.Cleanup(func() { deviceControlRequestTimeout = previous })
	stateDir := shortControlStateDir(t)
	lifecycle, manager, err := acquireDaemonAuthOwner(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle.Close()
	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- (&control.Server{
			Path:    socketPath,
			Handler: slowPairHandler{app: &controlApp{auth: manager}, delay: time.Second},
		}).Run(ctx)
	}()
	defer func() {
		stop()
		<-done
	}()
	waitForDeviceControlReady(t, socketPath)

	_, err = requestPairingToken(stateDir)
	if err == nil || !strings.Contains(err.Error(), "did not answer within") ||
		!strings.Contains(err.Error(), "mewla restart") {
		t.Fatalf("err %v", err)
	}
}
