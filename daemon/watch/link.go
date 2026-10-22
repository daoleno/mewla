package watch

import (
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

// A problem is tied to Work in two ways. Origin links (subject, evidence,
// mention) say the problem happened in or is about that Work. Fix links
// (context, brain, user) say the Work was opened to deal with it. Both are
// deterministic: ids and refs, never a model. Brain adds the rest while
// triaging, with `mewla watch problem link`.
//
// A Work makes a problem "fixing" while somebody is on it, and "fixed" once
// Brain accepted it after the problem's last evidence. A Work that closed
// before the latest evidence never counts as the fix, so a problem that
// returns reopens as "recurred".

// WatchContextRef is the context_ref of Work opened for a problem.
func WatchContextRef(problemID string) string { return "watch:" + problemID }

func isFixLink(by string) bool {
	return by == LinkContext || by == LinkBrain || by == LinkUser
}

// workHandled reports whether somebody is on the Work. Origin Work only
// counts while it runs: Work waiting on the user is the problem itself.
func workHandled(work brain.Work, by string) bool {
	switch work.Status {
	case brain.WorkRunning:
		return true
	case brain.WorkOpen, brain.WorkWaiting, brain.WorkNeedsInput:
		return isFixLink(by)
	}
	return false
}

// LinkWork records that a problem is tied to a piece of Work. It is
// idempotent; a stronger (fix) link replaces an origin link.
func (s *Store) LinkWork(problemID, workID, by string) (Problem, error) {
	problemID, workID = strings.TrimSpace(problemID), strings.TrimSpace(workID)
	if workID == "" {
		return Problem{}, fmt.Errorf("a work id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	problem := s.problems[problemID]
	if problem == nil {
		return Problem{}, fmt.Errorf("problem %q not found", problemID)
	}
	if addLink(problem, workID, by, s.now()) {
		problem.UpdatedAt = s.now()
		if err := s.saveProblemsLocked(); err != nil {
			return Problem{}, err
		}
	}
	return cloneProblem(problem), nil
}

// UnlinkWork removes a link Brain or the user made by mistake.
func (s *Store) UnlinkWork(problemID, workID string) (Problem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	problem := s.problems[strings.TrimSpace(problemID)]
	if problem == nil {
		return Problem{}, fmt.Errorf("problem %q not found", problemID)
	}
	kept := problem.Works[:0:0]
	for _, link := range problem.Works {
		if link.WorkID != strings.TrimSpace(workID) {
			kept = append(kept, link)
		}
	}
	problem.Works = kept
	problem.UpdatedAt = s.now()
	return cloneProblem(problem), s.saveProblemsLocked()
}

func addLink(problem *Problem, workID, by string, at time.Time) bool {
	for index, link := range problem.Works {
		if link.WorkID != workID {
			continue
		}
		if isFixLink(by) && !isFixLink(link.By) {
			problem.Works[index].By = by
			return true
		}
		return false
	}
	problem.Works = append(problem.Works, WorkLink{WorkID: workID, By: by, At: at})
	return true
}

// ApplyLinks derives links from ids the problems already carry, then moves
// each linked problem between new, fixing and fixed. It returns the number
// of problems that changed.
func (s *Store) ApplyLinks(works []brain.Work) (int, error) {
	if len(works) == 0 {
		return 0, nil
	}
	byID := make(map[string]brain.Work, len(works))
	for _, work := range works {
		byID[work.ID] = work
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	changed := 0
	for _, problem := range s.problems {
		if problem.MergedInto != "" {
			continue
		}
		touched := s.deriveLinksLocked(problem, works, byID, now)
		if applyWorkState(problem, byID, now) {
			touched = true
		}
		if touched {
			problem.UpdatedAt = now
			changed++
		}
	}
	if changed == 0 {
		return 0, nil
	}
	return changed, s.saveProblemsLocked()
}

func (s *Store) deriveLinksLocked(problem *Problem, works []brain.Work, byID map[string]brain.Work, at time.Time) bool {
	touched := false
	add := func(workID, by string) {
		if _, ok := byID[workID]; ok && addLink(problem, workID, by, at) {
			touched = true
		}
	}
	if problem.Rule == RuleWorkerStalls {
		add(problem.Subject, LinkSubject)
	}
	for _, id := range problem.Evidence {
		if index, ok := s.byID[id]; ok {
			add(s.signals[index].Refs["work"], LinkEvidence)
		}
	}
	text := problem.Title + "\n" + problem.Detail + "\n" + problem.Reason
	for _, work := range works {
		switch {
		case work.ContextRef == WatchContextRef(problem.ID) || strings.Contains(work.Objective, problem.ID):
			add(work.ID, LinkContext)
		case len(work.ID) >= 8 && strings.Contains(text, work.ID):
			add(work.ID, LinkMention)
		}
	}
	return touched
}

// applyWorkState moves a problem by the state of its linked Work. A user's
// dismissal always stands.
func applyWorkState(problem *Problem, byID map[string]brain.Work, now time.Time) bool {
	if problem.Status == StatusDismissed {
		return false
	}
	handled := false
	var accepted time.Time
	for _, link := range problem.Works {
		work, ok := byID[link.WorkID]
		if !ok {
			continue
		}
		if workHandled(work, link.By) {
			handled = true
		}
		if work.Status == brain.WorkDone && work.UpdatedAt.After(problem.LastSeen) && work.UpdatedAt.After(accepted) {
			accepted = work.UpdatedAt
		}
	}
	before := problem.Status
	switch {
	case handled:
		problem.Status = StatusFixing
	case !accepted.IsZero():
		if problem.Status != StatusFixed {
			problem.Status = StatusFixed
			problem.FixedAt = accepted
			problem.FixedReason = "The Work for it was accepted."
		}
	case problem.Status == StatusFixing:
		problem.Status = StatusNew
		if problem.Label != nil && problem.Label.Value == LabelReal {
			problem.Status = StatusConfirmed
		}
	case problem.Status == StatusFixed && !problem.FixedAt.IsZero() && problem.LastSeen.After(problem.FixedAt):
		problem.Status, problem.FixedReason = StatusRecurred, ""
	}
	problem.Active = problem.Status != StatusFixed && problem.Status != StatusDismissed
	return problem.Status != before
}
