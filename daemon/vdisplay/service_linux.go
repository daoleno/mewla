//go:build linux

package vdisplay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
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
	ownsRoot bool

	starting sync.Mutex // the screen starts, and its layout changes, one at a time
	inputMu  sync.Mutex // one input action at a time: the pointer and the keyboard focus are the screen's, not a monitor's

	mu       sync.Mutex
	shared   *sharedScreen
	displays map[string]*workerDisplay
}

// sharedScreen is the one display, its picture client and its window mover.
type sharedScreen struct {
	display *Display
	client  *Client
	mover   *mover
}

type workerDisplay struct {
	worker string
	screen *sharedScreen
	slot   int             // the monitor's place along the screen, from 1
	rect   image.Rectangle // the monitor, in the screen's pixels

	mu       sync.Mutex
	apps     []*App
	windows  int         // browser windows on the monitor
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
}

// NewService keeps its displays' files under root; empty means a short private
// directory made when the first display starts (a Unix socket's path may be
// at most about 100 bytes, which a state directory can exceed).
func NewService(root string) *Service {
	return &Service{root: root, displays: map[string]*workerDisplay{}}
}

// SetAlive tells the service which Workers still run; a display whose Worker
// has ended is closed.
func (s *Service) SetAlive(alive func(worker string) bool) { s.alive = alive }

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
	w, err := s.get(context.Background(), worker, true)
	if err != nil {
		return err
	}
	return w.begin()
}

// End closes a Begin and returns the notes the agent should hear.
func (s *Service) End(worker string) []string {
	s.mu.Lock()
	w := s.displays[worker]
	s.mu.Unlock()
	if w == nil {
		return nil
	}
	w.end()
	if text := w.notes(time.Now()); text != "" {
		return []string{text}
	}
	return nil
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
		return nil, fmt.Errorf("this computer already runs %d displays for agents", maxDisplays)
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
	scr.client.Close()
	scr.display.Stop()
}

// relayout makes the screen's monitors the parking monitor and one for each
// Worker that has one, and the screen as wide as the last of them.
func (s *Service) relayout(scr *sharedScreen) error {
	s.mu.Lock()
	last := 0
	screens := []Screen{{ID: 1, X: 0, W: MonitorWidth, H: MonitorHeight}}
	for _, w := range s.displays {
		if w.screen != scr {
			continue
		}
		screens = append(screens, Screen{ID: uint32(w.slot + 1), X: w.slot * MonitorWidth, W: MonitorWidth, H: MonitorHeight})
		last = max(last, w.slot)
	}
	s.mu.Unlock()
	sort.Slice(screens, func(i, j int) bool { return screens[i].X < screens[j].X })
	return scr.client.SetLayout((last+1)*MonitorWidth, MonitorHeight, screens)
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
	conn, monitorRelay, closeView := NewMonitorView(w.screen.client, func() image.Rectangle { return w.rect })
	holding := false // guarded by w.mu
	hold := func(on bool) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if on == holding {
			return
		}
		holding = on
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
		Hold: hold,
		Close: func() {
			once.Do(func() {
				closeView()
				hold(false)
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
		info := Info{ID: w.worker, Control: "agent", Stopped: w.halted, Width: w.rect.Dx(), Height: w.rect.Dy(), Apps: w.liveApps()}
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
}

// Close ends the screen and everything on it.
func (s *Service) Close() {
	s.mu.Lock()
	scr := s.shared
	s.shared = nil
	s.displays = map[string]*workerDisplay{}
	s.mu.Unlock()
	if scr != nil {
		scr.mover.stop()
		scr.client.Close()
		scr.display.Stop()
	}
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
