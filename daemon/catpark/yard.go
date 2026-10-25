package catpark

import (
	"strconv"
	"time"
)

const (
	// TreatsPerDay is how many things a new day brings to the yard; a closer
	// cat's yard brings a little more. Yesterday's that were not picked just
	// fade: missing a day costs nothing.
	TreatsPerDay = 3
	pantryCap    = 9
	starterFish  = 2
)

// yardGrows is what the yard grows, catnip the rarest.
var yardGrows = []string{"fish", "fish", "berry", "berry", "catnip"}

// Treat is one thing in the yard to pick up.
type Treat struct {
	Slot  int    `json:"slot"`
	Kind  string `json:"kind"`
	Taken bool   `json:"taken,omitempty"`
}

// Yard is the garden: today's treats, the pantry of what has been picked, and
// the snack packed for the next trip.
type Yard struct {
	Day    string         `json:"day"`
	Treats []Treat        `json:"treats"`
	Pantry map[string]int `json:"pantry"`
	// Bag is the snack packed for the next trip.
	Bag string `json:"bag,omitempty"`
}

func (y *Yard) grow(now time.Time, level int) bool {
	day := now.Format("2006-01-02")
	if y.Day == day {
		return false
	}
	y.Day = day
	y.Treats = y.Treats[:0:0]
	count := TreatsPerDay
	if level >= 3 {
		count++
	}
	if level >= 6 {
		count++
	}
	for slot := 0; slot < count; slot++ {
		y.Treats = append(y.Treats, Treat{Slot: slot, Kind: yardGrows[variantOf(day+"/"+strconv.Itoa(slot), len(yardGrows))]})
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
		if !y.add(treat.Kind) {
			return false
		}
		treat.Taken = true
		return true
	}
	return false
}

func (y *Yard) add(food string) bool {
	if y.Pantry == nil {
		y.Pantry = map[string]int{}
	}
	if y.Pantry[food] >= pantryCap {
		return false
	}
	y.Pantry[food]++
	return true
}

func (y *Yard) take(food string) bool {
	if y.Pantry[food] <= 0 {
		return false
	}
	y.Pantry[food]--
	return true
}

// Pack puts one snack from the pantry in the bag (a snack already there goes
// back). An empty kind unpacks.
func (y *Yard) Pack(kind string) bool {
	if y.Pantry == nil {
		y.Pantry = map[string]int{}
	}
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
