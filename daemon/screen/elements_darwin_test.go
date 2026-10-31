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
	read := func(batched bool) ElementTree {
		set := axLib.walkSet
		if !batched {
			axLib.walkSet.array = 0
			defer func() { axLib.walkSet = set }()
		}
		tree, err := readFocusedWindow(context.Background(), elementsRead)
		if err != nil {
			t.Skipf("no window to read: %v", err)
		}
		return tree
	}
	var single, batched ElementTree
	var singleTook, batchedTook time.Duration
	withPool(func() {
		start := time.Now()
		single = read(false)
		singleTook = time.Since(start)
		start = time.Now()
		batched = read(true)
		batchedTook = time.Since(start)
	})
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
