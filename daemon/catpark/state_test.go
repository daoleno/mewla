package catpark

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 24, 14, 0, 0, 0, time.Local)

func running(ids ...string) []RunningWork {
	out := make([]RunningWork, 0, len(ids))
	for _, id := range ids {
		out = append(out, RunningWork{ID: id, Title: "Fix the login bug " + id})
	}
	return out
}

// tripCards are the postcards trips sent: the welcome note and letters are written at home.
func tripCards(s *State) []Postcard {
	var out []Postcard
	for _, card := range s.Postcards {
		if card.WorkID != "" {
			out = append(out, card)
		}
	}
	return out
}

// seeded returns a state that already took its first look at an idle Brain.
func seeded(t *testing.T) *State {
	t.Helper()
	s := &State{}
	s.Apply(Observation{Now: t0})
	if !s.Seeded {
		t.Fatal("first look should seed the state")
	}
	if len(s.Postcards) != 1 || s.Postcards[0].Phase != PhaseWelcome || s.Yard.Pantry["fish"] != starterFish {
		t.Fatalf("a new cat writes one welcome note and has fish to share: %+v %+v", s.Postcards, s.Yard)
	}
	// the person has met the cat: from now on it may go out
	s.Care.Learned["pet"], s.Care.Learned["feed"] = true, true
	return s
}

func TestKindOf(t *testing.T) {
	cases := map[string]Kind{
		"Fix the flaky login test":     KindWorkshop,
		"Release v0.3 to the stores":   KindHarbour,
		"Design the new app icon":      KindStudio,
		"Research competitor pricing":  KindLibrary,
		"Clean the sales dataset":      KindGarden,
		"Audit memory use":             KindObservatory,
		"Reconcile the invoice totals": KindMarket,
		"Set up the docker host":       KindKitchen,
		"Reply to the Slack thread":    KindPostOffice,
		"Sync photos to the NAS":       KindStation,
		"Draft the launch blog":        KindNight,
		"发版 0.3":                       KindHarbour,
		"整理一下房间":                       KindTrail,
		"":                             KindTrail,
		"Something unrelated":          KindTrail,
		"Build the UI for settings":    KindStudio,
		"CI is red":                    KindHarbour,
		"The suitcase":                 KindTrail, // "ui" and "ci" match whole words only
	}
	for title, want := range cases {
		if got := KindOf(title); got != want {
			t.Errorf("KindOf(%q) = %s, want %s", title, got, want)
		}
	}
	for _, kind := range Kinds {
		if SouvenirItem(kind) == "" {
			t.Errorf("kind %s has no souvenir", kind)
		}
	}
}

func TestFirstLookNeverBackfillsHistory(t *testing.T) {
	s := &State{}
	s.Apply(Observation{Now: t0, Running: running("w1", "w2")})
	if len(s.Trips) != 2 || s.Presence(false).Away {
		t.Fatalf("running Work is followed, from home: %+v", s.Trips)
	}
	s.Apply(Observation{Now: t0.Add(2 * time.Hour), Running: running("w1", "w2")})
	if len(tripCards(s)) != 0 {
		t.Fatalf("history must not send postcards: %+v", s.Postcards)
	}
}

func TestStartLeavesAndSendsOnePostcard(t *testing.T) {
	s := seeded(t)
	obs := Observation{Now: t0.Add(time.Minute), Running: running("w1")}
	s.Apply(obs)
	if s.Presence(false).Away {
		t.Fatal("a Work that has only just begun does not take the cat out")
	}
	obs.Now = obs.Now.Add(StartDelay)
	s.Apply(obs)
	if p := s.Presence(false); !p.Away || p.Kind != KindWorkshop || p.Back != obs.Now.Add(OutingLength).UnixMilli() {
		t.Fatalf("cat should be out at the workshop until it is due back: %+v", p)
	}
	if len(tripCards(s)) != 0 {
		t.Fatal("a Work that has only just begun sends nothing")
	}
	obs.Now = obs.Now.Add(StartDelay)
	if !s.Apply(obs) || len(tripCards(s)) != 1 {
		t.Fatalf("one postcard expected: %+v", s.Postcards)
	}
	card := tripCards(s)[0]
	place, ok := World().Places[card.Place]
	if card.Phase != PhaseStart || card.Kind != KindWorkshop || !ok || !contains(place.Acts, card.Act) || card.Time == "" || card.Seed == 0 {
		t.Fatalf("bad postcard %+v", card)
	}
	for step := 1; step <= 50; step++ {
		obs.Now = obs.Now.Add(30 * time.Second)
		s.Apply(obs)
	}
	if len(tripCards(s)) != 1 {
		t.Fatalf("no second postcard while the trip is young: %+v", s.Postcards)
	}
}

