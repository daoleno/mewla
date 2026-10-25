package catpark

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strings"
	"time"
)

// Tunables. The cat is gentle: a postcard is rare, never a feed. It follows
// every Work, but it goes out only now and then and never for long, so it is
// home on most visits even when Brain is busy all day.
const (
	// StartDelay keeps a Work that ends within moments from taking the cat
	// out, and is how long an outing runs before its postcard comes.
	StartDelay = 15 * time.Second
	// OutingLength is how long the cat is out on one outing; it comes home on
	// its own after that, the Work still running.
	OutingLength = 15 * time.Minute
	// RestLength is how long it stays home after an outing before it goes out
	// again: under nonstop Work it is home three quarters of the time.
	RestLength = 45 * time.Minute
	// LongAfter is how long a Work runs before the cat may go out on it once
	// more, for one more postcard.
	LongAfter = 45 * time.Minute
	// MinInterval is the global quiet gap between any two trip postcards, so
	// many Works starting at once cannot flood the album.
	MinInterval = 10 * time.Minute
	// DeferLimit drops a start outing still held back (the cat resting, or the
	// quiet gap) after this long: by then the Work is no longer news.
	DeferLimit = 30 * time.Minute

	maxPostcards  = 2000
	maxSouvenirs  = 500
	maxTitleRunes = 60
)

