package watcher

import (
	"testing"
	"time"
)

// A parent-id cycle, as a reused pid makes on Windows, once grew the walk's
// queue until the daemon ran out of memory.
func TestDescendantWalksSurviveParentCycle(t *testing.T) {
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 20},
		20: {pid: 20, ppid: 10},
		30: {pid: 30, ppid: 20},
		40: {pid: 40, ppid: 20},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if got := descendantProcesses(10, processes); len(got) != 3 {
			t.Errorf("descendantProcesses = %d processes, want 3 (20, 30, 40)", len(got))
		}
		if got := descendantPIDsIncludingRoot(10, processes); len(got) != 4 {
			t.Errorf("descendantPIDsIncludingRoot = %v, want 10, 20, 30, 40", got)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("descendant walk did not end on a parent cycle")
	}
}

func TestDropReusedParents(t *testing.T) {
	base := time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC)
	processes := map[int]processInfo{
		// 5692's parent exited; its pid 4380 now names a later process.
		5692: {pid: 5692, ppid: 4380, startedAt: base},
		4380: {pid: 4380, ppid: 5692, startedAt: base.Add(time.Minute)},
		// A real parent, started first.
		100: {pid: 100, ppid: 0, startedAt: base.Add(-time.Hour)},
		200: {pid: 200, ppid: 100, startedAt: base},
		// An unknown start time proves nothing, so the parent stays.
		300: {pid: 300, ppid: 100},
	}
	dropReusedParents(processes)
	if got := processes[5692].ppid; got != 0 {
		t.Fatalf("5692 ppid = %d, want 0 (its recorded parent started after it)", got)
	}
	if got := processes[4380].ppid; got != 5692 {
		t.Fatalf("4380 ppid = %d, want 5692", got)
	}
	if processes[200].ppid != 100 || processes[300].ppid != 100 {
		t.Fatalf("real parents changed: %+v %+v", processes[200], processes[300])
	}
}
