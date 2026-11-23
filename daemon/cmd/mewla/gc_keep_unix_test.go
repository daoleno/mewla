//go:build linux || darwin

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/statedir"
	"github.com/daoleno/mewla/daemon/workerproc"
)

// keepSession lays out a Session's scratch and lease under a fresh state
// root; with recorded, the lease records this test process (as the runtime
// records a Session's processes).
func keepSession(t *testing.T, recorded bool) (root, scratch string) {
	t.Helper()
	root = t.TempDir()
	statedir.SetRoot(root)
	t.Cleanup(func() { statedir.SetRoot("") })
	owner := "abc"
	unit := "mewla-worker-abc-0123456789abcdef0123456789abcdef.scope"
	scratch = filepath.Join(root, "t", gc.ScratchName(unit))
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, gc.ScratchMarker), []byte(unit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lease := workerproc.Lease{Version: 1, ResourceID: unit}
	if recorded {
		records, err := workerproc.Processes(true)
		if err != nil {
			t.Fatal(err)
		}
		lease.Observed = []workerproc.ProcessIdentity{{PID: os.Getpid(), Start: records[os.Getpid()].Start}}
	}
	dir := filepath.Join(root, "run", "worker-resources", owner)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(lease)
	if err := os.WriteFile(filepath.Join(dir, unit+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEWLA_WORKER_RESOURCE_UNIT", unit)
	t.Setenv("MEWLA_WORKER_RESOURCE_OWNER", owner)
	t.Setenv("TMPDIR", scratch)
	return root, scratch
}

func TestGCKeepIsRefusedToAProcessTheSessionDidNotStart(t *testing.T) {
	_, scratch := keepSession(t, false)
	file := filepath.Join(scratch, "out.bin")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runGCKeep([]string{file, "--until", "3d", "--why", "x"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not one of") {
		t.Fatalf("keep by a stranger = %v", err)
	}
}

func TestGCKeepAndReleaseActOnlyOnTheSessionsOwnScratch(t *testing.T) {
	root, scratch := keepSession(t, true)
	file := filepath.Join(scratch, "out.bin")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "elsewhere")
	if err := os.WriteFile(outside, []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(scratch, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{outside, link, scratch, filepath.Join(scratch, gc.ScratchMarker)} {
		if err := runGCKeep([]string{bad, "--until", "3d", "--why", "x"}, io.Discard, io.Discard); err == nil {
			t.Fatalf("kept %s", bad)
		}
	}
	if err := runGCKeep([]string{file, "--until", "40d", "--why", "x"}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "ask Brain") {
		t.Fatalf("a 40-day keep = %v", err)
	}
	var out bytes.Buffer
	if err := runGCKeep([]string{file, "--until", "3d", "--why", "the release gate reads it"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "When this Session ends it moves to "+filepath.Join(root, "gc", "kept")) {
		t.Fatalf("keep printed %q", out.String())
	}
	out.Reset()
	if err := runGCStatus(nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Kept: 1") || !strings.Contains(out.String(), "the release gate reads it") {
		t.Fatalf("status:\n%s", out.String())
	}
	out.Reset()
	if err := runGCRelease([]string{file}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Moved "+file+" to the trash") {
		t.Fatalf("release printed %q", out.String())
	}
	if keeps, _ := gc.ListKeeps(root); len(keeps) != 0 {
		t.Fatalf("release left keeps %+v", keeps)
	}
	if items, _ := gc.List(root); len(items) != 1 || items[0].Reason != gc.ReasonReleased {
		t.Fatalf("trash %+v", items)
	}
	if err := runGCRelease([]string{outside}, io.Discard, io.Discard); err == nil {
		t.Fatal("released a path outside the scratch")
	}
}
