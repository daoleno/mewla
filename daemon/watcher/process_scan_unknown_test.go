package watcher

import (
	"testing"
	"time"
)

func TestPollKeepsWorkerIdentityWhenPSFails(t *testing.T) {
	w := New(time.Second)
	started := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	windows := []tmuxPane{{target: "sess-a:@1", name: "alpha", cwd: "/repo/a", command: "claude", panePID: 111}}
	content := map[string]string{"sess-a:@1": contentA}
	restore := installFakePollSeams(w, windows, content, map[int]processInfo{111: fakeProcess(111, started)})
	defer restore()

	w.poll()
	before := workerByID(w.Workers(), "sess-a:@1")
	if before == nil || before.ProcessID != 111 || before.StartedAt.IsZero() {
		t.Fatalf("first poll did not record the process identity: %+v", before)
	}
	wantCommand, wantStart, wantPID := before.Command, before.StartedAt, before.ProcessID

	// ps failing yields a nil table: unknown, not "no processes".
	w.snapshotProcesses = func() map[int]processInfo { return nil }
	w.poll()
	after := workerByID(w.Workers(), "sess-a:@1")
	if after == nil {
		t.Fatal("worker vanished after a failed ps")
	}
	if after.Command != wantCommand || !after.StartedAt.Equal(wantStart) || after.ProcessID != wantPID {
		t.Fatalf("failed ps rewrote identity: command %q->%q start %v->%v pid %d->%d",
			wantCommand, after.Command, wantStart, after.StartedAt, wantPID, after.ProcessID)
	}
}

func TestWaitForPaneProcessExitTreatsFailedPSAsUnknown(t *testing.T) {
	w := New(time.Second)
	w.snapshotProcesses = func() map[int]processInfo { return nil }
	if w.WaitForPaneProcessExit("sess-a:@1", 4242, 250*time.Millisecond) {
		t.Fatal("a failed ps was taken as proof that the process exited")
	}
	w.snapshotProcesses = func() map[int]processInfo { return map[int]processInfo{1: fakeProcess(1, time.Now())} }
	if !w.WaitForPaneProcessExit("sess-a:@1", 4242, 250*time.Millisecond) {
		t.Fatal("a process missing from a good snapshot must count as exited")
	}
}
