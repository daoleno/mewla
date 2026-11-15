package screen

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/vdisplay"
)

// AgentWindows are the Workers' browser windows on a Mac's agent display, as
// Screen lists them: the same contract as Linux's agent screen (vdisplay), one
// entry per Worker, so the app's list, its live view, Hold, Stop and the Work
// card's Screen button work the same on both. The window is the extension's,
// in the person's own Chrome; the bridge places it in a quarter of the agent
// display and says so (Opened), and the extension's closed report takes it off
// (Closed). The person's view is that quarter of the display, and what they do
// there reaches that window only, without the pointer or the app in front.
//
// A window lasts as long as its Worker's Session (SetAlive), not its turn: a
// page it left for the person (a sign-in) waits for them. It closes when the
// Worker closes, when the person closes it, or after IdleClose nobody used it.
type AgentWindows struct {
	viewer agentDisplayViewer

	mu      sync.Mutex
	windows map[string]*agentWindow // by the bridge's connection id
	owners  map[string]*windowOwner // by Worker, while it has a window
	// halted are the Workers the person stopped: a Stop outlives the window
	// (a new one does not lift it) and lasts until resumed, held and let go,
	// or the Worker's Session closes.
	halted map[string]bool
	seq    uint64
	alive  func(worker string) bool
	names  func(worker string) string
	closer func(key string)

	idleAfter  time.Duration
	idleClosed func(worker, name, why string)
	stop       chan struct{}
	// closed are the Workers whose windows Mewla closed, for their next
	// browser call to hear.
	closed map[string]windowsClosed

	now func() time.Time
}

// agentDisplayViewer is a backend that can show the person a part of its agent
// display and pass their input to the window there.
type agentDisplayViewer interface {
	// ViewAgentDisplay starts a capture of the whole agent display, holding the
	// display (under key) until release, and says where it is in global points.
	// The session's input methods (appClicker, appKeyer, ...) reach one app.
	ViewAgentDisplay(ctx context.Context, key string) (session Session, bounds image.Rectangle, release func(), err error)
	// AgentWindowAt is the front browser window on rect of the agent display,
	// with its app's pid, or false when none lies there.
	AgentWindowAt(rect image.Rectangle) (pid int, window AppWindow, ok bool)
	// FocusWindow makes window its app's key window without raising it or
	// touching the app the person is in, so keys sent to the app reach it.
	FocusWindow(pid int, window AppWindow) error
	// WindowPlace is where the browser window given rect really is: macOS
	// keeps a window below a display's menu bar, so one placed at the top of
	// the display sits lower than asked. False when none of rect's size lies
	// on it.
	WindowPlace(rect image.Rectangle) (image.Rectangle, bool)
}

// appScroller is a session that can scroll in one app's window behind others.
type appScroller interface {
	ScrollTo(pid int, window AppWindow, x, y, dx, dy int) error
}

type agentWindow struct {
	key, worker string
	rect        image.Rectangle // global points
	seq         uint64          // the order windows opened in
}

// windowOwner is one Worker's say over its windows, as a vdisplay monitor's.
type windowOwner struct {
	used     time.Time // the agent's last browser call, or when a window opened
	last     time.Time // the person's last input
	seen     time.Time // when the person last closed a view
	held     time.Time
	handback time.Time
	viewers  int
	person   int  // views that hold the agent off
	acting   int  // browser calls that are running
	told     bool // the agent heard that the person sees its window
	during   bool // the person's input landed while a call ran
	input    vdisplay.Input
	gone     chan struct{} // closed when its last window goes
}

type windowsClosed struct {
	at  time.Time
	why string
}

var (
	errWindowHalted = errors.New("The person stopped you from using the browser. Do not ask for it again; tell them what is done and what is left.")
	errWindowHeld   = errors.New("The person is holding your browser window. Do not poll; stop and report, then try again later when they let go.")
	errNoWindow     = errors.New("This agent has no browser window on the agent display")
	errWindowClosed = errors.New("The browser window closed")
)

// newAgentWindows makes the registry for a backend with an agent display.
func newAgentWindows(viewer agentDisplayViewer) *AgentWindows {
	return &AgentWindows{viewer: viewer, windows: map[string]*agentWindow{}, owners: map[string]*windowOwner{}, halted: map[string]bool{}, closed: map[string]windowsClosed{}, now: time.Now}
}

