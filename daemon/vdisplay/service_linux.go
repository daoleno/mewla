//go:build linux

package vdisplay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/jezek/xgb"
)

// maxDisplays bounds how many agents share a computer's agent screen at once.
const maxDisplays = 6

// The agent screen is one display for the whole computer, and every agent has
// a monitor on it: a rectangle of its own, MonitorWidth x MonitorHeight. What
// an agent opens appears on its monitor, its Chrome window (see package
// browser) is placed on it, the person is shown only that rectangle, and an
// agent's clicks and screenshots are relative to it. Monitor 0 is parked
// windows' place: a window starts there before it is moved to its agent's.
const (
	MonitorWidth  = 1280
	MonitorHeight = 800
)

// Service keeps the agent screen and decides who holds each agent's monitor.
// The display starts when an agent first needs one. Like the browser, the
// person's input never pauses an agent by rule: it is delivered and counted,
// and the agent hears about it with its next result. Hold and Stop are the
// two hard switches: Hold is the person saying "mine for now" and ends with
// the view.
type Service struct {
	rootMu   sync.Mutex
	root     string // made on first use: a short private directory, since socket paths are short
	alive    func(worker string) bool
	names    func(worker string) string
	ownsRoot bool

	// idleAfter closes a monitor nobody has used for that long (see SweepIdle);
	// idleClosed is told first, to close the Worker's browser windows and say so.
	idleAfter  time.Duration
	idleClosed func(worker, name, why string)
	idleStop   chan struct{}
	// evictable says whether a Worker's turn has ended, so its display may be
	// given to another agent when every one is taken (see makeRoom).
	evictable func(worker string) bool
	// detached is set once this daemon let go of the screen for the next one
	// (Detach): nothing is closed, forgotten or saved after it.
	detached bool
	// closedIdle are the Workers whose monitor Mewla closed, with when and why,
	// for their next result to say.
	closedIdle map[string]closedNote

	starting sync.Mutex // the screen starts, and its layout changes, one at a time
	inputMu  sync.Mutex // one input action at a time: the pointer and the keyboard focus are the screen's, not a monitor's

	mu       sync.Mutex
	shared   *sharedScreen
	displays map[string]*workerDisplay

	// reflower lays a Worker's browser window out for the person who holds it (SetReflower).
	reflower Reflower
}

// sharedScreen is the one display, its picture client and its window mover.
type sharedScreen struct {
	display *Display
	client  *Client
	mover   *mover

	// query answers questions about the screen's windows (Surface.Windows);
	// opened on first use.
	queryMu sync.Mutex
	query   *xgb.Conn
}

type workerDisplay struct {
	worker string
	screen *sharedScreen
	slot   int             // the monitor's place along the screen, from 1
	rect   image.Rectangle // the monitor, in the screen's pixels

	mu       sync.Mutex
	apps     []*App
	windows  int         // browser windows on the monitor
	viewers  int         // the person's open views of it
	told     bool        // the agent has heard that its window is the person's to see and take over
	restored time.Time   // taken back from a daemon before this one; its windows have not said so yet
	seen     time.Time   // when the person last closed a view of it
	person   int         // viewers that hold the display
	halted   bool        // the person stopped this Worker
	acting   int         // agent actions or tool calls that are running
	used     time.Time   // when the agent last began or ended one
	input    Input       // since the agent last heard
	during   bool        // some of it landed while the agent was acting
	last     time.Time   // of the latest input
	handback time.Time   // when the person last gave it back, not yet told
	held     time.Time   // when the person began to hold it
	pointer  image.Point // where the agent last had the pointer, on the monitor

	// While the person holds the display from a screen of another shape, its
	// browser window is laid out for that screen (Viewer.Fit): reflowed is the
	// page's area on the screen, reflowHost its host, reflowBottom how far down
	// the screen the window reaches; notify are the open views' listeners.
	reflowed     image.Rectangle
	reflowHost   string
	reflowBottom int
	notify       map[int]func(Reflow)
	nextView     int
}

// NewService keeps its displays' files under root; empty means a short private
// directory made when the first display starts (a Unix socket's path may be
// at most about 100 bytes, which a state directory can exceed).
func NewService(root string) *Service {
	return &Service{root: root, displays: map[string]*workerDisplay{}, closedIdle: map[string]closedNote{}}
}

// closedNote is a monitor Mewla closed, for its Worker's next result.
type closedNote struct {
	at  time.Time
	why string
}

// Why a monitor was closed, as the person and the agent are told.
const (
	whyIdle    = WhyIdle
	whyEvicted = "another agent needed its display"
)

// EvictQuiet is how long the person must have left a display alone (no input,
// no view open) before it may be given to another agent.
const EvictQuiet = 10 * time.Minute

// SetEvictable tells the service which Workers' turns have ended: when every
// display is taken, the one of those idle longest goes to the agent that asks.
func (s *Service) SetEvictable(f func(worker string) bool) { s.evictable = f }

