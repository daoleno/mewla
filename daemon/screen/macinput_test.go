package screen

import (
	"testing"
	"time"
)

func TestMacInputClicksDragsAndDoubleClicks(t *testing.T) {
	m := newMacInput(0.5)
	clock := time.Unix(0, 0)
	m.now = func() time.Time { return clock }

	events := m.pointer(100, 200, 0)
	if len(events) != 1 || events[0].Kind != macMouseMove || events[0].X != 50 || events[0].Y != 100 {
		t.Fatalf("move = %+v", events)
	}
	events = m.pointer(100, 200, 1)
	if len(events) != 1 || events[0].Kind != macMouseDown || events[0].Button != macButtonLeft || events[0].Clicks != 1 {
		t.Fatalf("down = %+v", events)
	}
	if events = m.pointer(100, 200, 0); len(events) != 1 || events[0].Kind != macMouseUp || events[0].Clicks != 1 {
		t.Fatalf("up = %+v", events)
	}
	clock = clock.Add(200 * time.Millisecond)
	if events = m.pointer(101, 201, 1); events[len(events)-1].Kind != macMouseDown || events[len(events)-1].Clicks != 2 {
		t.Fatalf("second press = %+v", events)
	}
	if events = m.pointer(101, 201, 0); events[0].Clicks != 2 {
		t.Fatalf("second release = %+v", events)
	}
	clock = clock.Add(time.Second)
	if events = m.pointer(101, 201, 1); events[0].Clicks != 1 {
		t.Fatalf("slow press = %+v", events)
	}
	events = m.pointer(300, 300, 1)
	if len(events) != 1 || events[0].Kind != macMouseDrag || events[0].Button != macButtonLeft || events[0].X != 150 {
		t.Fatalf("drag = %+v", events)
	}
	m.pointer(300, 300, 0)

	events = m.pointer(300, 300, 4)
	if len(events) != 1 || events[0].Kind != macMouseDown || events[0].Button != macButtonRight {
		t.Fatalf("right = %+v", events)
	}
	m.pointer(300, 300, 0)
}

func TestMacInputScrollsOncePerWheelPress(t *testing.T) {
	m := newMacInput(1)
	m.pointer(10, 10, 0)
	events := m.pointer(10, 10, 8)
	if len(events) != 1 || events[0].Kind != macScroll || events[0].Lines != [2]int32{macScrollLinesPerUp, 0} {
		t.Fatalf("wheel up = %+v", events)
	}
	if events = m.pointer(10, 10, 0); len(events) != 0 {
		t.Fatalf("wheel release = %+v", events)
	}
	if events = m.pointer(10, 10, 64); events[0].Lines != [2]int32{0, -macScrollLinesPerUp} {
		t.Fatalf("wheel right = %+v", events)
	}
}

func TestMacInputKeysAndModifiers(t *testing.T) {
	m := newMacInput(1)
	// Command (Super_L) + c.
	if events := m.key(0xffeb, true); events[0].KeyCode != 55 || events[0].Flags != macFlagCommand {
		t.Fatalf("command down = %+v", events)
	}
	events := m.key('c', true)
	if len(events) != 1 || events[0].KeyCode != 8 || !events[0].Down || events[0].Flags != macFlagCommand || events[0].Text != 0 {
		t.Fatalf("c down = %+v", events)
	}
	if events = m.key('c', false); events[0].KeyCode != 8 || events[0].Down {
		t.Fatalf("c up = %+v", events)
	}
	if events = m.key(0xffeb, false); events[0].Flags != 0 {
		t.Fatalf("command up = %+v", events)
	}

	// Shifted characters imply Shift; the release uses the pressed key code.
	if events = m.key('?', true); events[0].KeyCode != 44 || events[0].Flags != macFlagShift || events[0].Text != '?' {
		t.Fatalf("? = %+v", events)
	}
	if events = m.key('?', false); events[0].KeyCode != 44 || events[0].Down {
		t.Fatalf("? up = %+v", events)
	}
	if events = m.key('A', true); events[0].KeyCode != 0 || events[0].Flags != macFlagShift {
		t.Fatalf("A = %+v", events)
	}
	m.key('A', false)

	if events = m.key(0xff0d, true); events[0].KeyCode != 36 {
		t.Fatalf("Return = %+v", events)
	}
	m.key(0xff0d, false)

	// Characters off the ANSI layout are typed as text, once per press.
	if events = m.key(0x01004f60, true); len(events) != 1 || events[0].Kind != macText || events[0].Text != '你' {
		t.Fatalf("text = %+v", events)
	}
	if events = m.key(0x01004f60, false); len(events) != 0 {
		t.Fatalf("text release = %+v", events)
	}
	if events = m.key(0xe9, true); events[0].Kind != macText || events[0].Text != 'é' {
		t.Fatalf("latin-1 = %+v", events)
	}
	if events = m.key(0xfe03, true); len(events) != 0 {
		t.Fatalf("unknown keysym = %+v", events)
	}
}
