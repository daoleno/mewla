package brain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// A Work's progress is judged by a model, not reported by its Worker. The
// judge keeps a short weighted checklist of the Work's outcomes, drawn once
// from the brief's acceptance criteria, and marks each outcome todo, doing or
// done from evidence it can see (a commit, a passing run, a file written, a
// posted review). The percentage is computed here from those marks, never
// stated by the model, and the shown value is stable by construction:
//
//   - it only moves forward, unless the judge names an outcome that was
//     reopened (a review sent it back, a later check failed) or Brain added to
//     the Work; then it may go down, and Back says why;
//   - it never reaches 100: when every outcome is done the Work is in its last
//     stretch (the Worker reporting, Brain reading), shown as that state;
//   - a Work with no finish line (an open watch or investigation) shows no
//     number at all. Whether a Work waits on a person is a fact the Work
//     already carries (its question, its waiting state), not a judgment: the
//     client hides the number then.
//
// A replay of 78 finished Works chose this design. A model asked for a bare
// percentage went down without a reason in 11 % of steps; the checklist marks
// in 0.5 %. The computed value ranks moments by how far along the Work really
// was about as well (Spearman 0.76 against the share of time spent; the
// Worker's self-reported phase scored 0.28), and is about 15 points off on
// average, so clients round it.

// Progress stages a client draws.
const (
	ProgressMoving     = "moving"     // a number that moves forward
	ProgressBack       = "back"       // went down after a reopen; Back says why
	ProgressLast       = "last"       // every outcome done; reporting or being read
	ProgressUnmeasured = "unmeasured" // no finish line: no number
)

// Outcome marks.
const (
	OutcomeTodo  = "todo"
	OutcomeDoing = "doing"
	OutcomeDone  = "done"
)

const (
	// progressMaxOutcomes bounds the first checklist; progressMaxGrown the
	// checklist after Brain added to the Work, so a long Work that keeps
	// growing still takes in what was asked last.
	progressMaxOutcomes = 10
	progressMaxGrown    = 24
	progressDoingCredit = 0.35
	progressCeiling     = 99
)

// ProgressOutcome is one thing that will be true when the Work is done.
type ProgressOutcome struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Weight int    `json:"weight"`
	State  string `json:"state"`
}

// WorkProgress is a Work's judged progress.
type WorkProgress struct {
	Stage string `json:"stage"`
	// Percent is the shown value, 0-99. It is meaningful for moving and back;
	// the other stages keep it only so a later number never falls behind it.
	Percent  int               `json:"percent"`
	Outcomes []ProgressOutcome `json:"outcomes,omitempty"`
	// Back is why the value last went down, in a few words, while Stage is back.
	Back      string    `json:"back,omitempty"`
	JudgedAt  time.Time `json:"judged_at"`
	Judgments int       `json:"judgments"`
	// Reports are the Worker's last progress reports, kept for the judge.
	// They stay in the daemon: view drops them.
	Reports []ProgressReport `json:"reports,omitempty"`
}

// view is the progress as clients read it, or nil before the first judgment.
func (p *WorkProgress) view() *WorkProgress {
	if p == nil || p.Stage == "" {
		return nil
	}
	out := *p
	out.Outcomes = append([]ProgressOutcome(nil), p.Outcomes...)
	out.Reports = nil
	return &out
}

func cloneWorkProgress(p *WorkProgress) *WorkProgress {
	if p == nil {
		return nil
	}
	out := *p
	out.Outcomes = append([]ProgressOutcome(nil), p.Outcomes...)
	out.Reports = append([]ProgressReport(nil), p.Reports...)
	return &out
}

// ShowsNumber reports whether the stage carries a percentage.
func (p WorkProgress) ShowsNumber() bool {
	return p.Stage == ProgressMoving || p.Stage == ProgressBack
}

// progressVerdict is the judge's answer.
type progressVerdict struct {
	Measurable bool              `json:"measurable"`
	Outcomes   []ProgressOutcome `json:"outcomes"`
	Reopened   []struct {
		ID  string `json:"id"`
		Why string `json:"why"`
	} `json:"reopened"`
	Reason string `json:"reason"`
}

func parseProgressVerdict(raw []byte) (progressVerdict, error) {
	var verdict progressVerdict
	if err := json.Unmarshal(raw, &verdict); err != nil {
		return progressVerdict{}, fmt.Errorf("progress judge answer: %w", err)
	}
	return verdict, nil
}

func validOutcomeState(state string) bool {
	return state == OutcomeTodo || state == OutcomeDoing || state == OutcomeDone
}

