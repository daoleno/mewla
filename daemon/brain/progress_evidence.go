package brain

import (
	"strings"
	"time"
)

// ProgressEvidence is the evidence about a Work that can move the judge's
// answer: the Worker's turn, whether it ended, and its newest report. Seen is
// what the daemon has observed; Judged is what the last judgment saw. Both are
// kept on the Work, so a restart, which replays every Session's turn facts,
// finds Seen equal to Judged and asks the model nothing.
//
// The gate: a new turn (Brain or a person wrote to the Worker), a turn that
// ended, a report whose kind, phase or words differ from the last one, or
// Brain changing where the Work stands for the person (parked, closed, what it
// waits on, what it asks them). A heartbeat that repeats the last report, or a
// fact replayed after a restart, is not evidence.
type ProgressEvidence struct {
	Turn   string `json:"turn,omitempty"`
	Ended  bool   `json:"ended,omitempty"`
	Report string `json:"report,omitempty"`
	Brain  string `json:"brain,omitempty"`
}

func reportKey(report ProgressReport) string {
	return strings.Join([]string{report.Kind, report.Phase, report.Summary}, "\x1f")
}

// noteProgressEvidence folds one observation into the Work's Seen evidence and
// keeps the report for the judge when it says something new. due says Seen now
// differs from what was last judged. It is presentation state: the Work's
// revision does not move.
func (s *Store) noteProgressEvidence(workID, turnID string, turnEnd bool, report *ProgressReport) (due bool, err error) {
	err = s.updateWorkProgressLocked(workID, func(item *Work) (*WorkEvent, bool) {
		if item.Status == WorkDone || item.Status == WorkCancelled {
			return nil, false
		}
		progress := WorkProgress{}
		if item.Progress != nil {
			progress = *cloneWorkProgress(item.Progress)
		}
		seen := ProgressEvidence{}
		if progress.Seen != nil {
			seen = *progress.Seen
		}
		if turnID != "" && turnID != seen.Turn {
			seen = ProgressEvidence{Turn: turnID, Report: seen.Report, Brain: seen.Brain}
		}
		if turnEnd {
			seen.Ended = true
		}
		if report != nil {
			key := reportKey(*report)
			if key != seen.Report {
				seen.Report = key
				progress.Reports = append(progress.Reports, *report)
				if len(progress.Reports) > progressReportsMax {
					progress.Reports = progress.Reports[len(progress.Reports)-progressReportsMax:]
				}
			}
		}
		progress.Seen = &seen
		item.Progress = &progress
		judged := ProgressEvidence{}
		if progress.Judged != nil {
			judged = *progress.Judged
		}
		due = seen != judged
		return nil, false
	})
	return due, err
}

// brainKey is where Brain has put the Work for the person: closed, parked or
// active, what it waits on and what it asks them. A Work moving between
// running and Brain's review is not a change the person reads.
func brainKey(item Work) string {
	stand := "active"
	switch item.Status {
	case WorkDone, WorkCancelled:
		stand = "done"
	case WorkWaiting:
		stand = "waiting"
	}
	return strings.Join([]string{stand, strings.TrimSpace(item.WaitFor), strings.TrimSpace(item.Question)}, "\x1f")
}

// noteBrainEvidence folds Brain's current disposition of the Work into its
// Seen evidence. due says Seen now differs from what was last judged. A
// closed Work takes it too, once, so its row says how it ended; a cancelled
// one leaves the list and takes nothing.
func (s *Store) noteBrainEvidence(workID, key string) (due bool, err error) {
	err = s.updateWorkProgressLocked(workID, func(item *Work) (*WorkEvent, bool) {
		if item.Status == WorkCancelled {
			return nil, false
		}
		progress := WorkProgress{}
		if item.Progress != nil {
			progress = *cloneWorkProgress(item.Progress)
		}
		seen := ProgressEvidence{}
		if progress.Seen != nil {
			seen = *progress.Seen
		}
		seen.Brain = key
		progress.Seen = &seen
		item.Progress = &progress
		judged := ProgressEvidence{}
		if progress.Judged != nil {
			judged = *progress.Judged
		}
		due = seen != judged
		return nil, false
	})
	return due, err
}

// markProgressJudged records that a judgment saw the evidence it was given,
// when, and what it cost: the gate and the day's spend survive a restart.
func markProgressJudged(progress *WorkProgress, seen *ProgressEvidence, now time.Time, usd float64) {
	if seen != nil {
		judged := *seen
		progress.Judged = &judged
	}
	progress.Runs = append(runsWithinDay(append([]ProgressRun(nil), progress.Runs...), now), ProgressRun{At: now, USD: usd})
}
