package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/selfupdate"
)

func testRestarter(t *testing.T, reported string, probeErr error) (*inPlaceRestarter, *[]*daemonReexec) {
	t.Helper()
	var restarts []*daemonReexec
	r := &inPlaceRestarter{
		startup:  startupSnapshot{Executable: "/home/p/.local/bin/mewla", Args: []string{"mewla"}, Env: []string{"A=1"}, Dir: "/home/p"},
		stateDir: t.TempDir(),
		updating: func() bool { return false },
		probe: func(executable string) (string, error) {
			if executable != "/home/p/.local/bin/mewla" {
				t.Fatalf("probed %q", executable)
			}
			return reported, probeErr
		},
		restart: func(r *daemonReexec) { restarts = append(restarts, r) },
	}
	t.Cleanup(func() {
		for _, r := range restarts {
			stopRestartSignals(r)
		}
	})
	return r, &restarts
}

func stopRestartSignals(r *daemonReexec) {
	if r != nil && r.stops != nil {
		signal.Stop(r.stops)
	}
}

func TestDaemonUpgradeRefusesWithItsCause(t *testing.T) {
	t.Setenv(selfupdate.ChildEnv, "")
	for _, test := range []struct {
		name     string
		setup    func(*inPlaceRestarter)
		reported string
		probeErr error
		code     string
		message  string
	}{
		{"new executable does not run", nil, "", errors.New("exec format error"), "upgrade_not_runnable",
			"/home/p/.local/bin/mewla does not run: exec format error"},
		{"executable is another version", nil, "0.4.8", nil, "upgrade_wrong_version",
			"/home/p/.local/bin/mewla is Mewla 0.4.8, not 0.4.9"},
		{"own update in progress", func(r *inPlaceRestarter) { r.updating = func() bool { return true } }, "0.4.9", nil, "upgrade_in_progress",
			"Mewla is installing an update of its own; try again when it has restarted"},
		{"started from an unknown file", func(r *inPlaceRestarter) { r.startup.Executable = "" }, "0.4.9", nil, "upgrade_no_executable",
			"Mewla cannot tell which file it was started from"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, restarts := testRestarter(t, test.reported, test.probeErr)
			if test.setup != nil {
				test.setup(r)
			}
			response := r.handle("0.4.9")
			if response.OK || response.Error == nil || response.Error.Code != test.code || response.Error.Message != test.message {
				t.Fatalf("response %+v %+v", response, response.Error)
			}
			if response.After != nil || len(*restarts) != 0 {
				t.Fatal("a refused restart went ahead")
			}
		})
	}
	t.Run("update on trial", func(t *testing.T) {
		t.Setenv(selfupdate.ChildEnv, "1")
		r, restarts := testRestarter(t, "0.4.9", nil)
		response := r.handle("0.4.9")
		if response.OK || response.Error.Code != "upgrade_on_trial" || len(*restarts) != 0 {
			t.Fatalf("response %+v", response.Error)
		}
	})
}

