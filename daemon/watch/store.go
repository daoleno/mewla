package watch

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// Store keeps Watch state under <state>/watch: the setting, inferred
// projects, the append-only signal log, per-source cursors, the problem
// registry and the session/turn → cwd index used to place Work in projects.
// The daemon is the only writer; the CLI goes through the control socket.
type Store struct {
	dir string
	now func() time.Time

	mu        sync.Mutex
	config    Config
	projects  map[string]*Project
	byRoot    map[string]string
	cwdCache  map[string]string
	signals   []Signal
	seen      map[string]struct{}
	cursors   map[string]json.RawMessage
	problems  map[string]*Problem
	index     placeIndex
	dirtyMeta bool
}

// placeIndex remembers where turns, Sessions and Work ran.
type placeIndex struct {
	Turns    map[string]string `json:"turns"`    // turn uuid -> cwd
	Sessions map[string]string `json:"sessions"` // Session id -> cwd
	Works    map[string]string `json:"works"`    // Work id -> cwd
}

const (
	configFile   = "config.json"
	projectsFile = "projects.json"
	signalsFile  = "signals.jsonl"
	cursorsFile  = "cursors.json"
	problemsFile = "problems.json"
	indexFile    = "index.json"
)

// OpenStore loads Watch state from dir, creating it when absent.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		dir:      dir,
		now:      func() time.Time { return time.Now().UTC() },
		projects: map[string]*Project{},
		byRoot:   map[string]string{},
		cwdCache: map[string]string{},
		seen:     map[string]struct{}{},
		cursors:  map[string]json.RawMessage{},
		problems: map[string]*Problem{},
		index:    placeIndex{Turns: map[string]string{}, Sessions: map[string]string{}, Works: map[string]string{}},
	}
	if err := readJSON(filepath.Join(dir, configFile), &s.config); err != nil {
		return nil, err
	}
	var projects []*Project
	if err := readJSON(filepath.Join(dir, projectsFile), &projects); err != nil {
		return nil, err
	}
	for _, project := range projects {
		s.projects[project.ID] = project
		if project.Root != "" {
			s.byRoot[project.Root] = project.ID
		}
	}
	if err := readJSON(filepath.Join(dir, cursorsFile), &s.cursors); err != nil {
		return nil, err
	}
	var problems []*Problem
	if err := readJSON(filepath.Join(dir, problemsFile), &problems); err != nil {
		return nil, err
	}
	for _, problem := range problems {
		s.problems[problem.ID] = problem
	}
	if err := readJSON(filepath.Join(dir, indexFile), &s.index); err != nil {
		return nil, err
	}
	for _, m := range []*map[string]string{&s.index.Turns, &s.index.Sessions, &s.index.Works} {
		if *m == nil {
			*m = map[string]string{}
		}
	}
	if s.cursors == nil {
		s.cursors = map[string]json.RawMessage{}
	}
	if err := s.loadSignals(); err != nil {
		return nil, err
	}
	return s, nil
}

func readJSON(path string, out any) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(raw, '\n'), 0o600)
}

func (s *Store) loadSignals() error {
	file, err := os.Open(filepath.Join(s.dir, signalsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 1<<20)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			var signal Signal
			// A torn final line from a crash is ignored; its signal is
			// collected again because cursors are saved after appends.
			if json.Unmarshal(line, &signal) == nil && signal.ID != "" {
				if _, dup := s.seen[signal.ID]; !dup {
					s.seen[signal.ID] = struct{}{}
					s.signals = append(s.signals, signal)
				}
			}
		}
		if readErr != nil {
			return nil
		}
	}
}

// Config returns the Watch setting.
func (s *Store) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

// SetEnabled changes the Watch setting.
func (s *Store) SetEnabled(enabled bool) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Enabled = enabled
	return s.config, writeJSON(filepath.Join(s.dir, configFile), s.config)
}

// ProjectForCWD returns the project that owns cwd, inferring and recording
// it on first sight. ok is false when cwd is not inside a repository.
func (s *Store) ProjectForCWD(cwd string, seenAt time.Time) (string, bool) {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	s.mu.Lock()
	defer s.mu.Unlock()
	id, cached := s.cwdCache[cwd]
	if !cached {
		root, gitDir, ok := repoRoot(cwd)
		if ok {
			id = s.ensureProjectLocked(root, gitDir, seenAt)
		}
		s.cwdCache[cwd] = id
	}
	if id == "" {
		return "", false
	}
	s.touchLocked(id, seenAt)
	return id, true
}

