package brain

import (
	"errors"
	"fmt"
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
	if plan.Line != "Restarted after a crash" {
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
	prompt := watcher.AgentContinuationPrompt(end.Cause(), "- npm run dev (running 12m)", false)
	for _, want := range []string{"was killed (SIGKILL)", "same conversation", "Mewla stopped nothing",
		"may have finished, may still be running, or may have run only in part", "- npm run dev (running 12m)", "before you repeat any step"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("continuation lacks %q: %q", want, prompt)
		}
	}
	if prompt := watcher.AgentContinuationPrompt(end.Cause(), "", false); !strings.Contains(prompt, "Nothing it started is still running.") {
		t.Fatalf("continuation with no survivor = %q", prompt)
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
	if plan.Line != "Stopped after 3 restarts" {
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
	t.Run("a Worker spawned before launches were recorded, idle", func(t *testing.T) {
		// After an upgrade: nothing was lost and the person saw it end; log only.
		f := newRelaunchFixture(t)
		if _, err := f.store.RecordWorkWorker(WorkWorkerRecord{WorkID: f.item.ID, SessionID: "other", Name: "Old", Provider: "claude"}); err != nil {
			t.Fatal(err)
		}
		end := killedEnd(time.Now())
		end.SessionID, end.Command = "other", ""
		plan, err := f.service.PlanAgentRelaunch(end)
		if err != nil || plan.Action != watcher.AgentEndIgnore {
			t.Fatalf("plan = %+v, %v", plan, err)
		}
	})
	t.Run("a Worker spawned before launches were recorded, in a Turn", func(t *testing.T) {
		f := newRelaunchFixture(t)
		f.fw.sessions["other"] = &classifier.Worker{ID: "other", Delegated: true, State: classifier.StateRunning}
		if _, err := f.store.RecordWorkWorker(WorkWorkerRecord{WorkID: f.item.ID, SessionID: "other", Name: "Old", Provider: "claude"}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.fw.SubmitDelegatedWorkInput("other", "render", f.item.ID, "turn:old", "", "", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "other", TurnID: "turn:old", Class: watcher.EvidenceControl,
			Kind: "running", SourceID: "running-old", At: time.Now()}); err != nil {
			t.Fatal(err)
		}
		end := killedEnd(time.Now())
		end.SessionID, end.Command, end.TurnID = "other", "", "turn:old"
		plan, err := f.service.PlanAgentRelaunch(end)
		// Named from the record, not "The agent".
		if err != nil || plan.Action != watcher.AgentEndReport ||
			plan.Words != "Claude Code was killed (SIGKILL); not started again: Mewla has no record of how this Worker was launched." {
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
	// It ran an hour; its program was replaced a minute before it died.
	died := time.Now()
	end := exitedEnd(1, died)
	end.Exit.Ran = time.Hour
	path, _ := work.AgentLookPath("claude")
	updated := died.Add(-time.Minute)
	if err := os.Chtimes(path, updated, updated); err != nil {
		t.Fatal(err)
	}
	plan, err := f.service.PlanAgentRelaunch(end)
	if err != nil || plan.Action != watcher.AgentEndRelaunch ||
		plan.Words != "Claude Code exited with code 1 after it was updated; Mewla started it again in the same conversation." ||
		plan.Line != "Restarted after an update" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
}

// An agent that exits 0 between Turns is never started again, whatever
// happened to its program: Claude Code exits 0 on Ctrl-C and /exit, and
// updates itself about daily (the reviewer's probe, 11-22).
func TestAgentRelaunchNeverForACleanIdleExit(t *testing.T) {
	path := func(f *relaunchFixture) string { p, _ := work.AgentLookPath("claude"); return p }
	for _, test := range []struct {
		name    string
		updated func(ended time.Time) time.Time
	}{
		{"updated after it ended", func(ended time.Time) time.Time { return ended.Add(30 * time.Minute) }},
		{"updated while it ran", func(ended time.Time) time.Time { return ended.Add(-time.Minute) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newRelaunchFixture(t)
			ended := time.Now().Add(-time.Hour)
			end := exitedEnd(0, ended)
			end.Exit.Ran = 10 * time.Minute
			at := test.updated(ended)
			if err := os.Chtimes(path(f), at, at); err != nil {
				t.Fatal(err)
			}
			plan, err := f.service.PlanAgentRelaunch(end)
			if err != nil || plan.Action != watcher.AgentEndIgnore {
				t.Fatalf("plan = %+v, %v", plan, err)
			}
		})
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

// The Session's line in the app says what happened in a person's words; the
// exact signal and exit code stay in the words Brain reads.
func TestAgentRelaunchLineSaysItInAPersonsWords(t *testing.T) {
	oom := killedEnd(time.Now())
	oom.OutOfMemory = true
	for _, test := range []struct {
		name  string
		end   watcher.AgentEnd
		line  string
		words string
	}{
		{"killed", killedEnd(time.Now()), "Restarted after a crash", "was killed (SIGKILL)"},
		{"exited with a code", exitedEnd(1, time.Now()), "Restarted after a crash", "exited with code 1"},
		{"out of memory", oom, "Restarted after running out of memory", "out-of-memory killer (SIGKILL)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newRelaunchFixture(t)
			plan, err := f.service.PlanAgentRelaunch(test.end)
			if err != nil || plan.Action != watcher.AgentEndRelaunch {
				t.Fatalf("plan = %+v, %v", plan, err)
			}
			if plan.Line != test.line || !strings.Contains(plan.Words, test.words) {
				t.Fatalf("line %q words %q", plan.Line, plan.Words)
			}
			if strings.Contains(plan.Line, "SIG") || strings.ContainsAny(plan.Line, "0123456789") {
				t.Fatalf("line %q names a signal or code", plan.Line)
			}
		})
	}
}

// Starts that fail are tries: three of them reach the bound, and Brain hears
// the real cause, not "ended each time".
func TestAgentRelaunchStartsThatFailCountAndSayWhy(t *testing.T) {
	f := newRelaunchFixture(t)
	ended := time.Now().UTC().Add(-time.Minute)
	end := killedEnd(ended)
	for attempt := 1; attempt <= agentRelaunchLimit; attempt++ {
		plan, err := f.service.PlanAgentRelaunch(end)
		if err != nil || plan.Action != watcher.AgentEndRelaunch || plan.Attempt != attempt {
			t.Fatalf("try %d plan = %+v, %v", attempt, plan, err)
		}
		// The same end, tried again: each try is its own record.
		if err := f.service.RecordAgentRelaunch(watcher.AgentRelaunch{WorkID: f.item.ID, SessionID: "worker", Attempt: attempt,
			Cause: "was killed (SIGKILL)", EndedAt: ended, Outcome: "start_failed", Name: "Claude Code",
			Error: "the folder /repo is gone", At: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := f.service.PlanAgentRelaunch(end)
	if err != nil || plan.Action != watcher.AgentEndReport ||
		plan.Words != "Claude Code was killed (SIGKILL); not started again: it could not be started again (the folder /repo is gone), 3 times in the last 30 minutes." ||
		plan.Line != "Stopped: it could not start" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	events := mustWorkEvents(t, f.store, f.item.ID)
	found := false
	for _, event := range events {
		if event.Kind == workEventKindRelaunched && event.Summary == "Claude Code was killed (SIGKILL); Mewla could not start it again: the folder /repo is gone." {
			found = true
		}
	}
	if !found {
		t.Fatalf("no failed-start event in %+v", events)
	}
}

// sendPrompt prepares a delegated prompt the way a send does and, when sent,
// marks its transport started; the agent never checks in on it.
func (f *relaunchFixture) sendPrompt(t *testing.T, turnID string, sent bool) {
	t.Helper()
	if _, _, err := f.store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: f.item.ID, SessionID: "worker", ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: pendingSubmissionDigest("render the trailer " + turnID), ProcessIdentity: "delegated-process",
		PaneGeneration: "delegated-pane", AcceptedAt: time.Now().UTC(), Mode: watcher.InputAdmissionFresh, SignalProtocol: true,
	}); err != nil {
		t.Fatal(err)
	}
	if sent {
		if err := f.store.MarkInputAdmissionTransportStarted("worker", turnID); err != nil {
			t.Fatal(err)
		}
	}
}

// A prompt sent to the Worker that its agent died before checking in on is a
// Turn in flight, from the Turn ledger alone: it is sent again when the
// conversation provably lacks it, and Brain is asked to decide when that is
// unknown. Never an idle relaunch.
func TestAgentRelaunchPromptInFlightIsATurn(t *testing.T) {
	f := newRelaunchFixture(t)
	f.sendPrompt(t, "turn:prompt", true)

	end := killedEnd(time.Now())
	end.PendingTurnID, end.PromptDelivery = "turn:prompt", watcher.PromptNotHeld
	plan, err := f.service.PlanAgentRelaunch(end)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != watcher.AgentEndRelaunch || plan.TurnID != "" || plan.PendingTurnID != "turn:prompt" ||
		plan.Words != "Claude Code was killed (SIGKILL); Mewla started it again in the same conversation and sent it its task again: "+
			"the task (turn:prompt) had not reached the conversation before it ended, so nothing of it ran." {
		t.Fatalf("not held: plan = %+v", plan)
	}

	// The watcher did not read the conversation for it: not known.
	end.PendingTurnID, end.PromptDelivery = "", ""
	plan, err = f.service.PlanAgentRelaunch(end)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != watcher.AgentEndRelaunch || plan.PendingTurnID != "turn:prompt" ||
		!strings.Contains(plan.Words, "Whether its task (turn:prompt) had reached the conversation before it ended is unknown, so it was not sent again and nothing is running for it: decide whether to send it again.") {
		t.Fatalf("unknown: plan = %+v", plan)
	}

	// Ended by itself with a prompt in flight: reported, never ignored.
	clean := exitedEnd(0, time.Now())
	plan, err = f.service.PlanAgentRelaunch(clean)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != watcher.AgentEndReport || plan.PendingTurnID != "turn:prompt" || !strings.Contains(plan.Words, "it ended itself before it reported") {
		t.Fatalf("clean exit: plan = %+v", plan)
	}

	// Brain hears of it once, as that Turn lost; the prompt is released, so it
	// fences nothing and the next end is no longer about it.
	told, err := f.service.ReportAgentEnd(clean, plan)
	if err != nil || !told {
		t.Fatalf("report = %v, %v", told, err)
	}
	st, admission, err := f.store.fsmAdmission("worker", "turn:prompt")
	if err != nil || admission.Status != lifecycle.AdmissionAmbiguous || admission.Reason != watcher.AgentEndedAdmissionReason {
		t.Fatalf("admission after report = %+v, %v", admission, err)
	}
	if st.Review == nil || st.Review.Reason != "turn_lost" || st.Review.Ref != "turn:prompt" {
		t.Fatalf("review = %+v", st.Review)
	}
	if active := st.ActiveAdmission(); active != nil {
		t.Fatalf("released prompt still fences: %+v", active)
	}
	plan, err = f.service.PlanAgentRelaunch(killedEnd(time.Now().Add(time.Minute)))
	if err != nil || plan.PendingTurnID != "" {
		t.Fatalf("after the report: plan = %+v, %v", plan, err)
	}
}

// A prompt prepared but never sent (its transport did not start) is nothing
// the agent could have read: the end is an idle one.
func TestAgentRelaunchUnsentPromptIsNotInFlight(t *testing.T) {
	f := newRelaunchFixture(t)
	f.sendPrompt(t, "turn:unsent", false)
	plan, err := f.service.PlanAgentRelaunch(killedEnd(time.Now()))
	if err != nil || plan.Action != watcher.AgentEndRelaunch || plan.PendingTurnID != "" || plan.TurnID != "" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
}

// A provider outage is waited out: the end is held, no try is used, and once
// the provider answers the agent is started again as the first try.
func TestAgentRelaunchWaitsOutAProviderOutage(t *testing.T) {
	f := newRelaunchFixture(t)
	outage := &outageWatcher{fakeWatcher: f.fw, unavailable: true}
	f.service.watcher = outage
	end := killedEnd(time.Now())
	plan, err := f.service.PlanAgentRelaunch(end)
	if err != nil || plan.Action != watcher.AgentEndWait || plan.Line != "Waiting for Claude Code's provider" || plan.Provider != "claude" ||
		plan.Words != "Claude Code was killed (SIGKILL); it is started again once its provider answers (rate limit)." {
		t.Fatalf("during the outage: plan = %+v, %v", plan, err)
	}
	if records, _, _ := f.store.agentRelaunches(f.item.ID, "worker"); len(records) != 0 {
		t.Fatalf("a wait used a try: %+v", records)
	}
	// The end the watcher starts as the test (its probe, or the wait's
	// bound) is an ordinary relaunch, and Brain hears the outage was on.
	test := end
	test.ProviderTest = true
	plan, err = f.service.PlanAgentRelaunch(test)
	if err != nil || plan.Action != watcher.AgentEndRelaunch || plan.Attempt != 1 ||
		!strings.HasSuffix(plan.Words, " Its provider was still refusing requests (rate limit) when it was started.") {
		t.Fatalf("the test start: plan = %+v, %v", plan, err)
	}
	outage.unavailable = false
	plan, err = f.service.PlanAgentRelaunch(end)
	if err != nil || plan.Action != watcher.AgentEndRelaunch || plan.Attempt != 1 || plan.After != 0 {
		t.Fatalf("after the outage: plan = %+v, %v", plan, err)
	}
}

// A Worker past its bound during an outage is stopped at once with its cause,
// not held first: the outage only delays a restart that would happen.
func TestAgentRelaunchPastItsBoundIsNotHeldForAnOutage(t *testing.T) {
	f := newRelaunchFixture(t)
	f.service.watcher = &outageWatcher{fakeWatcher: f.fw, unavailable: true}
	base := time.Now().UTC()
	for attempt := 1; attempt <= agentRelaunchLimit; attempt++ {
		at := base.Add(time.Duration(attempt-agentRelaunchLimit-1) * time.Minute)
		if err := f.service.RecordAgentRelaunch(watcher.AgentRelaunch{WorkID: f.item.ID, SessionID: "worker", Attempt: attempt, Cause: "was killed (SIGKILL)",
			EndedAt: at, Outcome: "idle", Words: "w", At: at}); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := f.service.PlanAgentRelaunch(killedEnd(base))
	if err != nil || plan.Action != watcher.AgentEndReport || plan.Line != "Stopped after 3 restarts" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
}

// The lifetime bound: a task whose agent dies again and again, each time past
// the thirty-minute window, is stopped at its fifth relaunch, counted along
// its continuations.
func TestAgentRelaunchBoundForOneTask(t *testing.T) {
	f := newRelaunchFixture(t)
	f.startTurn(t)
	base := time.Now().UTC().Add(-10 * time.Hour)
	// The live Turn is the newest continuation of the chain.
	from := "turn:origin"
	for attempt := 1; attempt <= agentRelaunchTurnLimit; attempt++ {
		next := fmt.Sprintf("turn:continued-%d", attempt)
		if attempt == agentRelaunchTurnLimit {
			next = "turn:first"
		}
		at := base.Add(time.Duration(attempt) * time.Hour)
		if err := f.service.RecordAgentRelaunch(watcher.AgentRelaunch{WorkID: f.item.ID, SessionID: "worker", Attempt: 1, Cause: "was killed (SIGKILL)",
			EndedAt: at, FromTurnID: from, TurnID: next, Outcome: "continued", Words: "w", At: at}); err != nil {
			t.Fatal(err)
		}
		from = next
	}
	records, _, _ := f.store.agentRelaunches(f.item.ID, "worker")
	if got := relaunchesOfTask(records, from); got != agentRelaunchTurnLimit {
		t.Fatalf("relaunches of the task = %d", got)
	}
	if got := relaunchesOfTask(records, "turn:unrelated"); got != 0 {
		t.Fatalf("an unrelated turn counts %d", got)
	}
	end := killedEnd(time.Now())
	end.TurnID = "turn:first"
	plan, err := f.service.PlanAgentRelaunch(end)
	if err != nil || plan.Action != watcher.AgentEndReport || plan.Line != "Stopped after 5 restarts" ||
		!strings.Contains(plan.Words, "it was started again 5 times during this task and ended each time") {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
}

// The continuation a relaunch sends while the killed Turn still holds the
// Attempt steers that Attempt (the watcher's reuse decision for a live signal
// Turn); the agent's own report on the continuation's id makes it the Work's
// Turn, and its done is the Work's result.
func TestAgentRelaunchContinuationsDoneIsTheWorksResult(t *testing.T) {
	f := newRelaunchFixture(t)
	f.startTurn(t)
	payload := watcher.DelegatedLifecyclePayload(watcher.AgentContinuationPrompt("was killed (SIGKILL)", "", false), "turn:cont")
	if _, created, err := f.store.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: f.item.ID, SessionID: "worker", ProposedTurnID: "turn:cont", Receipt: "turn:cont",
		PayloadSHA256: pendingSubmissionDigest(payload), ProcessIdentity: "proc", PaneGeneration: "pane",
		AcceptedAt: time.Now().UTC(), Mode: watcher.InputAdmissionConditionalSteer,
		ExistingTurnID: "turn:first", BaselineActivityID: "observed-activity", SignalProtocol: true,
	}); err != nil || !created {
		t.Fatalf("continuation prepare created=%v err=%v", created, err)
	}
	result, err := f.service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: "turn:cont", Class: watcher.EvidenceControl,
		Kind: "done", SourceID: "cont-done", Summary: "trailer rendered", At: time.Now()})
	if err != nil || !result.Changed {
		t.Fatalf("done on the continuation = %+v, %v", result, err)
	}
	if err := f.service.ReconcileWorkChange(); err != nil {
		t.Fatal(err)
	}
	state, err := f.store.FSM().State(lifecycle.WorkID(f.item.ID))
	if err != nil {
		t.Fatal(err)
	}
	if state.LastSummary != "trailer rendered" || state.AdmissionByToken("turn:cont").ResultTurnToken != "turn:cont" {
		t.Fatalf("after the continuation's done: summary %q admission %+v", state.LastSummary, state.AdmissionByToken("turn:cont"))
	}
	requireReviewDelivered(t, f.store, f.item.ID)
}
