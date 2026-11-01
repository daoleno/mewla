package watcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

func TestPollUnavailableInventoryRetainsLiveOwnership(t *testing.T) {
	for _, phase := range []string{"inventory", "presence", "ownership"} {
		t.Run(phase, func(t *testing.T) {
			script := `cmd=
for arg in "$@"; do
 case "$arg" in list-panes|show-options) cmd=$arg ;; esac
 case "$arg" in *window_name*) cmd=inventory ;; esac
done
`
			switch phase {
			case "inventory":
				script += `echo 'no server running on fixture' >&2; exit 1`
			case "presence":
				script += `if [ "$cmd" = inventory ]; then printf '%%1\tfixture\t/repo\tcodex\t1\t0\t1\t\t\n'; exit 0; fi
echo 'no server running on fixture' >&2; exit 1`
			case "ownership":
				script += `if [ "$cmd" = inventory ]; then printf '%%1\tfixture\t/repo\tcodex\t1\t0\t1\t\t\n'; exit 0; fi
if [ "$cmd" = list-panes ]; then printf '%%1\n'; exit 0; fi
echo 'no server running on fixture' >&2; exit 1`
			}
			t.Setenv("PATH", writeFakeTmux(t, script))
			w, ledger := missingWorkerFixture(t)
			worker := w.workers["missing:@1"]
			delete(w.workers, "missing:@1")
			worker.ID = "%1"
			w.workers["%1"] = worker
			w.workerOrder = []string{"%1"}
			w.workerEpoch["%1"] = w.workerEpoch["missing:@1"]
			cached := w.ledgerTurns["missing:@1"]
			cached.SessionID = "%1"
			w.ledgerTurns["%1"] = cached
			if turn, ok := ledger.turns["missing:@1"]; ok {
				delete(ledger.turns, "missing:@1")
				turn.SessionID = "%1"
				ledger.turns["%1"] = turn
			}
			w.listPanes = w.listTmuxPanes
			w.poll()
			if w.GetWorker("%1") == nil {
				t.Fatal("unavailable observation discarded owned live Session")
			}
			if len(ledger.applied) != 0 || len(w.events) != 0 {
				t.Fatal("unavailable observation emitted disappearance evidence")
			}
			if w.SnapshotReady() {
				t.Fatal("failed first inventory advertised readiness")
			}
			// A later successful inventory is still authoritative: retaining
			// Unknown must not cache an identity forever after real removal.
			// The server is now really gone: nothing listens on its socket.
			t.Setenv("PATH", writeFakeTmux(t, `exit 1`))
			w.listPanes = func() ([]tmuxPane, error) { return nil, nil }
			w.poll()
			if w.GetWorker("%1") != nil || len(ledger.applied) != 1 || !ledger.applied[0].ProcessDead {
				t.Fatal("confirmed absence did not converge after the observation gap")
			}

		})
	}
}

func TestReleasedSignalTurnWithLiveActivityUsesFreshAdmission(t *testing.T) {
	for _, activity := range []string{"bound-activity", "newer-native-activity"} {
		t.Run(activity, func(t *testing.T) {
			ledger := newFakeTurnLedger()
			owner := newLedgerSessionInputOwner(newFakeSessionInputIO(), ledger)
			turn := TurnSnapshot{SessionID: "fixture:@1", TurnID: "released", Status: TurnUnknown, SignalProtocol: true, ActivityID: "bound-activity"}
			decision, err := owner.reconcileSubmissionActivity(turn.SessionID, turn, ProviderActivityObservation{ID: activity, Status: "running", Structured: true})
			if err != nil || decision.Mode != delegatedReuseFresh {
				t.Fatalf("released signal turn selected steer: mode=%v err=%v", decision.Mode, err)
			}
			if len(ledger.applied) != 0 {
				t.Fatal("reuse decision revived historical activity")
			}
		})
	}
}

func TestWaitForSnapshotCompletesOnlyAfterFullPollAndCancels(t *testing.T) {
	w := New(time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { waiting <- w.WaitForSnapshot(ctx) }()
	select {
	case <-waiting:
		t.Fatal("unstarted discovery passed the barrier")
	default:
	}
	cancel()
	select {
	case err := <-waiting:
		if err != context.Canceled {
			t.Fatalf("cancel=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("discovery wait did not cancel")
	}
	restore := installFakePollSeams(w, testWindows()[:1], map[string]string{"sess-a:@1": contentA}, liveSessionProcesses())
	defer restore()
	probe := &blockingActivityProbe{started: make(chan struct{}, 1), release: make(chan struct{})}
	w.SetActivityProbe(probe)
	done := make(chan struct{})
	go func() { defer close(done); w.poll() }()
	<-probe.started
	if w.SnapshotReady() {
		t.Fatal("partial discovery marked ready")
	}
	ready := make(chan error, 1)
	go func() { ready <- w.WaitForSnapshot(context.Background()) }()
	select {
	case <-ready:
		t.Fatal("slow provider evidence bypassed the barrier")
	default:
	}
	close(probe.release)
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("completed discovery did not release startup")
	}
	<-done
	if err := w.WaitForSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A computer with no tmux server running (after a reboot, or tmux never used)
// has an empty first inventory. Treating it as unavailable left the Watcher
// never ready, so Brain's host, which starts after the first inventory, never
// started and the Brain tab stayed on "Waking up".
func TestFirstInventoryWithNoTmuxServerIsReadyAndEmpty(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("PATH", writeFakeTmux(t, `echo 'no server running on fixture' >&2; exit 1`))
	noServer := filepath.Join(t.TempDir(), "no-server")
	w := New(time.Second)
	w.tmuxSocketPath = noServer
	w.listPanes = w.listTmuxPanes
	if w.SnapshotReady() {
		t.Fatal("ready before any poll")
	}
	w.poll()
	if !w.SnapshotReady() {
		t.Fatal("first inventory with no tmux server never became ready")
	}
	if err := w.WaitForSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Once an inventory has completed, a vanished server is Unknown again:
	// only the first observation may read "no server" as "no Sessions".
	w2 := New(time.Second)
	w2.tmuxSocketPath = noServer
	w2.listPanes = w2.listTmuxPanes
	w2.workers["%1"] = &classifier.Worker{ID: "%1"}
	w2.poll()
	if w2.SnapshotReady() {
		t.Fatal("an unavailable inventory with a known Session advertised readiness")
	}
}

// After a reboot the tmux server is gone and a Session recorded by the earlier
// run cannot be probed on a server. A process that never saw a server and finds
// the socket unreachable proves the recorded Session is gone, so Brain's host
// can be replaced; once a server has been seen, a vanished one stays Unknown.
func TestAbsenceAfterRebootButNotAfterAServerWasSeen(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("PATH", writeFakeTmux(t, `echo 'no server running on fixture' >&2; exit 1`))
	w := New(time.Second)
	// A unix socket path is limited to ~108 bytes; a long test temp dir would
	// make the dial fail for the wrong reason.
	shortDir, err := os.MkdirTemp("/tmp", "mw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(shortDir) })
	w.tmuxSocketPath = filepath.Join(shortDir, "s")

	absent, err := w.ResolveDelegatedAbsence("%1")
	if err != nil || !absent {
		t.Fatalf("never saw a server: absent=%v err=%v, want absent", absent, err)
	}

	w.sawTmuxServer.Store(true)
	if _, err := w.ResolveDelegatedAbsence("%1"); !errors.Is(err, ErrOwnershipProbeUnavailable) {
		t.Fatalf("server seen then gone: err=%v, want Unknown (ErrOwnershipProbeUnavailable)", err)
	}
}
