package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

func (f *progressJudgeFixture) pending() bool {
	f.service.progress.mu.Lock()
	defer f.service.progress.mu.Unlock()
	_, ok := f.service.progress.pending[f.workID]
	return ok
}

// noteAndWait notes a fact the way the watcher does and waits for its evidence to be folded in.
func (f *progressJudgeFixture) noteAndWait(t *testing.T, fact watcher.TurnFact, reported bool) {
	t.Helper()
	before := f.work(t).Progress
	f.service.noteProgressFact(fact, reported)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		after := f.work(t).Progress
		if after != nil && after.Seen != nil && (before == nil || before.Seen == nil || *after.Seen != *before.Seen || len(after.Reports) != len(before.Reports)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *progressJudgeFixture) fact(turnID, kind, summary string) watcher.TurnFact {
	return watcher.TurnFact{SessionID: f.sessionID, TurnID: turnID, Class: watcher.EvidenceControl, Kind: kind, Summary: summary, Phase: "working", At: f.at()}
}

func TestARestartJudgesNothingWithoutNewEvidence(t *testing.T) {
	f := newProgressJudgeFixture(t, answerShownZh)
	f.reportVia(t, "running", "working", "fix deployed")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatal("not judged")
	}
	// A restart: a new Service on the same store, and the watcher replays the turn's facts.
	restarted := NewService(f.store, nil, nil)
	restarted.SetProgressAsker(f.model.ask)
	restarted.now = func() time.Time { return f.clock }
	f.service = restarted
	f.service.seedProgressLastRun()
	f.noteAndWait(t, f.fact("turn:1", "running", "fix deployed"), true)
	time.Sleep(50 * time.Millisecond)
	f.clock = f.clock.Add(time.Hour)
	if f.pending() || f.judgeDue(t) {
		t.Fatal("a replayed fact was judged again")
	}
	if len(f.model.prompts) != 1 {
		t.Fatalf("%d model calls, want 1", len(f.model.prompts))
	}
	// The judgment it made is kept on the Work with its cost, and the gap since it came back with the restart.
	if runs := f.work(t).Progress.Runs; len(runs) != 1 || runs[0].USD != 0.01 {
		t.Fatalf("runs after restart = %#v", runs)
	}
	if _, ok := f.service.progress.lastRun[f.workID]; !ok {
		t.Fatal("the last judgment's time did not come back with the restart")
	}
}

func TestAHeartbeatIsNotEvidenceButNewWordsAndTurnsAre(t *testing.T) {
	f := newProgressJudgeFixture(t, answerShownZh)
	f.reportVia(t, "running", "working", "tests 10/20")
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	f.judgeDue(t)
	f.noteAndWait(t, f.fact("turn:1", "running", "tests 10/20"), true)
	time.Sleep(50 * time.Millisecond)
	if f.pending() {
		t.Fatal("a repeated report asked for a judgment")
	}
	if n := len(f.work(t).Progress.Reports); n != 1 {
		t.Fatalf("a repeated report was kept: %d reports", n)
	}
	f.noteAndWait(t, f.fact("turn:1", "running", "tests 20/20"), true)
	if !f.pending() {
		t.Fatal("new words were not evidence")
	}
	f.clock = f.clock.Add(progressMinGap + time.Second)
	f.judgeDue(t)
	// The turn ending is evidence; seeing it ended again is not.
	f.noteAndWait(t, f.fact("turn:1", "done", ""), false)
	if !f.pending() {
		t.Fatal("a turn ending was not evidence")
	}
	f.clock = f.clock.Add(progressMinGap + time.Second)
	f.judgeDue(t)
	f.service.noteProgressFact(f.fact("turn:1", "done", ""), false)
	time.Sleep(100 * time.Millisecond)
	if f.pending() {
		t.Fatal("the same ended turn asked again")
	}
	// A new turn (Brain wrote to the Worker) is evidence.
	f.noteAndWait(t, f.fact("turn:2", "running", "on it"), true)
	if !f.pending() {
		t.Fatal("a new turn was not evidence")
	}
}
