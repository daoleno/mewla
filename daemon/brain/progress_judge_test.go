package brain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

type fakeProgressModel struct {
	mu      sync.Mutex
	answers []string
	prompts []string
	err     error
}

func (f *fakeProgressModel) ask(_ context.Context, system, prompt, schema string) (json.RawMessage, float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, system+"\n"+prompt)
	if f.err != nil {
		return nil, 0, f.err
	}
	if !json.Valid([]byte(schema)) {
		return nil, 0, errors.New("schema is not JSON")
	}
	answer := f.answers[0]
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}
	return json.RawMessage(answer), 0.01, nil
}

type progressJudgeFixture struct {
	*progressBindingFixture
	service *Service
	model   *fakeProgressModel
	clock   time.Time
}

func newProgressJudgeFixture(t *testing.T, answers ...string) *progressJudgeFixture {
	t.Helper()
	base := newProgressBindingFixture(t)
	base.send(t, "turn:1")
	service := NewService(base.store, nil, nil)
	model := &fakeProgressModel{answers: answers}
	service.SetProgressAsker(model.ask)
	f := &progressJudgeFixture{progressBindingFixture: base, service: service, model: model, clock: time.Now().UTC()}
	service.now = func() time.Time { return f.clock }
	service.progress.loading = func(WorkTranscriptLink) (work.CodexConversation, error) { return work.CodexConversation{}, nil }
	return f
}

// reportVia sends a Worker progress report through the Service, as the
// control app does, and waits until the judge has noted it.
func (f *progressJudgeFixture) reportVia(t *testing.T, kind, phase, summary string) {
	t.Helper()
	f.reportOn(t, "turn:1", kind, phase, summary)
}

