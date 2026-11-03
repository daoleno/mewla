//go:build darwin && !screenfake

package screen

import (
	"fmt"
	"image"
	"os"
	"strconv"
	"testing"
	"time"
)

// A spike, not a test: run on a Mac to see what a virtual display does.
//
//	MEWLA_VDISPLAY_SPIKE=<seconds to hold> [MEWLA_VDISPLAY_MOVE=<pid>] go test ./screen -run VirtualDisplaySpike -v
//
// It makes a 1600x1000 display, prints every display, moves the app's front
// window (if asked) onto it, then holds it for the seconds given, printing
// each change of the pointer's place and of the app in front, so input sent
// meanwhile (from another process) shows whether it moved the person's
// cursor or took their focus.
func TestVirtualDisplaySpike(t *testing.T) {
	hold, _ := strconv.Atoi(os.Getenv("MEWLA_VDISPLAY_SPIKE"))
	if hold <= 0 {
		t.Skip("set MEWLA_VDISPLAY_SPIKE=<seconds> to run")
	}
	// withPool needs the backend's functions loaded.
	if err := loadMac(); err != nil {
		t.Fatal(err)
	}
	say := func(format string, args ...any) {
		fmt.Printf("%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
	}
	before, err := activeDisplays()
	if err != nil {
		t.Fatal(err)
	}
	say("displays before: %+v", before)
	started := time.Now()
	vd, err := newVirtualDisplay("Mewla Agent", 1600, 1000, 60, false)
	if err != nil {
		t.Fatalf("virtual display: %v", err)
	}
	defer vd.Close()
	say("created id=%d in %s", vd.ID, time.Since(started).Round(time.Millisecond))
	time.Sleep(500 * time.Millisecond)
	after, _ := activeDisplays()
	say("displays after: %+v", after)
	var bounds image.Rectangle
	for _, d := range after {
		if d.ID == vd.ID {
			bounds = d.Bounds
		}
	}
	if bounds.Empty() {
		t.Fatal("the display isn't in the active list")
	}
	movePID, _ := strconv.Atoi(os.Getenv("MEWLA_VDISPLAY_MOVE"))
	var home image.Point
	var homeKnown bool
	if movePID > 0 {
		withPool(func() {
			home, homeKnown = frontWindowPosition(movePID)
			say("window of pid %d is at %v (known %v)", movePID, home, homeKnown)
			say("move it to %v: %v", bounds.Min, moveFrontWindow(movePID, bounds.Min.Add(image.Pt(40, 60))))
		})
	}
	var lastFront int
	var lastPointer cgPoint
	for deadline := time.Now().Add(time.Duration(hold) * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		withPool(func() {
			if front := workspaceFrontPID(); front != lastFront {
				say("front app pid %d (was %d)", front, lastFront)
				lastFront = front
			}
		})
		if p := pointerAt(); p != lastPointer {
			say("pointer %.0f,%.0f", p.X, p.Y)
			lastPointer = p
		}
	}
	if movePID > 0 && homeKnown {
		withPool(func() { say("put it back at %v: %v", home, moveFrontWindow(movePID, home)) })
	}
	say("closing")
}
