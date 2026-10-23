package watch

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Judgment state lives beside the append-only signal log: annotations
// (placements, judged authors and which signals judgment has seen) and the
// run log with each run's token cost. Judged problems share the problem
// registry with rule problems under RuleJudgment.

const (
	annotationsFile = "annotations.json"
	runsFile        = "judge-runs.jsonl"

	// RuleJudgment marks problems raised by the agent pass.
	RuleJudgment = "judgment"

	maxJudgedEvidence = 200
)

// JudgeRun records one judgment run.
type JudgeRun struct {
	ID              string    `json:"id"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
	Executor        string    `json:"executor"`
	Model           string    `json:"model"`
	Signals         int       `json:"signals"`
	PromptChars     int       `json:"prompt_chars"`
	From            time.Time `json:"from,omitzero"`
	Through         time.Time `json:"through,omitzero"`
	Placed          int       `json:"placed"`
	Authors         int       `json:"authors"`
	Verdicts        int       `json:"verdicts,omitempty"`
	ProblemsNew     int       `json:"problems_new"`
	ProblemsUpdated int       `json:"problems_updated"`
	Dropped         int       `json:"dropped,omitempty"`
	Usage           Usage     `json:"usage"`
	Error           string    `json:"error,omitempty"`
}

// Usage is one run's token use as the executor reported it.
type Usage struct {
	InputTokens         int     `json:"input_tokens"`
	CacheCreationTokens int     `json:"cache_creation_input_tokens"`
	CacheReadTokens     int     `json:"cache_read_input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CostUSD             float64 `json:"cost_usd"`
	Models              string  `json:"models,omitempty"`
	// Executor is the agent that ran the judgment (claude, codex, pi).
	Executor   string `json:"executor,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`
}

func (s *Store) applyAnnotationLocked(id string, annotation Annotation) {
	index, ok := s.byID[id]
	if !ok {
		return
	}
	signal := &s.signals[index]
	if signal.ProjectID == "" && annotation.ProjectID != "" {
		signal.ProjectID, signal.PlacedBy, signal.PlaceReason = annotation.ProjectID, annotation.PlacedBy, annotation.PlaceReason
	}
	if annotation.Author != "" {
		signal.Author, signal.AuthorReason = annotation.Author, annotation.AuthorReason
	}
	if annotation.Notification != nil {
		state := *annotation.Notification
		state.Responses = append([]NotificationResponse(nil), state.Responses...)
		signal.Notification = &state
	}
}

// Annotate merges annotations into the overlay and saves it. A placement
// never overrides a project the collector already assigned.
func (s *Store) Annotate(changes map[string]Annotation) error {
	if len(changes) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, change := range changes {
		if _, ok := s.byID[id]; !ok {
			continue
		}
		current := s.annotations[id]
		if change.ProjectID != "" && current.ProjectID == "" && s.signals[s.byID[id]].ProjectID == "" {
			current.ProjectID, current.PlacedBy, current.PlaceReason = change.ProjectID, change.PlacedBy, change.PlaceReason
		}
		if change.Author != "" {
			current.Author, current.AuthorReason = change.Author, change.AuthorReason
		}
		if change.Run != "" {
			current.Run = change.Run
		}
		if change.Notification != nil {
			current.Notification = mergeNotification(current.Notification, change.Notification)
		}
		s.annotations[id] = current
		s.applyAnnotationLocked(id, current)
	}
	return writeJSON(filepath.Join(s.dir, annotationsFile), s.annotations)
}

