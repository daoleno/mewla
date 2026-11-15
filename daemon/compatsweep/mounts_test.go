//go:build linux

package compatsweep

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMounts: TMPDIR on a noexec tmpfs, so on another filesystem than HOME
// (on disk), and a Worker folder on a read-only mount. (HOME itself on tmpfs
// is refused for Worker folders on purpose: memory-backed storage.) Needs
// MEWLA_SWEEP_MOUNT_ROOT naming a directory holding noexec/ (tmpfs, noexec) and
// ro/ (a read-only bind mount with project/ in it);
// scripts/tests/linux_sweep.sh mounts them in a user namespace, no root
// needed.
func TestMounts(t *testing.T) {
	root := os.Getenv("MEWLA_SWEEP_MOUNT_ROOT")
	if root == "" {
		t.Skip("scripts/tests/linux_sweep.sh runs it with MEWLA_SWEEP_MOUNTS=1")
	}
	tmp, err := os.MkdirTemp(filepath.Join(root, "noexec"), "tmp")
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, envOptions{
		executors:    "[[executors]]\nname = \"claude\"\ncommand = \"claude\"\n",
		hostExecutor: "claude",
		tmpdir:       tmp,
	})
	out := filepath.Join(e.dir, "launches")
	if err := os.MkdirAll(out, 0o700); err != nil {
		t.Fatal(err)
	}
	recordingAgent(t, filepath.Join(e.tools, "claude"), out)
	e.start()
	brain := waitBrainLaunch(out, 60*time.Second)
	record(t, "mount-noexec-tmpdir", map[bool]string{true: "pass", false: "fail"}[brain],
		fmt.Sprintf("Brain host started with TMPDIR on a noexec tmpfs (HOME on disk): %v", brain)+map[bool]string{true: "", false: "\n" + e.logTail(15)}[brain])
	if !brain {
		t.Error("Brain host did not start")
	}
	for _, folder := range []struct{ name, cwd string }{
		{"mount-worker-home-tmpdir-other-fs", filepath.Join(e.home, "project")},
		{"mount-worker-read-only", filepath.Join(root, "ro", "project")},
	} {
		_ = os.MkdirAll(folder.cwd, 0o700)
		clearLaunches(t, out)
		response, err := e.cli(60*time.Second, "worker", "spawn", "@state", "-name", folder.name, "-executor", "claude",
			"-cwd", folder.cwd, "-prompt", "mount check", "-profile", "quick")
		got, launched := waitLaunch(out, 30*time.Second)
		ok := err == nil && launched && got.Cwd == folder.cwd
		detail := "the agent started in " + folder.cwd
		if !ok {
			detail = fmt.Sprintf("spawn err=%v launched=%v cwd=%q: %s", err, launched, got.Cwd, oneLine(response))
		}
		record(t, folder.name, map[bool]string{true: "pass", false: "fail"}[ok], detail)
		if !ok {
			t.Error(detail)
		}
	}
}
