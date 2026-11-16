package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/platform"
)

type taskRunner struct {
	calls    [][]string
	existing string
}

func (r *taskRunner) run(name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if len(args) > 0 && args[0] == "/query" {
		if r.existing == "" {
			return []byte("ERROR: The system cannot find the file specified."), errors.New("exit status 1")
		}
		return []byte(r.existing), nil
	}
	return nil, nil
}

// taskConfig uses paths absolute on the OS running the test.
func taskConfig(t *testing.T) bootConfig {
	home := t.TempDir()
	return bootConfig{
		Binary:   filepath.Join(home, "Programs", "Mewla", "mewla.exe"),
		StateDir: filepath.Join(home, ".mewla dir"),
		Addr:     "127.0.0.1:9876",
		WorkDir:  home,
		PathEnv:  home,
		Home:     home,
	}
}

func withTaskUser(t *testing.T) {
	previous := bootTaskCurrentUser
	bootTaskCurrentUser = func() (string, string, error) { return `PC\ana`, "S-1-5-21-7", nil }
	t.Cleanup(func() { bootTaskCurrentUser = previous })
}

func TestBootTaskRunsTheDaemonHeadlessAtThisUsersLogon(t *testing.T) {
	withTaskUser(t)
	config := taskConfig(t)
	task, err := bootTaskDefinition(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(task.Command, `\System32\conhost.exe`) {
		t.Fatalf("command = %s", task.Command)
	}
	want := "--headless " + config.Binary + ` -state-dir "` + config.StateDir + `" -addr 127.0.0.1:9876`
	if task.Arguments != want {
		t.Fatalf("arguments = %s\nwant        %s", task.Arguments, want)
	}
	text := task.XML()
	for _, part := range []string{`<UserId>PC\ana</UserId>`, `<UserId>S-1-5-21-7</UserId>`, bootTaskMarker, `<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>`} {
		if !strings.Contains(text, part) {
			t.Fatalf("task lacks %q", part)
		}
	}
}

// Installed with nobody at the desktop (over SSH), Mewla is started directly,
// but as the task starts it: under a headless console host of its own, the
// parent `mewla update` and `mewla restart` look for before restarting it.
func TestBootTaskDirectStartIsTheTasksCommand(t *testing.T) {
	withTaskUser(t)
	config := taskConfig(t)
	task, err := bootTaskDefinition(config)
	if err != nil {
		t.Fatal(err)
	}
	cmd := bootTaskDirectCommand(config)
	if cmd.Path != task.Command && filepath.Base(cmd.Path) != filepath.Base(task.Command) {
		t.Fatalf("direct start runs %s, the task %s", cmd.Path, task.Command)
	}
	if got := platform.QuoteArgs(cmd.Args[1:]); got != task.Arguments {
		t.Fatalf("direct start arguments = %s\nthe task's            %s", got, task.Arguments)
	}
}

func TestBootTaskNeverTouchesAForeignTask(t *testing.T) {
	withTaskUser(t)
	runner := &taskRunner{existing: "<Task><RegistrationInfo><Description>someone else's</Description></RegistrationInfo></Task>"}
	var out bytes.Buffer
	if err := bootTaskUninstall(runner, &out); err == nil || !strings.Contains(err.Error(), "did not create") {
		t.Fatalf("uninstall of a foreign task = %v", err)
	}
	for _, call := range runner.calls {
		if call[1] == "/delete" || call[1] == "/end" {
			t.Fatalf("ran %v on a foreign task", call)
		}
	}
	runner.existing = "<Description>Starts Mewla when you log in. " + bootTaskMarker + ".</Description>"
	if err := bootTaskUninstall(runner, &out); err != nil {
		t.Fatal(err)
	}
	last := runner.calls[len(runner.calls)-1]
	if strings.Join(last, " ") != "schtasks /delete /tn Mewla (ana) /f" {
		t.Fatalf("last call = %v", last)
	}
}

// healthAs serves /health as the daemon id, on 127.0.0.1.
func healthAs(t *testing.T, daemonID string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"daemon_id": daemonID, "version": "0.4.6"})
	}))
	t.Cleanup(server.Close)
	return strings.TrimPrefix(server.URL, "http://")
}

