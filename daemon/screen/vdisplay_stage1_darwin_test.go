//go:build darwin && !screenfake

package screen

import (
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
	"unsafe"
)

// Stage 1 of the virtual display spike, run on a Mac:
//
//	MEWLA_VDISPLAY_STAGE1=<TextEdit pid> [MEWLA_VDISPLAY_SLEEP=1] go test ./screen -run VirtualDisplayStage1 -v
//
// It makes a virtual display, puts the TextEdit window on it, captures that
// display with ScreenCaptureKit, sets the text, clicks into it with the
// background click (SkyLight) at a point on the display, and reports where
// the caret went, whether the pointer or the front app moved, and (with
// MEWLA_VDISPLAY_SLEEP) whether the display and its capture survive the
// physical display sleeping. TextEdit must hold only an owned scratch file.
func TestVirtualDisplayStage1(t *testing.T) {
	pid, _ := strconv.Atoi(os.Getenv("MEWLA_VDISPLAY_STAGE1"))
	if pid <= 0 {
		t.Skip("set MEWLA_VDISPLAY_STAGE1=<TextEdit pid>")
	}
	if err := loadMac(); err != nil {
		t.Fatal(err)
	}
	spikeWindow, _ = strconv.Atoi(os.Getenv("MEWLA_VDISPLAY_WINDOW"))
	say := func(format string, args ...any) {
		fmt.Printf("%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	vd, err := newVirtualDisplay("Mewla Agent", 1600, 1000, 60, false)
	if err != nil {
		t.Fatalf("virtual display: %v", err)
	}
	defer vd.Close()
	time.Sleep(500 * time.Millisecond)
	displays, _ := activeDisplays()
	var bounds image.Rectangle
	for _, d := range displays {
		if d.ID == vd.ID {
			bounds = d.Bounds
		}
	}
	say("virtual display %d at %v", vd.ID, bounds)
	var home image.Point
	var homeKnown bool
	withPool(func() {
		home, homeKnown = frontWindowPosition(pid)
		say("move window: %v", moveFrontWindow(pid, bounds.Min.Add(image.Pt(40, 60))))
	})
	defer withPool(func() {
		if homeKnown {
			say("put it back: %v", moveFrontWindow(pid, home))
		}
	})
	time.Sleep(700 * time.Millisecond)

	// 1. Capture that display.
	captureDisplayID = func() uint32 { return vd.ID }
	defer func() { captureDisplayID = func() uint32 { return mac.CGMainDisplayID() } }()
	backend := newBackend()
	session, err := backend.Start(ctx)
	if err != nil {
		t.Fatalf("capture of the virtual display: %v", err)
	}
	defer session.Close()
	frame, err := session.Frame(ctx, 0)
	if err != nil {
		t.Fatalf("frame: %v", err)
	}
	colors := map[[3]uint8]int{}
	img := frame.Image
	for y := 0; y < img.Bounds().Dy(); y += 20 {
		for x := 0; x < img.Bounds().Dx(); x += 20 {
			o := img.PixOffset(x, y)
			colors[[3]uint8{img.Pix[o] >> 4 << 4, img.Pix[o+1] >> 4 << 4, img.Pix[o+2] >> 4 << 4}]++
		}
	}
	say("captured frame %dx%d, %d coarse colors (a blank display has 1-2)", img.Bounds().Dx(), img.Bounds().Dy(), len(colors))
	start, frames := frame.Seq, 0
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		f, err := session.Frame(ctx, frame.Seq)
		if err != nil {
			break
		}
		frame, frames = f, frames+1
	}
	say("frames in 2 s while nothing moves: %d (seq %d to %d)", frames, start, frame.Seq)

	// 2. Set the text, then click into it at a point on the display.
	apps := backend.(*macBackend)
	tree, err := apps.ReadApp(ctx, pid, 500)
	if err != nil {
		t.Fatal(err)
	}
	var area *Element
	for i := range tree.Elements {
		if tree.Elements[i].Role == "AXTextArea" {
			area = &tree.Elements[i]
		}
	}
	if area == nil {
		t.Fatal("no text area")
	}
	say("set text: %v", apps.ActOnElement(ctx, *area, "set_value", "hello world, hello again"))
	time.Sleep(300 * time.Millisecond)
	caret := func() string {
		var out string
		withPool(func() { out = selectedRange(area.ref) })
		return out
	}
	say("caret after set_value: %s", caret())
	list, _ := apps.Apps(ctx)
	var window AppWindow
	for _, a := range list {
		if a.PID == pid && len(a.Windows) > 0 {
			window = a.Windows[0]
		}
	}
	say("window %d at %v", window.ID, window.Bounds)
	beforePointer, beforeFront := pointerAt(), workspaceFrontPID()
	clicker := session.(appClicker)
	ok, why := clicker.BackgroundClicks()
	say("background clicks available: %v %s", ok, why)
	// Inside the text area, a little right of its top-left corner (the start
	// of the first line), in the display's own pixels.
	at := area.Frame.Min.Add(image.Pt(10, 10)).Sub(bounds.Min)
	say("text area frame %v", area.Frame)
	say("click at display pixel %v: %v", at, clicker.ClickTo(pid, window.ID, at.X, at.Y, 0, 1))
	time.Sleep(500 * time.Millisecond)
	say("caret after the click: %s", caret())
	afterPointer, afterFront := pointerAt(), workspaceFrontPID()
	say("pointer %v -> %v; front app %d -> %d", beforePointer, afterPointer, beforeFront, afterFront)

	// 3. The physical display sleeps.
	if os.Getenv("MEWLA_VDISPLAY_SLEEP") != "" {
		last := frame.Seq
		say("display sleep now: %v", exec.Command("pmset", "displaysleepnow").Run())
		time.Sleep(6 * time.Second)
		after, _ := activeDisplays()
		say("displays while the physical one sleeps: %+v", after)
		wake := exec.Command("caffeinate", "-u", "-t", "3")
		say("wake: %v", wake.Run())
		time.Sleep(2 * time.Second)
		fctx, fcancel := context.WithTimeout(ctx, 5*time.Second)
		f, err := session.Frame(fctx, last)
		fcancel()
		say("a frame after the wake: seq %d err %v", f.Seq, err)
		say("caret after the sleep: %s", caret())
		// The old stream is gone; a new session is what the next tool call gets.
		if err != nil {
			session.Close()
			again, err2 := backend.Start(ctx)
			say("new session after the wake: %v", err2)
			if err2 == nil {
				defer again.Close()
				cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
				f2, err3 := again.Frame(cctx, 0)
				ccancel()
				say("frame from the new session: seq %d %dx%d err %v", f2.Seq, f2.Image.Bounds().Dx(), f2.Image.Bounds().Dy(), err3)
				for _, c := range clickStyles {
					say("set text: %v", apps.ActOnElement(ctx, *area, "set_value", "hello world, hello again"))
					time.Sleep(300 * time.Millisecond)
					before := caret()
					err := again.(*macSession).clickTo(pid, window.ID, at.X, at.Y, 0, 1, c.style)
					time.Sleep(500 * time.Millisecond)
					say("%s: caret %s -> %s (click: %v)", c.name, before, caret(), err)
				}
			}
		}
	}
}

// selectedRange is the element's AXSelectedTextRange as "location,length".
func selectedRange(element uintptr) string {
	name := axLib.createString(0, "AXSelectedTextRange", cfStringEncodingUTF8)
	defer axLib.release(name)
	var value uintptr
	if code := axLib.copyAttribute(element, name, &value); code != 0 || value == 0 {
		return fmt.Sprintf("unreadable (%d)", code)
	}
	defer axLib.release(value)
	var r struct{ Location, Length int64 }
	const axValueCFRange = 4
	if !axLib.valueGetValue(value, axValueCFRange, unsafe.Pointer(&r)) {
		return "not a range"
	}
	return fmt.Sprintf("%d,%d", r.Location, r.Length)
}