// AgentWindows is the registry of agents' browser windows on this computer's
// agent display, or nil when it has none (Linux has its agent screen instead).
// The Service keeps one; every call returns it.
func (s *Service) AgentWindows() *AgentWindows {
	viewer, ok := s.backend.(agentDisplayViewer)
	if !ok {
		return nil
	}
	if parker, isParker := s.backend.(appParker); isParker && !parker.AgentDisplayAvailable() {
		return nil
	}
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if s.agentWindows == nil {
		s.agentWindows = newAgentWindows(viewer)
	}
	return s.agentWindows
}

// Capability says why agents have no browser window here, or nil.
func (a *AgentWindows) Capability() error { return nil }

// SetAlive tells which Workers are still open; a closed Worker's windows close.
func (a *AgentWindows) SetAlive(alive func(worker string) bool) {
	a.mu.Lock()
	a.alive = alive
	a.mu.Unlock()
}

// SetNames tells the Workers' names, for Screen's list.
func (a *AgentWindows) SetNames(names func(worker string) string) {
	a.mu.Lock()
	a.names = names
	a.mu.Unlock()
}

// SetCloser is how a window is closed: the bridge asks the extension.
func (a *AgentWindows) SetCloser(closer func(key string)) {
	a.mu.Lock()
	a.closer = closer
	a.mu.Unlock()
}

// Opened lists a Worker's window, open at rect (global points) on the agent
// display. A window that is no Worker's is not listed.
func (a *AgentWindows) Opened(key, worker string, rect image.Rectangle) {
	if key == "" || worker == "" || rect.Empty() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	a.windows[key] = &agentWindow{key: key, worker: worker, rect: rect, seq: a.seq}
	owner := a.owners[worker]
	if owner == nil {
		owner = &windowOwner{gone: make(chan struct{})}
		a.owners[worker] = owner
	}
	owner.used = a.now()
}

// Closed takes a window off the list: the extension said it closed (the
// person closed it, the agent finished, Mewla asked).
func (a *AgentWindows) Closed(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w := a.windows[key]
	if w == nil {
		return
	}
	delete(a.windows, key)
	if len(a.windowsOfLocked(w.worker)) == 0 {
		a.dropOwnerLocked(w.worker)
	}
}

func (a *AgentWindows) dropOwnerLocked(worker string) {
	if owner := a.owners[worker]; owner != nil {
		close(owner.gone)
		delete(a.owners, worker)
	}
}

// windowsOfLocked are a Worker's windows, the newest first.
func (a *AgentWindows) windowsOfLocked(worker string) []*agentWindow {
	var out []*agentWindow
	for _, w := range a.windows {
		if w.worker == worker {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq > out[j].seq })
	return out
}

// rectOf is where the person's view of a Worker shows: its newest window.
func (a *AgentWindows) rectOf(worker string) (image.Rectangle, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if list := a.windowsOfLocked(worker); len(list) > 0 {
		return list[0].rect, true
	}
	return image.Rectangle{}, false
}

// Forget closes a Worker's windows and takes them off the list.
func (a *AgentWindows) Forget(worker string) {
	a.mu.Lock()
	var keys []string
	for _, w := range a.windowsOfLocked(worker) {
		keys = append(keys, w.key)
		delete(a.windows, w.key)
	}
	a.dropOwnerLocked(worker)
	closer := a.closer
	a.mu.Unlock()
	if closer != nil {
		for _, key := range keys {
			closer(key)
		}
	}
}

// sweepGone closes the windows of Workers that are no longer open.
func (a *AgentWindows) sweepGone() {
	a.mu.Lock()
	alive := a.alive
	workers := make([]string, 0, len(a.owners))
	for worker := range a.owners {
		workers = append(workers, worker)
	}
	var stopped []string
	for worker := range a.halted {
		if a.owners[worker] == nil {
			stopped = append(stopped, worker)
		}
	}
	a.mu.Unlock()
	if alive == nil {
		return
	}
	for _, worker := range workers {
		if !alive(worker) {
			a.Forget(worker)
			a.mu.Lock()
			delete(a.halted, worker)
			a.mu.Unlock()
		}
	}
	for _, worker := range stopped {
		if !alive(worker) {
			a.mu.Lock()
			delete(a.halted, worker)
			a.mu.Unlock()
		}
	}
}