func (s *Store) ensureProjectLocked(root, gitDir string, seenAt time.Time) string {
	if id, ok := s.byRoot[root]; ok {
		if project := s.projects[id]; project != nil && gitDir != "" {
			if remotes := readRemotes(gitDir); len(remotes) > 0 {
				project.Remotes = remotes
			}
		}
		return id
	}
	id := projectID(root)
	s.projects[id] = &Project{
		ID: id, Name: filepath.Base(root), Root: root,
		Remotes: readRemotes(gitDir), FirstSeen: seenAt, LastSeen: seenAt,
	}
	s.byRoot[root] = id
	s.dirtyMeta = true
	return id
}

func (s *Store) touchLocked(id string, seenAt time.Time) {
	project := s.projects[id]
	if project == nil || seenAt.IsZero() {
		return
	}
	if project.FirstSeen.IsZero() || seenAt.Before(project.FirstSeen) {
		project.FirstSeen = seenAt
		s.dirtyMeta = true
	}
	if seenAt.After(project.LastSeen) {
		project.LastSeen = seenAt
		s.dirtyMeta = true
	}
}

// RuntimeProject returns the project that owns the daemon's own signals: the
// Mewla checkout when one is known, otherwise a built-in project.
func (s *Store) RuntimeProject() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.projects))
	for id := range s.projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		project := s.projects[id]
		if owner, repo, ok := githubRemoteSlug(project.Remotes, nil); ok &&
			strings.EqualFold(owner+"/"+repo, runtimeRepoSlug) {
			return id
		}
	}
	if _, ok := s.projects[RuntimeProjectID]; !ok {
		now := s.now()
		s.projects[RuntimeProjectID] = &Project{ID: RuntimeProjectID, Name: "Mewla", Builtin: true, FirstSeen: now, LastSeen: now}
		s.dirtyMeta = true
	}
	return RuntimeProjectID
}

// Projects returns all projects, most recently active first.
func (s *Store) Projects() []Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Project, 0, len(s.projects))
	for _, project := range s.projects {
		out = append(out, cloneProject(*project))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func cloneProject(project Project) Project {
	raw, _ := json.Marshal(project)
	var out Project
	_ = json.Unmarshal(raw, &out)
	return out
}

// UpdateProject applies change to one project and saves it.
func (s *Store) UpdateProject(id string, change func(*Project) error) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project := s.projects[strings.TrimSpace(id)]
	if project == nil {
		return Project{}, fmt.Errorf("project %q not found", id)
	}
	if err := change(project); err != nil {
		return Project{}, err
	}
	s.dirtyMeta = true
	return cloneProject(*project), s.flushMetaLocked()
}

// Append stores new signals and returns how many were not already present.
// Every text field is redacted first.
func (s *Store) Append(signals []Signal) (int, error) {
	if len(signals) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var lines []byte
	var added []Signal
	now := s.now()
	for _, signal := range signals {
		if signal.ID == "" {
			continue
		}
		if _, dup := s.seen[signal.ID]; dup {
			continue
		}
		signal = redactSignal(signal)
		signal.Time = signal.Time.UTC()
		if signal.CollectedAt.IsZero() {
			signal.CollectedAt = now
		}
		raw, err := json.Marshal(signal)
		if err != nil {
			return 0, err
		}
		lines = append(append(lines, raw...), '\n')
		s.seen[signal.ID] = struct{}{}
		added = append(added, signal)
	}
	if len(added) == 0 {
		return 0, nil
	}
	file, err := os.OpenFile(filepath.Join(s.dir, signalsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = file.Write(lines)
		if syncErr := file.Sync(); err == nil {
			err = syncErr
		}
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		for _, signal := range added {
			delete(s.seen, signal.ID)
		}
		return 0, err
	}
	s.signals = append(s.signals, added...)
	for _, signal := range added {
		if signal.ProjectID != "" {
			s.touchLocked(signal.ProjectID, signal.Time)
		}
	}
	return len(added), nil
}

// Signals returns matching signals, newest first.
func (s *Store) Signals(filter Filter) []Signal {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Signal
	for index := len(s.signals) - 1; index >= 0; index-- {
		signal := s.signals[index]
		if !filter.matches(signal.ProjectID, signal.Source, signal.Kind, signal.Time) {
			continue
		}
		out = append(out, signal)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out
}

// SignalCount returns how many signals are stored.
func (s *Store) SignalCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.signals)
}

