package brain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
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
	// Seen and Judged are the evidence observed and the evidence last judged
	// (progress_evidence.go); Runs the judgments of the last day and what
	// each cost. They keep the judge's gate across restarts and say what it
	// spent (Brain context), and stay in the daemon.
	Seen   *ProgressEvidence `json:"seen,omitempty"`
	Judged *ProgressEvidence `json:"judged,omitempty"`
	Runs   []ProgressRun     `json:"runs,omitempty"`
}

// ProgressRun is one judgment: when, and what the model run cost in dollars
// when the executor reports it.
type ProgressRun struct {
	At  time.Time `json:"at"`
	USD float64   `json:"usd,omitempty"`
}

// view is the progress as clients read it, or nil before the first judgment.
func (p *WorkProgress) view() *WorkProgress {
	if p == nil || p.Stage == "" {
		return nil
	}
	out := *p
	out.Outcomes = append([]ProgressOutcome(nil), p.Outcomes...)
	out.Reports, out.Seen, out.Judged, out.Runs = nil, nil, nil, nil
	return &out
}

func cloneWorkProgress(p *WorkProgress) *WorkProgress {
	if p == nil {
		return nil
	}
	out := *p
	out.Outcomes = append([]ProgressOutcome(nil), p.Outcomes...)
	out.Reports = append([]ProgressReport(nil), p.Reports...)
	out.Runs = append([]ProgressRun(nil), p.Runs...)
	if p.Seen != nil {
		seen := *p.Seen
		out.Seen = &seen
	}
	if p.Judged != nil {
		judged := *p.Judged
		out.Judged = &judged
	}
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
	// Dropped names outcomes not yet done that a later message from Brain
	// replaced or withdrew; taken only when Brain changed the Work.
	Dropped []struct {
		ID  string `json:"id"`
		Why string `json:"why"`
	} `json:"dropped"`
	Reason string `json:"reason"`
	// How the Work reads in the person's list (shown.go).
	Title   string `json:"title"`
	Line    string `json:"line"`
	Next    string `json:"next"`
	Project string `json:"project"`
	// Asked numbers the person's messages, as listed, that asked for the Work.
	Asked []int `json:"asked"`
	// language is the person's (personLanguage): an outcome written in
	// another is said again in theirs, once.
	language string
}

// shown is how the Work reads, given the person's messages as the judge saw
// them numbered.
func (v progressVerdict) shown(person []personMessage) shownVerdict {
	out := shownVerdict{Title: v.Title, Line: v.Line, Next: v.Next, Project: v.Project}
	for _, number := range v.Asked {
		if number >= 1 && number <= len(person) {
			out.Asked = append(out.Asked, person[number-1])
		}
	}
	return out
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
	dropped := map[string]bool{}
	if scopeAdded && len(next.Outcomes) > 0 {
		next.Outcomes = dropOutcomes(next.Outcomes, verdict)
		for _, outcome := range verdict.Dropped {
			dropped[strings.TrimSpace(outcome.ID)] = true
		}
	}
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
			if text := judgeClip(mark.Text, 140); verdict.language != "" && !inLanguage(outcome.Text, verdict.language) && inLanguage(text, verdict.language) {
				outcome.Text = text // said once in the person's language; kept as is after
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
				if known[id] || dropped[id] || len(next.Outcomes) == progressMaxGrown {
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

// dropOutcomes removes the outcomes the judge says a later message from Brain
// replaced or withdrew. A done outcome stays (dropping it would read as the
// Work going back), and at least one outcome is left.
func dropOutcomes(outcomes []ProgressOutcome, verdict progressVerdict) []ProgressOutcome {
	drop := map[string]bool{}
	for _, dropped := range verdict.Dropped {
		drop[strings.TrimSpace(dropped.ID)] = true
	}
	if len(drop) == 0 {
		return outcomes
	}
	kept := make([]ProgressOutcome, 0, len(outcomes))
	for _, outcome := range outcomes {
		if !drop[outcome.ID] || outcome.State == OutcomeDone {
			kept = append(kept, outcome)
		}
	}
	if len(kept) == 0 {
		return outcomes
	}
	return kept
}

// inLanguage says whether text is written in language, as personLanguage
// names it; for a language it does not name it answers true.
func inLanguage(text, language string) bool {
	var han, kana, hangul int
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			kana++
		case unicode.Is(unicode.Hangul, r):
			hangul++
		case unicode.Is(unicode.Han, r):
			han++
		}
	}
	switch language {
	case "Japanese":
		return kana > 0
	case "Korean":
		return hangul >= 2
	case "Chinese":
		return han >= 2
	}
	return true
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
