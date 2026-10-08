package brain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Objective is what Brain is currently working toward in the active chat
// thread, with how much of the Work it handed off for it has come back. The
// App shows it under the Brain title ("Ship v1.4 · 2 of 5 back").
//
// Brain declares it (mewla brain objective set); nothing infers it. It belongs
// to one chat thread, so a new chat starts without one. Child Work is the
// thread's Work created since the objective was set, excluding Calendar
// occurrences and resource telemetry; Work counts as back once it has a
// result or is done, and cancelled Work no longer counts.
type Objective struct {
	Title string    `json:"title"`
	SetAt time.Time `json:"set_at"`
	Total int       `json:"total"`
	Back  int       `json:"back"`
	// Done counts child Work closed as done; LastActivity is the newest of
	// SetAt, a child handed off and a result coming back (not UpdatedAt,
	// which every lifecycle projection stamps). They decide when the goal
	// line stops being current (objectiveCurrent) and are not on the wire.
	Done         int       `json:"-"`
	LastActivity time.Time `json:"-"`
}

// The apps show an objective only while it is current, so a goal never
// lingers over finished or abandoned Work. Brain's own context still sees it
// (CurrentObjective) and can clear or restate it.
const (
	// Everything is back and nothing has moved since: Brain has had time to
	// read the results and reply.
	objectiveSettleAfter = 30 * time.Minute
	// Nothing in the objective has moved for this long.
	objectiveStaleAfter = 12 * time.Hour
)

// objectiveCurrent reports whether the apps should still show the objective.
func objectiveCurrent(objective Objective, now time.Time) bool {
	quiet := now.Sub(objective.LastActivity)
	if objective.Total > 0 && objective.Done >= objective.Total {
		return false
	}
	if objective.Total > 0 && objective.Back >= objective.Total && quiet >= objectiveSettleAfter {
		return false
	}
	return quiet < objectiveStaleAfter
}

// objectiveExpiresAt is when a current objective stops being current if
// nothing else happens.
func objectiveExpiresAt(objective Objective) time.Time {
	if objective.Total > 0 && objective.Back >= objective.Total {
		return objective.LastActivity.Add(objectiveSettleAfter)
	}
	return objective.LastActivity.Add(objectiveStaleAfter)
}

const maxObjectiveTitleRunes = 200

// calendarWorkPrefix starts every Calendar occurrence's Work ID (calendarWorkID).
const calendarWorkPrefix = "calendar-"

// objectiveFile is its own state file: chat_state.json decodes strictly, so
// adding a field there would break a daemon rolled back to an older build.
type objectiveFile struct {
	ThreadID string    `json:"thread_id"`
	Title    string    `json:"title"`
	SetAt    time.Time `json:"set_at"`
}

func (s *Store) objectivePath() string {
	return filepath.Join(s.statePath(), "objective.json")
}

// SetObjective declares the current chat thread's objective. Setting the same
// title again keeps its start time, so re-declaring does not reset progress.
func (s *Store) SetObjective(title string) (Objective, error) {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return Objective{}, fmt.Errorf("objective title is required")
	}
	if len([]rune(title)) > maxObjectiveTitleRunes {
		return Objective{}, fmt.Errorf("objective title is longer than %d characters", maxObjectiveTitleRunes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readChatStateLocked("")
	if err != nil {
		return Objective{}, err
	}
	threadID := strings.TrimSpace(state.ThreadID)
	if threadID == "" {
		return Objective{}, fmt.Errorf("Brain has no current chat thread")
	}
	file := objectiveFile{ThreadID: threadID, Title: title, SetAt: s.nowUTC()}
	if previous, ok, err := s.readObjectiveFileLocked(); err != nil {
		return Objective{}, err
	} else if ok && previous.ThreadID == threadID && previous.Title == title {
		file.SetAt = previous.SetAt
	}
	if err := writeJSONFile(s.objectivePath(), file); err != nil {
		return Objective{}, err
	}
	// Clients refresh brain_snapshot on Work changes; the goal line rides it.
	s.broadcastWorkChange("")
	return s.objectiveProgressLocked(file)
}

// ClearObjective removes the objective; the goal line disappears.
func (s *Store) ClearObjective() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.objectivePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.broadcastWorkChange("")
	return nil
}