func TestDaemonUpgradeAcceptsOnceAndRestartsAfterAnswering(t *testing.T) {
	t.Setenv(selfupdate.ChildEnv, "")
	r, restarts := testRestarter(t, "0.4.9", nil)
	if err := os.MkdirAll(filepath.Join(r.stateDir, "run"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inPlaceRestartFailurePath(r.stateDir), []byte("old failure"), 0o600); err != nil {
		t.Fatal(err)
	}
	response := r.handle("0.4.9")
	if !response.OK || response.After == nil {
		t.Fatalf("response %+v", response.Error)
	}
	if len(*restarts) != 0 {
		t.Fatal("restarted before the answer was written")
	}
	if _, err := os.Stat(inPlaceRestartFailurePath(r.stateDir)); !os.IsNotExist(err) {
		t.Fatalf("a previous failure is still on record: %v", err)
	}
	response.After()
	if len(*restarts) != 1 {
		t.Fatalf("restarts %d", len(*restarts))
	}
	reexec := (*restarts)[0]
	if reexec.version != "0.4.9" || reexec.startup.Executable != "/home/p/.local/bin/mewla" || reexec.startup.Dir != "/home/p" {
		t.Fatalf("reexec %+v", reexec)
	}
	again := r.handle("0.4.9")
	if again.OK || again.Error.Code != "upgrade_in_progress" || again.Error.Message != "Mewla is already restarting" {
		t.Fatalf("second request %+v", again.Error)
	}
	// mewla restart with nothing new: whatever is installed.
	r2, _ := testRestarter(t, "0.4.8", nil)
	if response := r2.handle(""); !response.OK {
		t.Fatalf("restart onto the installed version: %+v", response.Error)
	}
}

// The control round trip: the CLI hears the daemon accept before the daemon
// starts shutting down, and an older daemon's "unknown request" becomes a
// cause the person can act on.
func TestDaemonUpgradeOverTheControlSocket(t *testing.T) {
	t.Setenv(selfupdate.ChildEnv, "")
	stateDir := shortControlStateDir(t)
	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	restarted := make(chan *daemonReexec, 1)
	app := &controlApp{stateDir: stateDir, addr: "127.0.0.1:9931"}
	app.restarter = &inPlaceRestarter{
		startup:  startupSnapshot{Executable: "/opt/mewla", Args: []string{"mewla", "-addr", "127.0.0.1:9931"}},
		stateDir: stateDir,
		probe:    func(string) (string, error) { return "0.4.9", nil },
		restart:  func(r *daemonReexec) { restarted <- r },
	}
	serveControl(t, socketPath, app)

	status, err := bootDaemonStatus(stateDir)
	if err != nil || status.PID != os.Getpid() || status.Version != Version || status.Addr != "127.0.0.1:9931" {
		t.Fatalf("daemon_status %+v (%v)", status, err)
	}
	if err := requestInPlaceRestart(stateDir, "0.5.0"); err == nil || err.Error() != "/opt/mewla is Mewla 0.4.9, not 0.5.0" {
		t.Fatalf("refusal over the socket: %v", err)
	}
	if err := requestInPlaceRestart(stateDir, "0.4.9"); err != nil {
		t.Fatalf("accepted restart: %v", err)
	}
	select {
	case r := <-restarted:
		stopRestartSignals(r)
		if r.version != "0.4.9" || strings.Join(r.startup.Args, " ") != "mewla -addr 127.0.0.1:9931" {
			t.Fatalf("reexec %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon never restarted")
	}

	olderStateDir := shortControlStateDir(t)
	olderSocket, err := control.DefaultSocketPath(olderStateDir)
	if err != nil {
		t.Fatal(err)
	}
	serveControl(t, olderSocket, olderDaemon{})
	if err := requestInPlaceRestart(olderStateDir, "0.4.9"); !errors.Is(err, errInPlaceUnsupported) {
		t.Fatalf("older daemon: %v", err)
	}
}

func serveControl(t *testing.T, socketPath string, handler control.Handler) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (&control.Server{Path: socketPath, Handler: handler}).Run(ctx) }()
	t.Cleanup(func() {
		stop()
		<-done
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := control.CallWithTimeout(socketPath, control.Request{Type: "daemon_status"}, 200*time.Millisecond)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("control socket did not answer: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// olderDaemon answers as a daemon from before daemon_upgrade.
type olderDaemon struct{}

func (olderDaemon) HandleControlRequest(request control.Request) control.Response {
	return control.ErrorResponse("unknown_request", "Unknown control request: "+request.Type)
}

// Ctrl+C while the daemon shuts down for a restart stops it instead.
func TestInPlaceRestartStopsOnASignalDuringShutdown(t *testing.T) {
	stops := make(chan os.Signal, 1)
	stops <- os.Interrupt
	stateDir := t.TempDir()
	err := runInPlaceRestart(&daemonReexec{
		startup:  startupSnapshot{Executable: os.Args[0], Args: os.Args},
		version:  "0.4.9",
		stateDir: stateDir,
		stops:    stops,
	})
	if err != nil {
		t.Fatalf("stopped on a signal: %v", err)
	}
	if _, err := os.Stat(inPlaceRestartFailurePath(stateDir)); !os.IsNotExist(err) {
		t.Fatal("a stop was recorded as a failed restart")
	}
}

// An executable that cannot start: the daemon records why for the CLI that
// is waiting on it.
func TestInPlaceRestartRecordsWhyItCouldNotStart(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stateDir, "run"), 0o700); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "mewla")
	err := runInPlaceRestart(&daemonReexec{
		startup:  startupSnapshot{Executable: missing, Args: []string{"mewla"}},
		version:  "0.4.9",
		stateDir: stateDir,
		stops:    make(chan os.Signal, 1),
	})
	if err == nil || !strings.Contains(err.Error(), "restart Mewla onto 0.4.9: could not start "+missing) {
		t.Fatalf("err %v", err)
	}
	recorded, readErr := os.ReadFile(inPlaceRestartFailurePath(stateDir))
	if readErr != nil || !strings.HasPrefix(string(recorded), "could not start "+missing+": ") {
		t.Fatalf("recorded %q (%v)", recorded, readErr)
	}
}
