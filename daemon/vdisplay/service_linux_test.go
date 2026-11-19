//go:build linux

package vdisplay

import (
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func newService(t *testing.T) *Service {
	t.Helper()
	if err := Capability(); err != nil {
		t.Skip(err)
	}
	s := NewService(t.TempDir())
	t.Cleanup(s.Close)
	return s
}

// viewerInput sends the person's messages through a Watch view.
func viewerInput(t *testing.T, v *Viewer, msgs ...[]byte) {
	t.Helper()
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- v.Input(r) }()
	for _, m := range msgs {
		if _, err := w.Write(m); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	if err := <-done; err != io.EOF {
		t.Fatalf("relay: %v", err)
	}
}

func TestAgentOpensAndSeesItsDisplay(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Act(ctx, "w1", Action{Action: "screenshot"}); err == nil {
		t.Fatal("a screenshot before open should say there is no display")
	}
	r, err := s.Act(ctx, "w1", Action{Action: "open", Command: []string{"xmessage", "-center", "hello"}})
	if err != nil || len(r.Image) < 1000 || !strings.Contains(r.Text, "xmessage") {
		t.Fatalf("open: %v %d %q", err, len(r.Image), r.Text)
	}
	list := s.List()
	if len(list) != 1 || list[0].ID != "w1" || list[0].Control != "agent" || list[0].Apps != 1 {
		t.Fatalf("list = %+v", list)
	}
	if _, err := s.Act(ctx, "w1", Action{Action: "click", X: 5000, Y: 5}); err == nil {
		t.Fatal("a click outside the display was accepted")
	}
}

func TestPersonInputIsToldNotEnforced(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Act(ctx, "w1", Action{Action: "open", Command: []string{"xmessage", "hello"}}); err != nil {
		t.Fatal(err)
	}
	v, err := s.View("w1")
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	viewerInput(t, v, pointer(1, 40, 40), pointer(0, 40, 40), key(true, 'h'), key(true, 'i'), key(true, 0xff0d))
	r, err := s.Act(ctx, "w1", Action{Action: "screenshot"})
	if err != nil {
		t.Fatalf("the agent was stopped by the person's input: %v", err)
	}
	for _, want := range []string{"1 click", "2 characters typed (not shown to you)", "1 other key", "read the picture again"} {
		if !strings.Contains(r.Text, want) {
			t.Fatalf("notes missing %q: %s", want, r.Text)
		}
	}
	if strings.Contains(r.Text, "hi") && strings.Contains(r.Text, "typed hi") {
		t.Fatal("typed content leaked")
	}
	again, err := s.Act(ctx, "w1", Action{Action: "screenshot"})
	if err != nil || strings.Contains(again.Text, "The person") {
		t.Fatalf("told twice: %v %q", err, again.Text)
	}
}

func TestHoldStopAndLetGo(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Act(ctx, "w1", Action{Action: "open", Command: []string{"xmessage", "hello"}}); err != nil {
		t.Fatal(err)
	}
	v, err := s.View("w1")
	if err != nil {
		t.Fatal(err)
	}
	v.Hold(true)
	if _, err := s.Act(ctx, "w1", Action{Action: "screenshot"}); err == nil || !strings.Contains(err.Error(), "holding") {
		t.Fatalf("err = %v", err)
	}
	if got := s.List()[0].Control; got != "person" {
		t.Fatalf("control = %q", got)
	}
	time.Sleep(2100 * time.Millisecond)
	v.Close()
	r, err := s.Act(ctx, "w1", Action{Action: "screenshot"})
	if err != nil || !strings.Contains(r.Text, "handed it back") {
		t.Fatalf("hand back: %v %q", err, r.Text)
	}
	// Stop holds the agent off until the person holds and lets go, or resumes it.
	if err := s.Halt("w1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Act(ctx, "w1", Action{Action: "screenshot"}); err == nil || !strings.Contains(err.Error(), "stopped you") {
		t.Fatalf("err = %v", err)
	}
	v, err = s.View("w1")
	if err != nil {
		t.Fatal(err)
	}
	v.Hold(true)
	v.Hold(false)
	v.Close()
	if _, err := s.Act(ctx, "w1", Action{Action: "screenshot"}); err != nil {
		t.Fatalf("after hold and let go: %v", err)
	}
	if err := s.Halt("w1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Resume("w1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Act(ctx, "w1", Action{Action: "screenshot"}); err != nil {
		t.Fatalf("after resume: %v", err)
	}
}

func TestTwoWorkersGetTwoDisplays(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	for _, w := range []string{"a", "b"} {
		if _, err := s.Act(ctx, w, Action{Action: "open", Command: []string{"xmessage", w}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.List()) != 2 {
		t.Fatalf("list = %+v", s.List())
	}
	if _, err := s.Act(ctx, "a", Action{Action: "close"}); err != nil {
		t.Fatal(err)
	}
	if l := s.List(); len(l) != 1 || l[0].ID != "b" {
		t.Fatalf("list = %+v", l)
	}
}

// Two Workers share one screen: each has a monitor, an app opens on its own
// monitor, a click is relative to the monitor, and a view shows only it.
func TestWorkersShareAScreen(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	for _, w := range []string{"a", "b"} {
		if _, err := s.Act(ctx, w, Action{Action: "open", Command: []string{"xmessage", "-center", w}}); err != nil {
			t.Fatal(err)
		}
	}
	ra, _ := s.Monitor(ctx, "a")
	rb, _ := s.Monitor(ctx, "b")
	if ra == rb || ra.Dx() != MonitorWidth || rb.Dy() != MonitorHeight {
		t.Fatalf("monitors %v %v", ra, rb)
	}
	scr := s.shared
	if len(scr.client.Screens()) != 3 { // parking + two
		t.Fatalf("screens %+v", scr.client.Screens())
	}
	// xmessage -center centres on the monitor it appeared on after the mover
	// put it there: its picture is in each agent's monitor and not the other's.
	time.Sleep(2 * time.Second)
	for _, tc := range []struct {
		r    image.Rectangle
		name string
	}{{ra, "a"}, {rb, "b"}} {
		pix := scr.client.Crop(tc.r)
		if blank(pix) {
			if png, err := scr.client.Screenshot(2 * time.Second); err == nil {
				_ = os.WriteFile("/tmp/vd-debug.png", png, 0o644)
			}
			t.Fatalf("monitor of %s is empty", tc.name)
		}
	}
	if !blank(scr.client.Crop(image.Rect(0, 0, MonitorWidth, MonitorHeight))) {
		t.Fatal("something stayed on the parking monitor")
	}
	// A monitor goes with its Worker; the other keeps its place.
	s.Forget("a")
	if len(scr.client.Screens()) != 2 {
		t.Fatalf("screens after forgetting %+v", scr.client.Screens())
	}
	rb2, _ := s.Monitor(ctx, "b")
	if rb2 != rb {
		t.Fatalf("b's monitor moved: %v -> %v", rb, rb2)
	}
}

func blank(pix []byte) bool {
	first := pix[:4]
	for i := 4; i+4 <= len(pix); i += 4 {
		if pix[i] != first[0] || pix[i+1] != first[1] || pix[i+2] != first[2] {
			return false
		}
	}
	return true
}

// An agent that only uses its browser is on Screen's list as soon as its
// window opens (the bridge asks for the Worker's monitor, then reports the
// window), under its name, and it hears once who sees the window.
func TestBrowserWindowListsItsWorker(t *testing.T) {
	s := newService(t)
	s.SetNames(func(w string) string { return map[string]string{"w1": "SignIn"}[w] })
	ctx := context.Background()
	if _, err := s.Monitor(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	s.Windows("w1", 1)
	list := s.List()
	if len(list) != 1 || list[0].ID != "w1" || list[0].Name != "SignIn" || list[0].Browser != 1 || list[0].Apps != 0 {
		t.Fatalf("list = %+v", list)
	}
	if err := s.Begin("w1"); err != nil {
		t.Fatal(err)
	}
	if notes := s.End("w1"); len(notes) != 1 || notes[0] != WindowNote {
		t.Fatalf("first notes = %q", notes)
	}
	_ = s.Begin("w1")
	if notes := s.End("w1"); len(notes) != 0 {
		t.Fatalf("the window note was repeated: %q", notes)
	}
	// The person closing the window (the extension reports it) takes it off.
	s.Windows("w1", 0)
	if list := s.List(); len(list) != 0 {
		t.Fatalf("list after the window closed = %+v", list)
	}
}

// A display lasts as long as its Worker is open (SetAlive), not its turn:
// a Worker that closes takes its display with it.
func TestDisplayGoesWithItsWorker(t *testing.T) {
	s := newService(t)
	open := map[string]bool{"w1": true}
	s.SetAlive(func(w string) bool { return open[w] })
	if _, err := s.Monitor(context.Background(), "w1"); err != nil {
		t.Fatal(err)
	}
	s.Windows("w1", 1)
	if len(s.List()) != 1 {
		t.Fatalf("list = %+v", s.List())
	}
	open["w1"] = false
	if list := s.List(); len(list) != 0 {
		t.Fatalf("a closed Worker's display stayed: %+v", list)
	}
}

// A display nobody uses closes after the idle time: an open view keeps it,
// and the idle time starts again when the view closes. The Worker hears it.
func TestIdleDisplayCloses(t *testing.T) {
	s := newService(t)
	s.SetNames(func(string) string { return "SignIn" })
	var closed []string
	s.SetIdleClose(time.Hour, func(worker, name, _ string) { closed = append(closed, worker+"/"+name) })
	if _, err := s.Monitor(context.Background(), "w1"); err != nil {
		t.Fatal(err)
	}
	s.Windows("w1", 1)
	now := time.Now()
	s.SweepIdle(now.Add(30 * time.Minute))
	if len(s.List()) != 1 || len(closed) != 0 {
		t.Fatalf("closed before the idle time: %v %+v", closed, s.List())
	}
	view, err := s.View("w1")
	if err != nil {
		t.Fatal(err)
	}
	s.SweepIdle(now.Add(2 * time.Hour))
	if len(s.List()) != 1 || len(closed) != 0 {
		t.Fatalf("closed while the person had it open: %v", closed)
	}
	view.Close()
	s.SweepIdle(time.Now().Add(59 * time.Minute))
	if len(s.List()) != 1 {
		t.Fatal("closed right after the person looked at it")
	}
	s.SweepIdle(time.Now().Add(61 * time.Minute))
	if len(s.List()) != 0 || len(closed) != 1 || closed[0] != "w1/SignIn" {
		t.Fatalf("not closed when idle: %v %+v", closed, s.List())
	}
	if notes := s.End("w1"); len(notes) != 1 || !strings.Contains(notes[0], "nobody had used them for 60 minutes") {
		t.Fatalf("the Worker was not told: %q", notes)
	}
}

// Every display taken: the one idle longest whose Worker's turn ended, and
// which the person left alone, goes to the agent that asks; the person and its
// Worker are told why. With none like that, the agent hears it in words.
func TestFullScreenEvicts(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	done := map[string]bool{}
	s.SetEvictable(func(w string) bool { return done[w] })
	var closed []string
	s.SetIdleClose(time.Hour, func(worker, _, why string) { closed = append(closed, worker+": "+why) })
	for i := 1; i <= maxDisplays; i++ {
		w := fmt.Sprintf("w%d", i)
		if _, err := s.Monitor(ctx, w); err != nil {
			t.Fatal(err)
		}
		s.Windows(w, 1)
	}
	// Every Worker still working: nothing is taken from anyone.
	if _, err := s.Monitor(ctx, "late"); err == nil || !strings.Contains(err.Error(), "are in use") {
		t.Fatalf("a full screen of working agents gave a display: %v", err)
	}
	if err := s.Begin("late"); err == nil || !strings.Contains(err.Error(), "Tell the person") {
		t.Fatalf("the browser call was not refused in words: %v", err)
	}
	// Two Workers are done; the one the person touched a moment ago is kept.
	done["w2"], done["w3"] = true, true
	quiet := time.Now().Add(-2 * EvictQuiet)
	for _, id := range []string{"w2", "w3"} {
		w := s.displays[id]
		w.mu.Lock()
		w.used, w.last = quiet, quiet
		w.mu.Unlock()
	}
	s.displays["w3"].mu.Lock()
	s.displays["w3"].last = time.Now()
	s.displays["w3"].mu.Unlock()
	if err := s.Begin("late"); err != nil {
		t.Fatalf("a done Worker's display was not freed for the next agent: %v", err)
	}
	if _, err := s.Monitor(ctx, "late"); err != nil {
		t.Fatal(err)
	}
	if len(closed) != 1 || closed[0] != "w2: "+whyEvicted {
		t.Fatalf("closed = %v", closed)
	}
	if !s.InUse("w3") || s.InUse("w2") {
		t.Fatal("the wrong display was given away")
	}
	if notes := s.End("w2"); len(notes) != 1 || !strings.Contains(notes[0], whyEvicted) {
		t.Fatalf("the Worker whose display was given away was not told: %q", notes)
	}
}

// A daemon that restarts takes the agent screen back: the same display, each
// Worker's monitor where it was, listed again. A monitor whose window never
// reports again is let go after restoreGrace.
func TestRestartKeepsTheScreen(t *testing.T) {
	if err := Capability(); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	first := NewService(dir)
	ctx := context.Background()
	for _, w := range []string{"w1", "w2"} {
		if _, err := first.Monitor(ctx, w); err != nil {
			t.Fatal(err)
		}
		first.Windows(w, 1)
	}
	number := first.shared.display.Number
	first.Detach()
	second := NewService(dir)
	t.Cleanup(second.Close)
	if n := second.Adopt(); n != 2 {
		t.Fatalf("took back %d monitors, want 2", n)
	}
	if second.shared.display.Number != number || len(second.List()) != 2 {
		t.Fatalf("display :%d (want :%d), list %+v", second.shared.display.Number, number, second.List())
	}
	r1, _ := second.Monitor(ctx, "w1")
	if r1.Min.X != MonitorWidth {
		t.Fatalf("w1's monitor moved: %v", r1)
	}
	if _, err := second.View("w1"); err != nil {
		t.Fatalf("no view of the monitor taken back: %v", err)
	}
	// w1's window reports again; w2's never does.
	second.Windows("w1", 1)
	second.SweepIdle(time.Now().Add(restoreGrace + time.Second))
	if l := second.List(); len(l) != 1 || l[0].ID != "w1" {
		t.Fatalf("after the grace = %+v", l)
	}
}