// List is every Worker with a window, in a stable order.
func (a *AgentWindows) List() []vdisplay.Info {
	a.sweepGone()
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	out := []vdisplay.Info{}
	for worker, owner := range a.owners {
		list := a.windowsOfLocked(worker)
		if len(list) == 0 {
			continue
		}
		rect := list[0].rect
		info := vdisplay.Info{ID: worker, Control: "agent", Stopped: a.halted[worker], Width: rect.Dx(), Height: rect.Dy(), Browser: len(list)}
		if a.names != nil {
			info.Name = a.names(worker)
		}
		if owner.person > 0 {
			info.Control = "person"
		}
		if owner.acting == 0 {
			info.IdleS = int(now.Sub(owner.used).Seconds())
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// InUse says whether the Worker has a browser window open on the agent display.
func (a *AgentWindows) InUse(worker string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.owners[worker] != nil
}

// Begin starts one browser call of the Worker: refused while the person holds
// or has stopped it. A Worker with no window here goes on as ever.
func (a *AgentWindows) Begin(worker string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.halted[worker] {
		return errWindowHalted
	}
	owner := a.owners[worker]
	if owner == nil {
		return nil
	}
	if owner.person > 0 {
		return errWindowHeld
	}
	owner.acting++
	owner.used = a.now()
	return nil
}

// End closes a Begin and returns what the agent should hear: that Mewla closed
// its window, what the person did in it, and once, that the person sees it.
func (a *AgentWindows) End(worker string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	if closed, ok := a.closed[worker]; ok {
		delete(a.closed, worker)
		out = append(out, fmt.Sprintf("Mewla closed your browser window at %s: %s. Open the page again if it is still needed.", closed.at.Format("15:04"), closed.why))
	}
	owner := a.owners[worker]
	if owner == nil {
		return out
	}
	now := a.now()
	if owner.acting > 0 {
		owner.acting--
	}
	owner.used = now
	if !owner.handback.IsZero() {
		out = append(out, fmt.Sprintf("The person had your browser window for about %s and handed it back.", roundAbout(owner.handback.Sub(owner.held))))
		owner.handback, owner.held = time.Time{}, time.Time{}
	}
	if !owner.input.Empty() {
		out = append(out, personInputNote(owner.input, now.Sub(owner.last), owner.during))
		owner.input, owner.during = vdisplay.Input{}, false
	}
	if !owner.told {
		owner.told = true
		out = append(out, vdisplay.WindowNote)
	}
	return out
}

func personInputNote(in vdisplay.Input, ago time.Duration, during bool) string {
	var did []string
	add := func(n int, one, many string) {
		if n == 1 {
			did = append(did, "1 "+one)
		} else if n > 1 {
			did = append(did, fmt.Sprintf("%d %s", n, many))
		}
	}
	add(in.Clicks, "click", "clicks")
	add(in.Scrolls, "scroll", "scrolls")
	add(in.Typed, "character typed (not shown to you)", "characters typed (not shown to you)")
	add(in.Keys-in.Typed, "other key", "other keys")
	add(in.Pastes, "paste (not shown to you)", "pastes (not shown to you)")
	text := fmt.Sprintf("The person used your browser window in Screen: %s, the last %s ago.", strings.Join(did, ", "), roundAbout(ago))
	if during {
		text += " Some of it landed while your browser call was running, so it may not have done what you meant."
	}
	return text + " A field may hold something you did not put there; read the page again and judge whether it matters before you go on."
}

func roundAbout(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d seconds", max(1, int(d.Round(time.Second).Seconds())))
	case d < 2*time.Minute:
		return "a minute"
	}
	return fmt.Sprintf("%d minutes", int(d.Round(time.Minute).Minutes()))
}

// Halt is Stop: the Worker's browser calls are refused until the person holds
// its window and lets go, or resumes it.
func (a *AgentWindows) Halt(worker string) error { return a.setHalted(worker, true) }

// Resume lifts a Stop.
func (a *AgentWindows) Resume(worker string) error { return a.setHalted(worker, false) }

func (a *AgentWindows) setHalted(worker string, on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !on {
		delete(a.halted, worker)
		return nil
	}
	if a.owners[worker] == nil {
		return errNoWindow
	}
	a.halted[worker] = true
	return nil
}

// SetIdleClose closes, from now on, the windows of every Worker nobody has used
// for after: no browser call, no input from the person, no view of it open.
// closed is told first (to tell the person); the Worker hears it with its next
// browser call. The same sweep closes the windows of Workers that closed.
func (a *AgentWindows) SetIdleClose(after time.Duration, closed func(worker, name, why string)) {
	a.mu.Lock()
	a.idleAfter, a.idleClosed = after, closed
	start := a.stop == nil
	if start {
		a.stop = make(chan struct{})
	}
	stop := a.stop
	a.mu.Unlock()
	if !start {
		return
	}
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				a.sweepGone()
				a.SweepIdle(now)
			}
		}
	}()
}

