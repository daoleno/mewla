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
	for _, dir := range []string{"daemon/connections", "client/ui/chat", "client/core/files", "client/ui/node_modules/pkg", "client/android/app", "daemon/webui/dist", "scripts"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	tree, err := newWatchTree(root)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	for _, path := range []string{"client/ui/chat/Composer.tsx", "client/ui/assets/icon.png", "client/ui/nsh.json", "client/core/files/upload.ts", "scripts/build-web-ui.sh", "daemon/connections/onboarding.go"} {
		if _, ok := tree.relevantPath(filepath.Join(root, path)); !ok {
			t.Errorf("change would leave the running build stale: %s", path)
		}
	}
	for _, path := range []string{"client/core/files/upload.test.ts", "client/ui/chat/Chat.test.tsx", "client/ui/testing/fakeDaemon.ts", "client/ui/node_modules/pkg/index.js", "client/android/app/build.gradle.kts", "daemon/webui/dist/index.html.gz", "daemon/tmp/mewla-dev", "daemon/connections/aws_test.go", "docs/plugins.md"} {
		if _, ok := tree.relevantPath(filepath.Join(root, path)); ok {
			t.Errorf("unnecessary rebuild: %s", path)
		}
	}
	if _, ok := tree.watched[filepath.Join(root, "client/ui/chat")]; !ok {
		t.Fatal("web sources are not watched")
	}
	if _, ok := tree.watched[filepath.Join(root, "client/ui/node_modules/pkg")]; ok {
		t.Fatal("watching dependencies")
	}
	if _, ok := tree.watched[filepath.Join(root, "client/android/app")]; ok {
		t.Fatal("watching the native hosts")
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

// fakeGo records go build calls and writes the -o output unless told to fail.
type fakeGo struct {
	calls      [][]string
	failTagged bool
	failPlain  bool
}

func (f *fakeGo) build(dir string, env, args []string) ([]byte, error) {
	f.calls = append(f.calls, append(append([]string(nil), env...), args...))
	tagged := strings.Contains(strings.Join(args, " "), "-tags ghosttyvt")
	if (tagged && f.failTagged) || (!tagged && f.failPlain) {
		return []byte("# github.com/daoleno/mewla/daemon/ghosttyvt\n/usr/bin/ld: cannot find -lghostty-vt\ncollect2: error: ld returned 1 exit status\n"), errors.New("exit status 1")
	}
	for i, arg := range args {
		if arg == "-o" {
			mode := "plain"
			if tagged {
				mode = "tagged"
			}
			return nil, os.WriteFile(args[i+1], []byte(mode), 0o755)
		}
	}
	return nil, errors.New("no -o")
}

func devRunnerForBuild(t *testing.T, libs bool, fake *fakeGo) (*devRunner, *strings.Builder) {
	t.Helper()
	root := t.TempDir()
	if libs {
		if err := os.MkdirAll(filepath.Join(root, engineLibDir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var stderr strings.Builder
	return &devRunner{root: root, binary: filepath.Join(root, "mewla-dev"), stderr: &stderr, stdout: &stderr, goBuild: fake.build}, &stderr
}

func TestDevBuildIsUntaggedWithoutEngineLibs(t *testing.T) {
	fake := &fakeGo{}
	r, _ := devRunnerForBuild(t, false, fake)
	if err := r.buildDaemon("-X x=y"); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 || strings.Contains(strings.Join(fake.calls[0], " "), "ghosttyvt") || fake.calls[0][0] != "CGO_ENABLED=0" {
		t.Fatalf("calls = %q", fake.calls)
	}
	if got, _ := os.ReadFile(r.binary); string(got) != "plain" {
		t.Fatalf("binary = %q", got)
	}
}

func TestDevBuildIsTaggedWithEngineLibs(t *testing.T) {
	fake := &fakeGo{}
	r, stderr := devRunnerForBuild(t, true, fake)
	if err := r.buildDaemon("-X x=y"); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "CGO_ENABLED=1" || !strings.Contains(strings.Join(fake.calls[0], " "), "-tags ghosttyvt ./cmd/mewla") {
		t.Fatalf("calls = %q", fake.calls)
	}
	if got, _ := os.ReadFile(r.binary); string(got) != "tagged" || stderr.Len() != 0 {
		t.Fatalf("binary = %q, stderr = %q", got, stderr.String())
	}
}

// TestDevBuildFallsBackWhenTheTaggedBuildFails: a reload never leaves the
// daemon down; one untagged build follows and one line says why.
func TestDevBuildFallsBackWhenTheTaggedBuildFails(t *testing.T) {
	fake := &fakeGo{failTagged: true}
	r, stderr := devRunnerForBuild(t, true, fake)
	if err := os.WriteFile(r.binary, []byte("previous"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.buildDaemon("-X x=y"); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 2 || fake.calls[1][0] != "CGO_ENABLED=0" || strings.Contains(strings.Join(fake.calls[1], " "), "ghosttyvt") {
		t.Fatalf("calls = %q", fake.calls)
	}
	if got, _ := os.ReadFile(r.binary); string(got) != "plain" {
		t.Fatalf("binary = %q", got)
	}
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "cannot find -lghostty-vt") || !strings.Contains(lines[0], "built without it") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if log, _ := os.ReadFile(r.binary + "-tagged-build.log"); !strings.Contains(string(log), "collect2") {
		t.Fatalf("tagged build log = %q", log)
	}
	if _, err := os.Stat(r.binary + ".building"); !os.IsNotExist(err) {
		t.Fatalf("leftover partial binary: %v", err)
	}
}

func TestDevBuildKeepsThePreviousBinaryWhenBothBuildsFail(t *testing.T) {
	fake := &fakeGo{failTagged: true, failPlain: true}
	r, _ := devRunnerForBuild(t, true, fake)
	if err := os.WriteFile(r.binary, []byte("previous"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.buildDaemon("-X x=y"); err == nil {
		t.Fatal("a failed build reported success")
	}
	if got, _ := os.ReadFile(r.binary); string(got) != "previous" {
		t.Fatalf("binary = %q", got)
	}
}
