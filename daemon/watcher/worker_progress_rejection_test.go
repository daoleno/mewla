package watcher

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

const progressTestSession = "mewla-worker-worker:@1"

// scriptedProgressLedger answers every delegated progress report with one
// scripted result, to prove the Worker sees exactly what the ledger decided.
type scriptedProgressLedger struct {
	*fakeTurnLedger
	result TurnProgressResult
}

func (l *scriptedProgressLedger) ApplyDelegatedTurnProgress(TurnFact) (TurnProgressResult, error) {
	return l.result, nil
}

func progressTestWatcher(result TurnProgressResult) (*Watcher, string) {
	w := New(time.Second)
	const id = progressTestSession
	w.registerCreatedSession(id, "/repo/mewla", CreateSessionOptions{Command: "claude", Name: "Worker"},
		time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC))
	<-w.Events()
	w.turnLedger = &scriptedProgressLedger{fakeTurnLedger: newFakeTurnLedger(), result: result}
	return w, id
}

func TestRefusedWorkerProgressSaysWhyAndWhichTurnToUse(t *testing.T) {
	const reason = "progress report refused: turn turn:one was superseded by a newer prompt. The Session's current delegated turn is turn:two: report with --turn-id turn:two."
	w, id := progressTestWatcher(TurnProgressResult{Owned: true, Rejection: reason, CurrentTurnID: "turn:two"})
	_, err := w.UpdateWorkerProgress(id, classifier.WorkerProgress{
		TurnID: "turn:one", Status: "running", Phase: "working", Attention: "none", Summary: "ask",
	})
	if err == nil || err.Error() != reason {
		t.Fatalf("refusal = %v, want the ledger's reason verbatim", err)
	}

	// An owned Session that gave no reason still names the identity it was given.
	w, id = progressTestWatcher(TurnProgressResult{Owned: true})
	_, err = w.UpdateWorkerProgress(id, classifier.WorkerProgress{
		TurnID: "turn:one", Status: "running", Phase: "working", Attention: "none", Summary: "ask",
	})
	if err == nil || !strings.Contains(err.Error(), "turn:one") {
		t.Fatalf("reasonless refusal = %v, want the turn identity named", err)
	}
	_, err = w.UpdateWorkerProgress(id, classifier.WorkerProgress{
		Status: "running", Phase: "working", Attention: "none", Summary: "ask",
	})
	if err == nil || !strings.Contains(err.Error(), "--turn-id") {
		t.Fatalf("missing turn refusal = %v, want --turn-id guidance", err)
	}
}

func TestRedirectedWorkerProgressReturnsTheNoticeToTheWorker(t *testing.T) {
	const notice = "Turn turn:one was superseded by turn turn:two; this report was recorded on turn:two. Use --turn-id turn:two from now on."
	w, id := progressTestWatcher(TurnProgressResult{
		Owned: true, Matched: true, Notice: notice,
		Turn: TurnSnapshot{SessionID: progressTestSession, TurnID: "turn:two", Status: TurnBlocked, SignalProtocol: true},
	})
	worker, got, err := w.ReportWorkerProgress(id, classifier.WorkerProgress{
		TurnID: "turn:one", Status: "blocked", Phase: "working", Attention: "user_input", Summary: "ask",
	})
	if err != nil || got != notice || worker == nil {
		t.Fatalf("redirected report = (%v, %q, %v), want the notice", worker, got, err)
	}
}

// TestRefusedWorkerProgressIsVisibleOnTheWorker: a refused report used to reach
// only the Worker's own shell (ZeroGAccountsPlans 11-15: seven minutes of
// "stale fence or turn token" while it asked for the owner). The refusal and
// what the Worker said are on the Worker and in the event stream every client
// reads, and an accepted report clears them.
func TestRefusedWorkerProgressIsVisibleOnTheWorker(t *testing.T) {
	const reason = "progress report refused: turn turn:one was superseded by a newer prompt."
	w, id := progressTestWatcher(TurnProgressResult{Owned: true, Rejection: reason})
	if _, err := w.UpdateWorkerProgress(id, classifier.WorkerProgress{
		TurnID: "turn:one", Status: "running", Phase: "working", Attention: "user_input", Summary: "GitHub login open on my Screen",
	}); err == nil {
		t.Fatal("report was not refused")
	}
	select {
	case event := <-w.Events():
		if event.Type != "worker_metadata_change" || event.Worker == nil || event.Worker.ProgressRefused != reason ||
			event.Worker.ProgressRefusedSummary != "GitHub login open on my Screen" || event.Worker.ProgressRefusedAt == nil {
			t.Fatalf("refusal event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("a refused report published nothing")
	}
	if worker := w.GetWorker(id); worker == nil || worker.ProgressRefused != reason {
		t.Fatalf("Worker does not carry the refusal: %+v", worker)
	}

	w.turnLedger = &scriptedProgressLedger{fakeTurnLedger: newFakeTurnLedger(), result: TurnProgressResult{
		Owned: true, Matched: true,
		Turn:  TurnSnapshot{SessionID: progressTestSession, TurnID: "turn:two", Status: TurnRunning, SignalProtocol: true},
	}}
	if _, err := w.UpdateWorkerProgress(id, classifier.WorkerProgress{
		TurnID: "turn:two", Status: "running", Phase: "working", Attention: "none", Summary: "on it",
	}); err != nil {
		t.Fatal(err)
	}
	if worker := w.GetWorker(id); worker == nil || worker.ProgressRefused != "" || worker.ProgressRefusedAt != nil {
		t.Fatalf("an accepted report left the refusal: %+v", worker)
	}
}
