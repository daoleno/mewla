package screen

import (
	"context"
	"errors"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/vdisplay"
)

// The agent display these tests use: right of a 1512-point main display,
// captured at twice its points (a Retina-like capture), as a Mac's is.
var testAgentDisplay = image.Rect(1512, 0, 3112, 1000)

// cellOf the bridge: the second quarter of the agent display.
var testCell = image.Rect(2312, 0, 3112, 500)

type fakeWindowViewer struct {
	mu       sync.Mutex
	session  *fakeWindowSession
	holds    map[string]bool
	window   AppWindow
	pid      int
	none     bool
	focused  []uint32
	viewErr  error
	lastRect image.Rectangle
}

func (f *fakeWindowViewer) ViewAgentDisplay(_ context.Context, key string) (Session, image.Rectangle, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.viewErr != nil {
		return nil, image.Rectangle{}, nil, f.viewErr
	}
	if f.holds == nil {
		f.holds = map[string]bool{}
	}
	f.holds[key] = true
	if f.session == nil {
		f.session = newFakeWindowSession(testAgentDisplay.Dx()*2, testAgentDisplay.Dy()*2)
	}
	return f.session, testAgentDisplay, func() {
		f.mu.Lock()
		delete(f.holds, key)
		f.mu.Unlock()
	}, nil
}

func (f *fakeWindowViewer) AgentWindowAt(rect image.Rectangle) (int, AppWindow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastRect = rect
	if f.none {
		return 0, AppWindow{}, false
	}
	return f.pid, f.window, true
}

func (f *fakeWindowViewer) FocusWindow(pid int, window AppWindow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focused = append(f.focused, window.ID)
	return nil
}

// fakeWindowSession is a capture of the whole agent display whose left and
// right halves are told apart by color, and which records the input it gets.
type fakeWindowSession struct {
	mu     sync.Mutex
	img    *image.RGBA
	seq    uint64
	clicks []string
	keys   []uint32
	scroll []string
	drags  []string
}

func newFakeWindowSession(w, h int) *fakeWindowSession {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{R: 200, A: 255} // the left half
			switch {
			case x >= w/2 && x < w/2+10 && y < 10:
				c = color.RGBA{255, 255, 255, 255} // the top-right quarter's corner
			case x >= w/2 && y < h/2:
				c = color.RGBA{B: 200, A: 255} // the top-right quarter
			case x >= w/2:
				c = color.RGBA{G: 200, A: 255}
			}
			at := img.PixOffset(x, y)
			img.Pix[at], img.Pix[at+1], img.Pix[at+2], img.Pix[at+3] = c.R, c.G, c.B, c.A
		}
	}
	return &fakeWindowSession{img: img, seq: 1}
}

func (s *fakeWindowSession) Frame(ctx context.Context, after uint64) (Frame, error) {
	s.mu.Lock()
	if s.seq > after {
		defer s.mu.Unlock()
		return Frame{Seq: s.seq, Image: s.img}, nil
	}
	s.mu.Unlock()
	<-ctx.Done()
	return Frame{}, context.Cause(ctx)
}
func (s *fakeWindowSession) Pointer(int, int, uint8) error { return errors.New("not to the screen") }
func (s *fakeWindowSession) Key(uint32, bool) error        { return errors.New("not to the screen") }
func (s *fakeWindowSession) Close() error                  { return nil }
func (s *fakeWindowSession) BackgroundClicks() (bool, string) {
	return true, ""
}
func (s *fakeWindowSession) ClickTo(pid int, window AppWindow, x, y, button, count int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clicks = append(s.clicks, sprint(pid, int(window.ID), x, y, button, count))
	return nil
}
func (s *fakeWindowSession) KeyTo(pid int, keysym uint32, down bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if down {
		s.keys = append(s.keys, keysym)
	}
	return nil
}
func (s *fakeWindowSession) ScrollTo(pid int, window AppWindow, x, y, dx, dy int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scroll = append(s.scroll, sprint(x, y, dx, dy))
	return nil
}
func (s *fakeWindowSession) DragTo(pid int, window AppWindow, x, y, dx, dy int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drags = append(s.drags, sprint(x, y, dx, dy))
	return nil
}

func sprint(v ...int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = itoa(n)
	}
	return strings.Join(parts, ",")
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func newTestWindows(t *testing.T) (*AgentWindows, *fakeWindowViewer, *[]string) {
	t.Helper()
	viewer := &fakeWindowViewer{pid: 4242, window: AppWindow{ID: 77, Bounds: testCell}}
	a := newAgentWindows(viewer)
	a.SetNames(func(w string) string { return map[string]string{"mh-signin": "SignIn", "mh-other": "Research"}[w] })
	var closed []string
	var mu sync.Mutex
	a.SetCloser(func(key string) {
		mu.Lock()
		closed = append(closed, key)
		mu.Unlock()
	})
	t.Cleanup(a.Close)
	return a, viewer, &closed
}

