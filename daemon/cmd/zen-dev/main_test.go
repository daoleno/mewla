package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSurfacesUnexpectedDaemonExit(t *testing.T) {
	// Keep this focused on the lifecycle contract rather than building the real
	// daemon: a finished child must be observable as a failure, never leave the
	// watcher presenting itself as a live daemon.
	cmd := exec.Command("sh", "-c", "exit 23")
	child := &runningProcess{cmd: cmd, done: make(chan error, 1)}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { child.done <- cmd.Wait() }()
	err := <-child.done
	if err == nil || !strings.Contains(err.Error(), "exit status 23") {
		t.Fatalf("child exit err=%v", err)
	}
	if got := unexpectedDaemonExit(err); !errors.Is(got, err) ||
		!strings.Contains(got.Error(), "daemon exited unexpectedly") {
		t.Fatalf("diagnostic=%v", got)
	}
	if got := unexpectedDaemonExit(nil); got == nil ||
		got.Error() != "daemon exited unexpectedly" {
		t.Fatalf("clean child exit diagnostic=%v", got)
	}
}

func TestDevBuildUsesReleasePublisherIdentity(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	flags, err := publisherFlags(root)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "..", "release", "plugin-publishers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		GitHub struct {
			ClientID string `json:"client_id"`
		} `json:"github"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.GitHub.ClientID == "" || !strings.Contains(flags, "connections.GitHubPublicClientID="+manifest.GitHub.ClientID) {
		t.Fatalf("dev publisher flags: %q", flags)
	}
	if _, err := publisherFlags(t.TempDir()); err == nil {
		t.Fatal("missing publisher configuration silently ignored")
	}
}