func (f Filter) matches(projectID, source, kind string, at time.Time) bool {
	if f.ProjectID != "" && f.ProjectID != projectID {
		return false
	}
	if f.Source != "" && f.Source != source {
		return false
	}
	if f.Kind != "" && f.Kind != kind {
		return false
	}
	return f.Since.IsZero() || !at.Before(f.Since)
}

// Cursor decodes a source cursor into out; ok is false when none is saved.
func (s *Store) Cursor(key string, out any) bool {
	s.mu.Lock()
	raw, ok := s.cursors[key]
	s.mu.Unlock()
	return ok && json.Unmarshal(raw, out) == nil
}

// SetCursor saves a source cursor. Callers append the signals a cursor
// covers before saving it, so a crash re-reads rather than skips.
func (s *Store) SetCursor(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursors[key] = raw
	return writeJSON(filepath.Join(s.dir, cursorsFile), s.cursors)
}

// DeleteCursor forgets a cursor so its source is read again from the start.
func (s *Store) DeleteCursor(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cursors, key)
	return writeJSON(filepath.Join(s.dir, cursorsFile), s.cursors)
}

// Place records where a turn, Session or Work ran.
func (s *Store) Place(kind, key, cwd string) {
	key, cwd = strings.TrimSpace(key), strings.TrimSpace(cwd)
	if key == "" || cwd == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.placeMapLocked(kind)
	if m[key] != cwd {
		m[key] = cwd
		s.dirtyMeta = true
	}
}

// PlaceOf returns where a turn, Session or Work ran.
func (s *Store) PlaceOf(kind, key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.placeMapLocked(kind)[strings.TrimSpace(key)]
}

func (s *Store) placeMapLocked(kind string) map[string]string {
	switch kind {
	case "turn":
		return s.index.Turns
	case "session":
		return s.index.Sessions
	default:
		return s.index.Works
	}
}

// Problems returns problems matching filter, most recent first.
func (s *Store) Problems(filter Filter) []Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Problem
	for _, problem := range s.problems {
		if filter.ProjectID != "" && filter.ProjectID != problem.ProjectID {
			continue
		}
		if filter.Kind != "" && filter.Kind != problem.Rule {
			continue
		}
		copied := *problem
		copied.Evidence = append([]string(nil), problem.Evidence...)
		out = append(out, copied)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].ID < out[j].ID
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out
}

// RecordProblems merges rule results into the registry. A problem keeps its
// status and first sighting; a subject no longer flagged becomes inactive.
func (s *Store) RecordProblems(rule string, flagged []Problem) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	current := map[string]bool{}
	changed := 0
	for _, next := range flagged {
		current[next.ID] = true
		existing := s.problems[next.ID]
		if existing == nil {
			next.Status = StatusNew
			next.Active = true
			next.UpdatedAt = now
			copied := next
			s.problems[next.ID] = &copied
			changed++
			continue
		}
		if existing.Active && existing.Count == next.Count && existing.LastSeen.Equal(next.LastSeen) &&
			existing.Detail == next.Detail && existing.Title == next.Title {
			continue
		}
		existing.Title, existing.Detail, existing.Severe = next.Title, next.Detail, next.Severe
		existing.Count, existing.Evidence, existing.LastSeen = next.Count, next.Evidence, next.LastSeen
		if next.FirstSeen.Before(existing.FirstSeen) {
			existing.FirstSeen = next.FirstSeen
		}
		existing.Active = true
		existing.UpdatedAt = now
		changed++
	}
	for id, problem := range s.problems {
		if problem.Rule == rule && problem.Active && !current[id] {
			problem.Active = false
			problem.UpdatedAt = now
			changed++
		}
	}
	if changed == 0 {
		return 0, nil
	}
	list := make([]*Problem, 0, len(s.problems))
	for _, problem := range s.problems {
		list = append(list, problem)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return changed, writeJSON(filepath.Join(s.dir, problemsFile), list)
}

// Flush saves projects and the place index when they changed.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushMetaLocked()
}

func (s *Store) flushMetaLocked() error {
	if !s.dirtyMeta {
		return nil
	}
	list := make([]*Project, 0, len(s.projects))
	for _, project := range s.projects {
		list = append(list, project)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	if err := writeJSON(filepath.Join(s.dir, projectsFile), list); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(s.dir, indexFile), s.index); err != nil {
		return err
	}
	s.dirtyMeta = false
	return nil
}
