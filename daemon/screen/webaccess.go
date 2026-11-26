package screen

import (
	"context"
	"sync"
	"time"
)

// An app built on Chromium (Electron apps such as Slack, VS Code and Cursor,
// and the browsers) builds the accessibility tree of its web pages only when
// something asks, so until then a read of it finds the frame and none of the
// page. Asking is a write of AXManualAccessibility on the app, read back to
// confirm; Chromium then builds the tree, which takes a moment. agent-desktop
// does the same (github.com/vercel-labs/agent-desktop, Apache-2.0; its
// renderer_activation code). The setting lasts until the app restarts, and
// the app may use more memory and processor in the meantime.

// webAccessOps is what a platform gives the switch.
type webAccessOps struct {
	// state reads the app's AXManualAccessibility: whether it is on, and
	// whether the read was answered (an app may answer only once it is set).
	state func(pid int) (on, supported bool)
	// enable writes it.
	enable func(pid int) error
	// size is a cheap count of the app's elements, to see its tree fill.
	size func(ctx context.Context, pid int) int
}

const (
	webAccessSettle = 3 * time.Second
	webAccessTick   = 200 * time.Millisecond
)

// webAccessNote is said once, in the answer that follows the switch.
const webAccessNote = "Turned on this app's web accessibility so its pages can be read; it lasts until the app restarts and may use more memory. If little shows yet, read it again in a moment.\n"

// webAccess remembers the apps it has looked at, by pid.
type webAccess struct {
	mu   sync.Mutex
	done map[int]bool
	// asked is when each app was last asked again (reassert).
	asked map[int]time.Time
}

// ensure makes sure the app with pid has built its web accessibility tree,
// and gives the note to add to the answer when it has just switched it on.
// An app without the attribute, or that won't take it, is left alone, once.
func (w *webAccess) ensure(ctx context.Context, pid int, ops webAccessOps, clock func() time.Time, sleep func(time.Duration)) string {
	w.mu.Lock()
	if w.done[pid] {
		w.mu.Unlock()
		return ""
	}
	if w.done == nil || len(w.done) > 512 {
		w.done = map[int]bool{}
	}
	w.done[pid] = true
	w.mu.Unlock()
	// An app may not list the attribute until it is set (Electron answers a
	// read only once asked), so an unsupported read still gets a try: the
	// write has to be read back as on to count.
	if on, _ := ops.state(pid); on {
		return ""
	}
	before := ops.size(ctx, pid)
	if ops.enable(pid) != nil {
		return ""
	}
	if on, supported := ops.state(pid); !on || !supported {
		return "" // not confirmed: don't claim it
	}
	// The tree fills over a second or so: wait until it has grown and
	// stopped growing, not longer than webAccessSettle.
	deadline := clock().Add(webAccessSettle)
	last, steady := before, 0
	for clock().Before(deadline) && ctx.Err() == nil {
		sleep(webAccessTick)
		n := ops.size(ctx, pid)
		if n > before && n == last {
			if steady++; steady >= 2 {
				break
			}
		} else {
			steady = 0
		}
		last = n
	}
	return webAccessNote
}

// webReassertEvery bounds how often an app that shows no page is asked again.
const webReassertEvery = 10 * time.Second

// reassert asks the app with pid to build its pages' tree again although its
// switch may read on: Chrome can stop building it for a client that is not
// a screen reader while the switch it answers stays on (the owner's Mac,
// 11-26: a Chrome up for a day showed its toolbars and none of its pages).
// At most every webReassertEvery for one app; true when asked, after the tree
// has had a moment to fill.
func (w *webAccess) reassert(ctx context.Context, pid int, ops webAccessOps, clock func() time.Time, sleep func(time.Duration)) bool {
	w.mu.Lock()
	if w.asked == nil || len(w.asked) > 512 {
		w.asked = map[int]time.Time{}
	}
	if last, ok := w.asked[pid]; ok && clock().Sub(last) < webReassertEvery {
		w.mu.Unlock()
		return false
	}
	w.asked[pid] = clock()
	w.mu.Unlock()
	before := ops.size(ctx, pid)
	if ops.enable(pid) != nil {
		return false
	}
	deadline := clock().Add(webAccessSettle)
	for clock().Before(deadline) && ctx.Err() == nil {
		sleep(webAccessTick)
		if ops.size(ctx, pid) > before {
			sleep(webAccessTick)
			break
		}
	}
	return true
}
