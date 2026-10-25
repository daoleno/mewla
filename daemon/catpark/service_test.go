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
	s.state.Enabled = true // switched on in Labs; TestServiceSwitchedOff covers off
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
	s.Act("pet", "")          // the person meets the cat
	s.Act("feed", "fish")
	c.at = c.at.Add(time.Minute)
	s.Observe(snapshotWith(delegated("w1", "Fix the login bug")))
	c.at = c.at.Add(StartDelay)
	s.Observe(snapshotWith(delegated("w1", "Fix the login bug")))
	// the welcome note is the only card so far
	if v := s.View(); !v.Presence.Away || v.Presence.Back == 0 || len(v.Postcards) != 1 {
		t.Fatalf("away with no postcard yet: %+v", v)
	}
	c.at = c.at.Add(StartDelay)
	s.Observe(snapshotWith(delegated("w1", "Fix the login bug")))
	if v := s.View(); len(v.Postcards) != 2 {
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
	if v := s.View(); len(v.Postcards) != 2 || len(views) != 0 {
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

func TestServiceActsPersistAndUnknownActsAreQuiet(t *testing.T) {
	dir := t.TempDir()
	var views []View
	c := &clock{at: t0}
	s := openTest(t, dir, c, nil, &views)
	s.Observe(snapshotWith())
	kind := s.View().Yard.Treats[1].Kind
	s.Act("collect", "1")
	s.Act("pack", kind)
	s.Act("pet", "")
	views = nil
	s.Act("style", "crayon")
	s.Act("../pet", "")
	if len(views) != 0 {
		t.Fatalf("unknown acts must not broadcast: %d", len(views))
	}
	reloaded := openTest(t, dir, c, nil, &views)
	v := reloaded.View()
	if v.Yard.Bag != kind || !v.Yard.Treats[1].Taken || v.Cat.Stars != 1 || !v.Cat.Learned["pet"] || !v.Cat.Learned["pack"] {
		t.Fatalf("acts must persist: %+v %+v", v.Yard, v.Cat)
	}
	if v.Cat.Level != 1 || v.Cat.Need <= 0 || v.Cat.Mood != "happy" || len(v.Cat.Memory) == 0 {
		t.Fatalf("the view tells the page about the cat: %+v", v.Cat)
	}
}

func TestAnOlderCatIsKeptBesideAndANewOneStarts(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "catpark")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"version":1,"seeded":true,"style":"crayon","trips":[],"postcards":[{"id":"pc:w1:start","phase":"start","kind":"workshop","variant":2}],"souvenirs":[],"yard":{}}`
	if err := os.WriteFile(filepath.Join(folder, "state.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	var views []View
	s := openTest(t, dir, &clock{at: t0}, nil, &views)
	s.Observe(snapshotWith())
	if kept, err := os.ReadFile(filepath.Join(folder, "state.json.v1")); err != nil || string(kept) != old {
		t.Fatal("the older state must be kept beside, untouched")
	}
	v := s.View()
	if len(v.Postcards) != 1 || v.Postcards[0].Phase != PhaseWelcome {
		t.Fatalf("a new cat starts with its welcome note: %+v", v.Postcards)
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

// The cat is a Labs feature: off by default it follows no Work and keeps no
// care, and the switch is remembered on the computer.
func TestServiceSwitchedOff(t *testing.T) {
	dir := t.TempDir()
	c := &clock{at: t0}
	var views []View
	s, err := Open(dir, nil, func(v View) { views = append(views, v) })
	if err != nil {
		t.Fatal(err)
	}
	s.now = c.now
	if s.View().Enabled {
		t.Fatal("the cat starts switched off")
	}
	s.Observe(brain.Snapshot{CurrentWork: []brain.CurrentWork{delegated("w1", "fix the login bug")}})
	s.Act("pet", "")
	if len(views) != 0 || s.View().Presence.Away {
		t.Fatalf("switched off, the cat does nothing: %d views", len(views))
	}
	s.SetEnabled(true)
	if len(views) != 1 || !views[0].Enabled {
		t.Fatalf("switching on is announced: %+v", views)
	}
	reopened, err := Open(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.View().Enabled {
		t.Fatal("the switch survives a restart")
	}
	s.Observe(brain.Snapshot{CurrentWork: []brain.CurrentWork{delegated("w1", "fix the login bug")}})
	if ids := s.state.OpenWorkIDs(); len(ids) != 1 || s.View().Presence.Away {
		t.Fatalf("switched on, the cat follows the running Work from home: %v", ids)
	}
	s.SetEnabled(false)
	if s.View().Enabled {
		t.Fatal("the switch turns off again")
	}
}
