package main

import (
	"strings"
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

// The configured default Worker route is fixed on a loop when it starts and
// then followed by the real `worker spawn -work <loop>` flow, whichever
// executor Brain runs on; an explicit executor wins; with no default the loop
// carries no route and spawn behaves as it does for any Worker.
func TestBrainLoopDefaultRouteIsPersistedAndFollowedBySpawn(t *testing.T) {
	store := newControlBrainStore(t)
	if err := store.SetHostSession("host:@codex", "codex"); err != nil {
		t.Fatal(err)
	}
	execs := work.NewExecutorConfig(map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude"},
		"codex":  {Name: "codex", Command: "codex"},
	})
	execs.ScheduledAction = work.LaunchRoute{Executor: "claude", Model: "claude-sonnet-5-5"}
	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher: fw, brainStore: store, brainService: brain.NewService(store, nil, execs), execs: execs, stateDir: t.TempDir(),
	}
	start := func(loop brain.WorkLoop) brain.Work {
		t.Helper()
		loop.IntervalSeconds = 600
		resp := app.HandleControlRequest(control.Request{Type: "brain_loop_start", BrainLoopStart: &brain.LoopStart{
			Title: "loop: quota", Objective: "Check the quota until approved.", DoneCriteriaRef: "quota approved", Loop: loop,
		}})
		if !resp.OK || resp.BrainWork == nil || resp.BrainWork.Loop == nil {
			t.Fatalf("start = %#v", resp)
		}
		return *resp.BrainWork
	}
	spawn := func(workID, executor, model string) string {
		t.Helper()
		before := len(fw.created)
		resp := app.HandleControlRequest(control.Request{
			Type: "worker_spawn", Name: "check", WorkID: workID, Executor: executor, ModelID: model,
			Cwd: t.TempDir(), Prompt: "check the quota",
		})
		if !resp.OK || len(fw.created) != before+1 {
			t.Fatalf("spawn = %#v", resp)
		}
		return fw.created[before].Command
	}

	// No route given: the user's default is persisted, under a Codex Host.
	defaulted := start(brain.WorkLoop{})
	if defaulted.Loop.WorkerExecutor != "claude" || defaulted.Loop.WorkerModel != "claude-sonnet-5-5" {
		t.Fatalf("default route not persisted: %+v", defaulted.Loop)
	}
	if !strings.Contains(defaulted.NextAction, "follows this loop's route (-executor claude -model claude-sonnet-5-5)") {
		t.Fatalf("next action = %q", defaulted.NextAction)
	}
	// A client cannot start a loop already paused.
	if paused := start(brain.WorkLoop{Paused: true}); paused.Loop.Paused {
		t.Fatal("a loop started paused")
	}
	// A later change of the configured default does not move the loop.
	execs.ScheduledAction = work.LaunchRoute{Executor: "codex"}
	if got := spawn(defaulted.ID, "", ""); !strings.HasPrefix(got, "claude ") || !strings.Contains(got, "--model claude-sonnet-5-5") {
		t.Fatalf("loop spawn under a Codex Host = %q, want Claude sonnet 5.5", got)
	}

	// Explicit override on the loop wins over the default; explicit spawn
	// flags win over the loop.
	explicit := start(brain.WorkLoop{WorkerExecutor: "codex"})
	if explicit.Loop.WorkerExecutor != "codex" || explicit.Loop.WorkerModel != "" {
		t.Fatalf("explicit route = %+v", explicit.Loop)
	}
	if got := spawn(explicit.ID, "", ""); !strings.HasPrefix(got, "codex ") {
		t.Fatalf("explicit codex loop spawn = %q", got)
	}
	override := start(brain.WorkLoop{})
	if override.Loop.WorkerExecutor != "codex" {
		t.Fatalf("second default = %+v", override.Loop)
	}
	if got := spawn(override.ID, "claude", ""); !strings.HasPrefix(got, "claude ") {
		t.Fatalf("explicit spawn executor must win over the loop route: %q", got)
	}

	// No configured default: no route is invented, and the loop starts.
	execs.ScheduledAction = work.LaunchRoute{}
	bare := start(brain.WorkLoop{})
	if bare.Loop.WorkerExecutor != "" {
		t.Fatalf("a route was invented: %+v", bare.Loop)
	}
}
