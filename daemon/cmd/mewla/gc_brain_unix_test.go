//go:build linux || darwin

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/gc"
)

// Brain's host has no Session scratch: once the daemon places the caller in
// it, keep and release act for Brain on the t/ root, outside every Session's
// scratch.
func TestBrainsHostKeepsAndReleasesInTheTRoot(t *testing.T) {
	root := t.TempDir()
	tdir := filepath.Join(root, "t")
	backup := filepath.Join(tdir, "mewla-dev.backup")
	loose := filepath.Join(tdir, "probe-out")
	scratch := filepath.Join(tdir, "Ab3dE6")
	for _, dir := range []string{loose, scratch} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{backup: "x", filepath.Join(scratch, gc.ScratchMarker): "mewla-worker-abc\n", filepath.Join(root, "brain.db"): "x"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MEWLA_WORKER_RESOURCE_UNIT", "")
	saved := gcBrainCaller
	t.Cleanup(func() { gcBrainCaller = saved })
	gcBrainCaller = func() (string, bool) { return "", false }
	if err := runGCKeep([]string{backup, "--until", "7d", "--why", "x"}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "not inside a Mewla Session") {
		t.Fatalf("keep outside a Session and Brain = %v", err)
	}
	gcBrainCaller = func() (string, bool) { return root, true }
	for _, bad := range []string{tdir, filepath.Join(root, "brain.db"), filepath.Join(scratch, gc.ScratchMarker)} {
		if err := runGCKeep([]string{bad, "--until", "7d", "--why", "x"}, io.Discard, io.Discard); err == nil {
			t.Fatalf("Brain kept %s", bad)
		}
	}
	var out bytes.Buffer
	if err := runGCKeep([]string{backup, "--until", "7d", "--why", "rollback binary"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	keeps, err := gc.ListKeeps(root)
	if err != nil || len(keeps) != 1 || keeps[0].Owner != gc.OwnerBrain || keeps[0].Path != backup || !strings.Contains(out.String(), "goes to the trash") {
		t.Fatalf("keeps = %+v %v; said %q", keeps, err, out.String())
	}
	if err := runGCRelease([]string{scratch}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "Session's scratch") {
		t.Fatalf("Brain released a Session's scratch: %v", err)
	}
	// A path a process uses stays: a child works in it.
	child := exec.Command("sleep", "30")
	child.Dir = loose
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	err = runGCRelease([]string{loose}, io.Discard, io.Discard)
	_ = child.Process.Kill()
	_ = child.Wait()
	if err == nil || !strings.Contains(err.Error(), "uses") {
		t.Fatalf("Brain released a path in use: %v", err)
	}
	out.Reset()
	if err := runGCRelease([]string{loose}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := runGCRelease([]string{keeps[0].ID}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	items, err := gc.List(root)
	if err != nil || len(items) != 2 {
		t.Fatalf("trash = %+v %v", items, err)
	}
	reasons := map[string]string{}
	for _, item := range items {
		reasons[item.Original] = item.Owner + ": " + item.Reason
	}
	if reasons[loose] != "brain: "+gc.ReasonReleasedByBrain || reasons[backup] != "brain: "+gc.ReasonReleasedByBrain {
		t.Fatalf("trashed = %v", reasons)
	}
	if left, _ := gc.ListKeeps(root); len(left) != 0 {
		t.Fatalf("keeps after release = %+v", left)
	}
}