// InUse says whether the Worker's display has something on it (a browser
// window or a running app). It only reads what the service holds.
func (s *Service) InUse(worker string) bool {
	s.mu.Lock()
	w := s.displays[worker]
	s.mu.Unlock()
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.windows > 0 || w.liveApps() > 0
}

// Capability says why agents cannot have a display here, or nil.
func (s *Service) Capability() error { return Capability() }

// SetAlive tells the service which Workers are still open; a display whose
// Worker has closed is closed. A Worker that only ended its turn keeps its
// display: what it left there (a sign-in page) is often waiting for the person.
func (s *Service) SetAlive(alive func(worker string) bool) { s.alive = alive }

// SetNames tells the service the Workers' names, for Screen's list.
func (s *Service) SetNames(names func(worker string) string) { s.names = names }

// SetReflower lets a held display's browser window be laid out for the person's screen.
func (s *Service) SetReflower(r Reflower) {
	s.mu.Lock()
	s.reflower = r
	s.mu.Unlock()
}

// SetIdleClose closes, from now on, every monitor nobody has used for after.
// closed is told first, whenever Mewla closes a monitor (idle, or given to
// another agent), with why in words, to close the Worker's browser windows and
// tell the person; the Worker hears it with its next result.
func (s *Service) SetIdleClose(after time.Duration, closed func(worker, name, why string)) {
	s.mu.Lock()
	s.idleAfter, s.idleClosed = after, closed
	start := s.idleStop == nil
	if start {
		s.idleStop = make(chan struct{})
	}
	stop := s.idleStop
	s.mu.Unlock()
	if !start {
		return
	}
	every := min(after/4, time.Minute)
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				s.SweepIdle(now)
			}
		}
	}()
}

// SweepIdle closes the monitors nobody has used for the idle time: no agent
// action, no input from the person, and no view of it open.
func (s *Service) SweepIdle(now time.Time) {
	s.mu.Lock()
	after := s.idleAfter
	var idle []*workerDisplay
	for _, w := range s.displays {
		idle = append(idle, w)
	}
	s.mu.Unlock()
	for _, w := range idle {
		w.mu.Lock()
		stale := !w.restored.IsZero() && w.windows == 0 && w.liveApps() == 0 && now.Sub(w.restored) >= restoreGrace
		w.mu.Unlock()
		if stale {
			// Taken back after a restart, but no window of its Worker said so.
			s.Forget(w.worker)
			continue
		}
		w.mu.Lock()
		since := w.used
		for _, t := range []time.Time{w.last, w.seen} {
			if t.After(since) {
				since = t
			}
		}
		unused := after > 0 && w.acting == 0 && w.person == 0 && w.viewers == 0 && now.Sub(since) >= after
		w.mu.Unlock()
		if !unused {
			continue
		}
		s.closeFor(w.worker, now, whyIdle)
	}
}

// closeFor closes a Worker's monitor and what is on it, telling the person
// (the closed hook) and, with its next result, the Worker.
func (s *Service) closeFor(worker string, now time.Time, why string) {
	s.mu.Lock()
	closed := s.idleClosed
	s.mu.Unlock()
	if closed != nil {
		closed(worker, s.nameOf(worker), why)
	}
	s.Forget(worker)
	s.mu.Lock()
	s.closedIdle[worker] = closedNote{at: now, why: why}
	s.mu.Unlock()
}

// makeRoom gives a display to an agent that asks when every one is taken: the
// one idle longest whose Worker's turn has ended, which nobody is acting on,
// holding or watching, and which the person has left alone for EvictQuiet. It
// says whether one was freed. The caller does not hold s.starting.
func (s *Service) makeRoom(now time.Time) bool {
	s.mu.Lock()
	full := len(s.displays) >= maxDisplays
	evictable := s.evictable
	all := make([]*workerDisplay, 0, len(s.displays))
	for _, w := range s.displays {
		all = append(all, w)
	}
	s.mu.Unlock()
	if !full {
		return true
	}
	if evictable == nil {
		return false
	}
	var victim string
	var oldest time.Time
	for _, w := range all {
		w.mu.Lock()
		since := w.used
		person := w.last
		if w.seen.After(person) {
			person = w.seen
		}
		if person.After(since) {
			since = person
		}
		ok := w.acting == 0 && w.person == 0 && w.viewers == 0 && now.Sub(person) >= EvictQuiet
		w.mu.Unlock()
		if ok && evictable(w.worker) && (victim == "" || since.Before(oldest)) {
			victim, oldest = w.worker, since
		}
	}
	if victim == "" {
		return false
	}
	s.closeFor(victim, now, whyEvicted)
	return true
}

// errAllInUse is what an agent hears when every display is in active use.
var errAllInUse = fmt.Errorf("All %d agent displays on this computer are in use: their agents are working, or the person is watching or using them. Tell the person, and try again once one of those agents is done.", maxDisplays)

