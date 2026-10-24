package catpark

import "time"

// TreatKinds are the small things that grow in the yard.
var TreatKinds = []string{"fish", "yarn", "leaf"}

const (
	// TreatsPerDay is how many treats a new day brings. Yesterday's that were
	// not picked just fade: missing a day costs nothing.
	TreatsPerDay = 3
	pantryCap    = 9
)

// Treat is one thing in the yard to pick up.
type Treat struct {
	Slot  int    `json:"slot"`
	Kind  string `json:"kind"`
	Taken bool   `json:"taken,omitempty"`
}

// Yard is the garden: today's treats, what has been picked, and what the cat
// will take along on its next trip. It is affection only and changes nothing
// about the Work.
type Yard struct {
	Day    string         `json:"day"`
	Treats []Treat        `json:"treats"`
	Pantry map[string]int `json:"pantry"`
	// Bag is the treat packed for the next trip.
	Bag string `json:"bag,omitempty"`
}

func (y *Yard) grow(now time.Time) bool {
	day := now.Format("2006-01-02")
	if y.Day == day {
		return false
	}
	y.Day = day
	y.Treats = y.Treats[:0:0]
	for slot := 0; slot < TreatsPerDay; slot++ {
		y.Treats = append(y.Treats, Treat{Slot: slot, Kind: TreatKinds[variantOf(day+"/"+string(rune('0'+slot)), len(TreatKinds))]})
	}
	if y.Pantry == nil {
		y.Pantry = map[string]int{}
	}
	return true
}

// Collect picks up today's treat in a slot. Picking twice is a no-op.
func (y *Yard) Collect(slot int) bool {
	for index := range y.Treats {
		treat := &y.Treats[index]
		if treat.Slot != slot || treat.Taken {
			continue
		}
		if y.Pantry == nil {
			y.Pantry = map[string]int{}
		}
		if y.Pantry[treat.Kind] >= pantryCap {
			return false
		}
		treat.Taken = true
		y.Pantry[treat.Kind]++
		return true
	}
	return false
}

// Pack puts one treat from the pantry in the bag (a treat already there goes
// back). An empty kind unpacks.
func (y *Yard) Pack(kind string) bool {
	if kind == y.Bag {
		return false
	}
	if kind != "" && y.Pantry[kind] <= 0 {
		return false
	}
	if y.Bag != "" {
		y.Pantry[y.Bag]++
	}
	y.Bag = ""
	if kind != "" {
		y.Pantry[kind]--
		y.Bag = kind
	}
	return true
}

func (s *State) takeBag() string {
	bag := s.Yard.Bag
	s.Yard.Bag = ""
	return bag
}
