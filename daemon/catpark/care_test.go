package catpark

import (
	"testing"
	"time"
)

// trip sends the cat on one Work and brings it home with what it found.
func trip(s *State, id, title string, at time.Time) {
	s.Apply(Observation{Now: at, Running: []RunningWork{{ID: id, Title: title}}})
	s.Apply(Observation{Now: at.Add(time.Hour), Outcomes: map[string]Outcome{id: OutcomeDone}})
}

func TestCareStarsAreCappedEachDayAndMissingDaysCostNothing(t *testing.T) {
	s := seeded(t)
	for i := 0; i < 6; i++ {
		s.Act("pet", "", t0.Add(time.Duration(i)*time.Minute))
	}
	if s.Care.Stars != 3 {
		t.Fatalf("petting drops three stars a day at most, got %d", s.Care.Stars)
	}
	// a week away: nothing is lost, and a new day can earn again
	later := t0.Add(7 * 24 * time.Hour)
	s.Act("pet", "", later)
	if s.Care.Stars != 4 || s.Care.Today["pet"] != 1 {
		t.Fatalf("a new day starts fresh without taking anything: %+v", s.Care)
	}
}

func TestFeedingFillsTheCatAndTheBowlWaitsWhileItIsAway(t *testing.T) {
	s := seeded(t)
	if !s.Act("feed", "fish", t0) || !s.Act("feed", "fish", t0.Add(time.Minute)) {
		t.Fatal("two meals a day")
	}
	if s.Yard.Pantry["fish"] != 0 || s.Care.Stars != 2+2 {
		t.Fatalf("meals take from the pantry and drop stars: %+v %+v", s.Yard.Pantry, s.Care.Stars)
	}
	if s.Care.Favorite() != "fish" || s.Mood(t0.Add(2*time.Minute)) != "full" {
		t.Fatalf("fish is its favourite and it is full: %q %q", s.Care.Favorite(), s.Mood(t0.Add(2*time.Minute)))
	}
	s.Yard.add("berry")
	if s.Act("feed", "berry", t0.Add(3*time.Minute)) || s.Yard.Pantry["berry"] != 1 {
		t.Fatal("a full cat turns food away and the pantry keeps it")
	}

	next := t0.Add(24 * time.Hour)
	s.Apply(Observation{Now: next, Running: running("w1")})
	if !s.Act("feed", "berry", next) || s.Care.Bowl != "berry" || s.Yard.Pantry["berry"] != 0 {
		t.Fatalf("food left while it is away goes in the bowl: %+v", s.Care)
	}
	if s.Act("feed", "berry", next) {
		t.Fatal("the bowl holds one thing")
	}
	s.Apply(Observation{Now: next.Add(time.Hour), Outcomes: map[string]Outcome{"w1": OutcomeDone}})
	if s.Care.Bowl != "" || s.Care.Habits["feed:berry"] != 1 {
		t.Fatalf("home again, it eats what was left: %+v", s.Care)
	}
}

func TestHobbiesAreLearntOnTripsOnePerLevel(t *testing.T) {
	s := seeded(t)
	trip(s, "w1", "Release v2 to the harbour", t0)
	if len(s.Care.Hobbies) != 1 || s.Care.Hobbies[0] != "fish" {
		t.Fatalf("the harbour teaches fishing: %+v", s.Care.Hobbies)
	}
	s.Care.Stars = 0 // as if the first trip had brought nothing: still level 1
	trip(s, "w2", "Fix the flaky login test", t0.Add(2*time.Hour))
	if len(s.Care.Hobbies) != 1 || len(s.Care.Pending) != 1 || s.Care.Pending[0] != "tinker" || s.Souvenirs[1].Hobby != "" {
		t.Fatalf("level 1 holds one hobby; the next waits: %+v", s.Care)
	}
	// three more stars from petting make six: level 2 lets the waiting hobby in
	for i := 0; i < 3; i++ {
		s.Act("pet", "", t0.Add(4*time.Hour+time.Duration(i)*time.Minute))
	}
	if s.Care.Level(World()) != 2 || !s.Care.knows("tinker") || len(s.Care.Pending) != 0 {
		t.Fatalf("a level takes up the waiting hobby: %+v", s.Care)
	}
	var learnt, levels int
	for _, moment := range s.Care.Memory {
		switch moment.What {
		case "learn":
			learnt++
		case "level":
			levels++
		}
	}
	if learnt != 2 || levels != 1 {
		t.Fatalf("the cat remembers learning both and the level: %+v", s.Care.Memory)
	}
}