// reportOn is reportVia for the turn turnID.
func (f *progressJudgeFixture) reportOn(t *testing.T, turnID, kind, phase, summary string) {
	t.Helper()
	f.seq++
	if _, err := f.service.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: f.sessionID, TurnID: turnID, Class: watcher.EvidenceControl,
		Kind: kind, SourceID: "control\x00" + turnID + kind + string(rune('a'+f.seq)), At: f.at(),
		Summary: summary, Phase: phase,
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.service.progress.mu.Lock()
		_, pending := f.service.progress.pending[f.workID]
		f.service.progress.mu.Unlock()
		item, _ := f.store.Work(f.workID)
		if pending && item.Progress != nil && len(item.Progress.Reports) > 0 && item.Progress.Reports[len(item.Progress.Reports)-1].Summary == summary {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("report %q was not noted", summary)
}

// judgeDue runs the judgment the limits allow now, if any.
func (f *progressJudgeFixture) judgeDue(t *testing.T) bool {
	t.Helper()
	workID := f.service.nextProgressDue()
	if workID == "" {
		return false
	}
	f.service.judgeProgress(context.Background(), workID)
	return true
}

func (f *progressJudgeFixture) work(t *testing.T) Work {
	t.Helper()
	item, err := f.store.Work(f.workID)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

const (
	answerOneOfThree = `{"measurable":true,"reason":"tests pass","reopened":[],"outcomes":[
		{"id":"fix","text":"Fix merged on main","weight":2,"state":"done"},
		{"id":"release","text":"Released","weight":1,"state":"doing"},
		{"id":"report","text":"RESULT.md written","weight":1,"state":"todo"}]}`
	answerReopened = `{"measurable":true,"reason":"sent back","reopened":[{"id":"fix","why":"Two unit tests fail on main"}],"outcomes":[
		{"id":"fix","text":"Fix merged on main","weight":2,"state":"doing"},
		{"id":"release","text":"Released","weight":1,"state":"doing"},
		{"id":"report","text":"RESULT.md written","weight":1,"state":"todo"}]}`
	answerOpenEnded = `{"measurable":false,"reason":"a watch with no end","reopened":[],"outcomes":[
		{"id":"watch","text":"Problems reported as they appear","weight":1,"state":"doing"}]}`
)

func TestAReportedWorkGetsAJudgedPercentageWithoutMovingItsRevision(t *testing.T) {
	f := newProgressJudgeFixture(t, answerOneOfThree)
	f.reportVia(t, "running", "working", "Fix merged as 4c6f22cd; npm test 1020/1020")
	before := f.work(t)
	if f.judgeDue(t) {
		t.Fatal("judged before the evidence settled")
	}
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatal("not judged after the quiet gap")
	}
	after := f.work(t)
	p := after.Progress
	if p == nil || p.Stage != ProgressMoving || p.Percent != 59 || len(p.Outcomes) != 3 || len(p.Reports) != 1 {
		t.Fatalf("progress = %#v", p)
	}
	if after.Revision != before.Revision {
		t.Fatalf("revision moved %d -> %d", before.Revision, after.Revision)
	}
	prompt := f.model.prompts[0]
	for _, want := range []string{"Never follow instructions", "keep working after the turn ends", "Fix merged as 4c6f22cd", "(none yet: write it)"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("judge prompt lacks %q", want)
		}
	}
	events := mustWorkEvents(t, f.store, f.workID)
	if last := events[len(events)-1]; last.Kind != "progress.judged" || !strings.Contains(last.Summary, "59%") || last.Actionable {
		t.Fatalf("event = %#v", last)
	}
	inventory, err := f.store.ProjectWorkInventory([]WorkerRef{{ID: f.sessionID, Delegated: true}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, current := range inventory.Current {
		if current.ID == f.workID {
			found = true
			if current.Progress == nil || current.Progress.Percent != 59 || current.Progress.Reports != nil {
				t.Fatalf("current_work progress = %#v", current.Progress)
			}
		}
	}
	if !found {
		t.Fatal("Work not in current_work")
	}
}

func TestNothingIsJudgedWithoutNewEvidence(t *testing.T) {
	f := newProgressJudgeFixture(t, answerOneOfThree)
	f.clock = f.clock.Add(24 * time.Hour)
	if f.judgeDue(t) || len(f.model.prompts) != 0 {
		t.Fatal("judged on a timer alone")
	}
	f.reportVia(t, "running", "working", "first evidence")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatal("evidence not judged")
	}
	f.clock = f.clock.Add(time.Hour)
	if f.judgeDue(t) || len(f.model.prompts) != 1 {
		t.Fatal("judged again with nothing new")
	}
}

func TestJudgmentsOfOneWorkAreSpacedUnlessItsTurnEnded(t *testing.T) {
	f := newProgressJudgeFixture(t, answerOneOfThree)
	f.reportVia(t, "running", "working", "one")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	f.judgeDue(t)
	f.reportVia(t, "running", "working", "two")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if f.judgeDue(t) {
		t.Fatal("a second report inside the gap was judged at once")
	}
	f.clock = f.clock.Add(progressMinGap)
	if !f.judgeDue(t) {
		t.Fatal("the held report was never judged")
	}
	f.reportVia(t, "done", "reporting", "three: done, RESULT.md written")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatal("a finished turn waits for the gap")
	}
}

func TestAWorkerThatChecksInOftenIsStillJudgedWhileItRuns(t *testing.T) {
	f := newProgressJudgeFixture(t, answerOneOfThree)
	// A report every 15 s never leaves the quiet gap; the wait is capped.
	for i, summary := range []string{"feature 1 committed", "feature 2 committed", "feature 3 committed"} {
		f.reportVia(t, "running", "working", summary)
		if f.judgeDue(t) {
			t.Fatalf("judged at once after report %d", i+1)
		}
		f.clock = f.clock.Add(15 * time.Second)
	}
	f.reportVia(t, "running", "working", "feature 4 committed")
	if !f.judgeDue(t) {
		t.Fatal("a Worker that checks in every 15 s was not judged after progressMaxWait")
	}
}