func (s *Service) nameOf(worker string) string {
	if s.names == nil {
		return ""
	}
	return s.names(worker)
}

// Act runs one agent action on the Worker's monitor.
func (s *Service) Act(ctx context.Context, worker string, in Action) (Result, error) {
	if worker == "" {
		return Result{}, errors.New("a Worker is required")
	}
	w, err := s.get(ctx, worker, in.Action == "open")
	if err != nil {
		return Result{}, err
	}
	if err := w.begin(); err != nil {
		return Result{}, err
	}
	defer w.end()

	text, err := s.do(ctx, w, in)
	if err != nil {
		return Result{}, err
	}
	var img *image.RGBA
	if in.Action != "close" {
		time.Sleep(settleFor(in.Action))
		if img, err = w.snapshot(); err != nil {
			return Result{}, err
		}
	}
	res := Result{Text: strings.TrimSpace(text + "\n" + w.notes(time.Now()))}
	if img != nil {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
			return Result{}, err
		}
		res.Image = buf.Bytes()
	}
	return res, nil
}

// begin says whether the agent may act now, and counts the action. The ones
// that refuse tell the agent what to do instead.
func (w *workerDisplay) begin() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case w.halted:
		return errHalted
	case w.person > 0:
		return errPerson
	}
	w.acting++
	w.used = time.Now()
	return nil
}

func (w *workerDisplay) end() {
	w.mu.Lock()
	if w.acting > 0 {
		w.acting--
	}
	w.used = time.Now()
	w.mu.Unlock()
}

// snapshot is the monitor's picture: the part of the screen that is the
// agent's.
func (w *workerDisplay) snapshot() (*image.RGBA, error) {
	full, err := w.screen.client.Snapshot(5 * time.Second)
	if err != nil {
		return nil, err
	}
	out := image.NewRGBA(image.Rect(0, 0, w.rect.Dx(), w.rect.Dy()))
	draw.Draw(out, out.Bounds(), full, w.rect.Min, draw.Src)
	for i := 3; i < len(out.Pix); i += 4 {
		out.Pix[i] = 255
	}
	return out, nil
}

// Tool calls of an agent's browser go through Begin and End, like its display
// actions, so the person's Hold and Stop and the notes about what they did
// reach the browser too. Begin refuses while the person holds the monitor or
// has stopped the Worker; End says what the person did meanwhile.
func (s *Service) Begin(worker string) error {
	w := s.live(worker)
	if w == nil {
		// No window yet: the Worker's first tool call opens one, on a display
		// it gets now; with none to be had it hears why instead of a timeout.
		if !s.makeRoom(time.Now()) {
			return errAllInUse
		}
		return nil
	}
	return w.begin()
}

// End closes a Begin and returns the notes the agent should hear.
func (s *Service) End(worker string) []string {
	s.mu.Lock()
	w := s.displays[worker]
	closedAt, wasClosed := s.closedIdle[worker]
	delete(s.closedIdle, worker)
	after := s.idleAfter
	s.mu.Unlock()
	var out []string
	if wasClosed {
		why := closedAt.why
		if why == whyIdle {
			why = "nobody had used them for " + roundSpan(after)
		}
		out = append(out, fmt.Sprintf("Mewla closed your browser window and display at %s: %s. Open the page again if it is still needed.", closedAt.at.Format("15:04"), why))
	}
	if w == nil {
		return out
	}
	w.end()
	if text := w.notes(time.Now()); text != "" {
		out = append(out, text)
	}
	w.mu.Lock()
	tell := w.windows > 0 && !w.told
	w.told = w.told || tell
	w.mu.Unlock()
	if tell {
		out = append(out, WindowNote)
	}
	return out
}

func settleFor(action string) time.Duration {
	switch action {
	case "open":
		return 1500 * time.Millisecond
	case "screenshot":
		return 0
	}
	return 300 * time.Millisecond
}

// Monitor is the Worker's monitor, given to it if it has none: where its
// windows go on the agent screen.
func (s *Service) Monitor(ctx context.Context, worker string) (image.Rectangle, error) {
	w, err := s.get(ctx, worker, true)
	if err != nil {
		return image.Rectangle{}, err
	}
	return w.rect, nil
}

// Windows tells the service how many browser windows a Worker has on its
// monitor. The monitor goes when the Worker has neither a window nor a
// program on it.
func (s *Service) Windows(worker string, n int) {
	s.mu.Lock()
	w := s.displays[worker]
	s.mu.Unlock()
	if w == nil {
		return
	}
	w.mu.Lock()
	w.windows = n
	if n > 0 {
		w.restored = time.Time{}
	}
	idle := w.windows == 0 && w.liveApps() == 0
	w.mu.Unlock()
	if idle {
		s.Forget(worker)
	}
}