// A Chrome-only Worker is on Screen's list as soon as the bridge says its
// window opened: under its name, the size of its place on the agent display,
// with its window count. A second window counts and becomes what is shown; a
// window that is no Worker's is not listed.
func TestAnOpenedWorkerWindowIsListedByNameRectAndCount(t *testing.T) {
	a, _, _ := newTestWindows(t)
	if list := a.List(); len(list) != 0 {
		t.Fatalf("listed before any window: %+v", list)
	}
	a.Opened("c1-1", "mh-signin", testCell)
	list := a.List()
	want := vdisplay.Info{ID: "mh-signin", Name: "SignIn", Width: 800, Height: 500, Control: "agent", Browser: 1}
	if len(list) != 1 || list[0] != want {
		t.Fatalf("list = %+v, want %+v", list, want)
	}
	if !a.InUse("mh-signin") || a.InUse("mh-other") {
		t.Fatal("InUse does not follow the open windows")
	}
	second := image.Rect(1512, 500, 2312, 1000)
	a.Opened("c1-2", "mh-signin", second)
	if list := a.List(); len(list) != 1 || list[0].Browser != 2 {
		t.Fatalf("a second window is not counted: %+v", list)
	}
	if r, _ := a.rectOf("mh-signin"); r != second {
		t.Fatalf("the view shows %v, want the newest window %v", r, second)
	}
	a.Opened("c1-3", "", testCell)
	a.Opened("c1-4", "mh-other", image.Rectangle{})
	if list := a.List(); len(list) != 1 {
		t.Fatalf("a window of no Worker (or nowhere) was listed: %+v", list)
	}
}

// The extension saying a window closed takes it off: the count drops, and the
// Worker leaves the list with its last window.
func TestAClosedWorkerWindowIsRemoved(t *testing.T) {
	a, _, closed := newTestWindows(t)
	a.Opened("c1-1", "mh-signin", testCell)
	a.Opened("c1-2", "mh-signin", image.Rect(1512, 500, 2312, 1000))
	a.Closed("c1-2")
	if list := a.List(); len(list) != 1 || list[0].Browser != 1 || list[0].Width != 800 {
		t.Fatalf("after one of two closed: %+v", list)
	}
	if r, _ := a.rectOf("mh-signin"); r != testCell {
		t.Fatalf("the view did not go back to the open window: %v", r)
	}
	a.Closed("c1-1")
	if list := a.List(); len(list) != 0 || a.InUse("mh-signin") {
		t.Fatalf("a Worker with no window is still listed: %+v", list)
	}
	a.Closed("c1-1")
	a.Closed("unknown")
	if len(*closed) != 0 {
		t.Fatalf("a window the extension closed was closed again: %v", *closed)
	}
}

// A window lasts as long as its Worker's Session: one that ends its turn keeps
// it, one that closes takes it with it (the extension is asked to close it).
func TestAWorkerWindowGoesWithItsWorker(t *testing.T) {
	a, _, closed := newTestWindows(t)
	open := map[string]bool{"mh-signin": true}
	var mu sync.Mutex
	a.SetAlive(func(w string) bool {
		mu.Lock()
		defer mu.Unlock()
		return open[w]
	})
	a.Opened("c1-1", "mh-signin", testCell)
	if len(a.List()) != 1 {
		t.Fatal("an open Worker's window is not listed")
	}
	mu.Lock()
	open["mh-signin"] = false
	mu.Unlock()
	if list := a.List(); len(list) != 0 {
		t.Fatalf("a closed Worker's window stayed: %+v", list)
	}
	if len(*closed) != 1 || (*closed)[0] != "c1-1" {
		t.Fatalf("the closed Worker's window was not closed in Chrome: %v", *closed)
	}
}

