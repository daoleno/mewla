package catpark

import (
	"strconv"
	"strings"
	"time"
)

// Acts are what the person can do in the house; the page sends one with an
// argument and the daemon checks it. Anything else is ignored.
var Acts = []string{"visit", "pet", "feed", "play", "hobby", "collect", "pack", "seen", "gift", "learn"}

// ValidAct reports whether the daemon knows an act.
func ValidAct(act string) bool { return contains(Acts, act) }

// Act applies one thing the person did and reports whether the state changed.
// The page answers every tap at once; this is the part that is remembered.
func (s *State) Act(act, arg string, now time.Time) bool {
	world := World()
	changed := s.Care.begin(now)
	if s.Yard.grow(now, s.Care.Level(world)) {
		changed = true
	}
	home := s.home()
	c := &s.Care
	switch act {
	case "visit":
		if c.visit(now) {
			changed = true
		}
	case "pet":
		if !home {
			return changed
		}
		c.Habits["pet"]++
		c.Learned["pet"] = true
		c.care("pet", world, now, 0)
		c.remember(now, "pet", "")
		changed = true
	case "feed":
		if !world.isFood(arg) || s.Yard.Pantry[arg] <= 0 {
			return changed
		}
		if !home {
			// left in the bowl for when it comes home
			if c.Bowl != "" {
				return changed
			}
			s.Yard.take(arg)
			c.Bowl = arg
			c.Learned["feed"] = true
			return true
		}
		if c.Today["feed"] >= FeedsPerDay {
			return changed // full: the page shows it shake its head
		}
		s.Yard.take(arg)
		c.eat(arg, now, world)
		changed = true
	case "play":
		if !home {
			return changed
		}
		c.Habits["play"]++
		c.Learned["play"] = true
		c.care("play", world, now, 0)
		c.remember(now, "play", "")
		changed = true
	case "hobby":
		if !home || !c.knows(arg) {
			return changed
		}
		c.Habits["hobby:"+arg]++
		c.care("hobby", world, now, 0)
		c.remember(now, "hobby", arg)
		s.hobbyYield(arg, now)
		changed = true
	case "collect":
		slot, err := strconv.Atoi(arg)
		if err != nil || !s.Yard.Collect(slot) {
			return changed
		}
		c.Learned["yard"] = true
		changed = true
	case "pack":
		if arg != "" && !world.isFood(arg) {
			return changed
		}
		if s.Yard.Pack(arg) {
			c.Learned["pack"] = true
			changed = true
		}
	case "seen":
		what, id, _ := strings.Cut(arg, ":")
		if s.markSeen(what, id) {
			if what == "postcard" {
				c.Learned["mail"] = true
				c.care("read", world, now, 0)
			}
			changed = true
		}
	case "gift":
		// a visitor's little gift: it goes in the pantry
		if !world.isFood(arg) || c.Today["gift"] >= starCaps["gift"][0] || !s.Yard.add(arg) {
			return changed
		}
		c.care("gift", world, now, 0)
		c.remember(now, "gift", arg)
		changed = true
	case "learn":
		// a page step, or a hobby's prop the person has now met
		hobby, isProp := strings.CutPrefix(arg, "prop:")
		if (pageSteps[arg] || isProp && world.isHobby(hobby)) && !c.Learned[arg] {
			c.Learned[arg] = true
			changed = true
		}
	}
	return changed
}

// hobbyYield is what a hobby gives back once a day: a fish from the pond, a
// berry from the garden, a letter in the mailbox.
func (s *State) hobbyYield(hobby string, now time.Time) {
	c := &s.Care
	key := "yield:" + hobby
	if c.Today[key] > 0 {
		return
	}
	switch hobby {
	case "fish":
		if s.Yard.add("fish") {
			c.Today[key]++
		}
	case "garden":
		if s.Yard.add("berry") {
			c.Today[key]++
		}
	case "letters":
		if s.letter(now) {
			c.Today[key]++
		}
	}
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