func (w *workerDisplay) liveApps() int {
	n := 0
	for _, a := range w.apps {
		select {
		case <-a.Done:
		default:
			n++
		}
	}
	return n
}

var (
	errHalted   = errors.New("The person stopped you from using this display. Do not ask for it again; tell them what is done and what is left.")
	errPerson   = errors.New("The person is holding the display. Do not poll; stop and report, then try again later when they let go.")
	errNoScreen = errors.New("You have no display yet: use action open with the program to start")
)

func (s *Service) rootDir() (string, error) {
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	if s.root != "" {
		return s.root, nil
	}
	base := os.Getenv("XDG_RUNTIME_DIR")
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		base = os.TempDir()
	}
	dir, err := os.MkdirTemp(base, "mewla-vd-")
	if err != nil {
		return "", err
	}
	s.root = dir
	s.ownsRoot = true
	return dir, nil
}

func (s *Service) get(ctx context.Context, worker string, create bool) (*workerDisplay, error) {
	if w := s.live(worker); w != nil {
		return w, nil
	}
	if !create {
		return nil, errNoScreen
	}
	w, err := s.create(ctx, worker)
	if errors.Is(err, errFull) {
		// Freed outside s.starting: closing a display takes it too.
		if !s.makeRoom(time.Now()) {
			return nil, errAllInUse
		}
		w, err = s.create(ctx, worker)
		if errors.Is(err, errFull) {
			return nil, errAllInUse
		}
	}
	return w, err
}

var errFull = errors.New("every display is taken")

// create makes the Worker's monitor, or says errFull.
func (s *Service) create(ctx context.Context, worker string) (*workerDisplay, error) {
	// One monitor is made at a time, so two opens from one Worker can't make two.
	s.starting.Lock()
	defer s.starting.Unlock()
	if w := s.live(worker); w != nil {
		return w, nil
	}
	scr, err := s.screen(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	taken := map[int]bool{}
	for _, w := range s.displays {
		taken[w.slot] = true
	}
	s.mu.Unlock()
	slot := 0
	for i := 1; i <= maxDisplays; i++ {
		if !taken[i] {
			slot = i
			break
		}
	}
	if slot == 0 {
		return nil, errFull
	}
	w := &workerDisplay{worker: worker, screen: scr, slot: slot, rect: image.Rect(slot*MonitorWidth, 0, (slot+1)*MonitorWidth, MonitorHeight), used: time.Now()}
	s.mu.Lock()
	s.displays[worker] = w
	s.mu.Unlock()
	if err := s.relayout(scr); err != nil {
		s.mu.Lock()
		delete(s.displays, worker)
		s.mu.Unlock()
		return nil, err
	}
	s.persist()
	return w, nil
}

// screen is the agent screen, started if it is not up. The caller holds
// s.starting.
func (s *Service) screen(ctx context.Context) (*sharedScreen, error) {
	s.mu.Lock()
	scr := s.shared
	s.mu.Unlock()
	if scr != nil && scr.display.Alive() && scr.client.Err() == nil {
		return scr, nil
	}
	if scr != nil { // it died: what was on it went with it
		s.dropScreen(scr)
	}
	root, err := s.rootDir()
	if err != nil {
		return nil, err
	}
	d, err := Start(ctx, filepath.Join(root, "screen"), MonitorWidth, MonitorHeight)
	if err != nil {
		return nil, err
	}
	c, err := d.Client()
	if err != nil {
		d.Stop()
		return nil, err
	}
	scr = &sharedScreen{display: d, client: c}
	scr.mover = startMover(d, s.monitorOfProcess)
	s.mu.Lock()
	s.shared = scr
	s.mu.Unlock()
	s.persist()
	return scr, nil
}

// Display is the agent screen's X display, started if it is not up: where a
// program of the agents' own (the agent browser's Chrome) is to run.
func (s *Service) Display(ctx context.Context) (*Display, error) {
	s.starting.Lock()
	defer s.starting.Unlock()
	scr, err := s.screen(ctx)
	if err != nil {
		return nil, err
	}
	if len(scr.client.Screens()) == 0 {
		if err := s.relayout(scr); err != nil {
			return nil, err
		}
	}
	return scr.display, nil
}

// DisplayEnv is Display for a program that runs outside this package: the
// screen's X display number and authority file.
func (s *Service) DisplayEnv(ctx context.Context) (int, string, error) {
	d, err := s.Display(ctx)
	if err != nil {
		return 0, "", err
	}
	return d.Number, d.AuthPath(), nil
}

func (s *Service) dropScreen(scr *sharedScreen) {
	s.mu.Lock()
	if s.shared == scr {
		s.shared = nil
	}
	gone := s.displays
	s.displays = map[string]*workerDisplay{}
	s.mu.Unlock()
	_ = gone
	scr.mover.stop()
	scr.closeQuery()
	scr.client.Close()
	scr.display.Stop()
	s.persist()
}

// relayout makes the screen's monitors the parking monitor and one for each
// Worker that has one, and the screen as wide as the last of them.
func (s *Service) relayout(scr *sharedScreen) error {
	s.mu.Lock()
	last := 0
	screens := []Screen{{ID: 1, X: 0, W: MonitorWidth, H: MonitorHeight}}
	var bottoms []int
	for _, w := range s.displays {
		if w.screen != scr {
			continue
		}
		w.mu.Lock()
		bottom := w.reflowBottom
		w.mu.Unlock()
		// A monitor whose window is laid out for a phone is as tall as that window while it is (Chrome keeps a
		// window at least half on a monitor); every other one keeps its size.
		screens = append(screens, Screen{ID: uint32(w.slot + 1), X: w.slot * MonitorWidth, W: MonitorWidth, H: layoutHeight([]int{bottom})})
		last = max(last, w.slot)
		bottoms = append(bottoms, bottom)
	}
	s.mu.Unlock()
	sort.Slice(screens, func(i, j int) bool { return screens[i].X < screens[j].X })
	// The screen grows below the monitors while a window is laid out for a phone.
	return scr.client.SetLayout((last+1)*MonitorWidth, layoutHeight(bottoms), screens)
}

// live is the Worker's display if it is up; one whose Worker has ended is
// cleared away.
func (s *Service) live(worker string) *workerDisplay {
	if s.alive != nil && !s.alive(worker) {
		s.Forget(worker)
		return nil
	}
	s.mu.Lock()
	w := s.displays[worker]
	if w != nil && (!w.screen.display.Alive() || w.screen.client.Err() != nil) {
		delete(s.displays, worker)
		w = nil
	}
	s.mu.Unlock()
	return w
}

// monitorOfProcess is the monitor of the Worker whose program a process
// belongs to, for the window mover.
func (s *Service) monitorOfProcess(pid int) (image.Rectangle, bool) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return image.Rectangle{}, false
	}
	s.mu.Lock()
	var all []*workerDisplay
	for _, w := range s.displays {
		all = append(all, w)
	}
	s.mu.Unlock()
	for _, w := range all {
		w.mu.Lock()
		for _, a := range w.apps {
			if a.Cmd.Process != nil && a.Cmd.Process.Pid == pgid {
				rect := w.rect
				w.mu.Unlock()
				return rect, true
			}
		}
		w.mu.Unlock()
	}
	return image.Rectangle{}, false
}