// A window nobody uses closes after the idle time and the person is told (the
// hook main wires to push.NotifyWorkerNote): an open view keeps it, the idle
// time starts again when the view closes, and the Worker hears it next call.
func TestAnIdleWorkerWindowClosesAndIsTold(t *testing.T) {
	a, _, closedKeys := newTestWindows(t)
	start := time.Now()
	now := start
	a.now = func() time.Time { return now }
	var told []string
	a.SetIdleClose(time.Hour, func(worker, name, why string) { told = append(told, worker+"/"+name+"/"+why) })
	a.Opened("c1-1", "mh-signin", testCell)
	a.SweepIdle(start.Add(30 * time.Minute))
	if len(a.List()) != 1 || len(told) != 0 {
		t.Fatalf("closed before the idle time: %v", told)
	}
	view, err := a.View("mh-signin")
	if err != nil {
		t.Fatal(err)
	}
	a.SweepIdle(start.Add(2 * time.Hour))
	if len(a.List()) != 1 || len(told) != 0 {
		t.Fatalf("closed while the person had it open: %v", told)
	}
	now = start.Add(2 * time.Hour)
	view.Close()
	a.SweepIdle(now.Add(59 * time.Minute))
	if len(a.List()) != 1 {
		t.Fatal("closed right after the person looked at it")
	}
	a.SweepIdle(now.Add(61 * time.Minute))
	if len(a.List()) != 0 || len(told) != 1 || told[0] != "mh-signin/SignIn/"+vdisplay.WhyIdle {
		t.Fatalf("not closed and told when idle: %v %+v", told, a.List())
	}
	if len(*closedKeys) != 1 || (*closedKeys)[0] != "c1-1" {
		t.Fatalf("the idle window was not closed in Chrome: %v", *closedKeys)
	}
	notes := a.End("mh-signin")
	if len(notes) != 1 || !strings.Contains(notes[0], "Mewla closed your browser window") || !strings.Contains(notes[0], vdisplay.WhyIdle) {
		t.Fatalf("the Worker was not told: %q", notes)
	}
	if notes := a.End("mh-signin"); len(notes) != 0 {
		t.Fatalf("told twice: %q", notes)
	}
}

// The person's say over the Worker's browser calls, as on Linux: Hold refuses
// them until let go, and the agent hears how long; Stop refuses them until
// resumed; what the person did in the window is told (counted, never what);
// the agent hears once that the person sees its window. A Worker with no
// window goes on as ever.
func TestHoldAndStopGovernAWorkersBrowserCalls(t *testing.T) {
	a, _, _ := newTestWindows(t)
	if err := a.Begin("mh-nowindow"); err != nil {
		t.Fatalf("a Worker with no window was refused: %v", err)
	}
	if notes := a.End("mh-nowindow"); len(notes) != 0 {
		t.Fatalf("a Worker with no window was told %q", notes)
	}
	a.Opened("c1-1", "mh-signin", testCell)
	if err := a.Begin("mh-signin"); err != nil {
		t.Fatal(err)
	}
	if notes := a.End("mh-signin"); len(notes) != 1 || notes[0] != vdisplay.WindowNote {
		t.Fatalf("first notes = %q", notes)
	}
	view, err := a.View("mh-signin")
	if err != nil {
		t.Fatal(err)
	}
	view.Hold(true)
	if list := a.List(); list[0].Control != "person" {
		t.Fatalf("a held window is not the person's: %+v", list)
	}
	if err := a.Begin("mh-signin"); !errors.Is(err, errWindowHeld) {
		t.Fatalf("a call while held: %v", err)
	}
	view.Hold(false)
	if err := a.Begin("mh-signin"); err != nil {
		t.Fatalf("a call after give back: %v", err)
	}
	if notes := a.End("mh-signin"); len(notes) != 1 || !strings.Contains(notes[0], "handed it back") {
		t.Fatalf("the hand back was not told: %q", notes)
	}
	if err := a.Halt("mh-signin"); err != nil {
		t.Fatal(err)
	}
	if list := a.List(); !list[0].Stopped {
		t.Fatalf("a stopped window is not listed stopped: %+v", list)
	}
	if err := a.Begin("mh-signin"); !errors.Is(err, errWindowHalted) {
		t.Fatalf("a call while stopped: %v", err)
	}
	if err := a.Resume("mh-signin"); err != nil {
		t.Fatal(err)
	}
	if err := a.Begin("mh-signin"); err != nil {
		t.Fatalf("a call after resume: %v", err)
	}
	a.End("mh-signin")
	// What the person types is counted for the agent, never shown to it.
	a.mu.Lock()
	a.owners["mh-signin"].input = vdisplay.Input{Clicks: 1, Keys: 5, Typed: 5}
	a.owners["mh-signin"].last = time.Now()
	a.mu.Unlock()
	notes := a.End("mh-signin")
	if len(notes) != 1 || !strings.Contains(notes[0], "1 click, 5 characters typed (not shown to you)") {
		t.Fatalf("the person's input was not told: %q", notes)
	}
	view.Close()
	if err := a.Halt("mh-gone"); err == nil {
		t.Fatal("stopped a Worker with no window")
	}
}