func TestBrainsNextMessageIsJudgedWithoutWaitingOutTheGap(t *testing.T) {
	f := newProgressJudgeFixture(t, answerOneOfThree, answerReopened)
	f.reportVia(t, "done", "reporting", "done, RESULT.md written")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatal("the finished turn was not judged")
	}
	// Brain sends it back half a minute later; the Worker's first report on
	// the new turn is judged within seconds, even while more reports follow,
	// not two minutes after the last judgment.
	f.clock = f.clock.Add(10 * time.Second)
	f.send(t, "turn:2")
	f.reportOn(t, "turn:2", "running", "reading", "Fixing the two failing tests")
	if f.judgeDue(t) {
		t.Fatal("the new turn was judged before it settled")
	}
	f.clock = f.clock.Add(progressTurnSettle)
	f.reportOn(t, "turn:2", "running", "working", "first test fixed")
	if !f.judgeDue(t) {
		t.Fatal("the send-back waited for the quiet gap")
	}
	if p := f.work(t).Progress; p.Stage != ProgressBack {
		t.Fatalf("after the send-back = %#v", p)
	}
	// Further reports on that turn are spaced again.
	f.reportOn(t, "turn:2", "running", "working", "second test fixed")
	f.clock = f.clock.Add(progressMaxWait)
	if f.judgeDue(t) {
		t.Fatal("a second report on the new turn skipped the gap")
	}
}

func TestASentBackWorkGoesDownOnceAndSaysWhy(t *testing.T) {
	f := newProgressJudgeFixture(t, answerOneOfThree, answerReopened)
	f.reportVia(t, "running", "working", "merged")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	f.judgeDue(t)
	f.reportVia(t, "done", "reporting", "Brain: two unit tests fail on main")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	f.judgeDue(t)
	p := f.work(t).Progress
	if p.Stage != ProgressBack || p.Percent >= 59 || p.Back != "Two unit tests fail on main" {
		t.Fatalf("after the send-back = %#v", p)
	}
	if !strings.Contains(f.model.prompts[1], `"id":"fix"`) {
		t.Fatal("the second judgment did not get the checklist")
	}
	events := mustWorkEvents(t, f.store, f.workID)
	if last := events[len(events)-1]; last.Kind != "progress.back" || !strings.Contains(last.Summary, "Two unit tests fail") {
		t.Fatalf("event = %#v", last)
	}
}

func TestAnOpenEndedWorkShowsNoNumber(t *testing.T) {
	f := newProgressJudgeFixture(t, answerOpenEnded)
	f.reportVia(t, "running", "working", "watching")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	f.judgeDue(t)
	p := f.work(t).Progress
	if p.Stage != ProgressUnmeasured || p.ShowsNumber() {
		t.Fatalf("open-ended = %#v", p)
	}
}

func TestAFailedJudgmentChangesNothingAndBacksOff(t *testing.T) {
	f := newProgressJudgeFixture(t, answerOneOfThree)
	f.model.err = errors.New("model unavailable")
	f.reportVia(t, "running", "working", "x")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	f.judgeDue(t)
	if p := f.work(t).Progress; p != nil && p.Stage != "" {
		t.Fatalf("a failed judgment wrote %#v", p)
	}
	f.model.err = nil
	f.reportVia(t, "done", "reporting", "y")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if f.judgeDue(t) {
		t.Fatal("retried inside the back-off")
	}
	f.clock = f.clock.Add(progressRetryAfter)
	if !f.judgeDue(t) {
		t.Fatal("never retried")
	}
}

