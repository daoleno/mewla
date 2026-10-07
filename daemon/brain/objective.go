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
	file := objectiveFile{ThreadID: threadID, Title: title, SetAt: time.Now().UTC()}
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
	for _, event := range database.BrainWorkEvents {
		if isProjectedWorkResultEvent(event.Kind) {
			hasResult[event.WorkID] = true
		}
	}
	objective := Objective{Title: file.Title, SetAt: file.SetAt}
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
	}
	return objective, nil
}

// isObjectiveChildWork leaves out Work the daemon creates on its own:
// Calendar occurrences and machine resource telemetry.
func isObjectiveChildWork(workID string) bool {
	return !strings.HasPrefix(workID, calendarWorkPrefix) &&
		!strings.HasPrefix(workID, resourcePressureWorkPrefix)
}