func TestPostcardsNeverCarryTheTitle(t *testing.T) {
	s := seeded(t)
	obs := Observation{Now: t0.Add(time.Minute), Running: []RunningWork{{ID: "w1", Title: "Rotate the AWS key sk-live-SECRET for acme-prod"}}}
	for _, step := range []time.Duration{0, StartDelay, StartDelay, LongAfter + RestLength, StartDelay} {
		obs.Now = obs.Now.Add(step)
		s.Apply(obs)
	}
	if len(tripCards(s)) != 2 {
		t.Fatalf("start and long postcards expected: %+v", s.Postcards)
	}
	wire, _ := json.Marshal(s.Postcards)
	for _, leak := range []string{"SECRET", "acme", "AWS", "sk-live"} {
		if strings.Contains(string(wire), leak) {
			t.Fatalf("postcard leaks %q: %s", leak, wire)
		}
	}
}

func TestLongWorkSendsOneMorePostcardOnce(t *testing.T) {
	s := seeded(t)
	obs := Observation{Now: t0, Running: running("w1")}
	s.Apply(obs)
	obs.Now = t0.Add(time.Minute)
	s.Apply(obs)
	obs.Now = t0.Add(LongAfter - time.Minute)
	s.Apply(obs)
	if len(tripCards(s)) != 1 {
		t.Fatalf("too early for a long-work postcard: %+v", s.Postcards)
	}
	for at := LongAfter; at < 20*time.Hour; at += 20 * time.Second {
		obs.Now = t0.Add(at)
		s.Apply(obs)
	}
	if cards := tripCards(s); len(cards) != 2 || cards[1].Phase != PhaseLong {
		t.Fatalf("exactly one start and one long postcard: %+v", s.Postcards)
	}
}

func TestAcceptedWorkComesHomeWithOneSouvenir(t *testing.T) {
	s := seeded(t)
	s.Apply(Observation{Now: t0.Add(time.Minute), Running: running("w1")})
	// The Work leaves the snapshot's current list once accepted; the durable
	// record says done.
	done := Observation{Now: t0.Add(time.Hour), Outcomes: map[string]Outcome{"w1": OutcomeDone}}
	if !s.Apply(done) {
		t.Fatal("coming home is a change")
	}
	if s.Presence(false).Away || len(s.Trips) != 0 {
		t.Fatalf("cat should be home: %+v", s.Trips)
	}
	if len(s.Souvenirs) != 1 || s.Souvenirs[0].Item != "wrench" || s.Souvenirs[0].WorkID != "w1" || s.Souvenirs[0].Hobby != "tinker" {
		t.Fatalf("one wrench souvenir that taught mending: %+v", s.Souvenirs)
	}
	if !s.Care.knows("tinker") || s.Care.Stars != 3 || s.Care.LastBack == 0 {
		t.Fatalf("home with a hobby and three stars: %+v", s.Care)
	}
	done.Now = done.Now.Add(time.Minute)
	if s.Apply(done) && len(s.Souvenirs) != 1 {
		t.Fatalf("souvenir must not repeat: %+v", s.Souvenirs)
	}
	// The same Work running again (a follow-up) does not earn a second one.
	s.Apply(Observation{Now: t0.Add(2 * time.Hour), Running: running("w1")})
	s.Apply(Observation{Now: t0.Add(3 * time.Hour), Outcomes: map[string]Outcome{"w1": OutcomeDone}})
	if len(s.Souvenirs) != 1 || len(tripCards(s)) > 2 {
		t.Fatalf("a Work earns one souvenir and at most two postcards: %+v %+v", s.Souvenirs, s.Postcards)
	}
}