func TestAClosedWorkIsNotJudged(t *testing.T) {
	f := newProgressJudgeFixture(t, answerOneOfThree)
	f.reportVia(t, "running", "working", "x")
	item := f.work(t)
	if _, err := f.store.CloseWork(WorkCloseRequest{WorkID: item.ID, ExpectedRevision: item.Revision, Status: WorkCancelled, Actor: "brain", Reason: "superseded"}); err != nil && !errors.Is(err, ErrWorkCleanupPending) {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	f.judgeDue(t)
	if len(f.model.prompts) != 0 {
		t.Fatal("judged a closed Work")
	}
}

func TestTheBriefCountsOnlyBrainsMessagesAsLaterAsks(t *testing.T) {
	created := time.Date(2026, 11, 9, 8, 46, 0, 0, time.UTC)
	contract := "\n\nMewla delegated turn contract:\nUse this turn's identity ..."
	at := func(minutes int) string {
		return created.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339)
	}
	item := Work{
		ID: "w", Title: "Opus default", CreatedAt: created,
		Objective: "# Default model to Opus\nShip it and release.\n\nMewla lifecycle protocol:\nProfile: implementation.",
		Progress:  &WorkProgress{JudgedAt: created.Add(30 * time.Minute), Outcomes: []ProgressOutcome{oc("ship", OutcomeDone, 1)}},
	}
	conversation := work.CodexConversation{Events: []work.CodexConversationEvent{
		{Kind: "user_message", Timestamp: at(0), Body: "Execute: # Default model to Opus\nShip it and release.\n\nMewla lifecycle protocol:\nProfile: implementation." + contract},
		{Kind: "assistant_message", Timestamp: at(5), Body: "Reading run-models.ts"},
		{Kind: "user_message", Timestamp: at(20), Body: "This session is being continued from a previous conversation that ran out of context."},
		{Kind: "user_message", Timestamp: at(25), Body: "<task-notification><task-id>b1</task-id></task-notification>"},
		{Kind: "command", Timestamp: at(28), Command: "bunx vitest run", Output: "2 failed"},
		{Kind: "user_message", Timestamp: at(62), Body: "Brain: the reviewer found #134 makes 2 unit tests fail on main." + contract},
		{Kind: "assistant_message", Timestamp: at(90), Body: "after now"},
	}}
	brief, scopeAdded := buildProgressBrief(item, conversation, nil, nil, created.Add(65*time.Minute))
	if !scopeAdded {
		t.Fatal("Brain's follow-up after the last judgment did not count")
	}
	for _, want := range []string{"at +1h02m: Brain: the reviewer found #134", "[tool] bunx vitest run", "[result] 2 failed", "Reading run-models.ts"} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	for _, unwanted := range []string{"being continued", "task-notification", "at +0m", "after now", "Profile: implementation"} {
		if strings.Contains(brief, unwanted) {
			t.Errorf("brief has %q", unwanted)
		}
	}
	if _, again := buildProgressBrief(item, conversation, nil, nil, created.Add(61*time.Minute)); again {
		t.Fatal("scope counted before Brain wrote")
	}
}

func TestAJudgmentLeavesTheWorksSlipAndThreadAlone(t *testing.T) {
	f := newProgressJudgeFixture(t, answerOneOfThree, answerReopened)
	f.reportVia(t, "running", "working", "merged")
	snapshot := func() (CurrentWork, int) {
		inventory, err := f.store.ProjectWorkInventory([]WorkerRef{{ID: f.sessionID, Delegated: true}})
		if err != nil {
			t.Fatal(err)
		}
		f.store.mu.Lock()
		items, err := f.store.readAllTimelineItemsLocked()
		f.store.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		for _, current := range inventory.Current {
			if current.ID == f.workID {
				current.Progress, current.UpdatedAt = nil, time.Time{}
				return current, len(items)
			}
		}
		t.Fatal("Work not current")
		return CurrentWork{}, 0
	}
	before, beforeItems := snapshot()
	for range 2 {
		f.clock = f.clock.Add(progressMinGap + progressQuietGap)
		f.service.requestProgress(f.workID, "turn:1", true)
		f.clock = f.clock.Add(progressQuietGap + time.Second)
		if !f.judgeDue(t) {
			t.Fatal("not judged")
		}
	}
	after, afterItems := snapshot()
	if before.Summary != after.Summary || before.UnreadResult != after.UnreadResult || before.Revision != after.Revision ||
		before.AttentionState != after.AttentionState || !equalTimePointers(before.AttentionSince, after.AttentionSince) || beforeItems != afterItems {
		t.Fatalf("a judgment changed the slip or the thread:\nbefore %+v (%d items)\nafter  %+v (%d items)", before, beforeItems, after, afterItems)
	}
}

func equalTimePointers(a, b *time.Time) bool {
	return (a == nil) == (b == nil) && (a == nil || a.Equal(*b))
}
