package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/control"
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
		image:       func(int) (string, error) { return self, nil },
		parentImage: func(int) (string, error) { return "conhost.exe", nil },
		health: func(string) (daemonHealth, error) {
			d.mu.Lock()
			defer d.mu.Unlock()
			if !d.running {
				return daemonHealth{}, errors.New("connection refused")
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
		wait:    2 * time.Second,
		poll:    5 * time.Millisecond,
		runWait: 50 * time.Millisecond,
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

func windowsTaskRunner(daemon *fakeDaemon, version string) *fakeBootRunner {
	runner := &fakeBootRunner{
		responses: map[string]string{
			"schtasks /query /tn Mewla /xml": "<Task><Description>Starts Mewla when you log in. " + bootTaskMarker + ".</Description></Task>",
		},
		handlers: map[string]func(){
			"schtasks /run /tn Mewla": func() { daemon.start(version) },
		},
	}
	return runner
}

func TestUpdateRestartsTheOldDaemonThroughTheLogonTask(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 4242, version: "0.4.5", running: true}
	runner := windowsTaskRunner(daemon, "0.4.6")
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
	if runner.count("schtasks /run /tn Mewla") != 1 || daemon.version != "0.4.6" {
		t.Fatalf("task runs %d, daemon version %s", runner.count("schtasks /run /tn Mewla"), daemon.version)
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
	runner := windowsTaskRunner(daemon, "0.4.6")
	rt := daemon.runtime(t, "windows", runner, `C:\Mewla\mewla.exe`)

	found, ok := rt.inspect(stateDir)
	if !ok {
		t.Fatal("old daemon not found")
	}
	if err := rt.restart(stateDir, found, "0.4.6"); err != nil {
		t.Fatal(err)
	}
	if got := runner.count("schtasks /run /tn Mewla"); got != 3 {
		t.Fatalf("task runs %d, want 3 (two dropped)", got)
	}
}

func TestRestartLeavesADaemonStartedByHandAndSaysWhatToRun(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 77, version: "0.4.5", running: true}
	runner := &fakeBootRunner{responses: map[string]string{}, failures: map[string]bool{"schtasks /query /tn Mewla /xml": true}}
	rt := daemon.runtime(t, "windows", runner, `C:\Mewla\mewla.exe`)

	var out bytes.Buffer
	if err := restartAfterUpdate(rt, stateDir, "0.4.5", "0.4.6", &out); err != nil {
		t.Fatalf("the update is installed; err %v", err)
	}
	if len(daemon.killed) != 0 {
		t.Fatalf("killed %v", daemon.killed)
	}
	for _, want := range []string{"Updated Mewla 0.4.5 → 0.4.6.", "Mewla 0.4.5 is running from a terminal", "PID 77", "Stop-Process -Id 77", "start it again with: mewla"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output %q lacks %q", out.String(), want)
		}
	}
	if strings.Contains(out.String(), "Restarting") {
		t.Fatalf("output %q promises a restart", out.String())
	}
	err := rt.restart(stateDir, runningDaemon{PID: 77, Version: "0.4.5"}, "0.4.6")
	var byHand *errDaemonStartedByHand
	if !errors.As(err, &byHand) || len(daemon.killed) != 0 {
		t.Fatalf("restart of a daemon started by hand: %v, killed %v", err, daemon.killed)
	}
}

// A default Windows install always has the logon task; a daemon started in
// a terminal on top of it is still the person's, not the task's.
func TestRestartLeavesADaemonStartedInATerminalBesideTheTask(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 88, version: "0.4.5", running: true}
	runner := windowsTaskRunner(daemon, "0.4.6")
	rt := daemon.runtime(t, "windows", runner, `C:\Mewla\mewla.exe`)
	rt.parentImage = func(int) (string, error) { return "powershell.exe", nil }

	var out bytes.Buffer
	if err := restartAfterUpdate(rt, stateDir, "0.4.5", "0.4.6", &out); err != nil {
		t.Fatal(err)
	}
	if len(daemon.killed) != 0 || runner.count("schtasks /run /tn Mewla") != 0 {
		t.Fatalf("killed %v, task runs %d", daemon.killed, runner.count("schtasks /run /tn Mewla"))
	}
	if !strings.Contains(out.String(), "Stop-Process -Id 88") {
		t.Fatalf("output %q", out.String())
	}
}

func TestRestartUsesTheSystemdUnitOnlyWhenItRunsTheDaemon(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	for _, test := range []struct {
		name    string
		mainPID string
		restart bool
	}{
		{"unit runs it", "500", true},
		{"started by hand", "0", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			daemon := &fakeDaemon{daemonID: daemonID, pid: 500, version: "0.4.5", running: true}
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
			restarted := runner.count("systemctl --user restart mewla.service") == 1
			if restarted != test.restart || err != nil {
				t.Fatalf("restarted %t, want %t (err %v)", restarted, test.restart, err)
			}
			if !test.restart && !strings.Contains(out.String(), "kill 500") {
				t.Fatalf("output %q", out.String())
			}
		})
	}
}

func TestOnlyThisInstallationsDaemonIsRestarted(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 9, version: "0.4.5", running: true}
	runner := windowsTaskRunner(daemon, "0.4.6")
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
	runner := windowsTaskRunner(daemon, "0.4.7")
	rt := daemon.runtime(t, "windows", runner, `C:\Mewla\mewla.exe`)

	t.Setenv("MEWLA_WORKER_ID", "w1")
	ensureCurrentDaemon(rt, stateDir, true, &bytes.Buffer{})
	if len(daemon.killed) != 0 {
		t.Fatal("a Worker's mewla restarted the daemon")
	}
	t.Setenv("MEWLA_WORKER_ID", "")
	ensureCurrentDaemon(rt, stateDir, false, &bytes.Buffer{})
	if len(daemon.killed) != 0 {
		t.Fatal("a non-interactive mewla restarted the daemon")
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
