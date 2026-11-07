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
	"time"
)

// maxDisplays bounds how many private displays a computer runs at once.
const maxDisplays = 6

// Service keeps one private display per Worker, started when the agent first
// asks for an app, and decides who holds it. Like the browser, the person's
// input never pauses an agent by rule: it is delivered and counted, and the
// agent hears about it with its next result. Take over and Stop are the two
// hard switches.
type Service struct {
	rootMu   sync.Mutex
	root     string // made on first use: a short private directory, since socket paths are short
	alive    func(worker string) bool
	ownsRoot bool

	starting sync.Mutex // one display starts at a time

	mu       sync.Mutex
	displays map[string]*workerDisplay
}

type workerDisplay struct {
	worker  string
	display *Display
	client  *Client

	mu       sync.Mutex
	person   int       // viewers that took over
	halted   bool      // the person stopped this Worker
	acting   bool      // an agent action is running
	input    Input     // since the agent last heard
	during   bool      // some of it landed while the agent was acting
	last     time.Time // of the latest input
	handback time.Time // when the person last gave it back, not yet told
	held     time.Time // when the person took over
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

var (
	errHalted   = errors.New("The person stopped you from using this display. Do not ask for it again; tell them what is done and what is left.")
	errPerson   = errors.New("The person took over the display. Do not poll; stop and report, then try again later when they hand it back.")
	errNoScreen = errors.New("You have no display yet: use action open with the program to start")
)

// Act runs one agent action on the Worker's display.
func (s *Service) Act(ctx context.Context, worker string, in Action) (Result, error) {
	if worker == "" {
		return Result{}, errors.New("a Worker is required")
	}
	w, err := s.get(ctx, worker, in.Action == "open")
	if err != nil {
		return Result{}, err
	}
	w.mu.Lock()
	switch {
	case w.halted:
		w.mu.Unlock()
		return Result{}, errHalted
	case w.person > 0:
		w.mu.Unlock()
		return Result{}, errPerson
	}
	w.acting = true
	w.mu.Unlock()
	defer func() { w.mu.Lock(); w.acting = false; w.mu.Unlock() }()

	text, err := s.do(ctx, w, in)
	if err != nil {
		return Result{}, err
	}
	var img *image.RGBA
	if in.Action != "close" {
		time.Sleep(settleFor(in.Action))
		if img, err = w.client.Snapshot(5 * time.Second); err != nil {
			return Result{}, err
		}
	}
	res := Result{Text: strings.TrimSpace(text + "\n" + w.notes(time.Now()))}
	if img != nil {
		var buf bytes.Buffer
		flat := image.NewRGBA(img.Bounds())
		draw.Draw(flat, flat.Bounds(), img, image.Point{}, draw.Src)
		if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 70}); err != nil {
			return Result{}, err
		}
		res.Image = buf.Bytes()
	}
	return res, nil
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

func (s *Service) get(ctx context.Context, worker string, create bool) (*workerDisplay, error) {
	if w := s.live(worker); w != nil {
		return w, nil
	}
	if !create {
		return nil, errNoScreen
	}
	// One display starts at a time, so two opens from one Worker can't start two.
	s.starting.Lock()
	defer s.starting.Unlock()
	if w := s.live(worker); w != nil {
		return w, nil
	}
	s.mu.Lock()
	n := len(s.displays)
	s.mu.Unlock()
	if n >= maxDisplays {
		return nil, fmt.Errorf("this computer already runs %d displays for agents", maxDisplays)
	}
	root, err := s.rootDir()
	if err != nil {
		return nil, err
	}
	d, err := Start(ctx, filepath.Join(root, safeName(worker)), 1280, 800)
	if err != nil {
		return nil, err
	}
	c, err := d.Client()
	if err != nil {
		d.Stop()
		return nil, err
	}
	w := &workerDisplay{worker: worker, display: d, client: c}
	s.mu.Lock()
	s.displays[worker] = w
	s.mu.Unlock()
	return w, nil
}

// live is the Worker's display if it is up; a dead one is cleared away.
func (s *Service) live(worker string) *workerDisplay {
	if s.alive != nil && !s.alive(worker) {
		s.Forget(worker)
		return nil
	}
	s.mu.Lock()
	w := s.displays[worker]
	if w != nil && !w.display.Alive() {
		delete(s.displays, worker)
		s.mu.Unlock()
		w.client.Close()
		return nil
	}
	s.mu.Unlock()
	return w
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
	return "d-" + name
}

