package brain

import (
	"testing"
	"time"
)

func verdictOf(measurable bool, outcomes ...ProgressOutcome) progressVerdict {
	return progressVerdict{Measurable: measurable, Outcomes: outcomes}
}

func oc(id, state string, weight int) ProgressOutcome {
	return ProgressOutcome{ID: id, Text: "outcome " + id, Weight: weight, State: state}
}

var progressNow = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

func TestProgressComesFromTheChecklistMarks(t *testing.T) {
	got, changed := applyProgressVerdict(nil, verdictOf(true, oc("a", OutcomeDone, 2), oc("b", OutcomeDoing, 1), oc("c", OutcomeTodo, 1)), false, progressNow)
	// (2 + 0.35) / 4
	if !changed || got.Stage != ProgressMoving || got.Percent != 59 || len(got.Outcomes) != 3 || got.Judgments != 1 {
		t.Fatalf("first judgment = %#v", got)
	}
}

func TestProgressNeverGoesDownWithoutAReopen(t *testing.T) {
	prev, _ := applyProgressVerdict(nil, verdictOf(true, oc("a", OutcomeDone, 1), oc("b", OutcomeDoing, 1)), false, progressNow)
	// The judge un-marks a done outcome without naming a reopen, and marks b
	// todo: done stays done, and the shown value does not fall.
	next, _ := applyProgressVerdict(&prev, verdictOf(true, oc("a", OutcomeTodo, 1), oc("b", OutcomeTodo, 1)), false, progressNow)
	if next.Outcomes[0].State != OutcomeDone || next.Percent != prev.Percent || next.Stage != ProgressMoving {
		t.Fatalf("unexplained drop applied: prev=%#v next=%#v", prev, next)
	}
	// A dropped outcome keeps its mark; texts and weights stay the first ones.
	again, _ := applyProgressVerdict(&next, verdictOf(true, ProgressOutcome{ID: "a", Text: "renamed", Weight: 3, State: OutcomeDone}), false, progressNow)
	if len(again.Outcomes) != 2 || again.Outcomes[0].Text != "outcome a" || again.Outcomes[0].Weight != 1 {
		t.Fatalf("checklist not kept: %#v", again.Outcomes)
	}
}

func TestAReopenedOutcomeLowersTheValueOnceAndSaysWhy(t *testing.T) {
	prev, _ := applyProgressVerdict(nil, verdictOf(true, oc("fix", OutcomeDone, 2), oc("release", OutcomeDone, 1), oc("report", OutcomeTodo, 1)), false, progressNow)
	if prev.Percent != 75 {
		t.Fatalf("before = %d", prev.Percent)
	}
	verdict := verdictOf(true, oc("fix", OutcomeDoing, 2), oc("release", OutcomeDone, 1), oc("report", OutcomeTodo, 1))
	verdict.Reopened = append(verdict.Reopened, struct {
		ID  string `json:"id"`
		Why string `json:"why"`
	}{ID: "fix", Why: "Two unit tests fail on main"})
	back, changed := applyProgressVerdict(&prev, verdict, false, progressNow)
	if !changed || back.Stage != ProgressBack || back.Percent != 43 || back.Back != "Two unit tests fail on main" {
		t.Fatalf("reopen = %#v", back)
	}
	// It stays amber while it does not move, and turns back to moving when it rises.
	same, _ := applyProgressVerdict(&back, verdict, false, progressNow)
	if same.Stage != ProgressBack || same.Percent != 43 {
		t.Fatalf("held = %#v", same)
	}
	up, _ := applyProgressVerdict(&same, verdictOf(true, oc("fix", OutcomeDone, 2), oc("release", OutcomeDone, 1), oc("report", OutcomeDoing, 1)), false, progressNow)
	if up.Stage != ProgressMoving || up.Back != "" || up.Percent != 84 {
		t.Fatalf("recovered = %#v", up)
	}
}

