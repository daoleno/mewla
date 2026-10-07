//go:build linux

package watcher

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/workerproc"
)

func TestRealWorkerCloseCleansDetachedDescendantsWithoutSupervisor(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	manager := newTestPortableResourceManager(t, "cleanupfixture")
	h.w.resources = manager
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(h.root, "owned-tool.pid")
	cmd := "setsid /bin/sh -c " + shellQuote("echo $$ > "+shellQuote(marker)+"; exec sleep 90") + " & exec /bin/sh"
	target, err := h.w.CreateSession("", CreateSessionOptions{Name: "ownership-cleanup", Cwd: cwd, Command: cmd, Detached: true, Delegated: true, ProgressEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.w.KillSession(target) })
	var pid int
	waitForHarness(t, "detached owned tool", func() bool {
		raw, err := os.ReadFile(marker)
		if err != nil {
			return false
		}
		pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		return pid > 1
	})
	records, err := workerproc.Processes(true)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := records[pid]
	if !ok || tool.ResourceID != manager.UnitForTarget(target) {
		t.Fatalf("tool ownership missing %+v", tool)
	}
	for _, p := range records {
		if p.ResourceID == tool.ResourceID && strings.Contains(p.Args, "__supervise") {
			t.Fatal("supervisor still exists")
		}
	}
	// A fresh manager simulates daemon restart without touching the live pane.
	replacement := newTestPortableResourceManager(t, "cleanupfixture")
	replacement.leaseDir = manager.leaseDir
	replacement.tempRoot = manager.tempRoot
	replacement.Reconcile([]tmuxPane{{target: target, delegated: true, resourceUnit: tool.ResourceID}})
	h.w.resources = replacement
	if !h.w.HasSession(target) {
		t.Fatal("re-observation killed the live Worker")
	}
	if err = h.w.KillSession(target); err != nil {
		t.Fatal(err)
	}
	records, err = workerproc.Processes(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok = records[pid]; ok {
		t.Fatal("detached descendant remains after close")
	}
	t.Logf("plain Worker %s re-observed; close reclaimed detached PID %d", target, pid)
}
