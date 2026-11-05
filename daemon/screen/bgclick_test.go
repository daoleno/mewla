package screen

import (
	"testing"
	"time"
)

func TestClickPlanOrderAndFields(t *testing.T) {
	global, local := fpoint{1700, 120}, fpoint{188, 50}
	steps := clickPlan(global, local, 1, true)
	want := []struct {
		kind  clickEvent
		at    fpoint
		stamp fpoint
		state int64
		phase int64
	}{
		{clickMove, global, local, 0, 2},
		{clickDown, fpoint{-1, -1}, fpoint{-1, -1}, 1, 1},
		{clickUp, fpoint{-1, -1}, fpoint{-1, -1}, 1, 2},
		{clickDown, global, local, 1, 3},
		{clickUp, global, local, 1, 3},
	}
	if len(steps) != len(want) {
		t.Fatalf("%d steps, want %d: %+v", len(steps), len(want), steps)
	}
	for i, w := range want {
		s := steps[i]
		if s.Kind != w.kind || s.At != w.at || s.Stamp != w.stamp || s.State != w.state || s.Phase != w.phase || s.Subtype != eventSubtypeTouch {
			t.Errorf("step %d = %+v, want %+v", i, s, w)
		}
	}
	if steps[2].Pause != 100*time.Millisecond || steps[4].Pause != 0 {
		t.Errorf("pauses: primer release %v, last release %v", steps[2].Pause, steps[4].Pause)
	}
}

func TestClickPlanStampIsTheGlobalPointBeforeMacOS27(t *testing.T) {
	global, local := fpoint{1700, 120}, fpoint{188, 50}
	for _, s := range clickPlan(global, local, 1, false) {
		if s.At == global && s.Stamp != global {
			t.Fatalf("stamp %v, want the global point %v", s.Stamp, global)
		}
	}
}

func TestClickPlanDoubleClickRaisesTheClickState(t *testing.T) {
	steps := clickPlan(fpoint{10, 10}, fpoint{5, 5}, 2, true)
	var states []int64
	for _, s := range steps {
		if s.Kind == clickDown && s.Phase == 3 {
			states = append(states, s.State)
		}
	}
	if len(states) != 2 || states[0] != 1 || states[1] != 2 {
		t.Fatalf("click states %v, want [1 2]", states)
	}
	if steps[4].Pause != 80*time.Millisecond {
		t.Errorf("gap between the pairs = %v", steps[4].Pause)
	}
	if got := len(clickPlan(fpoint{}, fpoint{}, 9, true)); got != 3+2*3 {
		t.Errorf("a count over three isn't clamped: %d steps", got)
	}
}

func TestFocusRecordBytes(t *testing.T) {
	r := focusRecord(0x01020304, 1)
	if r[0x04] != 0xf8 || r[0x08] != 0x0d || r[0x8a] != 1 {
		t.Fatalf("header %x %x %x", r[0x04], r[0x08], r[0x8a])
	}
	if r[0x3c] != 0x04 || r[0x3d] != 0x03 || r[0x3e] != 0x02 || r[0x3f] != 0x01 {
		t.Fatalf("window id bytes % x", r[0x3c:0x40])
	}
	for i, b := range r {
		switch i {
		case 0x04, 0x08, 0x3c, 0x3d, 0x3e, 0x3f, 0x8a:
		default:
			if b != 0 {
				t.Fatalf("byte %#x = %#x, want 0", i, b)
			}
		}
	}
	if focusRecord(7, 2)[0x8a] != 2 {
		t.Fatal("defocus marker")
	}
}

func TestMakeKeyRecordBytes(t *testing.T) {
	for _, kind := range []byte{1, 2} {
		r := makeKeyRecord(0xA0B0, kind)
		if r[0x04] != 0xf8 || r[0x08] != kind || r[0x3a] != 0x10 || r[0x3c] != 0xB0 || r[0x3d] != 0xA0 {
			t.Fatalf("kind %d: % x", kind, r[:0x40])
		}
		for i := 0x20; i < 0x30; i++ {
			if r[i] != 0xff {
				t.Fatalf("byte %#x = %#x, want ff", i, r[i])
			}
		}
	}
}

func TestWindowLocalStamp(t *testing.T) {
	for v, want := range map[string]bool{"27.0.1": true, "27": true, "26.4": false, "15.2": false, "": false, "x": false, "28.1\n": true} {
		if got := windowLocalStamp(v); got != want {
			t.Errorf("windowLocalStamp(%q) = %v, want %v", v, got, want)
		}
	}
}
