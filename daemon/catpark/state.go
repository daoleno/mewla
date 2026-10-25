package catpark

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strings"
	"time"
)

// Tunables. The cat is gentle: a postcard is rare, never a feed.
const (
	// StartDelay keeps a Work that ends within moments from sending anything.
	StartDelay = 15 * time.Second
	// LongAfter is how long a trip lasts before one more postcard may come.
	LongAfter = 45 * time.Minute
	// MinInterval is the global quiet gap between any two postcards, so many
	// Works starting at once cannot flood the album.
	MinInterval = 10 * time.Minute
	// DeferLimit drops a start postcard still held back by the quiet gap after
	// this long: by then it is no longer news.
	DeferLimit = 30 * time.Minute

	maxPostcards  = 2000
	maxSouvenirs  = 500
	maxTitleRunes = 60

	// StartVariants and LongVariants are how many lines the page has per kind
	// and phase; the daemon only picks an index.
	StartVariants = 3
	LongVariants  = 2
)

// Phases of a postcard.
const (
	PhaseStart = "start"
	PhaseLong  = "long"
)

// Outcome is what the durable Work record says about an open trip.
type Outcome string

const (
	OutcomeOpen      Outcome = ""
	OutcomeDone      Outcome = "done"
	OutcomeCancelled Outcome = "cancelled"
	// OutcomeMissing is a Work the record no longer has. The cat comes home
	// quietly; absence is never read as success.
	OutcomeMissing Outcome = "missing"
)

// Trip is the cat away on one Work.
type Trip struct {
	WorkID    string `json:"work_id"`
	Kind      Kind   `json:"kind"`
	Title     string `json:"title,omitempty"`
	OpenedAt  int64  `json:"opened_at"`
	Packed    string `json:"packed,omitempty"`
	StartDone bool   `json:"start_done,omitempty"`
	LongDone  bool   `json:"long_done,omitempty"`
}

// Postcard is one picture and one short line, which the page renders from
// kind, phase and variant.
type Postcard struct {
	ID      string `json:"id"`
	WorkID  string `json:"work_id"`
	Kind    Kind   `json:"kind"`
	Phase   string `json:"phase"`
	Variant int    `json:"variant"`
	// Treat is the snack the cat took along, when one was packed.
	Treat string `json:"treat,omitempty"`
	At    int64  `json:"at"`
	Seen  bool   `json:"seen,omitempty"`
}

// Souvenir is the keepsake of an accepted Work.
type Souvenir struct {
	ID     string `json:"id"`
	WorkID string `json:"work_id"`
	Kind   Kind   `json:"kind"`
	Item   string `json:"item"`
	// Title is the person's own Work title, shown on their own shelf only.
	Title string `json:"title,omitempty"`
	At    int64  `json:"at"`
	Seen  bool   `json:"seen,omitempty"`
}

// State is everything the cat remembers. It is the persisted document.
type State struct {
	Version   int        `json:"version"`
	Seeded    bool       `json:"seeded"`
	Trips     []Trip     `json:"trips"`
	Postcards []Postcard `json:"postcards"`
	Souvenirs []Souvenir `json:"souvenirs"`
	// LastPostcardAt is the newest postcard's time, for the quiet gap.
	LastPostcardAt int64 `json:"last_postcard_at,omitempty"`
	Yard           Yard  `json:"yard"`
	// Style is the person's chosen art; empty until they choose.
	Style string `json:"style,omitempty"`
}

// RunningWork is a delegated Work the snapshot shows running.
type RunningWork struct {
	ID    string
	Title string
}

// Observation is one reading of Brain: the running delegated Works, what the
// durable record says about the trips already open, and whether something
// waits on the person.
type Observation struct {
	Now      time.Time
	Running  []RunningWork
	Outcomes map[string]Outcome
	Waiting  bool
}

// Home is the cat's presence for the page; Night is the page's own business
// (it knows the local hour).
type Presence struct {
	Away    bool `json:"away"`
	Waiting bool `json:"waiting"`
	// Kind is the place of the newest open trip.
	Kind Kind `json:"kind,omitempty"`
}

// Presence derives from state and the observed waiting flag.
func (s *State) Presence(waiting bool) Presence {
	presence := Presence{Waiting: waiting}
	if len(s.Trips) > 0 {
		presence.Away = true
		presence.Kind = s.Trips[len(s.Trips)-1].Kind
	}
	return presence
}

