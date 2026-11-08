//go:build linux

package watcher

import (
	"os/exec"
	"testing"
	"time"
)

// Deciding which process a pane runs used to read every process of the machine
// on each decision. It must read the panes' own trees and leave the rest to
// the shared cold reading.
func TestTargetProcessSnapshotRereadsOnlyThePaneTree(t *testing.T) {
	pane := exec.Command("sleep", "63")
	if err := pane.Start(); err != nil {
		t.Skipf("cannot spawn: %v", err)
	}
	defer func() { _ = pane.Process.Kill(); _, _ = pane.Process.Wait() }()
	pid := pane.Process.Pid

	if _, err := snapshotProcessesNearErr(nil, pid); err != nil {
		t.Fatal(err)
	}
	full0, _ := selfReads(t)
	if _, err := snapshotProcessTable(); err != nil {
		t.Fatal(err)
	}
	full1, _ := selfReads(t)
	fullCost := full1 - full0

	near0, _ := selfReads(t)
	table, err := snapshotProcessesNearErr(nil, pid)
	if err != nil {
		t.Fatal(err)
	}
	near1, _ := selfReads(t)
	if fullCost < 200 {
		t.Skipf("only %d reads for a full walk: too few processes to compare", fullCost)
	}
	if (near1-near0)*4 > fullCost {
		t.Fatalf("a pane's snapshot took %d read syscalls against %d for a full walk", near1-near0, fullCost)
	}
	if got := table[pid]; got.comm != "sleep" || got.args != "sleep 63" || time.Since(got.startedAt) > time.Minute {
		t.Fatalf("pane process = %+v", got)
	}

	// A child started since is in the very next snapshot.
	child := exec.Command("sleep", "64")
	if err := child.Start(); err != nil {
		t.Skipf("cannot spawn: %v", err)
	}
	defer func() { _ = child.Process.Kill(); _, _ = child.Process.Wait() }()
	table, err = snapshotProcessesNearErr(nil, pid)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := table[child.Process.Pid]; !ok || got.args != "sleep 64" {
		t.Fatalf("a process started since the last snapshot is missing: %+v", got)
	}
}
