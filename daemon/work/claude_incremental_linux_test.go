//go:build linux

package work

import (
	"testing"
)

// A Worker appends to its transcript several times a second and the watcher
// parses it on each poll. An 8 MB transcript cost two 4 KiB-buffered passes
// (some 4000 read syscalls) per append; an append must cost the bytes it adds.
func TestClaudeIncrementalAppendCostsOnlyTheAppendedBytes(t *testing.T) {
	g := newClaudeGrowth(t)
	g.noLate = true
	for n := 0; n < 3000; n++ {
		g.turn(n)
	}
	g.flush()

	before := selfReadSyscalls(t)
	if _, err := parseClaudeConversationIncremental(g.path); err != nil {
		t.Fatal(err)
	}
	seed := selfReadSyscalls(t) - before

	g.turn(3000)
	g.flush()
	before = selfReadSyscalls(t)
	if _, err := parseClaudeConversationIncremental(g.path); err != nil {
		t.Fatal(err)
	}
	advance := selfReadSyscalls(t) - before

	if advance > 10 {
		t.Fatalf("an append took %d read syscalls (the first parse took %d)", advance, seed)
	}
	// The first parse of a file that is not a tail read is two passes; with a
	// large buffer that is a read per megabyte, not one per 4 KiB.
	if seed > 200 {
		t.Fatalf("the first parse took %d read syscalls", seed)
	}
}