// Phases of a postcard. Start and long come from a trip; welcome and letter
// are written at home and never pretend to be a trip.
const (
	PhaseStart   = "start"
	PhaseLong    = "long"
	PhaseWelcome = "welcome"
	PhaseLetter  = "letter"
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

// Trip is the cat following one Work, from when it first sees it until the
// Work is accepted (a souvenir) or dropped. The cat is not away for all of
// it: see Outing.
type Trip struct {
	WorkID   string `json:"work_id"`
	Kind     Kind   `json:"kind"`
	Title    string `json:"title,omitempty"`
	OpenedAt int64  `json:"opened_at"`
	// Packed is the treat that went out with the cat on this Work's outing.
	Packed string `json:"packed,omitempty"`
	// StartDone and LongDone are this Work's two outings, gone or let go.
	StartDone bool `json:"start_done,omitempty"`
	LongDone  bool `json:"long_done,omitempty"`
}

// Outing is the cat actually out of the house, on one Work, for a while.
type Outing struct {
	WorkID string `json:"work_id"`
	Kind   Kind   `json:"kind"`
	Phase  string `json:"phase"`
	Since  int64  `json:"since"`
	Until  int64  `json:"until"`
	// Card is the postcard this outing sent, once it has.
	Card string `json:"card,omitempty"`
}

// Postcard is one composed scene and one short line. The daemon chooses what
// is in it and keeps the choice, so the album never changes; the page draws
// it and writes the line from these ids and Seed.
type Postcard struct {
	ID     string `json:"id"`
	WorkID string `json:"work_id,omitempty"`
	Kind   Kind   `json:"kind,omitempty"`
	Phase  string `json:"phase"`
	Place  string `json:"place,omitempty"`
	Act    string `json:"act,omitempty"`
	// Time is the hour it was sent: dawn, day, dusk or night.
	Time    string `json:"time,omitempty"`
	Weather string `json:"weather,omitempty"`
	Friend  string `json:"friend,omitempty"`
	// Treat is the snack the cat took along, when one was packed.
	Treat string `json:"treat,omitempty"`
	// Hobby is what a letter is about, when it is about one.
	Hobby string `json:"hobby,omitempty"`
	Seed  uint32 `json:"seed"`
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
	// Hobby is the hobby this trip taught, if it taught one.
	Hobby string `json:"hobby,omitempty"`
	At    int64  `json:"at"`
	Seen  bool   `json:"seen,omitempty"`
}

// State is everything the cat remembers. It is the persisted document.
type State struct {
	Version int `json:"version"`
	// Enabled is the person's Settings → Labs switch for the cat. It lives
	// here, on the computer, so the App and the web UI agree; it starts off.
	Enabled bool   `json:"enabled,omitempty"`
	Seeded  bool   `json:"seeded"`
	Trips   []Trip `json:"trips"`
	// Out is the outing the cat is on; nil, it is home.
	Out *Outing `json:"out,omitempty"`
	// HomeAt is when it last came home from an outing; it rests after one.
	HomeAt    int64      `json:"home_at,omitempty"`
	Postcards []Postcard `json:"postcards"`
	Souvenirs []Souvenir `json:"souvenirs"`
	// LastPostcardAt is the newest trip postcard's time, for the quiet gap.
	LastPostcardAt int64 `json:"last_postcard_at,omitempty"`
	Yard           Yard  `json:"yard"`
	Care           Care  `json:"care"`
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

// Presence is the cat's whereabouts for the page; night is the page's own
// business (it knows the local hour).
type Presence struct {
	Away    bool `json:"away"`
	Waiting bool `json:"waiting"`
	// Kind is where the cat went.
	Kind Kind `json:"kind,omitempty"`
	// Back is when it will be home (ms); the room says so while it is out.
	Back int64 `json:"back,omitempty"`
	// Card is the postcard the outing sent, once it has.
	Card string `json:"card,omitempty"`
}

// Presence derives from state and the observed waiting flag.
func (s *State) Presence(waiting bool) Presence {
	presence := Presence{Waiting: waiting}
	if out := s.Out; out != nil {
		presence.Away, presence.Kind, presence.Back, presence.Card = true, out.Kind, out.Until, out.Card
	}
	return presence
}

// home reports whether the cat is in the house.
func (s *State) home() bool { return s.Out == nil }

// Apply folds one observation into the state and reports whether anything
// the person could see changed. It is pure: no clock, no I/O.
func (s *State) Apply(obs Observation) bool {
	changed := false
	now := obs.Now
	world := World()
	if s.Care.begin(now) {
		changed = true
	}

	// Close trips first: a Work accepted since the last look brings its
	// souvenir home, and the cat with it if it was out on that Work.
	kept := s.Trips[:0:0]
	for _, trip := range s.Trips {
		outcome := obs.Outcomes[trip.WorkID]
		if outcome == OutcomeOpen {
			kept = append(kept, trip)
			continue
		}
		changed = true
		onIt := s.Out != nil && s.Out.WorkID == trip.WorkID
		if onIt {
			s.comeHome(now)
		}
		if outcome == OutcomeDone {
			s.homecoming(trip, now, world)
		} else if onIt {
			s.Care.remember(now, "home", "")
		}
	}
	s.Trips = kept
	// The outing's postcard comes a little after it leaves, if the album has
	// been quiet long enough; an outing that cannot send one by the time it
	// is over sends none.
	if out := s.Out; out != nil && out.Card == "" && now.Sub(time.UnixMilli(out.Since)) >= StartDelay && s.quietEnough(now) {
		if trip := s.trip(out.WorkID); trip != nil && !s.hasPostcard(postcardID(trip.WorkID, out.Phase)) {
			s.addPostcard(trip, out.Phase, now, world)
			out.Card = postcardID(trip.WorkID, out.Phase)
			changed = true
		}
	}
	// An outing is short: the cat comes home on its own while the Work runs.
	if s.Out != nil && now.UnixMilli() >= s.Out.Until {
		s.comeHome(time.UnixMilli(s.Out.Until))
		s.Care.remember(now, "home", "")
		changed = true
	}
	if s.home() && s.Care.Bowl != "" {
		// The cat is home: whatever was left in its bowl gets eaten.
		food := s.Care.Bowl
		s.Care.Bowl = ""
		s.Care.eat(food, now, world)
		changed = true
	}

	for _, work := range obs.Running {
		if work.ID == "" || s.hasTrip(work.ID) || obs.Outcomes[work.ID] != OutcomeOpen {
			continue
		}
		// The trip starts when the cat first sees the Work, not when the Work
		// began: a Work found already old must not hand over its postcards at
		// once.
		trip := Trip{WorkID: work.ID, Kind: KindOf(work.Title), Title: clipRunes(work.Title, maxTitleRunes), OpenedAt: now.UnixMilli()}
		if s.Seeded {
			trip.StartDone = s.hasPostcard(postcardID(work.ID, PhaseStart))
			trip.LongDone = s.hasPostcard(postcardID(work.ID, PhaseLong))
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
		s.welcome(now)
		changed = true
	}

	if s.goOut(now) {
		changed = true
	}

	if s.Yard.grow(now, s.Care.Level(world)) {
		changed = true
	}
	s.trim()
	return changed
}

// goOut takes the cat out on a Work when it may go: its person has met it
// (petted and fed it once, the first visit's two steps at home), it is home
// and rested, and a Work is fresh (just begun) or long (running
// for a while) and has not had that outing yet. A fresh Work missed while the
// cat rested is let go once it is no longer news.
func (s *State) goOut(now time.Time) bool {
	changed := false
	var pick *Trip
	phase := ""
	for index := range s.Trips {
		trip := &s.Trips[index]
		age := now.Sub(time.UnixMilli(trip.OpenedAt))
		if !trip.StartDone && age >= DeferLimit {
			trip.StartDone = true
			changed = true
		}
		switch {
		case !trip.StartDone && age >= StartDelay:
			// the newest fresh Work is the best news
			if phase != PhaseStart || trip.OpenedAt > pick.OpenedAt {
				pick, phase = trip, PhaseStart
			}
		case phase == "" && trip.StartDone && !trip.LongDone && age >= LongAfter:
			pick, phase = trip, PhaseLong
		}
	}
	if pick == nil || !s.home() || !s.met() || now.Sub(time.UnixMilli(s.HomeAt)) < RestLength {
		return changed
	}
	if phase == PhaseStart {
		pick.StartDone = true
	} else {
		pick.LongDone = true
	}
	if pick.Packed == "" {
		pick.Packed = s.takeBag()
	}
	s.Out = &Outing{WorkID: pick.WorkID, Kind: pick.Kind, Phase: phase, Since: now.UnixMilli(), Until: now.Add(OutingLength).UnixMilli()}
	s.Care.remember(now, "leave", string(pick.Kind))
	return true
}

// met reports whether the first visit's steps at home are done; until then
// the cat stays in, so a first visit always finds it.
func (s *State) met() bool { return s.Care.Learned["pet"] && s.Care.Learned["feed"] }

// comeHome ends the outing; the cat rests before the next one.
func (s *State) comeHome(now time.Time) {
	s.Out = nil
	s.HomeAt = now.UnixMilli()
}

func (s *State) trip(workID string) *Trip {
	for index := range s.Trips {
		if s.Trips[index].WorkID == workID {
			return &s.Trips[index]
		}
	}
	return nil
}

// homecoming closes an accepted trip: a souvenir, stars, and the hobby the
// place teaches, learnt now or kept in mind until the bond allows it.
func (s *State) homecoming(trip Trip, now time.Time, world *Catalog) {
	id := "sv:" + trip.WorkID
	if s.hasSouvenir(id) {
		return
	}
	hobby := world.HobbyOf(trip.Kind)
	learnt := s.Care.meetHobby(hobby, world, now)
	souvenir := Souvenir{ID: id, WorkID: trip.WorkID, Kind: trip.Kind, Item: SouvenirItem(trip.Kind), Title: trip.Title, At: now.UnixMilli()}
	if learnt {
		souvenir.Hobby = hobby
	}
	s.Souvenirs = append(s.Souvenirs, souvenir)
	stars := 3
	if trip.Packed != "" {
		stars += 2
	}
	s.Care.earn(stars, world, now)
	s.Care.LastBack = now.UnixMilli()
	s.Care.remember(now, "back", string(trip.Kind))
}

// welcome is the cat's first note to a new person: written at home, so the
// page draws it as a letter, never as a trip.
func (s *State) welcome(now time.Time) {
	if s.hasPostcard("pc:welcome") {
		return
	}
	s.Postcards = append(s.Postcards, Postcard{ID: "pc:welcome", Phase: PhaseWelcome, At: now.UnixMilli()})
	// and a little to share, so the first meal needs no errand
	for n := 0; n < starterFish; n++ {
		s.Yard.add("fish")
	}
}

func (s *State) quietEnough(now time.Time) bool {
	return s.LastPostcardAt == 0 || now.Sub(time.UnixMilli(s.LastPostcardAt)) >= MinInterval
}

func postcardID(workID, phase string) string { return "pc:" + workID + ":" + phase }

func hashOf(key string) uint32 {
	sum := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint32(sum[:4])
}

func variantOf(key string, n int) int { return int(hashOf(key) % uint32(n)) }

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