// SweepIdle closes the windows of every Worker nobody used for the idle time.
func (a *AgentWindows) SweepIdle(now time.Time) {
	a.mu.Lock()
	after, closed, names := a.idleAfter, a.idleClosed, a.names
	var idle []string
	for worker, owner := range a.owners {
		since := owner.used
		for _, t := range []time.Time{owner.last, owner.seen} {
			if t.After(since) {
				since = t
			}
		}
		if after > 0 && owner.acting == 0 && owner.person == 0 && owner.viewers == 0 && now.Sub(since) >= after {
			idle = append(idle, worker)
		}
	}
	a.mu.Unlock()
	for _, worker := range idle {
		name := ""
		if names != nil {
			name = names(worker)
		}
		if closed != nil {
			closed(worker, name, vdisplay.WhyIdle)
		}
		a.Forget(worker)
		a.mu.Lock()
		a.closed[worker] = windowsClosed{at: now, why: vdisplay.WhyIdle}
		a.mu.Unlock()
	}
}

// Close stops the sweep. The windows stay: they are the extension's.
func (a *AgentWindows) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stop != nil {
		close(a.stop)
		a.stop = nil
	}
}

// View is the person's live view of the Worker's newest window: that part of
// the agent display, as RFB past the handshake. What they do reaches the window
// at once and is told to the agent with its next browser call; Hold holds the
// agent off instead until released or the view closes.
func (a *AgentWindows) View(worker string) (*vdisplay.Viewer, error) {
	a.mu.Lock()
	owner := a.owners[worker]
	if owner != nil {
		owner.viewers++
	}
	a.mu.Unlock()
	if owner == nil {
		return nil, errNoWindow
	}
	let := func() {
		a.mu.Lock()
		owner.viewers--
		owner.seen = a.now() // watching counts as using it: the idle time starts when the view closes
		a.mu.Unlock()
	}
	ctx, cancel := context.WithCancel(context.Background())
	session, bounds, release, err := a.viewer.ViewAgentDisplay(ctx, "view:"+worker)
	if err != nil {
		cancel()
		let()
		return nil, err
	}
	view := &windowView{
		inner:  session,
		bounds: bounds,
		rect:   func() (image.Rectangle, bool) { return a.rectOf(worker) },
		gone:   owner.gone,
		viewer: a.viewer,
	}
	conn, server := net.Pipe()
	go func() {
		defer server.Close()
		_ = serveRFB(ctx, server, view, a.nameOf(worker), nil)
	}()
	holding := false // guarded by a.mu
	hold := func(on bool) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if on == holding {
			return
		}
		holding = on
		if on {
			if owner.person == 0 {
				owner.held = a.now()
			}
			owner.person++
			delete(a.halted, worker) // holding lifts a Stop, as on Linux
			return
		}
		owner.person--
		if owner.person == 0 {
			owner.handback = a.now()
		}
	}
	var once sync.Once
	closeView := func() {
		once.Do(func() {
			cancel()
			_ = conn.Close()
			_ = session.Close()
			release()
			hold(false)
			let()
		})
	}
	return &vdisplay.Viewer{
		Conn: conn,
		Relay: func(src io.Reader) error {
			return vdisplay.RelayViewer(conn, src, func(in vdisplay.Input) {
				a.mu.Lock()
				defer a.mu.Unlock()
				if holding {
					return // the agent is held off: it hears how long, not what
				}
				owner.input = owner.input.Add(in)
				owner.last = a.now()
				if owner.acting > 0 {
					owner.during = true
				}
			})
		},
		Hold:  hold,
		Close: closeView,
	}, nil
}

func (a *AgentWindows) nameOf(worker string) string {
	a.mu.Lock()
	names := a.names
	a.mu.Unlock()
	if names != nil {
		if name := strings.TrimSpace(names(worker)); name != "" {
			return name
		}
	}
	return "Agent window"
}