// A Stop outlives the window it was given on: the Worker's next window does not
// lift it, nor does having no window at all; resuming, or its Session closing,
// does.
func TestAStopOutlivesTheWorkersWindow(t *testing.T) {
	a, _, _ := newTestWindows(t)
	open := true
	a.SetAlive(func(string) bool { return open })
	a.Opened("c1-1", "mh-signin", testCell)
	if err := a.Halt("mh-signin"); err != nil {
		t.Fatal(err)
	}
	a.Closed("c1-1")
	if err := a.Begin("mh-signin"); !errors.Is(err, errWindowHalted) {
		t.Fatalf("a stopped Worker with no window was let through: %v", err)
	}
	a.Opened("c1-2", "mh-signin", testCell)
	if list := a.List(); len(list) != 1 || !list[0].Stopped {
		t.Fatalf("a new window lifted the Stop: %+v", list)
	}
	if err := a.Begin("mh-signin"); !errors.Is(err, errWindowHalted) {
		t.Fatalf("a stopped Worker's new window was let through: %v", err)
	}
	if err := a.Resume("mh-signin"); err != nil {
		t.Fatal(err)
	}
	if err := a.Begin("mh-signin"); err != nil {
		t.Fatalf("resumed: %v", err)
	}
	a.End("mh-signin")
	_ = a.Halt("mh-signin")
	a.Closed("c1-2")
	open = false
	a.List()
	if a.halted["mh-signin"] {
		t.Fatal("a closed Worker's Stop was kept")
	}
}

