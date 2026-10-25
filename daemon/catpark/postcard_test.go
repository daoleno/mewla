package catpark

import (
	"fmt"
	"testing"
	"time"
)

// cards runs n Works of one kind back to back, each long enough for both
// outings (with the rest between them), and returns every trip postcard.
func cards(s *State, n int, title string, from time.Time) []Postcard {
	at := from
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("w%03d", i)
		run := func(after time.Duration) {
			s.Apply(Observation{Now: at.Add(after), Running: []RunningWork{{ID: id, Title: title}}})
		}
		long := LongAfter + RestLength
		for _, after := range []time.Duration{0, StartDelay, 2 * StartDelay, long, long + StartDelay} {
			run(after)
		}
		at = at.Add(long + StartDelay + time.Minute)
		s.Apply(Observation{Now: at, Outcomes: map[string]Outcome{id: OutcomeDone}})
		at = at.Add(RestLength)
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

func TestEverySnackHasAFriendDayAndNight(t *testing.T) {
	world := World()
	for _, friend := range world.Friends {
		if !contains(world.Foods, world.Likes[friend]) {
			t.Errorf("%s likes no food the cat has: %q", friend, world.Likes[friend])
		}
	}
	for _, food := range world.Foods {
		for _, hour := range []string{"day", "night"} {
			found := false
			for _, friend := range world.Friends {
				found = found || world.Likes[friend] == food && friendFits(friend, hour)
			}
			if !found {
				t.Errorf("no friend comes for %s by %s", food, hour)
			}
		}
	}
}

func TestTheSnackItTakesBringsAFriendWhoLikesIt(t *testing.T) {
	world := World()
	s := seeded(t) // a new cat: friends would otherwise wait for level 4
	at := t0.Add(3 * time.Hour)
	fond := 0
	for i := 0; i < 40; i++ {
		trip := &Trip{WorkID: fmt.Sprintf("s%02d", i), Kind: KindHarbour, Packed: "berry"}
		card := s.compose(trip, PhaseStart, at, world)
		if card.Friend == "" {
			continue
		}
		if world.Likes[card.Friend] != "berry" {
			t.Fatalf("%s came for berries but likes %s", card.Friend, world.Likes[card.Friend])
		}
		fond++
	}
	if fond < 15 || fond == 40 {
		t.Fatalf("a packed berry should often, not always, bring its friend: %d of 40", fond)
	}
}

func TestAFriendDropsByWhileYouAreAway(t *testing.T) {
	world := World()
	visitAfter := func(level int, away time.Duration, n int) (came int, pantry int) {
		for i := 0; i < n; i++ {
			s := seeded(t)
			s.Care.Stars = world.Levels[level-1]
			at := t0.Add(time.Duration(i) * 7 * time.Hour)
			s.Act("visit", "", at)
			s.Act("visit", "", at.Add(away))
			for _, m := range s.Care.Memory {
				if m.What == "friend" {
					came++
					if m.At <= at.UnixMilli() || m.At >= at.Add(away).UnixMilli() {
						t.Fatalf("a friend came at %d, outside the time away", m.At)
					}
					if !friendFits(m.Arg, timeOfDay(time.UnixMilli(m.At))) {
						t.Fatalf("%s came at the wrong hour", m.Arg)
					}
				}
			}
			for _, count := range s.Yard.Pantry {
				pantry += count
			}
		}
		return
	}
	if came, _ := visitAfter(4, 6*time.Hour, 30); came < 5 || came == 30 {
		t.Fatalf("a close cat's friends drop by now and then: %d of 30", came)
	}
	if came, _ := visitAfter(4, time.Hour, 20); came != 0 {
		t.Fatalf("an hour away is too short for a visitor: %d", came)
	}
	if came, _ := visitAfter(2, 6*time.Hour, 20); came != 0 {
		t.Fatalf("friends come from level 4: %d", came)
	}
}
