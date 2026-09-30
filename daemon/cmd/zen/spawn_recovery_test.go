package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestSpawnPendingSuccessThenPaneGoneCanRespawnSameWork(t *testing.T) {
	store := newControlBrainStore(t)
	fw := newFakeControlWatcher()
	fw.turnStore = store
	fw.sendErr = &watcher.InputSubmissionError{Result: watcher.InputResult{Outcome: watcher.InputAmbiguous}, Cause: errors.New("provider admission pending")}
	app := &controlApp{watcher: fw, brainStore: store, execs: work.NewExecutorConfig("codex", map[string]work.Executor{"codex": {Name: "codex", Command: "codex"}})}
	first := app.HandleControlRequest(control.Request{Type: "worker_spawn", Name: "first", Cwd: "/repo", Prompt: "brief"})
	if !first.OK || first.Confirmation == "" || first.BrainWork == nil {
		t.Fatalf("first spawn: %+v", first)
	}
	// Exactly the incident: success was returned while admission remained
	// prepared, then the provider pane disappeared before its first signal.
	delete(fw.workers, first.Worker.ID)
	fw.sendErr = nil
	next := app.HandleControlRequest(control.Request{Type: "worker_spawn", Name: "next", Cwd: "/repo", WorkID: first.BrainWork.ID, Prompt: "explicit replacement"})
	if !next.OK {
		t.Fatalf("successful-then-lost spawn stranded Work: %+v", next.Error)
	}
	st, _ := store.FSM().State(lifecycle.WorkID(first.BrainWork.ID))
	if st.Attempt == nil || st.Attempt.SessionID != next.Worker.ID {
		t.Fatal("replacement is not the sole owner")
	}
	for _, a := range st.Admissions {
		if a.SessionID == first.Worker.ID && a.Status != lifecycle.AdmissionRetired {
			t.Fatal("lost preparation retained authority")
		}
	}
}

func TestSpawnDeadPreparedSessionThenRespawnSameWork(t *testing.T) {
	store := newControlBrainStore(t)
	item, err := store.CreateWork(brain.Work{Title: "recover", Objective: "one command"})
	if err != nil {
		t.Fatal(err)
	}
	old := watcher.InputAdmission{WorkID: item.ID, SessionID: "gone:@1", ProposedTurnID: "turn:gone", Receipt: "turn:gone", PayloadSHA256: strings.Repeat("a", 64), ProcessIdentity: "old-process", PaneGeneration: "old-pane", Mode: watcher.InputAdmissionFresh, SignalProtocol: true, AcceptedAt: time.Now().UTC()}
	if _, _, err := store.PrepareInputAdmission(old); err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	fw.turnStore = store
	app := &controlApp{watcher: fw, brainStore: store, execs: work.NewExecutorConfig("codex", map[string]work.Executor{"codex": {Name: "codex", Command: "codex"}})}
	resp := app.HandleControlRequest(control.Request{Type: "worker_spawn", Name: "replacement", Cwd: "/repo", WorkID: item.ID, Prompt: "explicit replacement input"})
	if !resp.OK {
		t.Fatalf("dead preparation stranded Work: %+v", resp.Error)
	}
	st, _ := store.FSM().State(lifecycle.WorkID(item.ID))
	if st.Attempt == nil || st.Attempt.SessionID != resp.Worker.ID {
		t.Fatal("replacement has no sole canonical owner")
	}
	before := st.Revision
	_, _ = store.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: old.SessionID, TurnID: old.ProposedTurnID, Class: watcher.EvidenceControl, Kind: "running", SourceID: "late-old", At: time.Now().UTC()})
	st, _ = store.FSM().State(lifecycle.WorkID(item.ID))
	if st.Revision != before {
		t.Fatal("late retired signal changed ownership")
	}
}

func TestSpawnSuccessfulTransportThenPaneVanishes(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.dropWorkerOnSend = true
	app := &controlApp{watcher: fw, brainStore: newControlBrainStore(t), execs: work.NewExecutorConfig("codex", map[string]work.Executor{"codex": {Name: "codex", Command: "codex"}})}
	resp := app.HandleControlRequest(control.Request{Type: "worker_spawn", Name: "vanishes", Cwd: "/repo", Prompt: "brief"})
	if resp.OK || resp.Error == nil {
		t.Fatal("spawn fabricated success for a vanished provider pane")
	}
}