// point is a position in the picture the agent sees, on the screen; it must
// be on the monitor.
func (w *workerDisplay) point(x, y int) (int, int, error) {
	if x < 0 || y < 0 || x >= w.rect.Dx() || y >= w.rect.Dy() {
		return 0, 0, fmt.Errorf("point %d,%d is outside the %dx%d display", x, y, w.rect.Dx(), w.rect.Dy())
	}
	w.pointer = image.Pt(x, y)
	return x + w.rect.Min.X, y + w.rect.Min.Y, nil
}

func (s *Service) do(ctx context.Context, w *workerDisplay, in Action) (string, error) {
	c := w.screen.client
	// One input action at a time on the screen: the pointer is the screen's,
	// and a key goes to the window under it. Before keys, the pointer goes
	// back to where this agent left it, on its own monitor.
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	button := func() (byte, error) {
		switch strings.ToLower(in.Button) {
		case "", "left":
			return 1, nil
		case "middle":
			return 2, nil
		case "right":
			return 4, nil
		}
		return 0, fmt.Errorf("unknown button %q (left, middle, right)", in.Button)
	}
	switch in.Action {
	case "open":
		if len(in.Command) == 0 {
			return "", errors.New("open needs command: the program and its arguments")
		}
		home := filepath.Join(w.screen.display.Dir, "home-"+safeName(w.worker))
		if err := os.MkdirAll(home, 0o700); err != nil {
			return "", err
		}
		app, err := w.screen.display.Launch(home, in.Command...)
		if err != nil {
			return "", err
		}
		w.mu.Lock()
		w.apps = append(w.apps, app)
		w.mu.Unlock()
		return fmt.Sprintf("Started %s on a %dx%d display of its own. Coordinates in the picture are the display's.", in.Command[0], w.rect.Dx(), w.rect.Dy()), nil
	case "screenshot":
		return "", nil
	case "move":
		x, y, err := w.point(in.X, in.Y)
		if err != nil {
			return "", err
		}
		return "", c.Pointer(x, y, 0)
	case "click", "double_click":
		b, err := button()
		if err != nil {
			return "", err
		}
		n := 1
		if in.Action == "double_click" {
			n = 2
		}
		x, y, err := w.point(in.X, in.Y)
		if err != nil {
			return "", err
		}
		if err := c.Pointer(x, y, 0); err != nil {
			return "", err
		}
		for i := 0; i < n; i++ {
			if err := c.Pointer(x, y, b); err != nil {
				return "", err
			}
			if err := c.Pointer(x, y, 0); err != nil {
				return "", err
			}
			time.Sleep(60 * time.Millisecond)
		}
		return "", nil
	case "scroll":
		x, y, err := w.point(in.X, in.Y)
		if err != nil {
			return "", err
		}
		if err := c.Pointer(x, y, 0); err != nil {
			return "", err
		}
		wheel := func(mask byte, n int) error {
			for i := 0; i < n && i < 50; i++ {
				if err := c.Pointer(x, y, mask); err != nil {
					return err
				}
				if err := c.Pointer(x, y, 0); err != nil {
					return err
				}
			}
			return nil
		}
		for _, step := range []struct {
			up, down byte
			n        int
		}{{8, 16, in.DY}, {32, 64, in.DX}} {
			if step.n > 0 {
				if err := wheel(step.down, step.n); err != nil {
					return "", err
				}
			} else if step.n < 0 {
				if err := wheel(step.up, -step.n); err != nil {
					return "", err
				}
			}
		}
		return "", nil
	case "type", "key":
		if in.Action == "type" && in.Text == "" {
			return "", errors.New("type needs text")
		}
		x, y, err := w.point(w.pointer.X, w.pointer.Y)
		if err != nil {
			return "", err
		}
		if err := c.Pointer(x, y, 0); err != nil { // the keys go where the pointer is
			return "", err
		}
		if in.Action == "type" {
			return "", c.TypeText(in.Text)
		}
		return "", c.Press(in.Key)
	case "close":
		s.Forget(w.worker)
		return "Closed your display and everything on it.", nil
	}
	return "", fmt.Errorf("unknown action %q", in.Action)
}

