package catpark

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/live"
)

const stateVersion = 1

// View is the wire projection the page draws. It carries no Work titles
// except on the person's own souvenirs.
type View struct {
	Presence  Presence   `json:"presence"`
	Postcards []Postcard `json:"postcards"`
	Souvenirs []Souvenir `json:"souvenirs"`
	Yard      Yard       `json:"yard"`
	Style     Style      `json:"style"`
	Now       int64      `json:"now"`
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
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("catpark: %w", err)
	}
	s.state.Version = stateVersion
	return s, nil
}

// Observe reads Brain and folds it into the cat's state.
func (s *Service) Observe(snapshot brain.Snapshot) {
	now := s.now()
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
	open := s.state.OpenWorkIDs()
	s.mu.Unlock()
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
	view := View{
		Presence:  s.state.Presence(s.waiting),
		Postcards: append([]Postcard{}, s.state.Postcards...),
		Souvenirs: append([]Souvenir{}, s.state.Souvenirs...),
		Yard:      s.state.Yard.clone(),
		Style:     s.state.StyleOrDefault(),
		Now:       s.now().UnixMilli(),
	}
	return view
}

// Collect picks up a treat in today's yard.
func (s *Service) Collect(slot int) {
	s.update(func(state *State) bool { return state.Yard.Collect(slot) }, s.waitingNow())
}

// Pack puts a treat in the bag for the next trip; an empty kind unpacks.
func (s *Service) Pack(kind string) {
	s.update(func(state *State) bool { return state.Yard.Pack(kind) }, s.waitingNow())
}

// SetStyle records the art the person chose for the house.
func (s *Service) SetStyle(style string) {
	s.update(func(state *State) bool { return state.SetStyle(style) }, s.waitingNow())
}

// MarkSeen records that a postcard or souvenir was opened.
func (s *Service) MarkSeen(what, id string) {
	s.update(func(state *State) bool { return state.markSeen(what, id) }, s.waitingNow())
}

func (s *Service) waitingNow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiting
}

func (s *State) markSeen(what, id string) bool {
	switch what {
	case "postcard":
		for i := range s.Postcards {
			if s.Postcards[i].ID == id && !s.Postcards[i].Seen {
				s.Postcards[i].Seen = true
				return true
			}
		}
	case "souvenir":
		for i := range s.Souvenirs {
			if s.Souvenirs[i].ID == id && !s.Souvenirs[i].Seen {
				s.Souvenirs[i].Seen = true
				return true
			}
		}
	}
	return false
}

func (s State) clone() State {
	s.Trips = append([]Trip(nil), s.Trips...)
	s.Postcards = append([]Postcard(nil), s.Postcards...)
	s.Souvenirs = append([]Souvenir(nil), s.Souvenirs...)
	s.Yard = s.Yard.clone()
	return s
}

func (y Yard) clone() Yard {
	y.Treats = append([]Treat{}, y.Treats...)
	pantry := make(map[string]int, len(y.Pantry))
	for kind, count := range y.Pantry {
		pantry[kind] = count
	}
	y.Pantry = pantry
	return y
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
