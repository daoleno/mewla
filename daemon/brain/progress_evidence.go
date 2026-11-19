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
// ended, or a report whose kind, phase or words differ from the last one. A
// heartbeat that repeats the last report, or a fact replayed after a restart,
// is not evidence.
type ProgressEvidence struct {
	Turn   string `json:"turn,omitempty"`
	Ended  bool   `json:"ended,omitempty"`
	Report string `json:"report,omitempty"`
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
			seen = ProgressEvidence{Turn: turnID, Report: seen.Report}
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

// markProgressJudged records that a judgment saw the evidence it was given,
// and when, so the day's limits survive a restart.
func markProgressJudged(progress *WorkProgress, seen *ProgressEvidence, now time.Time) {
	if seen != nil {
		judged := *seen
		progress.Judged = &judged
	}
	progress.Recent = append(withinDay(append([]time.Time(nil), progress.Recent...), now), now)
}
