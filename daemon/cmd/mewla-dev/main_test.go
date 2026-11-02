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

func TestDevWatchesWebSourcesWithoutBuildOutputs(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"daemon/connections", "app/components/plugins", "app/node_modules/pkg", "daemon/webui/dist", "scripts"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	tree, err := newWatchTree(root)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	for _, path := range []string{"app/components/plugins/PluginsFlow.tsx", "app/assets/icon.png", "app/package.json", "bun.lock", "scripts/build-web-ui.sh", "daemon/connections/onboarding.go"} {
		if _, ok := tree.relevantPath(filepath.Join(root, path)); !ok {
			t.Errorf("change would leave the running build stale: %s", path)
		}
	}
	for _, path := range []string{"app/components/plugins/flow.test.ts", "app/node_modules/pkg/index.js", "app/dist/index.html", "daemon/webui/dist/index.html.gz", "daemon/tmp/mewla-dev", "daemon/connections/aws_test.go", "docs/plugins.md"} {
		if _, ok := tree.relevantPath(filepath.Join(root, path)); ok {
			t.Errorf("unnecessary rebuild: %s", path)
		}
	}
	if _, ok := tree.watched[filepath.Join(root, "app/components/plugins")]; !ok {
		t.Fatal("web sources are not watched")
	}
	if _, ok := tree.watched[filepath.Join(root, "app/node_modules/pkg")]; ok {
		t.Fatal("watching dependencies")
	}
}

func TestDevFailedWebExportKeepsPreviousBinary(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "daemon"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts/build-web-ui.sh"), []byte("exit 42\n"), 0644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "mewla")
	if err := os.WriteFile(binary, []byte("previous daemon"), 0755); err != nil {
		t.Fatal(err)
	}
	runner := &devRunner{root: filepath.Join(root, "daemon"), binary: binary, webDirty: true}
	if err := runner.rebuild(); err == nil || !strings.Contains(err.Error(), "web UI build failed") {
		t.Fatalf("export failure: %v", err)
	}
	got, err := os.ReadFile(binary)
	if err != nil || string(got) != "previous daemon" || !runner.webDirty {
		t.Fatalf("failed export replaced daemon or cleared retry: %s, %v", got, err)
	}
}
