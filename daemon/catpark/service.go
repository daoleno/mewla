package catpark

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/live"
)

// stateVersion 2 is the v4 game. An older file is set aside and the cat
// starts afresh: there is no migration.
const stateVersion = 2

// View is the wire projection the page draws. It carries no Work titles
// except on the person's own souvenirs.
type View struct {
	// Enabled is the Labs switch: off, the App shows no Cat tab.
	Enabled   bool       `json:"enabled"`
	Presence  Presence   `json:"presence"`
	Postcards []Postcard `json:"postcards"`
	Souvenirs []Souvenir `json:"souvenirs"`
	Yard      Yard       `json:"yard"`
	Cat       CatView    `json:"cat"`
	Now       int64      `json:"now"`
}

// CatView is the cat itself: its bond, hobbies, mood and what it remembers.
type CatView struct {
	Level int `json:"level"`
	// Stars are the stars toward the next level, out of Need (0 at the top).
	Stars    int             `json:"stars"`
	Need     int             `json:"need"`
	Hobbies  []string        `json:"hobbies"`
	Pending  []string        `json:"pending"`
	Mood     string          `json:"mood"`
	Favorite string          `json:"favorite,omitempty"`
	FavHobby string          `json:"fav_hobby,omitempty"`
	Today    map[string]int  `json:"today"`
	Learned  map[string]bool `json:"learned"`
	Bowl     string          `json:"bowl,omitempty"`
	// Gap is how long the person was away before this visit, in ms; Since is
	// when that last visit was, the line for "while you were away".
	Gap    int64    `json:"gap"`
	Since  int64    `json:"since,omitempty"`
	Name   string   `json:"name,omitempty"`
	Visits int      `json:"visits"`
	Memory []Moment `json:"memory"`
}

// Lookup reports how the durable record says a Work stands.
type Lookup func(workID string) Outcome

// Service owns the cat's persisted state for one daemon.
type Service struct {
	mu       sync.Mutex
	path     string
	state    State
	waiting  bool
	lookup   Lookup
	onChange func(View)
	now      func() time.Time
}

// Open loads (or starts) the state under dir. onChange is called without the
// lock held, only after the new state is on disk.
func Open(dir string, lookup Lookup, onChange func(View)) (*Service, error) {
	if dir == "" {
		return nil, errors.New("catpark: no state directory")
	}
	folder := filepath.Join(dir, "catpark")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return nil, fmt.Errorf("catpark: %w", err)
	}
	s := &Service{path: filepath.Join(folder, "state.json"), lookup: lookup, onChange: onChange, now: time.Now}
	data, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &s.state); err != nil {
			// A damaged file is set aside, never overwritten silently.
			_ = os.Rename(s.path, s.path+".damaged")
			s.state = State{}
		} else if s.state.Version != stateVersion {
			// An older cat: kept beside, and this one starts afresh.
			_ = os.Rename(s.path, s.path+".v"+strconv.Itoa(s.state.Version))
			s.state = State{}
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("catpark: %w", err)
	}
	s.state.Version = stateVersion
	return s, nil
}

// Observe reads Brain and folds it into the cat's state.
func (s *Service) Observe(snapshot brain.Snapshot) {
	now := s.clock()
	waiting := live.Project(snapshot, now).Kind == live.KindNeedsYou
	obs := Observation{Now: now, Waiting: waiting}
	current := map[string]brain.CurrentWork{}
	for _, item := range snapshot.CurrentWork {
		current[item.ID] = item
		if item.AttemptDelegated && item.Status == brain.WorkRunning {
			obs.Running = append(obs.Running, RunningWork{ID: item.ID, Title: item.Title})
		}
	}

	s.mu.Lock()
	enabled, open := s.state.Enabled, s.state.OpenWorkIDs()
	s.mu.Unlock()
	if !enabled {
		return // switched off in Labs: the cat stays home and follows nothing
	}
	obs.Outcomes = make(map[string]Outcome, len(open))
	for _, id := range open {
		if item, ok := current[id]; ok && item.Status != brain.WorkDone && item.Status != brain.WorkCancelled {
			continue
		}
		if s.lookup != nil {
			obs.Outcomes[id] = s.lookup(id)
		}
	}
	s.update(func(state *State) bool { return state.Apply(obs) }, waiting)
}