// notes is what the person did since the agent last heard, never what they typed.
func (w *workerDisplay) notes(now time.Time) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var parts []string
	if !w.handback.IsZero() {
		parts = append(parts, fmt.Sprintf("The person had the display for about %s and handed it back.", roundSpan(w.handback.Sub(w.held))))
		w.handback, w.held = time.Time{}, time.Time{}
	}
	if !w.input.Empty() {
		var did []string
		add := func(n int, one, many string) {
			if n > 0 {
				did = append(did, fmt.Sprintf("%d %s", n, plural(n, one, many)))
			}
		}
		add(w.input.Clicks, "click", "clicks")
		add(w.input.Scrolls, "scroll", "scrolls")
		add(w.input.Typed, "character typed (not shown to you)", "characters typed (not shown to you)")
		other := w.input.Keys - w.input.Typed
		add(other, "other key", "other keys")
		add(w.input.Pastes, "paste (not shown to you)", "pastes (not shown to you)")
		text := fmt.Sprintf("The person used the display while you held it: %s, the last %s ago.", strings.Join(did, ", "), roundSpan(now.Sub(w.last)))
		if w.during {
			text += " Some of it landed while your action was running, so the action may not have done what you meant."
		}
		text += " Windows may have moved and a field may hold something you did not put there; read the picture again and judge whether it matters before you go on."
		parts = append(parts, text)
		w.input, w.during = Input{}, false
	}
	return strings.Join(parts, " ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func roundSpan(d time.Duration) string {
	switch {
	case d < 2*time.Second:
		return "a moment"
	case d < 90*time.Second:
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	return fmt.Sprintf("%d minutes", int(d.Minutes()+0.5))
}

// View opens the Worker's monitor for a person: they are shown that
// rectangle of the screen and nothing else. What they do reaches it at once
// and is told to the agent with its next result, which keeps going. Hold,
// while the view is open, holds the agent off instead until released or the
// view closes.
func (s *Service) View(worker string) (*Viewer, error) {
	w, err := s.get(context.Background(), worker, false)
	if err != nil {
		return nil, err
	}
	rect := func() image.Rectangle {
		w.mu.Lock()
		defer w.mu.Unlock()
		return viewRect(w.rect, w.reflowed)
	}
	client := w.screen.client
	conn, monitorRelay, closeView := NewMonitorView(client, rect)
	w.mu.Lock()
	w.viewers++
	w.nextView++
	id := w.nextView
	w.mu.Unlock()
	holding := false // guarded by w.mu
	var fitMu sync.Mutex
	hold := func(on bool) {
		w.mu.Lock()
		if on == holding {
			w.mu.Unlock()
			return
		}
		holding = on
		if !on {
			w.mu.Unlock()
			// Letting go puts the window back as the agent left it.
			fitMu.Lock()
			s.endReflow(w)
			fitMu.Unlock()
			w.mu.Lock()
		}
		defer w.mu.Unlock()
		if on {
			if w.person == 0 {
				w.held = time.Now()
			}
			w.person++
			w.halted = false // holding lifts a Stop, as in the browser
			return
		}
		w.person--
		if w.person == 0 {
			w.handback = time.Now()
		}
	}
	var once sync.Once
	return &Viewer{
		Conn: conn,
		Relay: func(src io.Reader) error {
			return monitorRelay(src, func(in Input) {
				w.mu.Lock()
				defer w.mu.Unlock()
				if holding {
					return // the agent is held off: it is told how long, not what
				}
				w.input = w.input.Add(in)
				w.last = time.Now()
				if w.acting > 0 {
					w.during = true
				}
			})
		},
		Picture: func() ([]byte, int, int) {
			r := rect()
			return client.Crop(r), r.Dx(), r.Dy()
		},
		Watch: client.Watch,
		Hold:  hold,
		Fit: func(width, height int, dpr float64) Reflow {
			fitMu.Lock()
			defer fitMu.Unlock()
			w.mu.Lock()
			held := holding
			now := Reflow{On: !w.reflowed.Empty(), Host: w.reflowHost}
			w.mu.Unlock()
			if !held {
				// Only the view that holds the display lays it out (or puts it back): another person's view
				// of it, on a desktop or in the float, is told how it is.
				return now
			}
			if width <= 0 {
				s.endReflow(w)
				return Reflow{}
			}
			return s.reflow(w, width, height, dpr)
		},
		Notify: func(f func(Reflow)) {
			w.mu.Lock()
			if w.notify == nil {
				w.notify = map[int]func(Reflow){}
			}
			w.notify[id] = f
			w.mu.Unlock()
		},
		Close: func() {
			once.Do(func() {
				closeView()
				hold(false)
				w.mu.Lock()
				delete(w.notify, id)
				w.viewers--
				// Watching counts as using it: the idle time starts when the view closes.
				w.seen = time.Now()
				w.mu.Unlock()
			})
		},
	}, nil
}

// Halt is Stop: this Worker may not use its display until the person holds it
// and lets go, or resumes it.
func (s *Service) Halt(worker string) error {
	w, err := s.get(context.Background(), worker, false)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.halted = true
	w.mu.Unlock()
	return nil
}

// Resume lifts a Stop: the Worker may use its display again.
func (s *Service) Resume(worker string) error {
	w, err := s.get(context.Background(), worker, false)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.halted = false
	w.mu.Unlock()
	return nil
}

// List is every Worker's monitor, in a stable order.
func (s *Service) List() []Info {
	s.mu.Lock()
	var workers []string
	for id := range s.displays {
		workers = append(workers, id)
	}
	s.mu.Unlock()
	var out []Info
	for _, id := range workers {
		w := s.live(id)
		if w == nil {
			continue
		}
		w.mu.Lock()
		info := Info{ID: w.worker, Name: s.nameOf(w.worker), Control: "agent", Stopped: w.halted, Width: w.rect.Dx(), Height: w.rect.Dy(), Apps: w.liveApps(), Browser: w.windows}
		if w.person > 0 {
			info.Control = "person"
		}
		if w.acting == 0 {
			info.IdleS = int(time.Since(w.used).Seconds())
		}
		w.mu.Unlock()
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Forget takes a Worker's monitor away, as when the Worker ends: its
// programs end. The screen stays while any Worker has a monitor.
func (s *Service) Forget(worker string) {
	s.mu.Lock()
	if s.detached {
		s.mu.Unlock()
		return
	}
	w := s.displays[worker]
	delete(s.displays, worker)
	s.mu.Unlock()
	if w == nil {
		return
	}
	w.mu.Lock()
	apps := w.apps
	w.apps = nil
	w.mu.Unlock()
	for _, a := range apps {
		if a.Cmd.Process != nil {
			_ = syscall.Kill(-a.Cmd.Process.Pid, syscall.SIGTERM)
		}
	}
	s.starting.Lock()
	defer s.starting.Unlock()
	s.mu.Lock()
	scr := s.shared
	s.mu.Unlock()
	if scr == w.screen && scr.display.Alive() {
		_ = s.relayout(scr) // the monitor is gone from the layout
	}
	s.persist()
}

// Close ends the screen and everything on it.
func (s *Service) Close() {
	s.mu.Lock()
	if s.idleStop != nil {
		close(s.idleStop)
		s.idleStop = nil
	}
	scr := s.shared
	s.shared = nil
	s.displays = map[string]*workerDisplay{}
	s.mu.Unlock()
	if scr != nil {
		scr.mover.stop()
		scr.closeQuery()
		scr.client.Close()
		scr.display.Stop()
	}
	s.persist()
	s.rootMu.Lock()
	if s.ownsRoot {
		_ = os.RemoveAll(s.root)
		s.root, s.ownsRoot = "", false
	}
	s.rootMu.Unlock()
}

func safeName(worker string) string {
	var b strings.Builder
	for _, r := range worker {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if len(name) > 24 {
		name = name[:24]
	}
	return name
}

func (scr *sharedScreen) closeQuery() {
	scr.queryMu.Lock()
	defer scr.queryMu.Unlock()
	if scr.query != nil {
		scr.query.Close()
		scr.query = nil
	}
}

// restoreGrace is how long a monitor taken back after a restart waits for its
// Worker's browser window to report again before it is let go.
const restoreGrace = 2 * time.Minute

// savedScreen is the agent screen as the daemon keeps it on disk, so the next
// daemon can take the screen back after a restart (Adopt).
type savedScreen struct {
	Number   int            `json:"number"`
	Cookie   string         `json:"cookie"`
	Monitors []savedMonitor `json:"monitors"`
}

type savedMonitor struct {
	Worker string `json:"worker"`
	Slot   int    `json:"slot"`
}

func (s *Service) statePath() string {
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	if s.root == "" {
		return ""
	}
	return filepath.Join(s.root, "state.json")
}

// persist saves the screen and its monitors; with no screen it removes them.
func (s *Service) persist() {
	path := s.statePath()
	if path == "" {
		return
	}
	s.mu.Lock()
	if s.detached {
		s.mu.Unlock()
		return
	}
	scr := s.shared
	var saved savedScreen
	if scr != nil {
		saved = savedScreen{Number: scr.display.Number, Cookie: scr.display.Cookie()}
		for _, w := range s.displays {
			if w.screen == scr {
				saved.Monitors = append(saved.Monitors, savedMonitor{Worker: w.worker, Slot: w.slot})
			}
		}
	}
	s.mu.Unlock()
	if scr == nil {
		_ = os.Remove(path)
		return
	}
	sort.Slice(saved.Monitors, func(i, j int) bool { return saved.Monitors[i].Slot < saved.Monitors[j].Slot })
	raw, err := json.Marshal(saved)
	if err != nil {
		return
	}
	if err := atomicfile.Write(path, raw, 0o600); err != nil {
		log.Printf("display: couldn't save the agent screen: %v", err)
	}
}

// Adopt takes back the agent screen a daemon before this one left running
// (Detach), with every Worker's monitor where it was; a monitor whose Worker's
// window does not report again within restoreGrace is let go. It returns how
// many monitors it took back.
func (s *Service) Adopt() int {
	path := s.statePath()
	if path == "" {
		return 0
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var saved savedScreen
	if json.Unmarshal(raw, &saved) != nil || saved.Number == 0 {
		_ = os.Remove(path)
		return 0
	}
	s.starting.Lock()
	defer s.starting.Unlock()
	s.mu.Lock()
	busy := s.shared != nil
	s.mu.Unlock()
	if busy {
		return 0
	}
	root, _ := s.rootDir()
	d, err := Adopt(filepath.Join(root, "screen"), saved.Number, saved.Cookie, MonitorWidth, MonitorHeight)
	if err != nil {
		log.Printf("display: the agent screen a daemon before this one left is not there any more: %v", err)
		_ = os.Remove(path)
		return 0
	}
	c, err := d.Client()
	if err != nil {
		log.Printf("display: couldn't see the agent screen a daemon before this one left: %v", err)
		d.Stop()
		_ = os.Remove(path)
		return 0
	}
	scr := &sharedScreen{display: d, client: c}
	scr.mover = startMover(d, s.monitorOfProcess)
	now := time.Now()
	s.mu.Lock()
	s.shared = scr
	for _, m := range saved.Monitors {
		if m.Worker == "" || m.Slot < 1 || m.Slot > maxDisplays {
			continue
		}
		s.displays[m.Worker] = &workerDisplay{worker: m.Worker, screen: scr, slot: m.Slot, rect: image.Rect(m.Slot*MonitorWidth, 0, (m.Slot+1)*MonitorWidth, MonitorHeight), used: now, restored: now}
	}
	n := len(s.displays)
	s.mu.Unlock()
	if err := s.relayout(scr); err != nil {
		log.Printf("display: couldn't lay out the agent screen taken back: %v", err)
	}
	log.Printf("display: took back the agent screen :%d with %d monitor(s) after a restart", d.Number, n)
	return n
}

// Detach lets go of the agent screen without closing anything, for the next
// daemon to take back (Adopt): the screen and the browser on it keep running
// in their keepers. Agents' desktop apps end with this daemon.
func (s *Service) Detach() {
	s.mu.Lock()
	s.detached = true
	if s.idleStop != nil {
		close(s.idleStop)
		s.idleStop = nil
	}
	scr := s.shared
	s.shared = nil
	s.mu.Unlock()
	if scr != nil {
		scr.mover.stop()
		scr.closeQuery()
		scr.client.Close()
		scr.display.Detach()
	}
}

// StableRoot is the agent screen's directory for a state directory: the same
// across restarts, private, and short enough for the sockets in it.
func StableRoot(stateDir string) (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		base = os.TempDir()
	}
	sum := sha256.Sum256([]byte(stateDir))
	dir := filepath.Join(base, "mewla-vd-"+hex.EncodeToString(sum[:6]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return dir, os.Chmod(dir, 0o700)
}
