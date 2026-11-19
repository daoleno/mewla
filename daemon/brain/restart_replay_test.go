package brain

import (
	"testing"
	"time"
)

// restartFixture is a new Service on the same store, as a daemon restart makes.
func (f *progressJudgeFixture) restartService() {
	restarted := NewService(f.store, nil, nil)
	restarted.SetProgressAsker(f.model.ask)
	restarted.now = func() time.Time { return f.clock }
	restarted.progress.loading = f.service.progress.loading
	f.service = restarted
	f.service.seedProgressLastRun()
}

// 11-20 00:50: on a restart the watcher replays the facts of every turn it
// knows, an older turn's among them. A judged Work whose older turn replays
// first has nothing new: it is not judged again.
func TestARestartReplayingAnOlderTurnJudgesNothing(t *testing.T) {
	f := newProgressJudgeFixture(t, answerShownZh, answerShownLater, answerShownLater)
	f.noteAndWait(t, f.fact("turn:1", "running", "fix written"), true)
	f.noteAndWait(t, f.fact("turn:1", "done", "fix written"), false)
	f.service.requestChanged() // the live loop's scan: Brain's disposition is noted once
	f.clock = f.clock.Add(progressMinGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatal("turn:1 not judged")
	}
	f.noteAndWait(t, f.fact("turn:2", "running", "verifying on mobile"), true)
	f.clock = f.clock.Add(progressMinGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatal("turn:2 not judged")
	}
	calls := len(f.model.prompts)
	f.restartService()
	for _, fact := range [][3]string{{"turn:1", "running", "fix written"}, {"turn:1", "done", "fix written"}, {"turn:2", "running", "verifying on mobile"}} {
		f.service.noteProgressFact(f.fact(fact[0], fact[1], fact[2]), fact[1] != "done")
	}
	time.Sleep(100 * time.Millisecond)
	f.service.requestChanged()
	f.clock = f.clock.Add(time.Hour)
	if f.judgeDue(t) || len(f.model.prompts) != calls {
		t.Fatalf("a restart replaying an older turn judged again: %d model calls, want %d", len(f.model.prompts), calls)
	}
}

// A report not yet judged when the daemon restarts is still judged, but no
// sooner than its ten-minute gap allows: a restart is not a turn ending.
func TestAReportLeftUnjudgedAtARestartKeepsItsGap(t *testing.T) {
	f := newProgressJudgeFixture(t, answerShownZh, answerShownLater)
	f.reportVia(t, "running", "working", "tests 10/20")
	f.service.requestChanged() // the live loop's scan: Brain's disposition is noted once
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatal("not judged")
	}
	judgedAt := f.clock
	f.reportVia(t, "running", "working", "tests 20/20")
	f.clock = f.clock.Add(2 * time.Minute)
	f.restartService()
	f.service.requestChanged()
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if f.judgeDue(t) {
		t.Fatalf("judged %v after the last judgment, inside the gap", f.clock.Sub(judgedAt))
	}
	f.clock = judgedAt.Add(progressMinGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatal("the report left at the restart was never judged")
	}
}
