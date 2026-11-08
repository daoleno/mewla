package watcher

import "testing"

// A poll that has just seen a pane alive knows its generation: the digest of
// the pane id. It must not start tmux to learn it.
func TestObservedPaneGenerationNeedsNoTmux(t *testing.T) {
	w := &Watcher{tmuxSocketPath: "/nonexistent/mewla-test-tmux-socket"}
	got := w.observedPaneGeneration("%73")
	if got == "" || got != sessionInputPaneGeneration("%73") {
		t.Fatalf("generation = %q, want the digest of the pane id", got)
	}
	if got == w.observedPaneGeneration("%74") {
		t.Fatal("two panes share a generation")
	}
	if generation := w.observedPaneGeneration("work:1.0"); generation != "" {
		t.Fatalf("a target that is not a pane id has generation %q", generation)
	}
}

func TestObservedPaneGenerationHonorsThePollSeam(t *testing.T) {
	w := &Watcher{pollSources: &PollSources{PaneGeneration: func(id string) string { return "seam:" + id }}}
	if got := w.observedPaneGeneration("%1"); got != "seam:%1" {
		t.Fatalf("generation = %q, want the seam's", got)
	}
}