// The Task Scheduler gives back the principal as the user's SID or account.
func TestBootTaskOfAnotherWindowsUserIsTheirs(t *testing.T) {
	withTaskUser(t)
	legacy := func(principal string) string {
		return "<Task><RegistrationInfo><Description>Starts Mewla when you log in. " + bootTaskMarker + ".</Description></RegistrationInfo>" +
			"<Triggers><LogonTrigger><UserId>PC\\x</UserId></LogonTrigger></Triggers><Principals><Principal id=\"Author\"><UserId>" + principal + "</UserId></Principal></Principals></Task>"
	}
	for principal, ours := range map[string]bool{"S-1-5-21-7": true, `PC\ana`: true, "ana": true, "S-1-5-21-8": false, `PC\bo`: false, "bo": false} {
		runner := &fakeBootRunner{responses: map[string]string{"schtasks /query /tn Mewla /xml": legacy(principal)}, failures: map[string]bool{"schtasks /query /tn Mewla (ana) /xml": true}}
		task, err := bootTaskFind(runner)
		if err != nil {
			t.Fatal(err)
		}
		if task.Registered != ours || (ours && (task.Name != "Mewla" || !task.Legacy)) || (!ours && task.Name != "Mewla (ana)") {
			t.Fatalf("principal %s: %+v", principal, task)
		}
	}
	if got := bootTaskNameFor(`PC\a:b*c`); got != "Mewla (a_b_c)" {
		t.Fatalf("name = %q", got)
	}
}

// Two people share one Windows computer (the second ran install.ps1 after the
// first). The second's install used to replace the first's "Mewla" task and
// take the first's daemon on 9876 for its own ("Mewla is already running").
func TestBootInstallLeavesAnotherWindowsUsersTaskAndDaemonAlone(t *testing.T) {
	withTaskUser(t)
	config := taskConfig(t)
	if err := os.MkdirAll(filepath.Dir(config.Binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir, ours := restartStateDir(t)
	config.StateDir = stateDir
	// Another user's Mewla answers on the address boot install starts from.
	previous := bootTaskScanFrom
	bootTaskScanFrom = healthAs(t, "someone-else")
	t.Cleanup(func() { bootTaskScanFrom = previous })
	config.Addr = bootTaskScanFrom

	running, note, err := bootTaskPickAddr(&config)
	if err != nil || running || config.Addr == bootTaskScanFrom || !strings.Contains(note, "another user's Mewla") {
		t.Fatalf("picked %s running=%v note=%q err=%v", config.Addr, running, note, err)
	}
	// The address taken by this user's own Mewla is the one it keeps.
	config.Addr = healthAs(t, ours)
	if running, note, err := bootTaskPickAddr(&config); err != nil || !running || note != "" {
		t.Fatalf("own daemon: running=%v note=%q err=%v", running, note, err)
	}

	foreign := "<Task><Description>Starts Mewla when you log in. " + bootTaskMarker + ".</Description><Principals><Principal id=\"Author\"><UserId>S-1-5-21-8</UserId></Principal></Principals></Task>"
	runner := &fakeBootRunner{responses: map[string]string{"schtasks /query /tn Mewla /xml": foreign}, failures: map[string]bool{"schtasks /query /tn Mewla (ana) /xml": true}}
	var out bytes.Buffer
	if err := bootTaskInstall(config, runner, &out); err != nil {
		t.Fatalf("install: %v\n%s", err, out.String())
	}
	for _, call := range runner.calls {
		if len(call) > 3 && call[2] == "/tn" && call[3] == "Mewla" && call[1] != "/query" {
			t.Fatalf("touched the other user's task: %v", call)
		}
	}
	if runner.count("schtasks /create /tn Mewla (ana) /xml "+bootTaskXMLPath(config)+" /f") != 1 || !strings.Contains(out.String(), `"Mewla (ana)"`) {
		t.Fatalf("calls %v\n%s", runner.calls, out.String())
	}

	// This user's own task of an earlier version moves to its own name.
	runner = &fakeBootRunner{responses: map[string]string{"schtasks /query /tn Mewla /xml": strings.Replace(foreign, "S-1-5-21-8", "S-1-5-21-7", 1)}, failures: map[string]bool{"schtasks /query /tn Mewla (ana) /xml": true}}
	if err := bootTaskInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	if runner.count("schtasks /delete /tn Mewla /f") != 1 {
		t.Fatalf("the earlier task stayed: %v", runner.calls)
	}
}