func TestCancelledOrMissingWorkBringsNothingHome(t *testing.T) {
	for _, outcome := range []Outcome{OutcomeCancelled, OutcomeMissing} {
		s := seeded(t)
		s.Apply(Observation{Now: t0.Add(time.Minute), Running: running("w1")})
		s.Apply(Observation{Now: t0.Add(time.Hour), Outcomes: map[string]Outcome{"w1": outcome}})
		if s.Presence(false).Away || len(s.Souvenirs) != 0 || len(s.Care.Hobbies) != 0 || s.Care.Stars != 0 {
			t.Fatalf("%s: home with no souvenir, hobby or stars expected: %+v %+v", outcome, s.Souvenirs, s.Care)
		}
	}
}

func TestAbsenceFromSnapshotIsNotCompletion(t *testing.T) {
	s := seeded(t)
	s.Apply(Observation{Now: t0.Add(time.Minute), Running: running("w1")})
	// The Work is no longer running (the Worker's session closed) but the
	// record has no verdict yet: the trip stays open and nothing is shelved.
	s.Apply(Observation{Now: t0.Add(time.Hour)})
	if len(s.Trips) != 1 || len(s.Souvenirs) != 0 {
		t.Fatalf("cat must wait for the real verdict: %+v %+v", s.Trips, s.Souvenirs)
	}
}

func TestWaitingIsPresenceNotHistory(t *testing.T) {
	s := seeded(t)
	if p := s.Presence(true); !p.Waiting || p.Away {
		t.Fatalf("waiting at home: %+v", p)
	}
	s.Apply(Observation{Now: t0.Add(time.Minute), Running: running("w1")})
	s.Apply(Observation{Now: t0.Add(time.Minute + StartDelay), Running: running("w1")})
	if p := s.Presence(true); !p.Waiting || !p.Away {
		t.Fatalf("waiting while away: %+v", p)
	}
}

func TestRestartRepeatsNothing(t *testing.T) {
	s := seeded(t)
	obs := Observation{Now: t0.Add(time.Minute), Running: running("w1")}
	s.Apply(obs)
	obs.Now = obs.Now.Add(time.Minute)
	s.Apply(obs)
	s.Apply(Observation{Now: obs.Now.Add(time.Hour), Outcomes: map[string]Outcome{"w1": OutcomeDone}})
	wire, _ := json.Marshal(s)

	var reloaded State
	if err := json.Unmarshal(wire, &reloaded); err != nil {
		t.Fatal(err)
	}
	cards, shelf := len(reloaded.Postcards), len(reloaded.Souvenirs)
	for i := 0; i < 5; i++ {
		reloaded.Apply(Observation{Now: obs.Now.Add(2*time.Hour + time.Duration(i)*time.Minute), Outcomes: map[string]Outcome{"w1": OutcomeDone}})
	}
	if len(reloaded.Postcards) != cards || len(reloaded.Souvenirs) != shelf {
		t.Fatalf("a restart repeated something: %+v", reloaded)
	}
}