// update runs a mutation on a copy, persists it, then publishes it.
func (s *Service) update(mutate func(*State) bool, waiting bool) {
	s.mu.Lock()
	next := s.state.clone()
	changed := mutate(&next)
	waitingChanged := waiting != s.waiting
	if !changed && !waitingChanged {
		s.mu.Unlock()
		return
	}
	if changed {
		if err := s.save(next); err != nil {
			// Nothing is announced that is not durable: a restart must not
			// repeat it, and a failed write must not pretend it happened.
			s.mu.Unlock()
			return
		}
		s.state = next
	}
	s.waiting = waiting
	view := s.viewLocked()
	s.mu.Unlock()
	if s.onChange != nil {
		s.onChange(view)
	}
}

func (s *Service) save(state State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicfile.Write(s.path, data, 0o600)
}

// View is the current projection.
func (s *Service) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked()
}

func (s *Service) viewLocked() View {
	now := s.now()
	world := World()
	care := &s.state.Care
	level, stars, need := world.LevelOf(care.Stars)
	view := View{
		Enabled:   s.state.Enabled,
		Presence:  s.state.Presence(s.waiting),
		Postcards: append([]Postcard{}, s.state.Postcards...),
		Souvenirs: append([]Souvenir{}, s.state.Souvenirs...),
		Yard:      s.state.Yard.clone(),
		Cat: CatView{
			Level: level, Stars: stars, Need: need,
			Hobbies:  append([]string{}, care.Hobbies...),
			Pending:  append([]string{}, care.Pending...),
			Mood:     s.state.Mood(now),
			Favorite: care.Favorite(),
			FavHobby: care.FavoriteHobby(),
			Today:    cloneMap(care.Today),
			Learned:  cloneMap(care.Learned),
			Bowl:     care.Bowl,
			Visits:   care.Visits,
			Name:     care.Name,
		},
		Now: now.UnixMilli(),
	}
	if care.PrevVisit != 0 {
		view.Cat.Gap = care.LastVisit - care.PrevVisit
		view.Cat.Since = care.PrevVisit
	}
	memory := care.Memory
	if len(memory) > 30 {
		memory = memory[len(memory)-30:]
	}
	view.Cat.Memory = append([]Moment{}, memory...)
	return view
}

// Act applies one thing the person did in the house.
func (s *Service) Act(act, arg string) {
	if !ValidAct(act) {
		return
	}
	now := s.clock()
	s.update(func(state *State) bool { return state.Enabled && state.Act(act, arg, now) }, s.waitingNow())
}

// SetEnabled is the Labs switch. Turning it on does not observe Brain by
// itself; the caller does that next, so the cat joins the Work running now.
func (s *Service) SetEnabled(on bool) {
	s.update(func(state *State) bool {
		if state.Enabled == on {
			return false
		}
		state.Enabled = on
		return true
	}, s.waitingNow())
}

// SetClock replaces the service's clock; previews use it to move through days.
func (s *Service) SetClock(now func() time.Time) {
	s.mu.Lock()
	s.now = now
	s.mu.Unlock()
}

func (s *Service) clock() time.Time {
	s.mu.Lock()
	now := s.now
	s.mu.Unlock()
	return now()
}

func (s *Service) waitingNow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiting
}

func (s State) clone() State {
	s.Trips = append([]Trip(nil), s.Trips...)
	s.Postcards = append([]Postcard(nil), s.Postcards...)
	s.Souvenirs = append([]Souvenir(nil), s.Souvenirs...)
	s.Yard = s.Yard.clone()
	s.Care = s.Care.clone()
	return s
}

func (c Care) clone() Care {
	c.Hobbies = append([]string(nil), c.Hobbies...)
	c.Pending = append([]string(nil), c.Pending...)
	c.Today = cloneMap(c.Today)
	c.Habits = cloneMap(c.Habits)
	c.Learned = cloneMap(c.Learned)
	c.Memory = append([]Moment(nil), c.Memory...)
	return c
}

func (y Yard) clone() Yard {
	y.Treats = append([]Treat{}, y.Treats...)
	y.Pantry = cloneMap(y.Pantry)
	return y
}

func cloneMap[K comparable, V any](in map[K]V) map[K]V {
	out := make(map[K]V, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// OutcomeOf maps a durable Work to the cat's verdict on its trip.
func OutcomeOf(work brain.Work, err error) Outcome {
	if err != nil {
		if errors.Is(err, brain.ErrWorkNotFound) {
			return OutcomeMissing
		}
		return OutcomeOpen // unknown: ask again, never assume
	}
	switch work.Status {
	case brain.WorkDone:
		return OutcomeDone
	case brain.WorkCancelled:
		return OutcomeCancelled
	}
	return OutcomeOpen
}
