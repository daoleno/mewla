package watcher

import "testing"

// A poll that has just seen a Session alive knows its generation: the digest
// of its id. It must not ask the session host to learn it.
func TestObservedPaneGenerationNeedsNoHost(t *testing.T) {
	w := &Watcher{}
	const id = "mh-0123456789abcdef01234567"
	got := w.observedPaneGeneration(id)
	if got == "" || got != (&hostBackend{}).observedGeneration(id) {
		t.Fatalf("generation = %q, want the digest of the id", got)
	}
	if got == w.observedPaneGeneration("mh-0123456789abcdef01234568") {
		t.Fatal("two Sessions share a generation")
	}
	if generation := w.observedPaneGeneration("work:1.0"); generation != "" {
		t.Fatalf("a target that is not a Session id has generation %q", generation)
	}
}

func TestObservedPaneGenerationHonorsThePollSeam(t *testing.T) {
	w := &Watcher{pollSources: &PollSources{PaneGeneration: func(id string) string { return "seam:" + id }}}
	if got := w.observedPaneGeneration("%1"); got != "seam:%1" {
		t.Fatalf("generation = %q, want the seam's", got)
	}
}
