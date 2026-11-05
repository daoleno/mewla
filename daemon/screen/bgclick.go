package screen

import (
	"encoding/binary"
	"strconv"
	"strings"
	"time"
)

// The background click and key recipe, after trycua/cua's cua-driver (MIT;
// libs/cua-driver/rust/crates/platform-macos/src/input/skylight.rs and
// mouse.rs). A click or a key reaches an app that is not in front only when
// the app's window is made key for the app without raising it or touching
// the person's front app, and the events carry the window's routing fields.
// This file holds what has no platform code, so every byte and field is
// tested everywhere; skylight_darwin.go posts them.

// Raw CGEvent field numbers the recipe stamps (SkyLight's integer fields).
const (
	fieldGesturePhase   = 0  // kCGMouseEventNumber: move 2, primer down 1, primer up 2, target 3
	fieldClickState     = 1  // kCGMouseEventClickState
	fieldButtonNumber   = 3  // kCGMouseEventButtonNumber
	fieldSubtype        = 7  // kCGMouseEventSubtype: NSEventSubtypeTouch is 3
	fieldTargetPID      = 40 // Chromium's synthetic-event filter
	fieldWindowNumber   = 51
	fieldClickGroup     = 58 // one id across a gesture so its events coalesce
	fieldWindowUnder    = 91 // kCGMouseEventWindowUnderMousePointer
	fieldWindowUnderCan = 92 // ...ThatCanHandleThisEvent
)

const eventSubtypeTouch = 3

type fpoint struct{ X, Y float64 }

type clickEvent int

const (
	clickMove clickEvent = iota
	clickDown
	clickUp
)

// clickStep is one event of a background click.
type clickStep struct {
	Kind clickEvent
	// At is the event's own location, in global points. Stamp is what goes to
	// CGEventSetWindowLocation: the point in the window on macOS 27, the
	// global point before.
	At, Stamp fpoint
	State     int64
	Phase     int64
	Subtype   int64
	// Pause is how long to wait before the next step.
	Pause time.Duration
}

// clickPlan is the event stream for a click that reaches an app behind other
// windows: a mouse-moved at the target, so AppKit hit-tests the press at the
// right point; a press and release outside every window, which Chromium takes
// as the user activation its pages need and ignores otherwise; then the
// target's presses and releases, click state rising for a double click.
// windowLocal says whether the stamp is the point inside the window.
func clickPlan(global, local fpoint, count int, windowLocal bool) []clickStep {
	stamp := global
	if windowLocal {
		stamp = local
	}
	off := fpoint{-1, -1}
	steps := []clickStep{
		{Kind: clickMove, At: global, Stamp: stamp, Phase: 2, Subtype: eventSubtypeTouch, Pause: 15 * time.Millisecond},
		{Kind: clickDown, At: off, Stamp: off, State: 1, Phase: 1, Subtype: eventSubtypeTouch, Pause: time.Millisecond},
		{Kind: clickUp, At: off, Stamp: off, State: 1, Phase: 2, Subtype: eventSubtypeTouch, Pause: 100 * time.Millisecond},
	}
	count = max(1, min(count, 3))
	for n := 1; n <= count; n++ {
		pause := time.Duration(0)
		if n < count {
			pause = 80 * time.Millisecond
		}
		steps = append(steps,
			clickStep{Kind: clickDown, At: global, Stamp: stamp, State: int64(n), Phase: 3, Subtype: eventSubtypeTouch, Pause: time.Millisecond},
			clickStep{Kind: clickUp, At: global, Stamp: stamp, State: int64(n), Phase: 3, Subtype: eventSubtypeTouch, Pause: pause},
		)
	}
	return steps
}

// recordSize is the size of the window server's event record.
const recordSize = 0xF8

// focusRecord is the record that tells an app one of its windows gained
// (focus 1) or lost (focus 2) focus. wid is little-endian at 0x3c.
func focusRecord(wid uint32, focus byte) [recordSize]byte {
	var r [recordSize]byte
	r[0x04] = recordSize
	r[0x08] = 0x0d
	binary.LittleEndian.PutUint32(r[0x3c:], wid)
	r[0x8a] = focus
	return r
}

// makeKeyRecord is the record pair (kinds 1 and 2) that makes a window the
// app's key window.
func makeKeyRecord(wid uint32, kind byte) [recordSize]byte {
	var r [recordSize]byte
	r[0x04] = recordSize
	r[0x08] = kind
	r[0x3a] = 0x10
	binary.LittleEndian.PutUint32(r[0x3c:], wid)
	for i := 0x20; i < 0x30; i++ {
		r[i] = 0xff
	}
	return r
}

// windowLocalStamp says whether the window server hands the window-location
// stamp of an app-routed click to the app as the point inside the window:
// macOS 27 does (cua measured a screen point landing shifted by the window's
// origin); earlier releases derived the point themselves.
func windowLocalStamp(productVersion string) bool {
	major, err := strconv.Atoi(strings.SplitN(strings.TrimSpace(productVersion), ".", 2)[0])
	return err == nil && major >= 27
}

// Window server status for "no usable connection to the window server for
// that app": every focus record is dropped.
const statusInvalidConnection = 1002
