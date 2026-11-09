package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
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
	if strings.Join(last, " ") != "schtasks /delete /tn Mewla /f" {
		t.Fatalf("last call = %v", last)
	}
}
