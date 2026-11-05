package main

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/work"
)

func TestBrainLoopControlStartValidatesWorkerRouteAndManagesState(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	execs := work.NewExecutorConfig(map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude --model claude-opus-5-5"},
		"codex":  {Name: "codex", Command: "codex"},
	})
	app := &controlApp{brainService: brain.NewService(store, nil, execs), execs: execs}
	start := func(executor, model, reasoning string) control.Response {
		return app.HandleControlRequest(control.Request{Type: "brain_loop_start", BrainLoopStart: &brain.LoopStart{
			Title: "loop: quota", Objective: "Check the quota until approved.", DoneCriteriaRef: "quota approved",
			Loop: brain.WorkLoop{IntervalSeconds: 600, WorkerExecutor: executor, WorkerModel: model, WorkerReasoning: reasoning},
		}})
	}
	for _, bad := range [][3]string{{"nope", "", ""}, {"claude", "", "ultra"}, {"claude", "bad\nmodel", ""}} {
		if resp := start(bad[0], bad[1], bad[2]); resp.OK {
			t.Fatalf("worker route %v accepted: %#v", bad, resp)
		}
	}
	if loops, _ := store.Loops(true); len(loops) != 0 {
		t.Fatalf("a refused start left a loop: %#v", loops)
	}
	resp := start("claude", "claude-sonnet-5-5", "medium")
	if !resp.OK || resp.BrainWork == nil || resp.BrainWork.Loop == nil || resp.BrainWork.Loop.WorkerModel != "claude-sonnet-5-5" {
		t.Fatalf("start = %#v", resp)
	}
	id := resp.BrainWork.ID

	status := app.HandleControlRequest(control.Request{Type: "brain_loop_status"})
	if !status.OK || len(status.BrainLoops) != 1 || status.BrainLoops[0].State != brain.LoopScheduled || status.BrainLoops[0].NextCheckAt == nil {
		t.Fatalf("status = %#v", status)
	}
	if paused := app.HandleControlRequest(control.Request{Type: "brain_loop_pause", WorkID: id}); !paused.OK {
		t.Fatalf("pause = %#v", paused)
	}
	one := app.HandleControlRequest(control.Request{Type: "brain_loop_status", WorkID: id})
	if !one.OK || one.BrainLoops[0].State != brain.LoopPaused {
		t.Fatalf("paused status = %#v", one)
	}
	later := time.Now().Add(time.Hour)
	if resumed := app.HandleControlRequest(control.Request{Type: "brain_loop_resume", WorkID: id, BrainLoopAt: &later}); !resumed.OK {
		t.Fatalf("resume = %#v", resumed)
	}
	if past := time.Now().Add(-time.Hour); app.HandleControlRequest(control.Request{Type: "brain_loop_resume", WorkID: id, BrainLoopAt: &past}).OK {
		t.Fatal("resumed into the past")
	}

	// An ordinary Work is not a loop.
	plain, err := store.CreateWork(brain.Work{Title: "plain", Objective: "not a loop", CompletionPolicy: brain.CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	notLoop := app.HandleControlRequest(control.Request{Type: "brain_loop_pause", WorkID: plain.ID})
	if notLoop.OK || notLoop.Error == nil || notLoop.Error.Code != "brain_loop_not_found" {
		t.Fatalf("pausing a non-loop = %#v", notLoop)
	}
}
