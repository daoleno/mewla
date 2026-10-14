package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/control"
)

func launchdKeys(t *testing.T, config bootConfig) (print, bootstrap, bootout, ps, lsof string) {
	t.Helper()
	path, err := bootLaunchdPlistPath()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := bootLifecycleLockPath(config.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	pid := strconv.Itoa(os.Getpid())
	return "launchctl print " + bootLaunchdTarget(),
		"launchctl bootstrap " + bootLaunchdDomain() + " " + path,
		"launchctl bootout " + bootLaunchdTarget(),
		"ps -o comm= -p " + pid,
		"lsof -t -- " + lock
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
// daemon (this test process) that takes the state lock and serves /health.
func launchdFreshInstall(t *testing.T) (bootConfig, *fakeBootRunner) {
	t.Helper()
	config, _ := newBootTestEnvironment(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config.Binary = executable
	daemonID := bootWriteIdentity(t, config.StateDir)
	config.Addr = bootFreeLoopbackAddr(t)
	print, bootstrap, bootout, ps, lsof := launchdKeys(t, config)

	runner := bootTestRunner()
	runner.fail(print, "Could not find service \""+bootLaunchdLabel+"\" in domain for user gui: 501")
	runner.set(ps, executable)
	runner.set(lsof, strconv.Itoa(os.Getpid()))
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
		"<key>SuccessfulExit</key>\n\t\t<false/>",
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
	_, bootstrap, bootout, _, _ := launchdKeys(t, config)
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
	if want, _ := renderLaunchdPlist(config); string(data) != want {
		t.Fatalf("installed plist differs:\n%s", data)
	}
	if metadata, err := readBootMetadata(bootMetadataPath(path)); err != nil || !metadata.same(config) {
		t.Fatalf("metadata = %+v, %v", metadata, err)
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
}

func TestBootLaunchdInstallChangedConfigRestarts(t *testing.T) {
	config, runner := launchdFreshInstall(t)
	_, bootstrap, bootout, _, _ := launchdKeys(t, config)
	if err := bootLaunchdInstall(config, runner, io.Discard); err != nil {
		t.Fatal(err)
	}
	runner.reset()
	// launchd keeps listing the job briefly after bootout returns.
	print, _, _, _, _ := launchdKeys(t, config)
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
	_, bootstrap, _, _, _ := launchdKeys(t, config)

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
	_, _, bootout, ps, _ := launchdKeys(t, config)
	runner.set(ps, "/usr/local/bin/something-else")
	err := bootLaunchdInstall(config, runner, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not the installed binary") {
		t.Fatalf("install = %v", err)
	}
	if !runner.called(bootout) {
		t.Fatal("failed install left the job loaded")
	}
}
