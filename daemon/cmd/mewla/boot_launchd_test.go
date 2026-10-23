package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/macapp"
)

func launchdKeys(t *testing.T, config bootConfig) (print, bootstrap, bootout string) {
	t.Helper()
	path, err := bootLaunchdPlistPath()
	if err != nil {
		t.Fatal(err)
	}
	return "launchctl print " + bootLaunchdTarget(),
		"launchctl bootstrap " + bootLaunchdDomain() + " " + path,
		"launchctl bootout " + bootLaunchdTarget()
}

// launchdStandInDaemon makes the control socket look served by the daemon with
// the given PID and executable, the way the daemon holding the state lock is.
func launchdStandInDaemon(t *testing.T, pid int, executable string) {
	t.Helper()
	previousPeer, previousStatus := bootControlPeerPID, bootDaemonStatus
	bootControlPeerPID = func(string) (int, error) { return pid, nil }
	bootDaemonStatus = func(string) (control.DaemonStatus, error) {
		return control.DaemonStatus{PID: pid, Executable: executable}, nil
	}
	t.Cleanup(func() { bootControlPeerPID, bootDaemonStatus = previousPeer, previousStatus })
}

func launchdRunning() string {
	return "gui/501/" + bootLaunchdLabel + " = {\n\tactive count = 1\n\tstate = running\n\tpid = " + strconv.Itoa(os.Getpid()) + "\n\tendpoints = {\n\t\tstate = active\n\t}\n}\n"
}

func (f *fakeBootRunner) succeed(key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.failures, key)
	f.responses[key] = value
}

// launchdFreshInstall models a Mac with nothing loaded: bootstrap starts a
// daemon (this test process, standing in for Mewla.app) that takes the state
// lock and serves /health.
func launchdFreshInstall(t *testing.T) (bootConfig, *fakeBootRunner) {
	t.Helper()
	config, _ := newBootTestEnvironment(t)
	daemonID := bootWriteIdentity(t, config.StateDir)
	config.Addr = bootFreeLoopbackAddr(t)
	print, bootstrap, bootout := launchdKeys(t, config)

	runner := bootTestRunner()
	runner.fail(print, "Could not find service \""+bootLaunchdLabel+"\" in domain for user gui: 501")
	launchdStandInDaemon(t, os.Getpid(), bootLaunchdDaemonConfig(config).Binary)
	var lock *control.LifecycleLock
	var health sync.Once
	runner.on(bootstrap, func() {
		health.Do(func() { bootStartHealthServerOn(t, config.Addr, daemonID) })
		if acquired, ok, err := control.TryAcquireLifecycleLock(config.StateDir); err == nil && ok {
			lock = acquired
		}
		runner.succeed(print, launchdRunning())
	})
	runner.on(bootout, func() {
		if lock != nil {
			_ = lock.Close()
			lock = nil
		}
		runner.fail(print, "Could not find service")
	})
	t.Cleanup(func() {
		if lock != nil {
			_ = lock.Close()
		}
	})
	return config, runner
}

func TestRenderLaunchdPlistIsWellFormedAndEscaped(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	config.WorkDir = config.WorkDir + " & <more>"
	plist, err := renderLaunchdPlist(config)
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(strings.NewReader(plist))
	for {
		if _, err := decoder.Token(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("plist is not well-formed XML: %v\n%s", err, plist)
		}
	}
	for _, want := range []string{
		bootLaunchdMarker,
		"<string>" + bootLaunchdLabel + "</string>",
		"<string>" + config.Binary + "</string>",
		"<string>-addr</string>",
		"&amp; &lt;more&gt;",
		"<key>AbandonProcessGroup</key>\n\t<true/>",
		"<string>Aqua</string>",
		"<key>AssociatedBundleIdentifiers</key>\n\t<string>com.daoleno.mewla</string>",
		"<key>KeepAlive</key>\n\t<true/>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist lacks %q:\n%s", want, plist)
		}
	}
	config.LAN = true
	config.Addr = bootLANAddr
	plist, err = renderLaunchdPlist(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plist, "<string>-lan</string>") || strings.Contains(plist, "<string>-addr</string>") {
		t.Fatalf("LAN plist should pass -lan only:\n%s", plist)
	}
}

func TestParseLaunchdPrintReadsTopLevelStateAndPID(t *testing.T) {
	job := parseLaunchdPrint(launchdRunning())
	if !job.running() || job.PID != os.Getpid() || job.State != "running" {
		t.Fatalf("job = %+v", job)
	}
	job = parseLaunchdPrint("x = {\n\tstate = not running\n\tlast exit code = 1\n}\n")
	if job.running() || job.State != "not running" {
		t.Fatalf("job = %+v", job)
	}
}