// checklistPercent is the weighted share of the outcomes done, with partial
// credit for those under way.
func checklistPercent(outcomes []ProgressOutcome) int {
	total, got := 0.0, 0.0
	for _, outcome := range outcomes {
		weight := float64(outcome.Weight)
		total += weight
		switch outcome.State {
		case OutcomeDone:
			got += weight
		case OutcomeDoing:
			got += weight * progressDoingCredit
		}
	}
	if total == 0 {
		return 0
	}
	return int(100*got/total + 0.5)
}

// applyProgressVerdict folds one judgment into the previous progress. scopeAdded
// says Brain sent the Worker a new message about the Work since the last
// judgment: only then may the checklist grow. It returns the new progress and
// whether it changed what a person sees.
func applyProgressVerdict(prev *WorkProgress, verdict progressVerdict, scopeAdded bool, now time.Time) (WorkProgress, bool) {
	next := WorkProgress{JudgedAt: now.UTC()}
	if prev != nil {
		next.Percent, next.Judgments = prev.Percent, prev.Judgments
		next.Outcomes = append([]ProgressOutcome(nil), prev.Outcomes...)
	}
	next.Judgments++

	reopenWhy := map[string]string{}
	for _, reopened := range verdict.Reopened {
		if id := strings.TrimSpace(reopened.ID); id != "" {
			reopenWhy[id] = judgeClip(reopened.Why, 120)
		}
	}
	marks := map[string]ProgressOutcome{}
	var order []string
	for _, outcome := range verdict.Outcomes {
		outcome.ID = strings.TrimSpace(outcome.ID)
		if outcome.ID == "" || !validOutcomeState(outcome.State) {
			continue
		}
		if _, seen := marks[outcome.ID]; seen {
			continue
		}
		marks[outcome.ID] = outcome
		order = append(order, outcome.ID)
	}

	back, grew := "", false
	if len(next.Outcomes) == 0 {
		// The first checklist is the judge's, within bounds.
		for _, id := range order {
			if len(next.Outcomes) == progressMaxOutcomes {
				break
			}
			outcome := marks[id]
			outcome.Text = judgeClip(outcome.Text, 140)
			outcome.Weight = min(max(outcome.Weight, 1), 3)
			next.Outcomes = append(next.Outcomes, outcome)
		}
	} else {
		known := map[string]bool{}
		for index := range next.Outcomes {
			outcome := &next.Outcomes[index]
			known[outcome.ID] = true
			mark, ok := marks[outcome.ID]
			if !ok {
				continue // the judge dropped it: it keeps its mark
			}
			if outcome.State == OutcomeDone && mark.State != OutcomeDone {
				why, named := reopenWhy[outcome.ID]
				if !named {
					continue // done stays done unless the judge says it was reopened
				}
				back = firstNonEmpty(back, why, "Reopened")
			}
			outcome.State = mark.State
		}
		if scopeAdded {
			for _, id := range order {
				if known[id] || len(next.Outcomes) == progressMaxGrown {
					continue
				}
				outcome := marks[id]
				outcome.Text = judgeClip(outcome.Text, 140)
				outcome.Weight = min(max(outcome.Weight, 1), 3)
				next.Outcomes = append(next.Outcomes, outcome)
				grew = true
			}
		}
	}

	raw := min(checklistPercent(next.Outcomes), progressCeiling)
	allDone := len(next.Outcomes) > 0
	for _, outcome := range next.Outcomes {
		allDone = allDone && outcome.State == OutcomeDone
	}
	switch {
	case back != "" || grew:
		next.Percent = raw
		if prev != nil && raw < prev.Percent {
			next.Stage = ProgressBack
			next.Back = firstNonEmpty(back, "More was asked")
		} else {
			next.Stage = ProgressMoving
		}
	default:
		next.Percent = max(next.Percent, raw)
		next.Stage = ProgressMoving
		if prev != nil && prev.Stage == ProgressBack && next.Percent <= prev.Percent {
			next.Stage, next.Back = ProgressBack, prev.Back // stays amber until it moves again
		}
	}
	switch {
	case !verdict.Measurable:
		next.Stage, next.Back = ProgressUnmeasured, ""
	case allDone:
		next.Stage, next.Back = ProgressLast, ""
	}
	changed := prev == nil || prev.Stage != next.Stage || prev.Back != next.Back ||
		(next.ShowsNumber() && prev.Percent != next.Percent) || !sameOutcomes(prev.Outcomes, next.Outcomes)
	return next, changed
}

func sameOutcomes(a, b []ProgressOutcome) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
