package catpark

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

func snapshotWith(items ...brain.CurrentWork) brain.Snapshot {
	return brain.Snapshot{CurrentWork: items}
}

func delegated(id, title string) brain.CurrentWork {
	return brain.CurrentWork{ID: id, Title: title, Status: brain.WorkRunning, AttemptDelegated: true}
}

type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func openTest(t *testing.T, dir string, c *clock, lookup Lookup, views *[]View) *Service {
	t.Helper()
	s, err := Open(dir, lookup, func(v View) { *views = append(*views, v) })
	if err != nil {
		t.Fatal(err)
	}
	s.now = c.now
	return s
}

func TestServiceLifecycleAndRestart(t *testing.T) {
	dir := t.TempDir()
	c := &clock{at: t0}
	var views []View
	outcomes := map[string]Outcome{}
	lookup := func(id string) Outcome { return outcomes[id] }
	s := openTest(t, dir, c, lookup, &views)

	s.Observe(snapshotWith()) // first look: idle
	c.at = c.at.Add(time.Minute)
	s.Observe(snapshotWith(delegated("w1", "Fix the login bug")))
	if v := s.View(); !v.Presence.Away || len(v.Postcards) != 0 {
		t.Fatalf("away with no postcard yet: %+v", v)
	}
	c.at = c.at.Add(StartDelay)
	s.Observe(snapshotWith(delegated("w1", "Fix the login bug")))
	if v := s.View(); len(v.Postcards) != 1 {
		t.Fatalf("one postcard: %+v", v.Postcards)
	}

	// A restart in the middle of the trip: same files, new Service.
	views = nil
	s = openTest(t, dir, c, lookup, &views)
	if !s.View().Presence.Away {
		t.Fatal("the trip must survive a restart")
	}
	for i := 0; i < 5; i++ {
		c.at = c.at.Add(20 * time.Second)
		s.Observe(snapshotWith(delegated("w1", "Fix the login bug")))
	}
	if v := s.View(); len(v.Postcards) != 1 || len(views) != 0 {
		t.Fatalf("a restart re-announced something: %d postcards, %d views", len(v.Postcards), len(views))
	}

	// Accepted: the Work leaves the snapshot; the record says done.
	outcomes["w1"] = OutcomeDone
	c.at = c.at.Add(time.Minute)
	s.Observe(snapshotWith())
	v := s.View()
	if v.Presence.Away || len(v.Souvenirs) != 1 || v.Souvenirs[0].WorkID != "w1" {
		t.Fatalf("home with a souvenir: %+v", v)
	}
	if len(views) == 0 {
		t.Fatal("coming home must be announced")
	}
}

func TestServiceWaitingFollowsTheQuestion(t *testing.T) {
	var views []View
	c := &clock{at: t0}
	s := openTest(t, t.TempDir(), c, nil, &views)
	s.Observe(snapshotWith())
	asking := brain.CurrentWork{ID: "w1", Title: "x", Status: brain.WorkNeedsInput, Question: "Which one?", Choices: []string{"A", "B"}}
	s.Observe(snapshotWith(asking))
	if !s.View().Presence.Waiting {
		t.Fatal("a question on the person makes the cat wait")
	}
	s.Observe(snapshotWith())
	if s.View().Presence.Waiting {
		t.Fatal("answered: the cat stops waiting")
	}
}

func TestServiceYardActionsPersist(t *testing.T) {
	dir := t.TempDir()
	var views []View
	c := &clock{at: t0}
	s := openTest(t, dir, c, nil, &views)
	s.Observe(snapshotWith())
	kind := s.View().Yard.Treats[1].Kind
	s.Collect(1)
	s.Pack(kind)
	reloaded := openTest(t, dir, c, nil, &views)
	if y := reloaded.View().Yard; y.Bag != kind || !y.Treats[1].Taken {
		t.Fatalf("yard actions must persist: %+v", y)
	}
}

func TestDamagedStateIsSetAside(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "catpark")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "state.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	var views []View
	openTest(t, dir, &clock{at: t0}, nil, &views)
	if _, err := os.Stat(filepath.Join(folder, "state.json.damaged")); err != nil {
		t.Fatal("the damaged file should be kept aside")
	}
}