// Unjudged returns the signals judgment has not seen, oldest first.
func (s *Store) Unjudged() []Signal {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Signal
	for _, signal := range s.signals {
		annotation := s.annotations[signal.ID]
		if annotation.Run != "" {
			continue
		}
		// A notification is judged once its outcome is known.
		if signal.Source == SourceNotification && (annotation.Notification == nil || annotation.Notification.SettledAt.IsZero()) {
			continue
		}
		out = append(out, signal)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// Signal returns one signal by ID.
func (s *Store) Signal(id string) (Signal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, ok := s.byID[id]
	if !ok {
		return Signal{}, false
	}
	return s.signals[index], true
}

// Unplaced returns signals without a project.
func (s *Store) Unplaced() []Signal {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Signal
	for _, signal := range s.signals {
		if signal.ProjectID == "" {
			out = append(out, signal)
		}
	}
	return out
}

// judgedProblemID gives a new judged problem an ID that a re-run of the same
// batch reproduces, so a crash between saving problems and the overlay cannot
// duplicate it.
func judgedProblemID(projectID string, evidence []string) string {
	sorted := append([]string(nil), evidence...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(projectID + "\x00" + strings.Join(sorted, ",")))
	return "j_" + hex.EncodeToString(sum[:])[:12]
}

// JudgedProblem is one problem as a judgment run reports it, with evidence
// already resolved to signal IDs.
type JudgedProblem struct {
	ID          string
	ProjectID   string
	Title       string
	Evidence    []string
	Merges      []string
	Cost        Cost
	Reason      string
	Suggestion  string
	LooksFixed  bool
	Run         string
	EvidenceAt  []time.Time
	EvidenceSrc []string
}

// RecordJudged adds or updates judged problems and links merged rule
// problems. It returns how many problems were new and how many changed.
func (s *Store) RecordJudged(judged []JudgedProblem) (created, updated int, err error) {
	if len(judged) == 0 {
		return 0, 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, next := range judged {
		id := next.ID
		existing := s.problems[id]
		if existing == nil {
			id = judgedProblemID(next.ProjectID, next.Evidence)
			existing = s.problems[id]
		}
		if existing == nil {
			existing = &Problem{ID: id, ProjectID: next.ProjectID, Rule: RuleJudgment, Subject: id, Status: StatusNew, Active: true}
			s.problems[id] = existing
			created++
		} else {
			updated++
		}
		existing.Title = next.Title
		if next.ProjectID != "" {
			existing.ProjectID = next.ProjectID
		}
		existing.Reason = next.Reason
		if next.Suggestion != "" {
			existing.Suggestion = next.Suggestion
		}
		cost := next.Cost
		existing.Cost = &cost
		existing.Run = next.Run
		seen := map[string]bool{}
		for _, evidence := range existing.Evidence {
			seen[evidence] = true
		}
		grew := false
		for _, evidence := range next.Evidence {
			if !seen[evidence] {
				seen[evidence] = true
				existing.Evidence = append(existing.Evidence, evidence)
				grew = true
			}
		}
		if len(existing.Evidence) > maxJudgedEvidence {
			existing.Evidence = existing.Evidence[len(existing.Evidence)-maxJudgedEvidence:]
		}
		existing.Count = len(existing.Evidence)
		for _, at := range next.EvidenceAt {
			if existing.FirstSeen.IsZero() || at.Before(existing.FirstSeen) {
				existing.FirstSeen = at
			}
			if at.After(existing.LastSeen) {
				existing.LastSeen = at
			}
		}
		existing.Sources = mergeSorted(existing.Sources, next.EvidenceSrc)
		switch {
		case next.LooksFixed && existing.Status != StatusDismissed:
			existing.Status, existing.FixedReason = StatusFixed, next.Reason
		case existing.Status == StatusFixed && grew:
			existing.Status, existing.FixedReason = StatusRecurred, ""
		}
		existing.Active = existing.Status != StatusFixed && existing.Status != StatusDismissed
		for _, merged := range next.Merges {
			candidate := s.problems[merged]
			if candidate == nil || candidate.Rule == RuleJudgment {
				continue
			}
			candidate.MergedInto = id
			existing.MergedFrom = mergeSorted(existing.MergedFrom, []string{merged})
		}
		existing.UpdatedAt = now
	}
	return created, updated, s.saveProblemsLocked()
}

func mergeSorted(values, more []string) []string {
	set := map[string]bool{}
	for _, value := range append(append([]string(nil), values...), more...) {
		if value != "" {
			set[value] = true
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (s *Store) saveProblemsLocked() error {
	list := make([]*Problem, 0, len(s.problems))
	for _, problem := range s.problems {
		list = append(list, problem)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return writeJSON(filepath.Join(s.dir, problemsFile), list)
}

// LabelProblem records the user's verdict. real confirms the problem, not
// dismisses it, later keeps it as it is.
//
// rest is how long a "later" hides the problem; zero means DefaultLaterFor.
func (s *Store) LabelProblem(id, value, note, by string, rest time.Duration) (Problem, error) {
	switch value {
	case LabelReal, LabelNot, LabelLater:
	default:
		return Problem{}, fmt.Errorf("label must be %s, %s or %s", LabelReal, LabelNot, LabelLater)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	problem := s.problems[strings.TrimSpace(id)]
	if problem == nil {
		return Problem{}, fmt.Errorf("problem %q not found", id)
	}
	if by != LabelByBrain {
		by = LabelByUser
	}
	if rest != 0 && value != LabelLater {
		return Problem{}, errors.New("a rest only goes with later")
	}
	problem.Label = &Label{Value: value, Note: strings.TrimSpace(Redact(note)), At: s.now(), By: by}
	if value == LabelLater && rest > 0 {
		problem.Label.Until = problem.Label.At.Add(rest)
	}
	switch value {
	case LabelReal:
		if problem.Status == StatusNew || problem.Status == StatusDismissed {
			problem.Status = StatusConfirmed
		}
	case LabelNot:
		problem.Status = StatusDismissed
	}
	problem.Active = problem.Status != StatusFixed && problem.Status != StatusDismissed
	problem.UpdatedAt = s.now()
	copied := *problem
	return copied, s.saveProblemsLocked()
}

// AppendRun adds a run to the run log.
func (s *Store) AppendRun(run JudgeRun) error {
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(filepath.Join(s.dir, runsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = file.Write(append(raw, '\n'))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Runs returns the run log, oldest first.
func (s *Store) Runs() ([]JudgeRun, error) {
	file, err := os.Open(filepath.Join(s.dir, runsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var runs []JudgeRun
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var run JudgeRun
		if json.Unmarshal(scanner.Bytes(), &run) == nil && run.ID != "" {
			runs = append(runs, run)
		}
	}
	return runs, scanner.Err()
}
