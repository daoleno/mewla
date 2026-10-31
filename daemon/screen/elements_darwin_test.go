//go:build darwin && !screenfake

package screen

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ebitengine/purego"
)

// requireMacAXEnv turns what a test needs from the Mac into a failure instead
// of a skip: set it while verifying on a Mac, where a skipped comparison would
// read as a pass.
const requireMacAXEnv = "MEWLA_REQUIRE_MAC_AX"

// macAXUnavailable skips, or fails when the run is a verification.
func macAXUnavailable(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv(requireMacAXEnv) != "" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format+" (set %s=1 to fail instead of skip)", append(args, requireMacAXEnv)...)
}

// processTrusted says whether this process may use accessibility. The grant
// belongs to the app a terminal, ssh or launchd job runs under, so a test run
// from a session that was never granted it reads nothing.
func processTrusted() (trusted, known bool) {
	lib, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return false, false
	}
	if _, err := purego.Dlsym(lib, "AXIsProcessTrusted"); err != nil {
		return false, false
	}
	var isTrusted func() bool
	purego.RegisterLibFunc(&isTrusted, lib, "AXIsProcessTrusted")
	return isTrusted(), true
}

// batchedReadTargets are the apps the comparison reads, in order of
// preference: ones with plenty of elements and nothing that ticks.
var batchedReadTargets = []string{"Finder", "Notes", "System Settings"}

// Reading with batched requests gives what reading one attribute at a time
// does. It reads an app picked by name from the window list, so it does not
// matter which app is in front: the terminal that runs `go test` is, and
// focus-based reads would see that. It needs a Mac, Accessibility granted to
// the process that runs the test, and Finder or Notes with a window open.
//
//	MEWLA_REQUIRE_MAC_AX=1 go test ./screen -run TestBatchedReadsMatchSingleReads -v
//
// MEWLA_REQUIRE_MAC_AX=1 makes anything missing a failure, not a skip.
func TestBatchedReadsMatchSingleReads(t *testing.T) {
	if err := loadMac(); err != nil {
		macAXUnavailable(t, "macOS frameworks are not available: %v", err)
	}
	if !loadApps() || !axLib.batch {
		macAXUnavailable(t, "accessibility or batched reads are not available here (loaded %v, batched %v)", axLib.ok, axLib.batch)
	}
	if trusted, known := processTrusted(); known && !trusted {
		macAXUnavailable(t, "this process is not trusted for Accessibility: grant it to the app that runs the test (Terminal, iTerm, or the ssh session's parent) in System Settings > Privacy & Security > Accessibility")
	}

	var (
		target                  App
		apps                    []App
		appsErr                 error
		single, batched         ElementTree
		singleErr, batchedErr   error
		singleTook, batchedTook time.Duration
	)
	read := func(batched bool) (ElementTree, error) {
		if !batched {
			set := axLib.walkSet
			axLib.walkSet.array = 0
			defer func() { axLib.walkSet = set }()
		}
		return readApp(context.Background(), target.PID, elementsRead)
	}
	// Skipping happens out here: withPool locks the thread and owns an
	// autorelease pool, and the callback must return normally.
	withPool(func() {
		if apps, appsErr = runningApps(); appsErr != nil {
			return
		}
		for _, name := range batchedReadTargets {
			for _, app := range apps {
				if app.Name == name && target.PID == 0 {
					target = app
				}
			}
		}
		if target.PID == 0 {
			return
		}
		start := time.Now()
		single, singleErr = read(false)
		singleTook = time.Since(start)
		start = time.Now()
		batched, batchedErr = read(true)
		batchedTook = time.Since(start)
	})
	defer (&macBackend{}).Release(single)
	defer (&macBackend{}).Release(batched)
	if appsErr != nil {
		macAXUnavailable(t, "can't list this Mac's windows: %v", appsErr)
	}
	if target.PID == 0 {
		names := make([]string, len(apps))
		for i, app := range apps {
			names[i] = app.Name
		}
		macAXUnavailable(t, "none of %v has a window; open one. Apps with windows: %v", batchedReadTargets, names)
	}
	if singleErr != nil || batchedErr != nil {
		macAXUnavailable(t, "reading %s (pid %d) failed: one by one: %v; batched: %v", target.Name, target.PID, singleErr, batchedErr)
	}
	if len(single.Elements) == 0 || len(batched.Elements) == 0 {
		macAXUnavailable(t, "%s (pid %d) answered with no elements: one by one %d, batched %d; it may have no window open, or accessibility is not granted", target.Name, target.PID, len(single.Elements), len(batched.Elements))
	}
	t.Logf("%s: %d elements one by one in %v, %d batched in %v", target.Name, len(single.Elements), singleTook, len(batched.Elements), batchedTook)
	if single.Truncated != batched.Truncated {
		t.Logf("truncation differs: single %v, batched %v", single.Truncated, batched.Truncated)
	}
	n := min(len(single.Elements), len(batched.Elements))
	differ := 0
	for i := 0; i < n; i++ {
		a, b := single.Elements[i], batched.Elements[i]
		if a.Depth != b.Depth || a.Role != b.Role || a.Subrole != b.Subrole || a.Title != b.Title || a.Value != b.Value || a.Frame != b.Frame || a.Enabled != b.Enabled || a.Focused != b.Focused {
			differ++
			t.Logf("element %d differs:\n  single  %+v\n  batched %+v", i, a, b)
		}
	}
	// The window may change between two reads; a different tree entirely is a bug.
	if differ*20 > n {
		t.Fatalf("%d of %d elements differ", differ, n)
	}
	t.Logf("%d of %d elements differ", differ, n)
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