// windowView is a Session over one part of the agent display: its frames are
// that part, and the person's pointer and keys reach the window lying there,
// in the background (no pointer, no app brought forward). Only a window on the
// agent display is ever given input: the person's own windows are not there.
type windowView struct {
	inner  Session
	bounds image.Rectangle // the agent display, global points
	rect   func() (image.Rectangle, bool)
	gone   <-chan struct{}
	viewer agentDisplayViewer

	mu      sync.Mutex
	place   image.Rectangle // where the window really is, looked up now and then
	placeOf image.Rectangle // the place it was looked up for
	placeAt time.Time
	img     *image.RGBA
	crop    image.Rectangle // the last frame's part, in display pixels
	scale   float64         // display pixels per point
	buttons uint8
	down    image.Point // where the button went down, in view pixels
	lastAt  time.Time   // the last click
	lastPt  image.Point
	clicks  int
}

const (
	doubleClickWithin = 400 * time.Millisecond
	clickSlop         = 6 // view pixels a press may move and stay a click
)

func (v *windowView) Frame(ctx context.Context, after uint64) (Frame, error) {
	select {
	case <-v.gone:
		return Frame{}, errWindowClosed
	default:
	}
	rect, ok := v.rect()
	if !ok {
		return Frame{}, errWindowClosed
	}
	// The window's closing ends a wait for the next frame too.
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-v.gone:
			cancel()
		case <-fctx.Done():
		}
	}()
	f, err := v.inner.Frame(fctx, after)
	if err != nil {
		select {
		case <-v.gone:
			return Frame{}, errWindowClosed
		default:
		}
		return Frame{}, err
	}
	scale := 1.0
	if v.bounds.Dx() > 0 {
		scale = float64(f.Image.Rect.Dx()) / float64(v.bounds.Dx())
	}
	part := v.windowPart(rect).Sub(v.bounds.Min)
	crop := image.Rect(int(float64(part.Min.X)*scale), int(float64(part.Min.Y)*scale), int(float64(part.Max.X)*scale), int(float64(part.Max.Y)*scale)).Intersect(f.Image.Rect)
	if crop.Empty() {
		return Frame{}, errors.New("the window isn't on the agent display")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.img == nil || v.img.Rect.Dx() != crop.Dx() || v.img.Rect.Dy() != crop.Dy() {
		v.img = image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	}
	for y := 0; y < crop.Dy(); y++ {
		at := f.Image.PixOffset(crop.Min.X, crop.Min.Y+y)
		copy(v.img.Pix[y*v.img.Stride:y*v.img.Stride+crop.Dx()*4], f.Image.Pix[at:at+crop.Dx()*4])
	}
	v.crop, v.scale = crop, scale
	return Frame{Seq: f.Seq, Image: v.img, At: f.At}, nil
}

// windowPart is what the view shows of the display: the window itself, where
// it really is, or its place when it cannot be found there. It is looked up
// at most once a second.
func (v *windowView) windowPart(rect image.Rectangle) image.Rectangle {
	v.mu.Lock()
	if v.placeOf == rect && time.Since(v.placeAt) < time.Second {
		defer v.mu.Unlock()
		return v.place
	}
	v.mu.Unlock()
	place := rect
	if found, ok := v.viewer.WindowPlace(rect); ok {
		place = found
	}
	v.mu.Lock()
	v.place, v.placeOf, v.placeAt = place, rect, time.Now()
	v.mu.Unlock()
	return place
}

// target is the window under the view and its app, and where the view's
// pixels lie on the display's frame.
func (v *windowView) target() (pid int, window AppWindow, origin image.Point, err error) {
	rect, ok := v.rect()
	if !ok {
		return 0, AppWindow{}, image.Point{}, errWindowClosed
	}
	pid, window, ok = v.viewer.AgentWindowAt(rect)
	if !ok {
		return 0, AppWindow{}, image.Point{}, errors.New("no browser window lies there")
	}
	v.mu.Lock()
	origin = v.crop.Min
	v.mu.Unlock()
	return pid, window, origin, nil
}