// Apply folds one observation into the state and reports whether anything
// the person could see changed. It is pure: no clock, no I/O.
func (s *State) Apply(obs Observation) bool {
	changed := false
	now := obs.Now

	// Close trips first: a Work accepted since the last look brings the cat
	// home before a new trip could take its place.
	kept := s.Trips[:0:0]
	for _, trip := range s.Trips {
		outcome := obs.Outcomes[trip.WorkID]
		switch outcome {
		case OutcomeOpen:
			kept = append(kept, trip)
		case OutcomeDone:
			changed = true
			id := "sv:" + trip.WorkID
			if !s.hasSouvenir(id) {
				s.Souvenirs = append(s.Souvenirs, Souvenir{
					ID: id, WorkID: trip.WorkID, Kind: trip.Kind, Item: SouvenirItem(trip.Kind),
					Title: trip.Title, At: now.UnixMilli(),
				})
			}
		default:
			changed = true
		}
	}
	s.Trips = kept

	for _, work := range obs.Running {
		if work.ID == "" || s.hasTrip(work.ID) || outcomeFinal(obs.Outcomes[work.ID]) {
			continue
		}
		// The trip starts when the cat first sees the Work, not when the Work
		// began: a Work found already old must not hand over its postcards at
		// once.
		trip := Trip{WorkID: work.ID, Kind: KindOf(work.Title), Title: clipRunes(work.Title, maxTitleRunes), OpenedAt: now.UnixMilli()}
		if s.Seeded {
			trip.StartDone = s.hasPostcard(postcardID(work.ID, PhaseStart))
			trip.LongDone = s.hasPostcard(postcardID(work.ID, PhaseLong))
			trip.Packed = s.takeBag()
		} else {
			// Work already running when the cat first learns of it is not
			// news: no postcards are backfilled for history.
			trip.StartDone, trip.LongDone = true, true
		}
		s.Trips = append(s.Trips, trip)
		changed = true
	}
	if !s.Seeded {
		s.Seeded = true
		changed = true
	}

	for index := range s.Trips {
		trip := &s.Trips[index]
		age := now.Sub(time.UnixMilli(trip.OpenedAt))
		if !trip.StartDone && age >= StartDelay {
			if s.quietEnough(now) {
				s.addPostcard(trip, PhaseStart, now)
				trip.StartDone = true
				changed = true
			} else if age >= DeferLimit {
				trip.StartDone = true
				changed = true
			}
		}
		if !trip.LongDone && age >= LongAfter && s.quietEnough(now) {
			s.addPostcard(trip, PhaseLong, now)
			trip.LongDone = true
			changed = true
		}
	}

	if s.Yard.grow(now) {
		changed = true
	}
	s.trim()
	return changed
}

func outcomeFinal(outcome Outcome) bool { return outcome != OutcomeOpen }

func (s *State) quietEnough(now time.Time) bool {
	return s.LastPostcardAt == 0 || now.Sub(time.UnixMilli(s.LastPostcardAt)) >= MinInterval
}

func (s *State) addPostcard(trip *Trip, phase string, now time.Time) {
	id := postcardID(trip.WorkID, phase)
	if s.hasPostcard(id) {
		return
	}
	variants := StartVariants
	if phase == PhaseLong {
		variants = LongVariants
	}
	s.Postcards = append(s.Postcards, Postcard{
		ID: id, WorkID: trip.WorkID, Kind: trip.Kind, Phase: phase,
		Variant: variantOf(trip.WorkID+"/"+phase, variants), Treat: trip.Packed, At: now.UnixMilli(),
	})
	s.LastPostcardAt = now.UnixMilli()
}

func postcardID(workID, phase string) string { return "pc:" + workID + ":" + phase }

func variantOf(key string, n int) int {
	sum := sha256.Sum256([]byte(key))
	return int(binary.BigEndian.Uint32(sum[:4]) % uint32(n))
}

func (s *State) hasTrip(workID string) bool {
	for _, trip := range s.Trips {
		if trip.WorkID == workID {
			return true
		}
	}
	return false
}

func (s *State) hasPostcard(id string) bool {
	for _, card := range s.Postcards {
		if card.ID == id {
			return true
		}
	}
	return false
}

func (s *State) hasSouvenir(id string) bool {
	for _, item := range s.Souvenirs {
		if item.ID == id {
			return true
		}
	}
	return false
}

func (s *State) trim() {
	if extra := len(s.Postcards) - maxPostcards; extra > 0 {
		s.Postcards = append([]Postcard(nil), s.Postcards[extra:]...)
	}
	if extra := len(s.Souvenirs) - maxSouvenirs; extra > 0 {
		s.Souvenirs = append([]Souvenir(nil), s.Souvenirs[extra:]...)
	}
}

func clipRunes(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

// OpenWorkIDs are the Works whose trips are open, in a stable order.
func (s *State) OpenWorkIDs() []string {
	ids := make([]string, 0, len(s.Trips))
	for _, trip := range s.Trips {
		ids = append(ids, trip.WorkID)
	}
	sort.Strings(ids)
	return ids
}
