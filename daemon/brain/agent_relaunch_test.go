package brain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

const (
	relaunchLaunchSession = "11111111-1111-4111-8111-111111111111"
	relaunchNewerSession  = "22222222-2222-4222-8222-222222222222"
)

type relaunchFixture struct {
	store   *Store
	service *Service
	fw      *fakeWatcher
	item    Work
}

// newRelaunchFixture is an open Work whose Worker "worker" was spawned as a
// Claude Code Worker; claude and cursor-agent are installed and older than any
// Session.
func newRelaunchFixture(t *testing.T) *relaunchFixture {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("host", "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{Title: "render", Objective: "render the trailer"})
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: store, outcomes: map[string]watcher.InputOutcome{}, sessions: map[string]*classifier.Worker{
		"host":   {ID: "host", Hidden: true, State: classifier.StateDone},
		"worker": {ID: "worker", Delegated: true, State: classifier.StateRunning},
	}}
	service := newTestService(t, store, fw, nil)
	service.SetWorkerLaunchEnv(func() map[string]string { return map[string]string{"MEWLA_STATE_DIR": "/state"} })
	bin := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(bin, old, old); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(work.SetAgentLookPath(func(name string) (string, error) {
		switch filepath.Base(name) {
		case "claude", "cursor-agent", "codex":
			return bin, nil
		}
		return "", errors.New("not found")
	}))
	if _, err := store.RecordWorkWorker(WorkWorkerRecord{
		WorkID: item.ID, SessionID: "worker", Name: "Franklin", ExecutorID: "claude", Provider: "claude", Cwd: "/repo",
		LaunchCommand: "claude --model claude-sonnet-5-5 --permission-mode bypassPermissions --session-id " + relaunchLaunchSession,
	}); err != nil {
		t.Fatal(err)
	}
	return &relaunchFixture{store: store, service: service, fw: fw, item: item}
}