// Pointer turns the person's pointer into background clicks, drags and
// scrolls on the window: a press and release in one place is a click (two in
// quick succession a double click), one that moved is a drag. Moves alone are
// not sent: the window is not under any pointer.
func (v *windowView) Pointer(x, y int, buttons uint8) error {
	v.mu.Lock()
	pressed := buttons &^ v.buttons
	released := v.buttons &^ buttons
	v.buttons = buttons
	at := image.Pt(x, y)
	if pressed&7 != 0 {
		v.down = at
	}
	down := v.down
	v.mu.Unlock()
	if pressed&(8|16|32|64) != 0 {
		dx, dy := 0, 0
		switch {
		case pressed&8 != 0:
			dy = -1
		case pressed&16 != 0:
			dy = 1
		case pressed&32 != 0:
			dx = -1
		case pressed&64 != 0:
			dx = 1
		}
		scroller, ok := v.inner.(appScroller)
		if !ok {
			return nil
		}
		pid, window, origin, err := v.target()
		if err != nil {
			return nil
		}
		return scroller.ScrollTo(pid, window, origin.X+x, origin.Y+y, dx, dy)
	}
	if released&7 == 0 {
		return nil
	}
	button := 0
	switch {
	case released&4 != 0:
		button = 2
	case released&2 != 0:
		button = 1
	}
	pid, window, origin, err := v.target()
	if err != nil {
		return nil
	}
	if abs(at.X-down.X) > clickSlop || abs(at.Y-down.Y) > clickSlop {
		if dragger, ok := v.inner.(appDragger); ok && button == 0 {
			return dragger.DragTo(pid, window, origin.X+down.X, origin.Y+down.Y, at.X-down.X, at.Y-down.Y)
		}
	}
	clicker, ok := v.inner.(appClicker)
	if !ok {
		return nil
	}
	v.mu.Lock()
	now := time.Now()
	if now.Sub(v.lastAt) <= doubleClickWithin && abs(down.X-v.lastPt.X) <= clickSlop && abs(down.Y-v.lastPt.Y) <= clickSlop {
		v.clicks = min(v.clicks+1, 3)
	} else {
		v.clicks = 1
	}
	v.lastAt, v.lastPt = now, down
	count := v.clicks
	v.mu.Unlock()
	return clicker.ClickTo(pid, window, origin.X+down.X, origin.Y+down.Y, button, count)
}

// Key sends the person's key to the window: it is made its app's key window
// before each press, so a key never lands in another of the app's windows.
func (v *windowView) Key(keysym uint32, down bool) error {
	keyer, ok := v.inner.(appKeyer)
	if !ok {
		return nil
	}
	pid, window, _, err := v.target()
	if err != nil {
		return nil
	}
	// Every press is aimed again: another of the app's windows (the person's)
	// may have become its key window since the last one. A release follows its
	// press; a key that cannot be aimed at the window is not sent.
	if down {
		if err := v.viewer.FocusWindow(pid, window); err != nil {
			return nil
		}
	}
	return keyer.KeyTo(pid, keysym, down)
}

func (v *windowView) Close() error { return nil }

// windowPlace is where the browser window placed at rect really is: a
// Chromium window wholly on the agent display, of rect's size (within a few
// points), overlapping it the most. A smaller window over it (a popup) is not
// it.
func windowPlace(onScreen []windowInfo, display, rect image.Rectangle, chromium func(pid int) bool) (image.Rectangle, bool) {
	var best image.Rectangle
	most := 0
	for _, w := range onScreen {
		if w.Layer != 0 || !w.OnScreen || w.PID <= 0 || !w.Bounds.In(display) {
			continue
		}
		if abs(w.Bounds.Dx()-rect.Dx()) > 4 || abs(w.Bounds.Dy()-rect.Dy()) > 4 {
			continue
		}
		overlap := w.Bounds.Intersect(rect)
		if area := overlap.Dx() * overlap.Dy(); area > most && chromium(w.PID) {
			best, most = w.Bounds, area
		}
	}
	return best, most > 0
}

// agentWindowAt picks, from the windows on the screen (front to back), the
// front app window lying on rect that is wholly on the agent display and
// belongs to a Chromium browser: the agent's window, or a page it opened over
// it (a sign-in popup goes where its opener is). Nothing off the agent display
// is ever picked, so the person's own windows never get the person's input
// from a view.
func agentWindowAt(onScreen []windowInfo, display, rect image.Rectangle, chromium func(pid int) bool) (int, AppWindow, bool) {
	for _, w := range onScreen {
		if w.Layer != 0 || !w.OnScreen || w.PID <= 0 || w.ID == 0 || w.Bounds.Empty() {
			continue
		}
		center := image.Pt((w.Bounds.Min.X+w.Bounds.Max.X)/2, (w.Bounds.Min.Y+w.Bounds.Max.Y)/2)
		if !w.Bounds.In(display) || !center.In(rect) || !chromium(w.PID) {
			continue
		}
		return w.PID, AppWindow{ID: w.ID, Bounds: w.Bounds}, true
	}
	return 0, AppWindow{}, false
}