// DisplayedObjective is CurrentObjective while it is still current
// (objectiveCurrent); the apps' goal line reads this.
func (s *Store) DisplayedObjective() (*Objective, error) {
	objective, err := s.CurrentObjective()
	if err != nil || objective == nil {
		return nil, err
	}
	now := s.nowUTC()
	if !objectiveCurrent(*objective, now) {
		return nil, nil
	}
	// Going quiet changes nothing, so nothing would push a fresh snapshot:
	// push one when the objective expires.
	s.armObjectiveExpiry(objectiveExpiresAt(*objective).Sub(now))
	return objective, nil
}

func (s *Store) armObjectiveExpiry(after time.Duration) {
	s.objectiveExpiryMu.Lock()
	defer s.objectiveExpiryMu.Unlock()
	if s.objectiveExpiry != nil {
		s.objectiveExpiry.Stop()
	}
	s.objectiveExpiry = time.AfterFunc(after+time.Second, func() { s.broadcastWorkChange("") })
}

// CurrentObjective returns the current chat thread's objective with live
// progress, or nil when the thread has none.
func (s *Store) CurrentObjective() (*Objective, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, ok, err := s.readObjectiveFileLocked()
	if err != nil || !ok {
		return nil, err
	}
	state, err := s.readChatStateLocked("")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(state.ThreadID) != file.ThreadID {
		return nil, nil
	}
	objective, err := s.objectiveProgressLocked(file)
	if err != nil {
		return nil, err
	}
	return &objective, nil
}

func (s *Store) readObjectiveFileLocked() (objectiveFile, bool, error) {
	raw, err := os.ReadFile(s.objectivePath())
	if errors.Is(err, os.ErrNotExist) {
		return objectiveFile{}, false, nil
	}
	if err != nil {
		return objectiveFile{}, false, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return objectiveFile{}, false, nil
	}
	var file objectiveFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return objectiveFile{}, false, fmt.Errorf("decode Brain objective: %w", err)
	}
	file.ThreadID = strings.TrimSpace(file.ThreadID)
	file.Title = strings.TrimSpace(file.Title)
	if file.ThreadID == "" || file.Title == "" {
		return objectiveFile{}, false, nil
	}
	return file, true, nil
}

func (s *Store) objectiveProgressLocked(file objectiveFile) (Objective, error) {
	database, err := s.loadPresentationLocked()
	if err != nil {
		return Objective{}, err
	}
	hasResult := map[string]bool{}
	lastResult := map[string]time.Time{}
	for _, event := range database.BrainWorkEvents {
		if isProjectedWorkResultEvent(event.Kind) {
			hasResult[event.WorkID] = true
			if event.CreatedAt.After(lastResult[event.WorkID]) {
				lastResult[event.WorkID] = event.CreatedAt
			}
		}
	}
	objective := Objective{Title: file.Title, SetAt: file.SetAt, LastActivity: file.SetAt}
	for _, item := range database.BrainWork {
		if strings.TrimSpace(item.SourceThreadID) != file.ThreadID ||
			item.CreatedAt.Before(file.SetAt) ||
			item.Status == WorkCancelled ||
			!isObjectiveChildWork(item.ID) {
			continue
		}
		objective.Total++
		if item.Status == WorkDone || hasResult[item.ID] {
			objective.Back++
		}
		if item.Status == WorkDone {
			objective.Done++
		}
		for _, at := range []time.Time{item.CreatedAt, lastResult[item.ID]} {
			if at.After(objective.LastActivity) {
				objective.LastActivity = at
			}
		}
	}
	return objective, nil
}

// isObjectiveChildWork leaves out Work the daemon creates on its own:
// Calendar occurrences and machine resource telemetry.
func isObjectiveChildWork(workID string) bool {
	return !strings.HasPrefix(workID, calendarWorkPrefix) &&
		!strings.HasPrefix(workID, resourcePressureWorkPrefix)
}