// The person's view is exactly the window's place on the agent display, at the
// capture's own scale, and their click and keys reach the window there: a
// click at the view's (10, 20) lands at that point of the window, keys go once
// the window is made its app's key window. A window that closes ends the view.
func TestTheWindowViewCropsToItsPlaceAndAimsInputAtIt(t *testing.T) {
	a, viewer, _ := newTestWindows(t)
	a.Opened("c1-1", "mh-signin", testCell)
	session, bounds, release, err := viewer.ViewAgentDisplay(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	owner := a.owners["mh-signin"]
	v := &windowView{inner: session, bounds: bounds, rect: func() (image.Rectangle, bool) { return a.rectOf("mh-signin") }, gone: owner.gone, viewer: viewer}
	f, err := v.Frame(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	// Twice the points: 800x500 points is 1600x1000 pixels, from (1600, 0).
	if f.Image.Rect.Dx() != 1600 || f.Image.Rect.Dy() != 1000 {
		t.Fatalf("the view is %v, want the window's 1600x1000 pixels", f.Image.Rect)
	}
	if v.crop != image.Rect(1600, 0, 3200, 1000) {
		t.Fatalf("crop %v", v.crop)
	}
	corner, inside, far := f.Image.RGBAAt(5, 5), f.Image.RGBAAt(12, 12), f.Image.RGBAAt(1599, 999)
	if corner != (color.RGBA{255, 255, 255, 255}) || inside.B != 200 || far.B != 200 || far.G != 0 {
		t.Fatalf("the view shows another part of the display: corner %v inside %v far corner %v", corner, inside, far)
	}
	_ = v.Pointer(10, 20, 1)
	_ = v.Pointer(10, 20, 0)
	fake := viewer.session
	if len(fake.clicks) != 1 || fake.clicks[0] != "4242,77,1610,20,0,1" {
		t.Fatalf("clicks %v", fake.clicks)
	}
	// A second click in the same place soon after is a double click.
	_ = v.Pointer(11, 21, 1)
	_ = v.Pointer(11, 21, 0)
	if len(fake.clicks) != 2 || !strings.HasSuffix(fake.clicks[1], ",0,2") {
		t.Fatalf("double click %v", fake.clicks)
	}
	if viewer.lastRect != testCell {
		t.Fatalf("input was aimed at %v, not the window's place", viewer.lastRect)
	}
	// Each press is aimed at the window again first (the person's own window of
	// the same app may have become its key window meanwhile); releases follow.
	_ = v.Key('h', true)
	_ = v.Key('h', false)
	_ = v.Key('i', true)
	if len(fake.keys) != 2 || len(viewer.focused) != 2 || viewer.focused[0] != 77 || viewer.focused[1] != 77 {
		t.Fatalf("keys %v focused %v", fake.keys, viewer.focused)
	}
	_ = v.Key('x', true)
	_ = v.Key('y', true)
	// A wheel notch scrolls there; a press that moved is a drag.
	_ = v.Pointer(30, 40, 16)
	_ = v.Pointer(30, 40, 0)
	_ = v.Pointer(30, 40, 1)
	_ = v.Pointer(130, 40, 1)
	_ = v.Pointer(130, 40, 0)
	if len(fake.scroll) != 1 || fake.scroll[0] != "1630,40,0,1" || len(fake.drags) != 1 || fake.drags[0] != "1630,40,100,0" {
		t.Fatalf("scroll %v drags %v", fake.scroll, fake.drags)
	}
	// No browser window there: nothing is sent anywhere.
	viewer.mu.Lock()
	viewer.none = true
	viewer.mu.Unlock()
	_ = v.Pointer(10, 20, 1)
	_ = v.Pointer(10, 20, 0)
	_ = v.Key('z', true)
	if len(fake.clicks) != 2 || len(fake.keys) != 4 {
		t.Fatalf("input went somewhere with no window: %v %v", fake.clicks, fake.keys)
	}
	// The window closing ends the view, including a wait for the next frame.
	waiting := make(chan error, 1)
	go func() {
		_, err := v.Frame(context.Background(), f.Seq)
		waiting <- err
	}()
	time.Sleep(20 * time.Millisecond)
	a.Closed("c1-1")
	select {
	case err := <-waiting:
		if !errors.Is(err, errWindowClosed) {
			t.Fatalf("the view of a closed window: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the view waited on after its window closed")
	}
}

// View serves the window as RFB and holds the agent display while it is open.
func TestViewHoldsTheAgentDisplayWhileOpen(t *testing.T) {
	a, viewer, _ := newTestWindows(t)
	if _, err := a.View("mh-signin"); !errors.Is(err, errNoWindow) {
		t.Fatalf("a view of no window: %v", err)
	}
	a.Opened("c1-1", "mh-signin", testCell)
	view, err := a.View("mh-signin")
	if err != nil {
		t.Fatal(err)
	}
	if view.Conn == nil || !viewer.holds["view:mh-signin"] {
		t.Fatalf("the view holds %v", viewer.holds)
	}
	view.Close()
	view.Close()
	if len(viewer.holds) != 0 {
		t.Fatalf("the view's hold stayed: %v", viewer.holds)
	}
	if a.owners["mh-signin"].viewers != 0 {
		t.Fatal("the view was counted after it closed")
	}
	viewer.viewErr = errors.New("no capture")
	if _, err := a.View("mh-signin"); err == nil || a.owners["mh-signin"].viewers != 0 {
		t.Fatalf("a failed view: %v, viewers %d", err, a.owners["mh-signin"].viewers)
	}
}

// Only a Chromium window wholly on the agent display, whose middle is in the
// view, gets the person's input: the front one there (a sign-in popup over
// the agent's window), never one on the person's own screen.
func TestOnlyABrowserWindowOnTheAgentDisplayIsAimedAt(t *testing.T) {
	chromium := func(pid int) bool { return pid == 10 }
	person := windowInfo{PID: 10, ID: 1, OnScreen: true, Bounds: image.Rect(0, 0, 1400, 900)}
	agent := windowInfo{PID: 10, ID: 2, OnScreen: true, Bounds: testCell}
	popup := windowInfo{PID: 10, ID: 3, OnScreen: true, Bounds: image.Rect(2412, 50, 3012, 450)}
	otherApp := windowInfo{PID: 20, ID: 4, OnScreen: true, Bounds: testCell}
	menu := windowInfo{PID: 10, ID: 5, OnScreen: true, Layer: 25, Bounds: testCell}
	if pid, w, ok := agentWindowAt([]windowInfo{person, menu, otherApp, popup, agent}, testAgentDisplay, testCell, chromium); !ok || pid != 10 || w.ID != 3 {
		t.Fatalf("picked %d %+v %v, want the popup over the agent's window", pid, w, ok)
	}
	if _, w, ok := agentWindowAt([]windowInfo{person, agent}, testAgentDisplay, testCell, chromium); !ok || w.ID != 2 {
		t.Fatalf("picked %+v %v, want the agent's window", w, ok)
	}
	// The person's window dragged half onto the agent display is not taken.
	straddling := windowInfo{PID: 10, ID: 6, OnScreen: true, Bounds: image.Rect(1400, 0, 2900, 600)}
	if _, w, ok := agentWindowAt([]windowInfo{straddling}, testAgentDisplay, testCell, chromium); ok {
		t.Fatalf("picked a window not wholly on the agent display: %+v", w)
	}
	if _, _, ok := agentWindowAt([]windowInfo{person}, testAgentDisplay, testCell, chromium); ok {
		t.Fatal("picked the person's window on their own screen")
	}
}