func TestBurstOfStartsNeverFloods(t *testing.T) {
	s := seeded(t)
	ids := make([]string, 25)
	for i := range ids {
		ids[i] = fmt.Sprintf("w%02d", i)
	}
	now := t0.Add(time.Minute)
	var times []int64
	for elapsed := time.Duration(0); elapsed <= 3*time.Hour; elapsed += 20 * time.Second {
		before := len(s.Postcards)
		s.Apply(Observation{Now: now.Add(elapsed), Running: running(ids...)})
		for _, card := range s.Postcards[before:] {
			times = append(times, card.At)
		}
	}
	if len(times) == 0 {
		t.Fatal("the first start should still send a postcard")
	}
	for i := 1; i < len(times); i++ {
		if gap := time.Duration(times[i]-times[i-1]) * time.Millisecond; gap < MinInterval {
			t.Fatalf("postcards %d and %d are only %s apart", i-1, i, gap)
		}
	}
	// 25 Works at once: well under 25 postcards in three hours.
	if limit := int(3*time.Hour/MinInterval) + 1; len(times) > limit {
		t.Fatalf("%d postcards in 3h exceeds %d", len(times), limit)
	}
	perWork := map[string]int{}
	for _, card := range s.Postcards {
		perWork[card.WorkID]++
	}
	for id, n := range perWork {
		if n > 2 {
			t.Fatalf("%s sent %d postcards", id, n)
		}
	}
}

func TestPackedTreatTravelsOnceAndMarksThePostcard(t *testing.T) {
	s := seeded(t)
	s.Yard.Pantry["fish"] = 2
	if !s.Yard.Pack("fish") || s.Yard.Bag != "fish" || s.Yard.Pantry["fish"] != 1 {
		t.Fatalf("pack: %+v", s.Yard)
	}
	obs := Observation{Now: t0.Add(time.Minute), Running: running("w1")}
	s.Apply(obs)
	if s.Yard.Bag != "fish" {
		t.Fatal("the bag waits until the cat goes out")
	}
	obs.Now = obs.Now.Add(StartDelay)
	s.Apply(obs)
	if s.Yard.Bag != "" || s.Trips[0].Packed != "fish" {
		t.Fatalf("the bag should leave with the cat: %+v %+v", s.Yard, s.Trips)
	}
	obs.Now = obs.Now.Add(StartDelay)
	s.Apply(obs)
	if cards := tripCards(s); len(cards) != 1 || cards[0].Treat != "fish" {
		t.Fatalf("postcard should mention the treat: %+v", s.Postcards)
	}
	s.Apply(Observation{Now: obs.Now.Add(time.Hour), Outcomes: map[string]Outcome{"w1": OutcomeDone}})
	if s.Care.Stars != 5 {
		t.Fatalf("a packed trip brings two more stars home: %d", s.Care.Stars)
	}
}

func TestYardGrowsDailyAndMissingADayCostsNothing(t *testing.T) {
	s := seeded(t)
	if len(s.Yard.Treats) != TreatsPerDay {
		t.Fatalf("treats: %+v", s.Yard)
	}
	if !s.Yard.Collect(0) || s.Yard.Collect(0) {
		t.Fatal("a treat is picked once")
	}
	kind := s.Yard.Treats[0].Kind
	if want := map[bool]int{true: starterFish + 1, false: 1}[kind == "fish"]; s.Yard.Pantry[kind] != want {
		t.Fatalf("pantry: %+v", s.Yard.Pantry)
	}
	day := s.Yard.Day
	s.Apply(Observation{Now: t0.Add(5 * 24 * time.Hour)})
	if s.Yard.Day == day || len(s.Yard.Treats) != TreatsPerDay {
		t.Fatalf("a new day grows three fresh treats: %+v", s.Yard)
	}
	for _, treat := range s.Yard.Treats {
		if treat.Taken {
			t.Fatal("fresh treats are untaken")
		}
	}
	if s.Yard.Collect(7) {
		t.Fatal("no such slot")
	}
}

func TestPackSwapsAndUnpacks(t *testing.T) {
	var y Yard
	y.Pantry = map[string]int{"fish": 1, "berry": 1}
	y.Pack("fish")
	y.Pack("berry")
	if y.Bag != "berry" || y.Pantry["fish"] != 1 || y.Pantry["berry"] != 0 {
		t.Fatalf("swap: %+v", y)
	}
	y.Pack("")
	if y.Bag != "" || y.Pantry["berry"] != 1 {
		t.Fatalf("unpack: %+v", y)
	}
	if y.Pack("leaf") {
		t.Fatal("cannot pack what the pantry lacks")
	}
}