// startTurn gives the Worker a live delegated Turn, turn:first.
func (f *relaunchFixture) startTurn(t *testing.T) {
	t.Helper()
	if _, err := f.fw.SubmitDelegatedWorkInput("worker", "render the trailer", f.item.ID, "turn:first", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: "turn:first", Class: watcher.EvidenceControl,
		Kind: "running", SourceID: "running-first", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func killedEnd(at time.Time) watcher.AgentEnd {
	code := 137
	return watcher.AgentEnd{SessionID: "worker", Name: "Franklin", Command: "claude --session-id " + relaunchLaunchSession,
		Exit: watcher.SessionExit{Code: &code, Signal: "kill", Ran: time.Minute, EndedAt: at}}
}

func exitedEnd(code int, at time.Time) watcher.AgentEnd {
	end := killedEnd(at)
	end.Exit.Code, end.Exit.Signal = &code, ""
	return end
}

func TestAgentRelaunchIdleResumesTheWorkersNewestConversation(t *testing.T) {
	f := newRelaunchFixture(t)
	// A /clear started a newer conversation than the one the launch named.
	if _, err := f.store.RecordWorkTranscript(WorkTranscriptLink{WorkID: f.item.ID, SessionID: "worker", Executor: "claude",
		ProviderSessionID: relaunchNewerSession, RecordedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	plan, err := f.service.PlanAgentRelaunch(killedEnd(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != watcher.AgentEndRelaunch || plan.WorkID != f.item.ID || plan.TurnID != "" || plan.Attempt != 1 || plan.After != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if !strings.Contains(plan.Command, "--resume "+relaunchNewerSession) || strings.Contains(plan.Command, "--session-id") ||
		!strings.Contains(plan.Command, "--model claude-sonnet-5-5") || plan.Conversation != relaunchNewerSession {
		t.Fatalf("command = %q conversation %q", plan.Command, plan.Conversation)
	}
	if plan.Name != "Franklin" || plan.Cwd != "/repo" || plan.Env["MEWLA_STATE_DIR"] != "/state" || plan.PrepareWorkspace == nil {
		t.Fatalf("launch = name %q cwd %q env %v", plan.Name, plan.Cwd, plan.Env)
	}
	if plan.Words != "Claude Code was killed (SIGKILL); Mewla started it again in the same conversation." {
		t.Fatalf("words = %q", plan.Words)
	}
	if plan.Line != "Restarted after SIGKILL" {
		t.Fatalf("line = %q", plan.Line)
	}
}

func TestAgentRelaunchWithoutALinkResumesTheLaunchsOwnConversation(t *testing.T) {
	f := newRelaunchFixture(t)
	plan, err := f.service.PlanAgentRelaunch(killedEnd(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != watcher.AgentEndRelaunch || !strings.Contains(plan.Command, "--resume "+relaunchLaunchSession) || strings.Contains(plan.Command, "--session-id") {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestAgentRelaunchMidTurnContinuesTheLiveTurn(t *testing.T) {
	f := newRelaunchFixture(t)
	f.startTurn(t)
	end := killedEnd(time.Now())
	end.TurnID = "turn:first"
	plan, err := f.service.PlanAgentRelaunch(end)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != watcher.AgentEndRelaunch || plan.TurnID != "turn:first" ||
		plan.Words != "Claude Code was killed (SIGKILL); Mewla started it again in the same conversation and asked it to continue the task." {
		t.Fatalf("plan = %+v", plan)
	}
	if prompt := watcher.AgentContinuationPrompt(end.Cause()); !strings.Contains(prompt, "was killed (SIGKILL)") ||
		!strings.Contains(prompt, "same conversation") || !strings.Contains(prompt, "were stopped") {
		t.Fatalf("continuation = %q", prompt)
	}
}

// The bound: three relaunches in thirty minutes, with growing waits; the
// fourth end is told to Brain with its cause. Records dedupe by the end.
func TestAgentRelaunchBound(t *testing.T) {
	f := newRelaunchFixture(t)
	base := time.Now().UTC()
	for attempt := 1; attempt <= agentRelaunchLimit; attempt++ {
		plan, err := f.service.PlanAgentRelaunch(killedEnd(base.Add(time.Duration(attempt) * time.Second)))
		if err != nil || plan.Action != watcher.AgentEndRelaunch || plan.Attempt != attempt || plan.After != agentRelaunchBackoff[attempt-1] {
			t.Fatalf("attempt %d plan = %+v, %v", attempt, plan, err)
		}
		record := watcher.AgentRelaunch{WorkID: f.item.ID, SessionID: "worker", Attempt: attempt, Cause: "was killed (SIGKILL)",
			EndedAt: base.Add(time.Duration(attempt) * time.Second), Outcome: "idle", Words: plan.Words, At: base.Add(time.Duration(attempt) * time.Second)}
		for range 2 { // the same end recorded twice is one record
			if err := f.service.RecordAgentRelaunch(record); err != nil {
				t.Fatal(err)
			}
		}
	}
	records, _, err := f.store.agentRelaunches(f.item.ID, "worker")
	if err != nil || len(records) != agentRelaunchLimit {
		t.Fatalf("records = %d, %v", len(records), err)
	}
	plan, err := f.service.PlanAgentRelaunch(exitedEnd(1, base.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != watcher.AgentEndReport ||
		plan.Words != "Claude Code exited with code 1; not started again: it was started again 3 times in the last 30 minutes and ended each time." {
		t.Fatalf("over the bound = %+v", plan)
	}
	if plan.Line != "Gave up after 3 restarts" {
		t.Fatalf("line = %q", plan.Line)
	}
}

func TestAgentRelaunchIsNotDoneFor(t *testing.T) {
	t.Run("an agent that ended itself between Turns", func(t *testing.T) {
		f := newRelaunchFixture(t)
		plan, err := f.service.PlanAgentRelaunch(exitedEnd(0, time.Now()))
		if err != nil || plan.Action != watcher.AgentEndIgnore {
			t.Fatalf("plan = %+v, %v", plan, err)
		}
	})
	t.Run("an agent that ended itself in a Turn", func(t *testing.T) {
		f := newRelaunchFixture(t)
		f.startTurn(t)
		plan, err := f.service.PlanAgentRelaunch(exitedEnd(0, time.Now()))
		if err != nil || plan.Action != watcher.AgentEndReport || !strings.Contains(plan.Words, "it ended itself before it reported") {
			t.Fatalf("plan = %+v, %v", plan, err)
		}
	})
	t.Run("a closed Work", func(t *testing.T) {
		f := newRelaunchFixture(t)
		current, err := f.store.Work(f.item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.CloseWork(WorkCloseRequest{WorkID: current.ID, ExpectedRevision: current.Revision, Status: WorkCancelled, Actor: "brain", Reason: "stop"}); err != nil &&
			!errors.Is(err, ErrWorkCleanupPending) {
			t.Fatal(err)
		}
		plan, err := f.service.PlanAgentRelaunch(killedEnd(time.Now()))
		if err != nil || plan.Action != watcher.AgentEndIgnore {
			t.Fatalf("plan = %+v, %v", plan, err)
		}
	})
	t.Run("a Session that is no Work's", func(t *testing.T) {
		f := newRelaunchFixture(t)
		end := killedEnd(time.Now())
		end.SessionID = "someone-elses"
		plan, err := f.service.PlanAgentRelaunch(end)
		if err != nil || plan.Action != watcher.AgentEndIgnore {
			t.Fatalf("plan = %+v, %v", plan, err)
		}
	})
	t.Run("Brain's own Host", func(t *testing.T) {
		f := newRelaunchFixture(t)
		end := killedEnd(time.Now())
		end.SessionID = "host"
		plan, err := f.service.PlanAgentRelaunch(end)
		if err != nil || plan.Action != watcher.AgentEndIgnore {
			t.Fatalf("plan = %+v, %v", plan, err)
		}
	})
	report := func(t *testing.T, f *relaunchFixture, want string, line string) {
		t.Helper()
		plan, err := f.service.PlanAgentRelaunch(killedEnd(time.Now()))
		if err != nil || plan.Action != watcher.AgentEndReport || !strings.Contains(plan.Words, want) {
			t.Fatalf("plan = %+v, %v (want %q)", plan, err, want)
		}
		if plan.Line != line {
			t.Fatalf("line = %q", plan.Line)
		}
	}
	t.Run("an agent no longer installed", func(t *testing.T) {
		f := newRelaunchFixture(t)
		t.Cleanup(work.SetAgentLookPath(func(string) (string, error) { return "", errors.New("not found") }))
		report(t, f, "is not installed on this computer", "Not restarted: not installed")
	})
	t.Run("an agent signed out", func(t *testing.T) {
		f := newRelaunchFixture(t)
		previous := hostAuth
		hostAuth = func(string) HostAuthState { return HostAuthUnauthenticated }
		t.Cleanup(func() { hostAuth = previous })
		report(t, f, "Claude Code is not signed in on this computer", "Not restarted: signed out")
	})
	t.Run("a Worker spawned before launches were recorded", func(t *testing.T) {
		f := newRelaunchFixture(t)
		if _, err := f.store.RecordWorkWorker(WorkWorkerRecord{WorkID: f.item.ID, SessionID: "other", Name: "Old"}); err != nil {
			t.Fatal(err)
		}
		end := killedEnd(time.Now())
		end.SessionID = "other"
		plan, err := f.service.PlanAgentRelaunch(end)
		if err != nil || plan.Action != watcher.AgentEndReport || !strings.Contains(plan.Words, "no record of how this Worker was launched") {
			t.Fatalf("plan = %+v, %v", plan, err)
		}
	})
	t.Run("an agent with no conversation recorded", func(t *testing.T) {
		f := newRelaunchFixture(t)
		if _, err := f.store.RecordWorkWorker(WorkWorkerRecord{WorkID: f.item.ID, SessionID: "cursor", Name: "Cursor", ExecutorID: "agent",
			Provider: "cursor", LaunchCommand: "cursor-agent --force --sandbox disabled"}); err != nil {
			t.Fatal(err)
		}
		end := killedEnd(time.Now())
		end.SessionID, end.Command = "cursor", "cursor-agent"
		plan, err := f.service.PlanAgentRelaunch(end)
		if err != nil || plan.Action != watcher.AgentEndReport ||
			plan.Words != "Cursor Agent was killed (SIGKILL); not started again: Mewla has no conversation of it to resume." {
			t.Fatalf("plan = %+v, %v", plan, err)
		}
	})
}

// An agent updated while it ran is started again even when it exited 0: the
// update ended it, not the Worker.
func TestAgentRelaunchAfterAnUpdate(t *testing.T) {
	f := newRelaunchFixture(t)
	end := exitedEnd(0, time.Now())
	end.Exit.Ran = time.Hour // started before the program file changed (48 h old → touch it now)
	path, _ := work.AgentLookPath("claude")
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	plan, err := f.service.PlanAgentRelaunch(end)
	if err != nil || plan.Action != watcher.AgentEndRelaunch ||
		plan.Words != "Claude Code exited with code 0 after it was updated; Mewla started it again in the same conversation." ||
		plan.Line != "Restarted after an update" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
}

// An idle Worker that is not started again is told to Brain once, as a
// turn_lost review in plain words.
func TestAgentEndNotRelaunchedReachesBrainOnce(t *testing.T) {
	f := newRelaunchFixture(t)
	t.Cleanup(work.SetAgentLookPath(func(string) (string, error) { return "", errors.New("not found") }))
	end := killedEnd(time.Now())
	plan, err := f.service.PlanAgentRelaunch(end)
	if err != nil || plan.Action != watcher.AgentEndReport {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	for range 2 {
		if told, err := f.service.ReportAgentEnd(end, plan); err != nil || !told {
			t.Fatalf("told = %v, %v", told, err)
		}
	}
	action, found, err := f.store.ClaimNextReviewAction("host")
	if err != nil || !found {
		t.Fatalf("claim = %+v %v %v", action, found, err)
	}
	if action.WorkID != f.item.ID || action.Kind != "turn_lost" || action.Summary != plan.Words {
		t.Fatalf("Brain receives %+v, want %q", action, plan.Words)
	}
	rows := 0
	for _, event := range mustWorkEvents(t, f.store, f.item.ID) {
		if event.Kind == "session.uncertain" {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("evidence rows = %d", rows)
	}
}

// A Worker that reported its result and then ended for good: Brain still has
// the result's review, so the loss waits for it and then reaches Brain once,
// in its own words.
func TestAgentEndNotRelaunchedWaitsForTheResultBrainHas(t *testing.T) {
	f := newRelaunchFixture(t)
	t.Cleanup(work.SetAgentLookPath(func(string) (string, error) { return "", errors.New("not found") }))
	result, err := f.store.FSM().OpenReview(lifecycle.WorkID(f.item.ID), "turn_done", "turn-said-hello")
	if err != nil || result.Review == nil {
		t.Fatalf("result review = %+v, %v", result, err)
	}
	end := killedEnd(time.Now())
	plan, err := f.service.PlanAgentRelaunch(end)
	if err != nil || plan.Action != watcher.AgentEndReport {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if told, err := f.service.ReportAgentEnd(end, plan); err != nil || told {
		t.Fatalf("told while the result is with Brain = %v, %v", told, err)
	}
	if _, err := f.store.FSM().ResolveReview(lifecycle.WorkID(f.item.ID), result.Review.EventID,
		lifecycle.ResolveReviewInput{Disposition: lifecycle.DispositionContinue, Actor: "brain"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if told, err := f.service.ReportAgentEnd(end, plan); err != nil || !told {
			t.Fatalf("told after the result = %v, %v", told, err)
		}
	}
	action, found, err := f.store.ClaimNextReviewAction("host")
	if err != nil || !found || action.Kind != "turn_lost" || action.Summary != plan.Words {
		t.Fatalf("Brain receives %+v (%v, %v), want %q", action, found, err, plan.Words)
	}
	if again, found, _ := f.store.ClaimNextReviewAction("host"); found {
		t.Fatalf("a second review reached Brain: %+v", again)
	}
}
