package catpark

import (
	"strconv"
	"time"
)

// Care is the bond between the cat and its person: the stars in the jar, the
// hobbies it has picked up, what you did together today and over time, and a
// short memory of it. Missing days costs nothing; each day only caps how many
// stars care can bring, so the jar cannot be ground.
type Care struct {
	Stars int `json:"stars"`
	// Hobbies are learnt, in the order learnt. Pending are hobbies the cat met
	// on a trip but could not take up yet: the bond allows one per level.
	Hobbies []string `json:"hobbies"`
	Pending []string `json:"pending,omitempty"`
	Day     string   `json:"day"`
	// Today counts what happened today; Habits counts it for ever
	// ("feed:fish", "hobby:paint", "pet", "play").
	Today  map[string]int `json:"today"`
	Habits map[string]int `json:"habits"`
	// Learned are the first-visit steps the person has done once.
	Learned map[string]bool `json:"learned"`
	Memory  []Moment        `json:"memory"`
	// Bowl is food left out while the cat is away; it eats it on coming home.
	Bowl      string `json:"bowl,omitempty"`
	LastVisit int64  `json:"last_visit,omitempty"`
	PrevVisit int64  `json:"prev_visit,omitempty"`
	Visits    int    `json:"visits,omitempty"`
	LastCare  int64  `json:"last_care,omitempty"`
	LastBack  int64  `json:"last_back,omitempty"`
}

// Moment is one thing done together. Memory is where the cat's words will
// come from once it can talk.
type Moment struct {
	At   int64  `json:"at"`
	What string `json:"what"`
	Arg  string `json:"arg,omitempty"`
}

const (
	maxMemory = 60
	// FeedsPerDay is how many meals the cat wants; after that it is full.
	FeedsPerDay = 2
	// revisitGap is how long the page must be away for an opening to count as
	// a new visit (a reconnect is not a visit).
	revisitGap = 5 * time.Minute
)

// starCaps are how many times a day each kind of care drops stars, and how many.
var starCaps = map[string][2]int{
	"pet": {3, 1}, "feed": {FeedsPerDay, 2}, "play": {2, 2}, "hobby": {3, 1}, "read": {4, 1}, "gift": {2, 1},
}

// Steps the page may mark learnt itself; the rest follow from what the person does.
var pageSteps = map[string]bool{"jar": true, "shelf": true, "album": true, "bag": true, "play": true, "window": true}

func (c *Care) ensure() {
	if c.Today == nil {
		c.Today = map[string]int{}
	}
	if c.Habits == nil {
		c.Habits = map[string]int{}
	}
	if c.Learned == nil {
		c.Learned = map[string]bool{}
	}
}

// begin starts a new day's counts.
func (c *Care) begin(now time.Time) bool {
	c.ensure()
	day := now.Format("2006-01-02")
	if c.Day == day {
		return false
	}
	c.Day = day
	c.Today = map[string]int{}
	return true
}

// Level is the bond level, from 1.
func (c *Care) Level(world *Catalog) int {
	level, _, _ := world.LevelOf(c.Stars)
	return level
}

// earn drops stars in the jar; a new level lets the cat take up hobbies it
// met but could not learn yet.
func (c *Care) earn(stars int, world *Catalog, now time.Time) {
	before := c.Level(world)
	c.Stars += stars
	after := c.Level(world)
	if after > before {
		c.remember(now, "level", strconv.Itoa(after))
		c.promote(world, now)
	}
}

// care counts one act of care and drops its stars while today's cap allows.
func (c *Care) care(what string, world *Catalog, now time.Time, bonus int) {
	c.ensure()
	c.Today[what]++
	c.LastCare = now.UnixMilli()
	if cap, ok := starCaps[what]; ok && c.Today[what] <= cap[0] {
		c.earn(cap[1]+bonus, world, now)
	}
}

func (c *Care) promote(world *Catalog, now time.Time) {
	for len(c.Pending) > 0 && len(c.Hobbies) < c.Level(world) {
		hobby := c.Pending[0]
		c.Pending = c.Pending[1:]
		c.learn(hobby, now)
	}
}

func (c *Care) learn(hobby string, now time.Time) {
	if !contains(c.Hobbies, hobby) {
		c.Hobbies = append(c.Hobbies, hobby)
		c.remember(now, "learn", hobby)
	}
}

// meetHobby is a trip showing the cat a hobby: it learns it if the bond
// allows one more, otherwise keeps it in mind. It reports whether it learnt.
func (c *Care) meetHobby(hobby string, world *Catalog, now time.Time) bool {
	if hobby == "" || contains(c.Hobbies, hobby) {
		return false
	}
	if len(c.Hobbies) < c.Level(world) {
		c.learn(hobby, now)
		return true
	}
	if !contains(c.Pending, hobby) {
		c.Pending = append(c.Pending, hobby)
	}
	return false
}

func (c *Care) knows(hobby string) bool { return contains(c.Hobbies, hobby) }

// eat is a meal: stars (one more for its favourite) and a habit.
func (c *Care) eat(food string, now time.Time, world *Catalog) {
	c.ensure()
	bonus := 0
	if food == c.Favorite() && food != "" {
		bonus = 1
	}
	c.Habits["feed:"+food]++
	c.care("feed", world, now, bonus)
	c.Learned["feed"] = true
	c.remember(now, "feed", food)
}

// Favorite is the food it has been given most, once there is one.
func (c *Care) Favorite() string { return c.top("feed:", 2) }

// FavoriteHobby is the hobby you have asked for most.
func (c *Care) FavoriteHobby() string { return c.top("hobby:", 2) }

func (c *Care) top(prefix string, least int) string {
	best, count := "", least-1
	for key, n := range c.Habits {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix && (n > count || n == count && key[len(prefix):] < best) {
			best, count = key[len(prefix):], n
		}
	}
	return best
}

// remember keeps a moment; the same thing again within a few minutes is the
// same moment.
func (c *Care) remember(now time.Time, what, arg string) {
	at := now.UnixMilli()
	if n := len(c.Memory); n > 0 {
		last := &c.Memory[n-1]
		if last.What == what && last.Arg == arg && at-last.At < int64(10*time.Minute/time.Millisecond) {
			last.At = at
			return
		}
	}
	c.Memory = append(c.Memory, Moment{At: at, What: what, Arg: arg})
	if extra := len(c.Memory) - maxMemory; extra > 0 {
		c.Memory = append([]Moment(nil), c.Memory[extra:]...)
	}
}

// visit records the page being opened; a reconnect within minutes is the same visit.
func (c *Care) visit(now time.Time) bool {
	at := now.UnixMilli()
	if c.LastVisit != 0 && at-c.LastVisit < int64(revisitGap/time.Millisecond) {
		c.LastVisit = at
		return false
	}
	c.PrevVisit, c.LastVisit = c.LastVisit, at
	c.Visits++
	return true
}

// Mood is how the cat feels, from what happened lately. The page adds night.
func (s *State) Mood(now time.Time) string {
	c := &s.Care
	at := now.UnixMilli()
	switch {
	case !s.home():
		return "away"
	case c.LastBack != 0 && at-c.LastBack < int64(30*time.Minute/time.Millisecond):
		return "excited"
	case c.PrevVisit != 0 && c.LastVisit-c.PrevVisit > int64(20*time.Hour/time.Millisecond) && at-c.LastVisit < int64(10*time.Minute/time.Millisecond):
		return "missed"
	case c.Today["feed"] >= FeedsPerDay:
		return "full"
	case c.LastCare != 0 && at-c.LastCare < int64(2*time.Hour/time.Millisecond):
		return "happy"
	}
	return "calm"
}
