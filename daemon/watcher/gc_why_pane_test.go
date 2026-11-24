//go:build linux || darwin

package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// The lookup gc why gets from the watcher names the Session of a listed pane,
// and Brain's hidden host as Brain.
func TestGCWhyLookupNamesThePaneOfASessionAndBrain(t *testing.T) {
	w := New(time.Second)
	w.resources = newTestPortableResourceManager(t, "abc123")
	w.workers["mh-brain"] = &classifier.Worker{ID: "mh-brain", Name: "mh-brain (mh-brain)", Hidden: true}
	w.workers["mh-worker"] = &classifier.Worker{ID: "mh-worker", Name: "fix the build (mh-worker)"}
	w.workerOrder = []string{"mh-brain", "mh-worker"}
	w.screens.inventory([]sessionPane{{target: "mh-brain", panePID: 4100}, {target: "mh-worker", panePID: 4200}, {target: "mh-gone", panePID: 4300, dead: true}})
	lookup, ok := w.GCWhyLookup()
	if !ok || lookup.Pane == nil {
		t.Fatal("no lookup")
	}
	if s, ok := lookup.Pane(4100); !ok || !s.Brain || s.Name != "Brain" || s.ID != "mh-brain" {
		t.Fatalf("Brain's pane = %+v %v", s, ok)
	}
	if s, ok := lookup.Pane(4200); !ok || s.Brain || s.Name != "fix the build" {
		t.Fatalf("a Worker's pane = %+v %v", s, ok)
	}
	for _, pid := range []int{4300, 4400} {
		if s, ok := lookup.Pane(pid); ok {
			t.Fatalf("pane %d = %+v", pid, s)
		}
	}
}
