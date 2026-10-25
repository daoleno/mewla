package catpark

import (
	"fmt"
	"testing"
	"time"
)

// cards runs n Works of one kind back to back, each long enough to send both
// postcards, and returns every trip postcard.
func cards(s *State, n int, title string, from time.Time) []Postcard {
	at := from
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("w%03d", i)
		s.Apply(Observation{Now: at, Running: []RunningWork{{ID: id, Title: title}}})
		at = at.Add(StartDelay)
		s.Apply(Observation{Now: at, Running: []RunningWork{{ID: id, Title: title}}})
		at = at.Add(LongAfter)
		s.Apply(Observation{Now: at, Running: []RunningWork{{ID: id, Title: title}}})
		at = at.Add(time.Minute)
		s.Apply(Observation{Now: at, Outcomes: map[string]Outcome{id: OutcomeDone}})
		at = at.Add(MinInterval)
	}
	return tripCards(s)
}

func TestNoTwoPostcardsShowTheSameScene(t *testing.T) {
	s := seeded(t)
	all := cards(s, 30, "Release v2 to the harbour", t0)
	if len(all) != 60 {
		t.Fatalf("thirty Works send sixty postcards, got %d", len(all))
	}
	seen := map[string]string{}
	for _, card := range all {
		if other, ok := seen[card.scene()]; ok {
			t.Fatalf("%s and %s show the same scene %s", other, card.ID, card.scene())
		}
		seen[card.scene()] = card.ID
	}
}

func TestPostcardsShowOnlyWhatTheCatKnows(t *testing.T) {
	world := World()
	s := seeded(t)
	// four trips: twelve stars, level 2 at most
	for _, card := range cards(s, 4, "Fix the flaky login test", t0) {
		place, ok := world.Places[card.Place]
		if !ok || !contains(place.Acts, card.Act) {
			t.Fatalf("%s: %s is not done at %s", card.ID, card.Act, card.Place)
		}
		if world.isHobby(card.Act) && !s.Care.knows(card.Act) {
			t.Fatalf("%s shows %s, a hobby the cat had not met", card.ID, card.Act)
		}
		if place.Hour != "" && card.Time != place.Hour {
			t.Fatalf("%s is painted at %s only, the card says %s", card.Place, place.Hour, card.Time)
		}
		if contains(world.Rare, card.Place) {
			t.Fatalf("%s: rare places wait for a close cat", card.ID)
		}
		if card.Friend != "" {
			t.Fatalf("%s: friends come from level 4", card.ID)
		}
	}
}

func TestACloseCatMeetsFriendsAndRarePlaces(t *testing.T) {
	world := World()
	s := seeded(t)
	s.Care.Stars = world.Levels[7]
	var friends, rare int
	for _, card := range cards(s, 40, "Release v2 to the harbour", t0) {
		if card.Friend != "" {
			friends++
			if card.Friend == "owl" && card.Time != "night" || card.Friend == "bird" && card.Time == "night" {
				t.Fatalf("%s: an owl only at night, a bird only by day: %+v", card.ID, card)
			}
		}
		if contains(world.Rare, card.Place) {
			rare++
			if card.Phase != PhaseLong {
				t.Fatalf("%s: a rare place is a long trip's", card.ID)
			}
		}
	}
	if friends == 0 || rare == 0 {
		t.Fatalf("a level 8 cat should meet friends (%d) and find rare places (%d)", friends, rare)
	}
}

func TestAPostcardIsTheSameEveryTimeItIsComposed(t *testing.T) {
	a, b := seeded(t), seeded(t)
	first, second := cards(a, 5, "Design the new app icon", t0), cards(b, 5, "Design the new app icon", t0)
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("the same history composes the same card: %+v vs %+v", first[i], second[i])
		}
	}
}

func TestWeatherIsOneADayAndSnowOnlyInWinter(t *testing.T) {
	world := World()
	for day := 0; day < 366; day++ {
		at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local).AddDate(0, 0, day)
		weather := weatherOf(at, world)
		if weather != weatherOf(at.Add(5*time.Hour), world) {
			t.Fatalf("%s has two weathers", at.Format("2006-01-02"))
		}
		if month := at.Month(); weather == "snow" && month > time.February && month < time.December {
			t.Fatalf("snow in %s", month)
		}
	}
}