func TestHobbiesGiveBackOnceADay(t *testing.T) {
	s := seeded(t)
	if s.Act("hobby", "fish", t0) {
		t.Fatal("a hobby it does not know is ignored")
	}
	trip(s, "w1", "Release v2 to the harbour", t0)
	at := t0.Add(2 * time.Hour)
	fish := s.Yard.Pantry["fish"]
	s.Act("hobby", "fish", at)
	s.Act("hobby", "fish", at.Add(time.Minute))
	if s.Yard.Pantry["fish"] != fish+1 {
		t.Fatalf("fishing brings one fish a day: %d -> %d", fish, s.Yard.Pantry["fish"])
	}
	if s.Care.FavoriteHobby() != "fish" {
		t.Fatalf("asked twice, fishing is its favourite: %q", s.Care.FavoriteHobby())
	}
}

func TestLettersWriteOneLetterADay(t *testing.T) {
	s := seeded(t)
	s.Care.Hobbies = []string{"letters"}
	s.Act("hobby", "letters", t0)
	s.Act("hobby", "letters", t0.Add(time.Hour))
	var letters []Postcard
	for _, card := range s.Postcards {
		if card.Phase == PhaseLetter {
			letters = append(letters, card)
		}
	}
	if len(letters) != 1 || letters[0].Hobby != "letters" || letters[0].WorkID != "" {
		t.Fatalf("one letter a day, written at home: %+v", letters)
	}
	s.Act("hobby", "letters", t0.Add(24*time.Hour))
	if n := len(s.Postcards); s.Postcards[n-1].Phase != PhaseLetter || s.Postcards[n-1].ID == letters[0].ID {
		t.Fatal("a new day, a new letter")
	}
}

func TestUnknownActsAndArgumentsAreIgnored(t *testing.T) {
	s := seeded(t)
	before := s.Care.Stars
	for _, act := range [][2]string{{"style", "crayon"}, {"feed", "leaf"}, {"pack", "../x"}, {"learn", "pet"}, {"learn", "prop:gold"}, {"learn", "prop:"}, {"collect", "nine"}, {"gift", "gold"}, {"seen", "postcard:nope"}} {
		s.Act(act[0], act[1], t0)
	}
	if s.Care.Stars != before || s.Care.Learned["pet"] || s.Yard.Bag != "" {
		t.Fatalf("nothing should change: %+v", s.Care)
	}
	if ValidAct("style") || !ValidAct("feed") {
		t.Fatal("the act list is the allowlist")
	}
	if !s.Act("learn", "prop:fish", t0) || !s.Care.Learned["prop:fish"] || s.Act("learn", "prop:fish", t0) {
		t.Fatal("meeting a hobby's prop is remembered once, by the daemon")
	}
}

func TestReadingMailTeachesAndPetsWhileAwayDoNothing(t *testing.T) {
	s := seeded(t)
	if !s.Act("seen", "postcard:pc:welcome", t0) || !s.Care.Learned["mail"] || s.Care.Stars != 1 {
		t.Fatalf("reading the welcome note: %+v", s.Care)
	}
	if s.Act("seen", "postcard:pc:welcome", t0) {
		t.Fatal("a card is read once")
	}
	s.Apply(Observation{Now: t0.Add(time.Minute), Running: running("w1")})
	if s.Act("pet", "", t0.Add(time.Minute)) || s.Act("play", "", t0.Add(time.Minute)) {
		t.Fatal("there is no cat to pet while it is away")
	}
}

func TestMemoryMergesRepeatsAndStaysShort(t *testing.T) {
	var c Care
	c.remember(t0, "pet", "")
	c.remember(t0.Add(time.Minute), "pet", "")
	if len(c.Memory) != 1 {
		t.Fatalf("petting twice in a minute is one moment: %+v", c.Memory)
	}
	for i := 0; i < 100; i++ {
		c.remember(t0.Add(time.Duration(i)*time.Hour), "feed", "fish")
	}
	if len(c.Memory) != maxMemory {
		t.Fatalf("memory keeps the last %d moments, has %d", maxMemory, len(c.Memory))
	}
}

func TestVisitsAndMissingYou(t *testing.T) {
	s := seeded(t)
	s.Act("visit", "", t0)
	s.Act("visit", "", t0.Add(time.Minute))
	if s.Care.Visits != 1 {
		t.Fatalf("a reconnect is the same visit: %d", s.Care.Visits)
	}
	back := t0.Add(3 * 24 * time.Hour)
	s.Act("visit", "", back)
	if s.Care.Visits != 2 || s.Mood(back) != "missed" {
		t.Fatalf("three days later it missed you: %d %q", s.Care.Visits, s.Mood(back))
	}
	if s.Mood(back.Add(time.Hour)) == "missed" {
		t.Fatal("missing you passes once you are here")
	}
}

func TestYardGrowsWithTheBond(t *testing.T) {
	var y Yard
	y.grow(t0, 1)
	small := len(y.Treats)
	y.grow(t0.Add(24*time.Hour), 6)
	if small != TreatsPerDay || len(y.Treats) != TreatsPerDay+2 {
		t.Fatalf("a closer cat's yard grows more: %d then %d", small, len(y.Treats))
	}
}