func (s *Service) do(ctx context.Context, w *workerDisplay, in Action) (string, error) {
	c := w.client
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
		home := filepath.Join(w.display.Dir, "home")
		if err := os.MkdirAll(home, 0o700); err != nil {
			return "", err
		}
		if _, err := w.display.Launch(home, in.Command...); err != nil {
			return "", err
		}
		return fmt.Sprintf("Started %s on a %dx%d display of its own. Coordinates in the picture are the display's.", in.Command[0], w.display.Width, w.display.Height), nil
	case "screenshot":
		return "", nil
	case "move":
		return "", c.Pointer(in.X, in.Y, 0)
	case "click", "double_click":
		b, err := button()
		if err != nil {
			return "", err
		}
		n := 1
		if in.Action == "double_click" {
			n = 2
		}
		if err := c.Pointer(in.X, in.Y, 0); err != nil {
			return "", err
		}
		for i := 0; i < n; i++ {
			if err := c.Pointer(in.X, in.Y, b); err != nil {
				return "", err
			}
			if err := c.Pointer(in.X, in.Y, 0); err != nil {
				return "", err
			}
			time.Sleep(60 * time.Millisecond)
		}
		return "", nil
	case "scroll":
		if err := c.Pointer(in.X, in.Y, 0); err != nil {
			return "", err
		}
		wheel := func(mask byte, n int) error {
			for i := 0; i < n && i < 50; i++ {
				if err := c.Pointer(in.X, in.Y, mask); err != nil {
					return err
				}
				if err := c.Pointer(in.X, in.Y, 0); err != nil {
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
	case "type":
		if in.Text == "" {
			return "", errors.New("type needs text")
		}
		return "", c.TypeText(in.Text)
	case "key":
		return "", c.Press(in.Key)
	case "close":
		s.mu.Lock()
		delete(s.displays, w.worker)
		s.mu.Unlock()
		w.client.Close()
		w.display.Stop()
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

// View opens the display for a person. With takeOver the agent is held off
// until the view closes; otherwise the person only watches and what they do is
// told to the agent.
func (s *Service) View(worker string, takeOver bool) (*Viewer, error) {
	w, err := s.get(context.Background(), worker, false)
	if err != nil {
		return nil, err
	}
	conn, err := w.display.Dial()
	if err != nil {
		return nil, err
	}
	if err := Handshake(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if takeOver {
		w.mu.Lock()
		if w.person == 0 {
			w.held = time.Now()
		}
		w.person++
		w.halted = false // taking over lifts a Stop, as in the browser
		w.mu.Unlock()
	}
	var once sync.Once
	return &Viewer{
		Conn: conn,
		Relay: func(src io.Reader) error {
			return RelayViewer(conn, src, func(in Input) {
				if takeOver {
					return
				}
				w.mu.Lock()
				w.input = w.input.Add(in)
				w.last = time.Now()
				if w.acting {
					w.during = true
				}
				w.mu.Unlock()
			})
		},
		Close: func() {
			once.Do(func() {
				_ = conn.Close()
				if takeOver {
					w.mu.Lock()
					w.person--
					if w.person == 0 {
						w.handback = time.Now()
					}
					w.mu.Unlock()
				}
			})
		},
	}, nil
}

// Halt is Stop: this Worker may not use its display until the person takes
// it over and hands it back.
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

// List is every Worker's display, in a stable order.
func (s *Service) List() []Info {
	s.mu.Lock()
	var workers []string
	for id := range s.displays {
		workers = append(workers, id)
	}
	s.mu.Unlock()
	var all []*workerDisplay
	for _, id := range workers {
		if w := s.live(id); w != nil {
			all = append(all, w)
		}
	}
	var out []Info
	for _, w := range all {
		w.mu.Lock()
		d := w.display
		info := Info{ID: w.worker, Control: "agent", Stopped: w.halted}
		if w.person > 0 {
			info.Control = "person"
		}
		w.mu.Unlock()
		if d == nil || !d.Alive() {
			continue
		}
		info.Width, info.Height = d.Width, d.Height
		d.mu.Lock()
		info.Apps = len(d.apps)
		d.mu.Unlock()
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Forget ends a Worker's display, as when the Worker ends.
func (s *Service) Forget(worker string) {
	s.mu.Lock()
	w := s.displays[worker]
	delete(s.displays, worker)
	s.mu.Unlock()
	if w != nil && w.display != nil {
		w.client.Close()
		w.display.Stop()
	}
}

// Close ends every display.
func (s *Service) Close() {
	s.mu.Lock()
	all := s.displays
	s.displays = map[string]*workerDisplay{}
	s.mu.Unlock()
	for _, w := range all {
		if w.display != nil {
			w.client.Close()
			w.display.Stop()
		}
	}
	s.rootMu.Lock()
	if s.ownsRoot {
		_ = os.RemoveAll(s.root)
		s.root, s.ownsRoot = "", false
	}
	s.rootMu.Unlock()
}
