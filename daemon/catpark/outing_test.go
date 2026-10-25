package catpark

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// A Brain busy all day: a new Work every 20 minutes, each running 50, so two
// or three overlap at any time, and now and then one that is over in seconds.
// The cat looks every 20 seconds, as the daemon does.
func TestBusyDayTheCatIsMostlyHome(t *testing.T) {
	s := seeded(t)
	type work struct {
		id         string
		start, end time.Duration
	}
	var works []work
	for at := time.Duration(0); at < 12*time.Hour; at += 20 * time.Minute {
		works = append(works, work{fmt.Sprintf("w%03d", len(works)), at, at + 50*time.Minute})
	}
	quick := map[string]bool{}
	for at := 7 * time.Minute; at < 12*time.Hour; at += 90 * time.Minute {
		id := fmt.Sprintf("q%03d", len(quick))
		quick[id] = true
		works = append(works, work{id, at, at + 10*time.Second})
	}

	samples, home, outings := 0, 0, 0
	var last *Outing
	for at := time.Duration(0); at <= 13*time.Hour; at += 20 * time.Second {
		obs := Observation{Now: t0.Add(at), Outcomes: map[string]Outcome{}}
		for _, w := range works {
			switch {
			case at >= w.start && at < w.end:
				obs.Running = append(obs.Running, RunningWork{ID: w.id, Title: "Release " + w.id + " to the harbour"})
			case at >= w.end:
				obs.Outcomes[w.id] = OutcomeDone
			}
		}
		s.Apply(obs)
		samples++
		if s.Presence(false).Away {
			if quick[s.Out.WorkID] {
				t.Fatalf("a Work over in seconds took the cat out: %+v", s.Out)
			}
			if last == nil || last.Since != s.Out.Since {
				outings++
			}
			last = s.Out
		} else {
			home++
		}
	}
	share := float64(home) / float64(samples)
	t.Logf("home on %.0f%% of looks, %d outings, %d souvenirs", share*100, outings, len(s.Souvenirs))
	if share < 0.7 {
		t.Fatalf("home on %.0f%% of looks, want most of them", share*100)
	}
	if outings == 0 || outings > 13 {
		t.Fatalf("%d outings in a busy twelve hours: it should go out, about once an hour at most", outings)
	}
	if len(s.Souvenirs) != len(works) {
		t.Fatalf("every accepted Work brings its souvenir: %d of %d", len(s.Souvenirs), len(works))
	}
}

func TestTheFirstVisitStartsWithTheCatHome(t *testing.T) {
	s := &State{}
	s.Apply(Observation{Now: t0})
	for at := time.Minute; at < 3*time.Hour; at += 20 * time.Second {
		s.Apply(Observation{Now: t0.Add(at), Running: running("w1")})
		if s.Presence(false).Away {
			t.Fatalf("the cat went out before its person met it (at %s)", at)
		}
	}
	// petted but not yet fed: still in
	s.Act("pet", "", t0.Add(3*time.Hour))
	s.Apply(Observation{Now: t0.Add(3*time.Hour + time.Minute), Running: running("w1")})
	if s.Presence(false).Away {
		t.Fatal("the first visit's steps at home come first")
	}
	// met: now it may go
	s.Act("feed", "fish", t0.Add(3*time.Hour+2*time.Minute))
	s.Apply(Observation{Now: t0.Add(3*time.Hour + 3*time.Minute), Running: running("w1")})
	if p := s.Presence(false); !p.Away {
		t.Fatalf("once met, it goes out on the Work: %+v", p)
	}
}

func TestAnOutingEndsOnItsOwnAndTheCatRests(t *testing.T) {
	s := seeded(t)
	obs := Observation{Now: t0, Running: running("w1")}
	s.Apply(obs)
	obs.Now = t0.Add(StartDelay)
	s.Apply(obs)
	if !s.Presence(false).Away {
		t.Fatal("out on the Work")
	}
	obs.Now = t0.Add(StartDelay + OutingLength)
	s.Apply(obs)
	if s.Presence(false).Away || len(s.Trips) != 1 || s.Care.Memory[len(s.Care.Memory)-1].What != "home" {
		t.Fatalf("home on its own, the Work still followed: %+v %+v", s.Presence(false), s.Trips)
	}
	// a fresh Work while it rests does not take it out again
	obs.Running = running("w1", "w2")
	for at := obs.Now; at.Before(obs.Now.Add(RestLength - time.Minute)); at = at.Add(20 * time.Second) {
		s.Apply(Observation{Now: at, Running: obs.Running})
		if s.Presence(false).Away {
			t.Fatalf("out again while resting, at %s", at.Sub(t0))
		}
	}
	// accepted while home: the souvenir still comes, and stars
	s.Apply(Observation{Now: obs.Now.Add(RestLength), Outcomes: map[string]Outcome{"w1": OutcomeDone, "w2": OutcomeDone}})
	if len(s.Souvenirs) != 2 || s.Presence(false).Away {
		t.Fatalf("souvenirs land with the cat at home: %+v", s.Souvenirs)
	}
}

func TestAcceptingTheWorkItIsOutOnBringsItHome(t *testing.T) {
	s := seeded(t)
	s.Apply(Observation{Now: t0, Running: running("w1", "w2")})
	s.Apply(Observation{Now: t0.Add(StartDelay), Running: running("w1", "w2")})
	other := "w1"
	if s.Out.WorkID == "w1" {
		other = "w2"
	}
	// the other Work accepted: its souvenir lands, the cat stays out
	s.Apply(Observation{Now: t0.Add(time.Minute), Running: running(s.Out.WorkID), Outcomes: map[string]Outcome{other: OutcomeDone}})
	if !s.Presence(false).Away || len(s.Souvenirs) != 1 {
		t.Fatalf("another Work's souvenir does not call the cat home: %+v", s.Souvenirs)
	}
	s.Apply(Observation{Now: t0.Add(2 * time.Minute), Outcomes: map[string]Outcome{s.Out.WorkID: OutcomeDone}})
	if s.Presence(false).Away || len(s.Souvenirs) != 2 || s.Care.Memory[len(s.Care.Memory)-1].What != "back" {
		t.Fatalf("home with the souvenir: %+v %+v", s.Souvenirs, s.Care.Memory)
	}
}

// A state written before outings: an open trip and nothing else. The cat is
// home, and the trip is still followed for its souvenir.
func TestAnOlderStateWithAnOpenTripReadsHome(t *testing.T) {
	var s State
	old := `{"version":2,"enabled":true,"seeded":true,"trips":[{"work_id":"f5","kind":"trail","title":"cat","opened_at":1792904718542,"start_done":true,"long_done":true}],"postcards":[],"souvenirs":[],"yard":{},"care":{"learned":{"feed":true}}}`
	if err := json.Unmarshal([]byte(old), &s); err != nil {
		t.Fatal(err)
	}
	if s.Presence(false).Away {
		t.Fatal("an old open trip must not leave the room empty")
	}
	now := time.UnixMilli(1792904718542).Add(4 * time.Hour)
	s.Apply(Observation{Now: now, Running: []RunningWork{{ID: "f5", Title: "cat"}}})
	if s.Presence(false).Away || len(s.Trips) != 1 {
		t.Fatalf("still home, still following: %+v %+v", s.Out, s.Trips)
	}
	s.Apply(Observation{Now: now.Add(time.Hour), Outcomes: map[string]Outcome{"f5": OutcomeDone}})
	if len(s.Souvenirs) != 1 {
		t.Fatal("its souvenir comes when the Work is accepted")
	}
}
