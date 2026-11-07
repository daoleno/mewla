//go:build darwin && !screenfake

package screen

import (
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A probe, not a test: does a background drag leave the person's own window
// key? Cua Driver Bench counts any didResignKey of the person's window as a
// disturbance, and its first Mewla background drag (the app stays inactive, the
// pointer stays put) still made it resign for about 0.3 s. The earlier probe
// watched the helper app's own state; this one runs two copies of testdata/axprobe,
// one as the target behind and one as the person's own app in front, and counts
// the person's window's key and activation events while the target is dragged in
// each shape in dragShapeOrder, three times each.
//
//	MEWLA_DRAG_PROBE=build  go test ./screen -run DragShapeProbe -v -count=1   compile only; no screen
//	MEWLA_DRAG_PROBE=run    go test ./screen -run DragShapeProbe -v -count=1   about a minute of the screen
//
// It waits while the screen is locked and for 20 s without input, starts the two
// helper apps and quits them by pid, touches no other app, and ends with
//
//	CHOSEN: <shape>   the first shape that left the person's window key in every repeat
//	                  and moved the handle every time, or "none"
//
// The probe is only valid if the base shape does make the person's window resign
// here; if it does not, it says so and chooses none. The report also goes to
// $MEWLA_DRAG_PROBE_OUT (default $TMPDIR/mewla-dragprobe-report.txt).
func TestDragShapeProbe(t *testing.T) {
	mode := os.Getenv("MEWLA_DRAG_PROBE")
	if mode != "build" && mode != "run" {
		t.Skip("set MEWLA_DRAG_PROBE=build (compile the helper app) or run (the probe, about a minute of the screen)")
	}
	dir := t.TempDir()
	bin := buildAXProbe(t, dir)
	fmt.Println("helper app built:", bin)
	if mode == "build" {
		return
	}
	if err := loadMac(); err != nil {
		t.Fatal(err)
	}
	report := &strings.Builder{}
	say := func(format string, args ...any) {
		line := fmt.Sprintf("%s "+format, append([]any{time.Now().Format("15:04:05.000")}, args...)...)
		fmt.Println(line)
		report.WriteString(line + "\n")
	}
	defer func() {
		out := os.Getenv("MEWLA_DRAG_PROBE_OUT")
		if out == "" {
			out = filepath.Join(os.TempDir(), "mewla-dragprobe-report.txt")
		}
		_ = os.WriteFile(out, []byte(report.String()), 0o600)
		fmt.Println("report written to", out)
	}()

	backend := newBackend()
	wait, err := time.ParseDuration(os.Getenv("MEWLA_AX_PROBE_WAIT"))
	if err != nil || wait <= 0 {
		wait = 45 * time.Minute
	}
	for deadline, last := time.Now().Add(wait), time.Now(); ; time.Sleep(3 * time.Second) {
		if reporter, ok := backend.(presenceReporter); !ok || !reporter.Presence().Locked {
			break
		}
		if time.Since(last) > time.Minute {
			say("the screen is locked; waiting for it to be unlocked")
			last = time.Now()
		}
		if time.Now().After(deadline) {
			say("the screen stayed locked for %s: nothing was run", wait)
			t.Skip("locked")
		}
	}
	t.Setenv("MEWLA_ACCEPT_WAIT_IDLE", wait.String())
	if idle, ok := waitForIdle(20); !ok {
		say("the Mac was used %.0f s ago: nothing was run", idle)
		t.Skip("in use")
	}
	say("macOS %s", productVersion())

	// The target first, active once; then the person's own app over it.
	targetLog := probeLog{path: filepath.Join(dir, "target.jsonl")}
	target := exec.Command(bin, targetLog.path)
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = target.Process.Signal(syscall.SIGTERM) }()
	layout := func(l probeLog) image.Point {
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			for _, e := range l.events(0) {
				if e["ev"] == "layout" {
					return image.Pt(int(e["handleX"].(float64)), int(e["handleY"].(float64)))
				}
			}
		}
		return image.Point{}
	}
	handle0 := layout(targetLog)
	if handle0 == (image.Point{}) {
		t.Fatal("the target app never reported its layout")
	}
	time.Sleep(700 * time.Millisecond)
	personLog := probeLog{path: filepath.Join(dir, "person.jsonl")}
	person := exec.Command(bin, personLog.path, "person")
	if err := person.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = person.Process.Signal(syscall.SIGTERM) }()
	if layout(personLog) == (image.Point{}) {
		t.Fatal("the person's app never reported its layout")
	}
	time.Sleep(700 * time.Millisecond)
	if front := workspaceFrontPID(); front != person.Process.Pid {
		t.Fatalf("the person's app (%d) is not in front (%d): someone is using the Mac; nothing was dragged", person.Process.Pid, front)
	}
	say("target pid %d (handle at %v), person's app pid %d in front", target.Process.Pid, handle0, person.Process.Pid)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	session, err := backend.Start(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Close()
	mb := backend.(*macBackend)
	clicker := session.(*macSession)
	source := mac.CGEventSourceCreate(1)
	defer mac.CFRelease(source)
	parkPointer(source, cgPoint{900, 600})

	// current is where the handle's centre is now, in the view's own coordinates (it starts at 40,40).
	curX, curY := 40, 40
	count := func(l probeLog, since int64, name string) int {
		n := 0
		for _, e := range l.events(since) {
			if e["ev"] == "window" || e["ev"] == "app" {
				if strings.Contains(fmt.Sprint(e["name"]), name) {
					n++
				}
			}
		}
		return n
	}
	type result struct {
		resign, resignActive, targetActive, targetKey int
		moved                                         bool
	}
	run := func(shape dragShape, dx, dy int) result {
		window := windowOf(t, mb, target.Process.Pid)
		since := time.Now().UnixMilli()
		at := image.Pt(handle0.X+curX-40, handle0.Y+curY-40)
		if err := clicker.swipeTo(target.Process.Pid, window, at.X, at.Y, dx, dy, SwipeDrag, false, shape); err != nil {
			say("   drag: %v", err)
		}
		time.Sleep(700 * time.Millisecond)
		res := result{
			resign:       count(personLog, since, "ResignKey"),
			resignActive: count(personLog, since, "ResignActive"),
			targetActive: count(targetLog, since, "BecomeActive"),
			targetKey:    count(targetLog, since, "BecomeKey"),
		}
		for _, e := range targetLog.events(since) {
			if e["ev"] == "up" {
				var x, y int
				fmt.Sscanf(fmt.Sprint(e["handle"]), "%d,%d", &x, &y)
				if x != curX || y != curY {
					res.moved = true
					curX, curY = x, y
				}
			}
		}
		// The person's window must be key again before the next one.
		if res.resign > 0 || res.resignActive > 0 {
			_ = mb.RaiseApp(ctx, person.Process.Pid)
			time.Sleep(700 * time.Millisecond)
		}
		return res
	}

	// A control: nothing is sent; the person's window must see no key change.
	since := time.Now().UnixMilli()
	time.Sleep(1500 * time.Millisecond)
	say("control (nothing sent): person resigned key %d times, resigned active %d", count(personLog, since, "ResignKey"), count(personLog, since, "ResignActive"))

	const repeats = 3
	chosen := "none"
	baseResigns := false
	for _, name := range dragShapeOrder {
		shape := dragShapes[name]
		clean := true
		var lines []string
		for i := 0; i < repeats; i++ {
			dx, dy := 120, 30
			if i%2 == 1 {
				dx, dy = -120, -30
			}
			r := run(shape, dx, dy)
			lines = append(lines, fmt.Sprintf("person ResignKey×%d ResignActive×%d | target BecomeActive×%d BecomeKey×%d | handle moved: %v", r.resign, r.resignActive, r.targetActive, r.targetKey, r.moved))
			if r.resign > 0 || r.resignActive > 0 || !r.moved {
				clean = false
			}
			if name == "base" && (r.resign > 0 || r.resignActive > 0) {
				baseResigns = true
			}
		}
		say("---- shape %q (%+v): clean in all %d repeats: %v", name, shape, repeats, clean)
		for _, l := range lines {
			say("     %s", l)
		}
		if name == "base" && !baseResigns {
			say("the base shape did NOT make the person's window resign here, so this probe does not reproduce the benchmark's effect: no shape is chosen")
			break
		}
		if clean && chosen == "none" {
			chosen = name
		}
	}
	say("CHOSEN: %s", chosen)
}
