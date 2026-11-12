package main

import (
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/answered"
	"github.com/daoleno/mewla/daemon/classifier"
)

// An approval the agent settled itself was answered in its terminal; one
// nobody answered in time was not answered. Mewla's own answers are recorded
// by whoever gave them, so the hub's "mewla" resolution records nothing.
func TestAgentResolutionsAreRecorded(t *testing.T) {
	ledger, err := answered.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hub := agentevents.NewHub()
	hub.OnApprovalResolved(func(workerID string, approval agentevents.Approval, e agentevents.Event) {
		recordAgentResolution(ledger, workerID, approval, e, nil)
	})
	ask := func(id string) {
		hub.Publish(agentevents.Event{WorkerID: "%1", Kind: agentevents.KindApprovalRequest, At: time.Now(),
			Approval: &agentevents.Approval{ID: id, Kind: "exec", Tool: "shell", Summary: "make release",
				Options: []agentevents.ApprovalOption{{ID: "allow", Label: "Allow"}, {ID: "deny", Label: "Deny"}}}})
	}
	ask("by-agent")
	hub.Publish(agentevents.Event{WorkerID: "%1", Kind: agentevents.KindApprovalResolved, At: time.Now(), ApprovalID: "by-agent", By: "agent"})
	ask("timed-out")
	hub.Publish(agentevents.Event{WorkerID: "%1", Kind: agentevents.KindApprovalResolved, At: time.Now(), ApprovalID: "timed-out", By: "timeout"})
	ask("by-mewla")
	if err := hub.Decide("%1", "by-mewla", "allow", time.Now()); err != nil {
		t.Fatal(err)
	}

	if got, ok := ledger.Get("approval:%1:by-agent"); !ok || got.By.Kind != answered.ByTerminal || got.Asked != "make release" || got.Outcome != answered.OutcomeAnswered {
		t.Fatalf("agent = %+v %v", got, ok)
	}
	if got, ok := ledger.Get("approval:%1:timed-out"); !ok || got.By.Kind != answered.ByTimeout || got.Outcome != answered.OutcomeNotAnswered || got.Answer != "" {
		t.Fatalf("timeout = %+v %v", got, ok)
	}
	if _, ok := ledger.Get("approval:%1:by-mewla"); ok {
		t.Fatal("the hub recorded Mewla's own answer without who gave it")
	}
}

func TestOperatorAnswererNamesBrainForItsWorkers(t *testing.T) {
	if got := operatorAnswerer(&classifier.Worker{Delegated: true}, " the brief allows pushes "); got.Kind != answered.ByBrain || got.Basis != "the brief allows pushes" {
		t.Fatalf("delegated = %+v", got)
	}
	if got := operatorAnswerer(&classifier.Worker{}, ""); got.Kind != answered.ByOperator {
		t.Fatalf("forced = %+v", got)
	}
}

// An approval answered in the terminal is recorded at once, so the prompt that
// drew it is not sealed a second time, and told better (approved) when the
// transcript has the call's result a moment later.
func TestAgentResolutionIsSettledAfterItIsRecorded(t *testing.T) {
	ledger, err := answered.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	asks := 0
	settle := func(record answered.Request, callID string) answered.Request {
		mu.Lock()
		defer mu.Unlock()
		asks++
		if asks >= 2 { // the result is not in the transcript on the first read
			record.Outcome = answered.OutcomeApproved
		}
		return record
	}
	approval := agentevents.Approval{ID: "a1", Kind: "permission", Tool: "Bash", Summary: "curl -sI https://example.org"}
	recordAgentResolution(ledger, "%1", approval, agentevents.Event{By: "agent", At: time.Now()}, settle)
	if got, ok := ledger.Get("approval:%1:a1"); !ok || got.Outcome != answered.OutcomeAnswered || got.Summary != "curl -sI https://example.org" {
		t.Fatalf("at once = %+v %v", got, ok)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := ledger.Get("approval:%1:a1"); got.Outcome == answered.OutcomeApproved {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the outcome was never settled")
}