func TestTheChecklistGrowsOnlyWhenBrainAddsToTheWork(t *testing.T) {
	prev, _ := applyProgressVerdict(nil, verdictOf(true, oc("a", OutcomeDone, 1), oc("b", OutcomeDone, 1), oc("c", OutcomeDoing, 1)), false, progressNow)
	more := verdictOf(true, oc("a", OutcomeDone, 1), oc("b", OutcomeDone, 1), oc("c", OutcomeDoing, 1), oc("d", OutcomeTodo, 3))
	same, _ := applyProgressVerdict(&prev, more, false, progressNow)
	if len(same.Outcomes) != 3 {
		t.Fatalf("grew without a message: %#v", same.Outcomes)
	}
	grown, _ := applyProgressVerdict(&prev, more, true, progressNow)
	if len(grown.Outcomes) != 4 || grown.Stage != ProgressBack || grown.Percent >= prev.Percent || grown.Back != "More was asked" {
		t.Fatalf("scope add = %#v", grown)
	}
}

func TestNoHundredBeforeTheWorkIsAccepted(t *testing.T) {
	got, _ := applyProgressVerdict(nil, verdictOf(true, oc("a", OutcomeDone, 1), oc("b", OutcomeDone, 2)), false, progressNow)
	if got.Stage != ProgressLast || got.Percent > progressCeiling || got.ShowsNumber() {
		t.Fatalf("all done = %#v", got)
	}
}

func TestUnmeasurableShowsNoNumberAndKeepsTheLastValue(t *testing.T) {
	prev, _ := applyProgressVerdict(nil, verdictOf(true, oc("a", OutcomeDone, 1), oc("b", OutcomeTodo, 1)), false, progressNow)
	open, _ := applyProgressVerdict(&prev, verdictOf(false, oc("a", OutcomeDone, 1), oc("b", OutcomeTodo, 1)), false, progressNow)
	if open.Stage != ProgressUnmeasured || open.ShowsNumber() || open.Percent != prev.Percent {
		t.Fatalf("unmeasurable = %#v", open)
	}
	// When it can be measured again the number comes back where it was.
	resumed, _ := applyProgressVerdict(&open, verdictOf(true, oc("a", OutcomeDone, 1), oc("b", OutcomeTodo, 1)), false, progressNow)
	if resumed.Stage != ProgressMoving || resumed.Percent != prev.Percent {
		t.Fatalf("resumed = %#v", resumed)
	}
}

func TestAFullChecklistStillTakesInWhatBrainAddsLater(t *testing.T) {
	var first []ProgressOutcome
	for i := 0; i < progressMaxOutcomes; i++ {
		first = append(first, oc(string(rune('a'+i)), OutcomeDone, 1))
	}
	prev, _ := applyProgressVerdict(nil, verdictOf(true, first...), false, progressNow)
	if prev.Stage != ProgressLast {
		t.Fatalf("all done = %#v", prev)
	}
	grown, _ := applyProgressVerdict(&prev, verdictOf(true, append(first, oc("new", OutcomeTodo, 2))...), true, progressNow)
	if len(grown.Outcomes) != progressMaxOutcomes+1 || grown.Stage != ProgressBack {
		t.Fatalf("a full checklist dropped the new ask: %#v", grown)
	}
}

func TestBadOutcomesAreDroppedAndBounded(t *testing.T) {
	var many []ProgressOutcome
	for i := 0; i < 14; i++ {
		many = append(many, oc(string(rune('a'+i)), OutcomeTodo, 9))
	}
	many = append(many, ProgressOutcome{ID: "", State: OutcomeDone, Weight: 1}, ProgressOutcome{ID: "x", State: "finished", Weight: 1})
	got, _ := applyProgressVerdict(nil, verdictOf(true, many...), false, progressNow)
	if len(got.Outcomes) != progressMaxOutcomes || got.Outcomes[0].Weight != 3 {
		t.Fatalf("bounds = %#v", got.Outcomes)
	}
}