func TestBootLaunchdInstallIsIdempotentAndUninstalls(t *testing.T) {
	config, runner := launchdFreshInstall(t)
	_, bootstrap, bootout := launchdKeys(t, config)
	var out bytes.Buffer
	if err := bootLaunchdInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	if !runner.called(bootstrap) || !strings.Contains(out.String(), "Installed and started") {
		t.Fatalf("fresh install: %s", out.String())
	}
	path, _ := bootLaunchdPlistPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	daemon := bootLaunchdDaemonConfig(config)
	if want, _ := renderLaunchdPlist(daemon); string(data) != want || !strings.Contains(want, "Mewla.app/Contents/MacOS/mewla") {
		t.Fatalf("installed plist differs:\n%s", data)
	}
	if metadata, err := readBootMetadata(bootMetadataPath(path)); err != nil || !metadata.same(daemon) {
		t.Fatalf("metadata = %+v, %v", metadata, err)
	}
	bundle := macapp.Path(config.Home)
	if program, _ := os.ReadFile(daemon.Binary); string(program) != "#!/bin/sh\n" || !macapp.Managed(bundle) {
		t.Fatalf("bundle executable = %q", program)
	}
	if !runner.called("codesign --force --sign - --identifier com.daoleno.mewla " + bundle + ".new") {
		t.Fatal("bundle was not signed")
	}

	runner.reset()
	out.Reset()
	if err := bootLaunchdInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	if runner.called(bootstrap) || runner.called(bootout) || !strings.Contains(out.String(), "Already installed and running") {
		t.Fatalf("idempotent install restarted: %s", out.String())
	}

	out.Reset()
	if err := bootLaunchdStatus(runner, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"service: running (PID", "ownership: state directory is owned by " + bootLaunchdLabel, "health: ok"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("status lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := bootLaunchdUninstall(runner, &out); err != nil {
		t.Fatal(err)
	}
	if !runner.called(bootout) {
		t.Fatal("uninstall did not boot out the job")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("plist remains: %v", err)
	}
	if _, err := os.Stat(bootMetadataPath(path)); !os.IsNotExist(err) {
		t.Fatalf("metadata remains: %v", err)
	}
	if _, err := os.Stat(bundle); !os.IsNotExist(err) {
		t.Fatalf("Mewla.app remains: %v", err)
	}
}

func TestBootLaunchdUpdateReinstallsWithInstalledConfiguration(t *testing.T) {
	config, runner := launchdFreshInstall(t)
	var ran []string
	previous := bootUpdateRun
	bootUpdateRun = func(command *exec.Cmd) error {
		ran = command.Args
		return nil
	}
	defer func() { bootUpdateRun = previous }()

	if ok, err := bootLaunchdApplyUpdate("/usr/local/bin/mewla", io.Discard, io.Discard); ok || err != nil || ran != nil {
		t.Fatalf("update without a LaunchAgent = %v, %v, ran %q", ok, err, ran)
	}
	if err := bootLaunchdInstall(config, runner, io.Discard); err != nil {
		t.Fatal(err)
	}
	if ok, err := bootLaunchdApplyUpdate("/Users/me/.local/bin/mewla", io.Discard, io.Discard); !ok || err != nil {
		t.Fatalf("update = %v, %v", ok, err)
	}
	want := []string{"/Users/me/.local/bin/mewla", "boot", "install", "-state-dir", config.StateDir,
		"-addr", config.Addr, "-work-dir", config.WorkDir, "-path-env", config.PathEnv}
	if strings.Join(ran, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ran %q, want %q", ran, want)
	}

	bootUpdateRun = func(*exec.Cmd) error { return errors.New("exit status 1") }
	_, err := bootLaunchdApplyUpdate("/Users/me/.local/bin/mewla", io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-work-dir '"+config.WorkDir+"'") {
		t.Fatalf("failed update should name the retry command: %v", err)
	}
}

func TestOpeningMewlaAppStartsTheLaunchAgent(t *testing.T) {
	previous := bootGOOS
	bootGOOS = "darwin"
	defer func() { bootGOOS = previous }()
	config, runner := launchdFreshInstall(t)
	kickstart := "launchctl kickstart " + bootLaunchdTarget()
	bundleExecutable := bootLaunchdDaemonConfig(config).Binary

	if handed, err := bootLaunchdHandOff(nil, bundleExecutable, runner); handed || err != nil {
		t.Fatalf("handed off before install: %v %v", handed, err)
	}
	if err := bootLaunchdInstall(config, runner, io.Discard); err != nil {
		t.Fatal(err)
	}
	runner.reset()
	for _, launch := range []struct {
		name       string
		args       []string
		executable string
		xpc        string
	}{
		{"the CLI binary", nil, config.Binary, ""},
		{"a command line with flags", []string{"-lan"}, bundleExecutable, ""},
		{"launchd's own start", nil, bundleExecutable, bootLaunchdLabel},
	} {
		t.Setenv("XPC_SERVICE_NAME", launch.xpc)
		if handed, err := bootLaunchdHandOff(launch.args, launch.executable, runner); handed || err != nil {
			t.Fatalf("%s was handed off: %v %v", launch.name, handed, err)
		}
	}
	if runner.called(kickstart) {
		t.Fatal("kickstarted for a launch that should serve")
	}

	// LaunchServices opening the app: no arguments, its own XPC service name.
	t.Setenv("XPC_SERVICE_NAME", "application.com.daoleno.mewla.1234")
	if handed, err := bootLaunchdHandOff(nil, bundleExecutable, runner); !handed || err != nil {
		t.Fatalf("open Mewla.app: %v %v", handed, err)
	}
	if !runner.called(kickstart) {
		t.Fatal("open Mewla.app did not start the LaunchAgent")
	}
}

// launchdLocalIdentity makes the fake `security` report an existing local
// identity.
func launchdLocalIdentity(runner *fakeBootRunner, config bootConfig) string {
	hash := "6BDB705C42A0A3258558E5D2F5BA0E7629A5B750"
	keychain := macapp.IdentityKeychain(config.Home)
	runner.set("security find-certificate -c "+macapp.IdentityName+" -Z "+keychain, "SHA-1 hash: "+hash+"\n")
	return hash
}

func TestBootLaunchdInstallSignsWithTheLocalIdentity(t *testing.T) {
	config, runner := launchdFreshInstall(t)
	hash := launchdLocalIdentity(runner, config)
	keychain := macapp.IdentityKeychain(config.Home)
	if err := os.MkdirAll(strings.TrimSuffix(keychain, "/mewla-signing.keychain-db"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keychain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := bootLaunchdInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	bundle := macapp.Path(config.Home)
	if !runner.called("codesign --force --sign " + hash + " --identifier com.daoleno.mewla --keychain " + keychain + " " + bundle + ".new") {
		t.Fatalf("bundle not signed with the local identity: %q", runner.calls)
	}
	if !strings.Contains(out.String(), "Signing: "+bootSigningLocal) {
		t.Fatalf("install output: %s", out.String())
	}
	out.Reset()
	if err := bootLaunchdStatus(runner, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "signing: "+bootSigningLocal) {
		t.Fatalf("status: %s", out.String())
	}

	// A lost identity is reported, not hidden.
	if err := os.Remove(keychain); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := bootLaunchdStatus(runner, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "signing: local identity missing") {
		t.Fatalf("status with a lost identity: %s", out.String())
	}
}

func TestBootLaunchdInstallFallsBackToAdHoc(t *testing.T) {
	config, runner := launchdFreshInstall(t)
	hash := launchdLocalIdentity(runner, config)
	bundle := macapp.Path(config.Home)
	runner.fail("codesign --force --sign "+hash+" --identifier com.daoleno.mewla --keychain "+macapp.IdentityKeychain(config.Home)+" "+bundle+".new", "errSecInternalComponent")
	var out bytes.Buffer
	if err := bootLaunchdInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	if !runner.called("codesign --force --sign - --identifier com.daoleno.mewla " + bundle + ".new") {
		t.Fatal("no ad hoc fallback")
	}
	if !strings.Contains(out.String(), "Signing: "+bootSigningAdHoc+" (") || !strings.Contains(out.String(), "errSecInternalComponent") {
		t.Fatalf("install output: %s", out.String())
	}
	path, _ := bootLaunchdPlistPath()
	if metadata, _ := readBootMetadata(bootMetadataPath(path)); !strings.HasPrefix(metadata.Signing, bootSigningAdHoc) {
		t.Fatalf("metadata signing = %q", metadata.Signing)
	}
}

func TestBootLaunchdInstallNewBinaryRestarts(t *testing.T) {
	config, runner := launchdFreshInstall(t)
	_, bootstrap, bootout := launchdKeys(t, config)
	if err := bootLaunchdInstall(config, runner, io.Discard); err != nil {
		t.Fatal(err)
	}
	runner.reset()
	if err := os.WriteFile(config.Binary, []byte("#!/bin/sh\n# update\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := bootLaunchdInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	if !runner.called(bootout) || !runner.called(bootstrap) || !strings.Contains(out.String(), "Updated and restarted") {
		t.Fatalf("new binary did not restart: %s", out.String())
	}
	if program, _ := os.ReadFile(bootLaunchdDaemonConfig(config).Binary); !strings.Contains(string(program), "update") {
		t.Fatalf("bundle executable = %q", program)
	}
}

func TestBootLaunchdInstallRefusesForeignBundle(t *testing.T) {
	config, runner := launchdFreshInstall(t)
	info := macapp.Path(config.Home) + "/Contents/Info.plist"
	if err := os.MkdirAll(strings.TrimSuffix(info, "/Info.plist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(info, []byte("<plist><dict/></plist>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := bootLaunchdInstall(config, runner, io.Discard); err == nil || !strings.Contains(err.Error(), "did not create") {
		t.Fatalf("install over a foreign Mewla.app = %v", err)
	}
	if data, _ := os.ReadFile(info); string(data) != "<plist><dict/></plist>" {
		t.Fatal("foreign Mewla.app was modified")
	}
}

func TestBootLaunchdInstallChangedConfigRestarts(t *testing.T) {
	config, runner := launchdFreshInstall(t)
	_, bootstrap, bootout := launchdKeys(t, config)
	if err := bootLaunchdInstall(config, runner, io.Discard); err != nil {
		t.Fatal(err)
	}
	runner.reset()
	// launchd keeps listing the job briefly after bootout returns.
	print, _, _ := launchdKeys(t, config)
	runner.mu.Lock()
	unload := runner.handlers[bootout]
	runner.mu.Unlock()
	runner.on(bootout, func() {
		unload()
		runner.push(print, launchdRunning())
	})
	config.PathEnv = "/opt/homebrew/bin:/usr/bin:/bin"
	var out bytes.Buffer
	if err := bootLaunchdInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	if !runner.called(bootout) || !runner.called(bootstrap) || !strings.Contains(out.String(), "Updated and restarted") {
		t.Fatalf("changed config did not restart: %s", out.String())
	}
}

func TestBootLaunchdInstallRefusesForeignPlistAndOutsideOwner(t *testing.T) {
	config, runner := launchdFreshInstall(t)
	_, bootstrap, _ := launchdKeys(t, config)

	bootHoldStateLock(t, config.StateDir)
	err := bootLaunchdInstall(config, runner, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("install with a manual daemon = %v", err)
	}
	if runner.called(bootstrap) {
		t.Fatal("install bootstrapped despite a foreign owner")
	}

	path, _ := bootLaunchdPlistPath()
	if err := os.MkdirAll(strings.TrimSuffix(path, "/"+bootLaunchdLabel+".plist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := bootLaunchdInstall(config, runner, io.Discard); err == nil || !strings.Contains(err.Error(), "not managed") {
		t.Fatalf("install over a foreign plist = %v", err)
	}
	if err := bootLaunchdUninstall(runner, io.Discard); err == nil || !strings.Contains(err.Error(), "not managed") {
		t.Fatalf("uninstall of a foreign plist = %v", err)
	}
}

func TestBootLaunchdInstallVerificationFailureBootsOut(t *testing.T) {
	previousTimeout, previousInterval := bootVerifyTimeout, bootVerifyInterval
	bootVerifyTimeout, bootVerifyInterval = 300*time.Millisecond, 50*time.Millisecond
	defer func() { bootVerifyTimeout, bootVerifyInterval = previousTimeout, previousInterval }()

	config, runner := launchdFreshInstall(t)
	_, _, bootout := launchdKeys(t, config)
	launchdStandInDaemon(t, os.Getpid(), "/usr/local/bin/something-else")
	err := bootLaunchdInstall(config, runner, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not the installed binary") {
		t.Fatalf("install = %v", err)
	}
	if !runner.called(bootout) {
		t.Fatal("failed install left the job loaded")
	}
}

// The kernel names the process serving the control socket, whatever release
// it is, and a daemon that supports daemon_status adds its executable.
func TestBootControlSocketIdentifiesTheServingDaemon(t *testing.T) {
	stateDir, err := os.MkdirTemp("/tmp", "mewla-boot-status-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	if _, err := bootControlPeerPID(stateDir); !errors.Is(err, control.ErrNotRunning) {
		t.Fatalf("no daemon: %v", err)
	}
	if _, err := bootDaemonStatus(stateDir); !errors.Is(err, control.ErrNotRunning) {
		t.Fatalf("no daemon: %v", err)
	}
	socket, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := &control.Server{Path: socket, Handler: &controlApp{}}
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	defer func() { cancel(); <-done }()
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	pid, err := bootControlPeerPID(stateDir)
	if err != nil || pid != os.Getpid() {
		t.Fatalf("peer pid = %d, %v; want %d", pid, err, os.Getpid())
	}
	status, err := bootDaemonStatus(stateDir)
	executable, _ := os.Executable()
	if err != nil || status.PID != os.Getpid() || status.Executable != executable {
		t.Fatalf("status = %+v, %v; want pid %d executable %s", status, err, os.Getpid(), executable)
	}
}

// Updating over a daemon of the previous release, which has no daemon_status,
// must still recognize the running job as the owner of the state.
func TestBootLaunchdUpdateOverADaemonWithoutDaemonStatus(t *testing.T) {
	config, runner := launchdFreshInstall(t)
	_, bootstrap, bootout := launchdKeys(t, config)
	if err := bootLaunchdInstall(config, runner, io.Discard); err != nil {
		t.Fatal(err)
	}
	// The running daemon is now the previous release: the kernel still names
	// it, but it answers daemon_status with an error.
	bootDaemonStatus = func(string) (control.DaemonStatus, error) {
		return control.DaemonStatus{}, errors.New("unknown control request type daemon_status")
	}
	preflight := bootLaunchdInspectOwner(config, runner, false)
	if !preflight.Job.running() || !preflight.LockHeld || !preflight.jobHoldsLock() {
		t.Fatalf("preflight = %+v", preflight)
	}
	// An update restarts the job: bootout, bootstrap, and the new daemon verifies.
	bootDaemonStatus = func(string) (control.DaemonStatus, error) {
		return control.DaemonStatus{PID: os.Getpid(), Executable: bootLaunchdDaemonConfig(config).Binary}, nil
	}
	changed := config
	changed.PathEnv = config.PathEnv + ":/extra"
	if err := bootLaunchdInstall(changed, runner, io.Discard); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !runner.called(bootout) || runner.count(bootstrap) < 2 {
		t.Fatalf("update did not restart the job (bootout %v, bootstrap %d)", runner.called(bootout), runner.count(bootstrap))
	}
}

func TestBootLaunchdSnapshotVerifiesOwnerFromTheKernelAndTheDaemon(t *testing.T) {
	config := bootConfig{Binary: "/Applications/Mewla.app/Contents/MacOS/mewla", StateDir: "/state", Addr: "127.0.0.1:1"}
	healthy := bootLaunchdSnapshot{
		Job:        bootLaunchdJob{Loaded: true, State: "running", PID: 700},
		LockHeld:   true,
		OwnerPID:   700,
		Executable: config.Binary,
		ServedID:   "id",
		ExpectedID: "id",
	}
	if err := healthy.verified(config); err != nil {
		t.Fatalf("healthy: %v", err)
	}
	for name, tc := range map[string]struct {
		change func(*bootLaunchdSnapshot)
		want   string
	}{
		"nothing serves the socket": {func(s *bootLaunchdSnapshot) { s.OwnerErr = errors.New("connection refused") }, "who it is"},
		"another process owns":      {func(s *bootLaunchdSnapshot) { s.OwnerPID = 701 }, "outside"},
		"daemon cannot say":         {func(s *bootLaunchdSnapshot) { s.ExecutableErr = errors.New("unknown request") }, "which binary"},
		"other executable":          {func(s *bootLaunchdSnapshot) { s.Executable = "/usr/local/bin/mewla" }, "not the installed binary"},
		"lock free":                 {func(s *bootLaunchdSnapshot) { s.LockHeld = false }, "no daemon owns"},
		"job not running":           {func(s *bootLaunchdSnapshot) { s.Job.State = "waiting"; s.Job.PID = 0 }, "not running"},
	} {
		snapshot := healthy
		tc.change(&snapshot)
		if err := snapshot.verified(config); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: verified = %v, want %q", name, err, tc.want)
		}
	}
	outside := healthy
	outside.OwnerPID = 701
	if text := outside.describeOwner(); !strings.Contains(text, "outside") || !strings.Contains(text, "701") {
		t.Errorf("describeOwner = %q", text)
	}
	silent := healthy
	silent.OwnerErr = errors.New("connection refused")
	if text := silent.describeOwner(); !strings.Contains(text, "outside") || !strings.Contains(text, "not answering") {
		t.Errorf("describeOwner = %q", text)
	}
}
