//go:build darwin && !screenfake

package screen

import (
	"context"
	"testing"
	"time"
)

// Reading with batched requests gives what reading one attribute at a time
// does. It needs a Mac with Accessibility granted to the test binary and an
// app with a window in front, so it skips elsewhere; run it with
// `go test ./screen -run TestBatchedReadsMatchSingleReads -v` while Finder or
// Notes is in front.
func TestBatchedReadsMatchSingleReads(t *testing.T) {
	if !loadAX() || !axLib.batch {
		t.Skip("accessibility or batched reads are not available here")
	}
	read := func(batched bool) (ElementTree, error) {
		set := axLib.walkSet
		if !batched {
			axLib.walkSet.array = 0
			defer func() { axLib.walkSet = set }()
		}
		return readFocusedWindow(context.Background(), elementsRead)
	}
	// Skipping happens out here: withPool locks the thread and owns an
	// autorelease pool, and the callback must return normally.
	var single, batched ElementTree
	var singleErr, batchedErr error
	var singleTook, batchedTook time.Duration
	withPool(func() {
		start := time.Now()
		single, singleErr = read(false)
		singleTook = time.Since(start)
		start = time.Now()
		batched, batchedErr = read(true)
		batchedTook = time.Since(start)
	})
	if singleErr != nil || batchedErr != nil {
		t.Skipf("no window to read: %v / %v", singleErr, batchedErr)
	}
	t.Logf("%d elements one by one in %v, %d batched in %v", len(single.Elements), singleTook, len(batched.Elements), batchedTook)
	if single.Truncated != batched.Truncated && len(single.Elements) == len(batched.Elements) {
		t.Logf("truncation differs: single %v, batched %v", single.Truncated, batched.Truncated)
	}
	n := min(len(single.Elements), len(batched.Elements))
	differ := 0
	for i := 0; i < n; i++ {
		a, b := single.Elements[i], batched.Elements[i]
		if a.Depth != b.Depth || a.Role != b.Role || a.Title != b.Title || a.Value != b.Value || a.Frame != b.Frame || a.Enabled != b.Enabled || a.Focused != b.Focused {
			differ++
			t.Logf("element %d differs:\n  single  %+v\n  batched %+v", i, a, b)
		}
	}
	// The window may change between two reads; a different tree entirely is a bug.
	if n == 0 || differ*20 > n {
		t.Fatalf("%d of %d elements differ", differ, n)
	}
}

// The attribute lists are built when accessibility loads: a read indexes
// into them, so an empty one would panic on the first element.
func TestAttributeSetsAreBuilt(t *testing.T) {
	if !loadAX() {
		t.Skip("accessibility is not available here")
	}
	for name, want := range map[string]int{"walk": 9, "print app": 3, "print window": 2} {
		set := map[string]attrSet{"walk": axLib.walkSet, "print app": axLib.printAppSet, "print window": axLib.printWindowSet}[name]
		if len(set.names) != want || (axLib.batch && set.array == 0) {
			t.Errorf("%s set: %d names (want %d), array %v, batch %v", name, len(set.names), want, set.array, axLib.batch)
		}
	}
}
