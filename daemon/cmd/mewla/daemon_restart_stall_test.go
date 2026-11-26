package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// The owner's Mac, 11-26: Mewla 0.4.31 accepted the restart onto 0.4.32, shut
// down (its lifecycle lock free), and never ran the new image: the macOS exec
// stall. The CLI stops such a daemon so its launchd job starts the installed
// one.
func TestInPlaceRestartStoppedInItsExecIsStoppedSoLaunchdStartsIt(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 81085, version: "0.4.31", running: true, addr: "127.0.0.1:9876"}
	rt := daemon.runtime(t, "darwin", &fakeBootRunner{}, "/Applications/Mewla.app/Contents/Helpers/mewla")
	stalled := false
	rt.upgrade = func(string, string) error {
		daemon.mu.Lock()
		defer daemon.mu.Unlock()
		stalled = true
		return nil
	}
	health := rt.health
	rt.health = func(addr string) (daemonHealth, error) {
		daemon.mu.Lock()
		quiet := stalled && daemon.pid == 81085
		daemon.mu.Unlock()
		if quiet {
			return daemonHealth{}, errConnRefused
		}
		return health(addr)
	}
	rt.lockHeld = func(string) (bool, error) {
		daemon.mu.Lock()
		defer daemon.mu.Unlock()
		return !stalled || daemon.pid != 81085, nil
	}
	kill := rt.kill
	rt.kill = func(pid int) error {
		err := kill(pid)
		// launchd's KeepAlive: the job starts again from the installed app.
		go func() {
			time.Sleep(20 * time.Millisecond)
			daemon.start("0.4.32")
		}()
		return err
	}
	rt.stallWait = 100 * time.Millisecond
	var notes bytes.Buffer
	rt.notes = &notes
	found := runningDaemon{PID: 81085, Version: "0.4.31", Addr: "127.0.0.1:9876"}
	if err := rt.restart(stateDir, found, "0.4.32"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if len(daemon.killed) != 1 || daemon.killed[0] != 81085 {
		t.Fatalf("killed %v, want the stalled 81085", daemon.killed)
	}
	if daemon.version != "0.4.32" || !strings.Contains(notes.String(), "did not start 0.4.32 within 100ms; stopping it so launchd starts it again") {
		t.Fatalf("version %s, notes %q", daemon.version, notes.String())
	}
}

// A daemon still holding its state is not stalled: it is shutting down, or
// its new image is starting. Linux never needs this.
func TestInPlaceRestartLeavesADaemonThatHoldsItsStateAlone(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		stateDir, daemonID := restartStateDir(t)
		daemon := &fakeDaemon{daemonID: daemonID, pid: 77, version: "0.4.31", running: true}
		rt := daemon.runtime(t, goos, &fakeBootRunner{}, "/opt/mewla")
		rt.upgrade = func(string, string) error { return nil }
		rt.health = func(string) (daemonHealth, error) { return daemonHealth{}, errConnRefused }
		held := goos == "darwin"
		rt.lockHeld = func(string) (bool, error) { return held, nil }
		rt.stallWait, rt.inPlaceWait = 20*time.Millisecond, 300*time.Millisecond
		err := rt.restartInPlace(stateDir, runningDaemon{PID: 77, Version: "0.4.31", Addr: "127.0.0.1:9876"}, "0.4.32")
		if err == nil || len(daemon.killed) != 0 {
			t.Fatalf("%s: err %v, killed %v", goos, err, daemon.killed)
		}
	}
}

// Under Mewla for Mac's host the daemon restarts by ending; launchd starts the
// job again as a new process, which is the restart, not a failure.
func TestInPlaceRestartWaitsForLaunchdAfterTheDaemonEnds(t *testing.T) {
	stateDir, daemonID := restartStateDir(t)
	daemon := &fakeDaemon{daemonID: daemonID, pid: 500, version: "0.4.33", running: true}
	rt := daemon.runtime(t, "darwin", &fakeBootRunner{}, "/Applications/Mewla.app/Contents/Helpers/mewla")
	rt.upgrade = func(string, string) error {
		daemon.mu.Lock()
		daemon.running = false
		daemon.mu.Unlock()
		go func() {
			time.Sleep(100 * time.Millisecond)
			daemon.start("0.4.34")
		}()
		return nil
	}
	rt.lockHeld = func(string) (bool, error) { return false, nil }
	if err := rt.restart(stateDir, runningDaemon{PID: 500, Version: "0.4.33", Addr: "127.0.0.1:9876"}, "0.4.34"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if daemon.pid != 501 || daemon.version != "0.4.34" || len(daemon.killed) != 0 {
		t.Fatalf("pid %d version %s killed %v", daemon.pid, daemon.version, daemon.killed)
	}
}

func TestOnlyADaemonUnderItsHostRestartsByEnding(t *testing.T) {
	for _, tc := range []struct {
		host, ppid int
		want       bool
	}{
		{host: 81083, ppid: 81083, want: true},
		{host: 0, ppid: 1},     // a terminal or launchd job without the host
		{host: 81083, ppid: 1}, // the host is gone: stopWithHost stops it
		{host: 1, ppid: 1},     // never launchd itself
	} {
		if got := restartsThroughAppHost(tc.host, tc.ppid); got != tc.want {
			t.Fatalf("host %d ppid %d: %v, want %v", tc.host, tc.ppid, got, tc.want)
		}
	}
}

var errConnRefused = errors.New("connection refused")
