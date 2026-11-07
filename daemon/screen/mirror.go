package screen

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sort"
	"strconv"
	"strings"
	"time"
)

// iPhone use drives the person's iPhone through iPhone Mirroring, the Mac app
// that shows a locked iPhone nearby in a window and passes clicks and keys to
// it. The window is one app on this Mac, so an agent works in it like in any
// app behind the person's windows: it is claimed by one Worker, moved to the
// agent display when there is one, read as a picture and clicked and typed
// into through the window server (bgclick.go), never with the pointer. A
// person who unlocks the iPhone pauses the mirror; Take over, Stop and the
// switch end the agent as for any app.

// MirrorBundleID is iPhone Mirroring's bundle identifier.
const MirrorBundleID = "com.apple.ScreenContinuity"

// ErrNoMirror is the answer on a computer with no iPhone Mirroring.
var ErrNoMirror = errors.New("this Mac has no iPhone Mirroring (it needs macOS 15 or later, and an Apple account outside the EU)")

// MirrorKey names the keys an agent sends to the iPhone.
const (
	MirrorReturn    = "return"
	MirrorDelete    = "delete"
	MirrorTab       = "tab"
	MirrorEscape    = "escape"
	MirrorSelectAll = "select_all"
	// Menu items of iPhone Mirroring's View menu.
	MirrorHome        = "home"
	MirrorAppSwitcher = "app_switcher"
	MirrorSpotlight   = "spotlight"
)

// KeyStroke is one key, with the modifier flags held while it is pressed and
// the character it types, if any.
type KeyStroke struct {
	Code  uint16
	Flags uint64
	Text  rune
}

// mirrorKeys are the keys an agent may send, as strokes.
var mirrorKeys = map[string]KeyStroke{
	MirrorReturn:    {Code: 36},
	MirrorDelete:    {Code: 51},
	MirrorTab:       {Code: 48},
	MirrorEscape:    {Code: 53},
	MirrorSelectAll: {Code: 0, Flags: macFlagCommand},
}

// mirrorMenu are the View menu's items, by their shortcut.
var mirrorMenu = map[string]string{MirrorHome: "1", MirrorAppSwitcher: "2", MirrorSpotlight: "3"}

// asciiStrokes is text as key strokes on a US keyboard, which is what the
// mirror passes on: it drops a key event that carries only its character.
// ok is false when the text has a character no key types.
func asciiStrokes(text string) (strokes []KeyStroke, ok bool) {
	for _, r := range text {
		if r == '\n' {
			strokes = append(strokes, mirrorKeys[MirrorReturn])
			continue
		}
		key, found := macANSIKeys[r]
		if !found {
			return nil, false
		}
		stroke := KeyStroke{Code: key.code, Text: r}
		if key.shift {
			stroke.Flags = macFlagShift
		}
		strokes = append(strokes, stroke)
	}
	return strokes, true
}

// SwipeHow is the kind of event a swipe is sent as. iPhone Mirroring on
// macOS 26 and later scrolls for a trackpad's phased scroll and reads a
// plain mouse drag as a tap; which one a Mac takes is measured on it.
type SwipeHow string

const (
	SwipeScroll SwipeHow = "scroll" // phased trackpad scroll events
	SwipeDrag   SwipeHow = "drag"   // button down, drag, up
	SwipeWheel  SwipeHow = "wheel"  // plain line wheel events
)

// TextLine is one line of text read off a picture, in its pixels.
type TextLine struct {
	Text       string
	Box        image.Rectangle
	Confidence float64
}

// mirrorInput is a session that can do more than click in one app's window
// behind others: hold, swipe and send key strokes and text.
type mirrorInput interface {
	HoldTo(pid int, window AppWindow, x, y int, hold time.Duration) error
	SwipeTo(pid int, window AppWindow, x, y, dx, dy int, how SwipeHow) error
	StrokesTo(pid int, window AppWindow, strokes []KeyStroke) error
	// PasteTo puts text on the pasteboard, sends ⌘V and puts back what the
	// pasteboard held.
	PasteTo(pid int, window AppWindow, text string) error
}

// mirrorHolder is a backend that knows whether iPhone Mirroring is on this
// computer at all (macOS 15 and later).
type mirrorHolder interface {
	MirrorInstalled() bool
}

// textReader is a backend that reads the text on a picture.
type textReader interface {
	ReadText(ctx context.Context, img *image.RGBA) ([]TextLine, error)
}

// MirrorInfo is iPhone Mirroring as it stands, read without touching it.
type MirrorInfo struct {
	// Supported is false on a computer that can't have it.
	Supported bool
	// Running is whether the app is open, and Window whether it shows one.
	Running bool
	Window  bool
	// Front is whether it is the app in front: the person's, then.
	Front bool
	// Worker is the agent working in it.
	Worker string
}

// isMirror says whether an app is iPhone Mirroring.
func isMirror(app App) bool {
	if app.BundleID != "" {
		return app.BundleID == MirrorBundleID
	}
	return app.Bundle == "iPhone Mirroring.app" || app.Name == "iPhone Mirroring"
}

// namesMirror says whether an app name an agent gave mewla_computer means
// iPhone Mirroring, which only the iPhone tools work in.
func namesMirror(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == strings.ToLower(MirrorBundleID) || strings.Contains(n, "iphone mirroring") || strings.Contains(n, "iphone 镜像") || strings.Contains(n, "iphone镜像")
}

// ErrMirrorForIPhoneTools is mewla_computer's answer for iPhone Mirroring.
var ErrMirrorForIPhoneTools = errors.New("iPhone Mirroring is the person's iPhone: use the mewla_iphone tools for it, not the computer's")

// MirrorInfo reads iPhone Mirroring's state, for the status.
func (s *Service) MirrorInfo(ctx context.Context) MirrorInfo {
	reader, ok := s.backend.(appReader)
	if !ok {
		return MirrorInfo{}
	}
	info := MirrorInfo{Supported: s.backend.Capability(ctx).Supported}
	if holder, ok := s.backend.(mirrorHolder); !ok || !holder.MirrorInstalled() {
		info.Supported = false
	}
	apps, err := reader.Apps(ctx)
	if err != nil {
		return info
	}
	for _, app := range apps {
		if isMirror(app) {
			info.Running, info.Window, info.Front = true, len(app.Windows) > 0, app.Active
			s.agentMu.Lock()
			info.Worker = s.appOwners[app.PID]
			s.agentMu.Unlock()
			break
		}
	}
	return info
}

// mirrorNoPark leaves the mirror where it is instead of moving it to the
// agent display: the spike measures both.
var mirrorNoPark = false

// mirrorOpenWait is how long a mirror that was just opened has to show its window.
const mirrorOpenWait = 8 * time.Second

// mirrorTarget is the mirror's window, ready for one action of an agent.
type mirrorTarget struct {
	agent  *agentSession
	app    App
	window AppWindow
	parked bool
}

// withMirror runs one action of a Worker in iPhone Mirroring: through the
// same gates as any agent action, with the app opened if it isn't, claimed
// for the Worker and on the agent display when there is one. It is always
// work behind the person's windows, so it neither waits for them nor takes
// the pointer.
func (s *Service) withMirror(ctx context.Context, worker string, fn func(t mirrorTarget) error) error {
	if strings.TrimSpace(worker) == "" {
		return errors.New("iPhone use needs the Worker it acts for")
	}
	if _, err := s.AgentReady(ctx); err != nil {
		return err
	}
	agent, err := s.agentFor(worker)
	if err != nil {
		return err
	}
	defer agent.endUse() // agentFor counted it
	select {
	case <-agent.ready:
	case <-time.After(agentFirstFrame):
		return errors.New("the screen did not answer in time")
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := context.Cause(agent.ctx); err != nil {
		return err
	}
	if agent.apps == nil {
		return ErrNoMirror
	}
	app, err := s.openMirror(ctx, agent)
	if err != nil {
		return err
	}
	if app.Active && !agent.isParked(app.PID) {
		return ErrMirrorInFront
	}
	if err := s.claimApp(app.PID, worker); err != nil {
		return errors.New(strings.Replace(err.Error(), "is working in that app. Work in another app, or wait until it is done.", "is using the iPhone. Wait until it is done.", 1))
	}
	if !mirrorNoPark {
		if _, err := agent.maybePark(ctx, AgentAction{Action: "left_click", App: strconv.Itoa(app.PID)}); err != nil {
			return err
		}
	}
	// Where its window is now, after any move.
	if app, err = findMirror(ctx, agent.apps); err != nil {
		return err
	}
	if len(app.Windows) == 0 {
		return ErrMirrorNoWindow
	}
	err = fn(mirrorTarget{agent: agent, app: app, window: app.Windows[0], parked: agent.isParked(app.PID)})
	agent.mu.Lock()
	agent.lastAct = time.Now()
	agent.mu.Unlock()
	return err
}

// Refusals of iPhone use an agent can act on.
var (
	ErrMirrorInFront  = errors.New("The person has iPhone Mirroring in front on the Mac and is using it. Stop and tell them; it is yours again when they switch to another app.")
	ErrMirrorNoWindow = errors.New("iPhone Mirroring is open but shows no window. Tell the person to open iPhone Mirroring on the Mac once, and stop.")
)

func findMirror(ctx context.Context, reader appReader) (App, error) {
	apps, err := reader.Apps(ctx)
	if err != nil {
		return App{}, err
	}
	for _, app := range apps {
		if isMirror(app) {
			return app, nil
		}
	}
	return App{}, errMirrorNotRunning
}

var errMirrorNotRunning = errors.New("iPhone Mirroring isn't running")

// openMirror finds the mirror, opening it behind the app in front if it
// isn't running, and gives the front back if opening it took it.
func (s *Service) openMirror(ctx context.Context, agent *agentSession) (App, error) {
	app, err := findMirror(ctx, agent.apps)
	if err == nil && len(app.Windows) > 0 {
		return app, nil
	}
	if !errors.Is(err, errMirrorNotRunning) && err != nil {
		return App{}, err
	}
	opener, ok := agent.apps.(appOpener)
	if !ok {
		return App{}, ErrNoMirror
	}
	before := 0
	if fronter, ok := agent.apps.(interface {
		FrontPID(context.Context) (int, error)
	}); ok {
		before, _ = fronter.FrontPID(ctx)
	}
	// Not running, or running with its window closed: opening it again shows one.
	if err := opener.OpenApp(ctx, MirrorBundleID); err != nil {
		return App{}, fmt.Errorf("%w: %v", ErrNoMirror, err)
	}
	deadline := time.Now().Add(mirrorOpenWait)
	for {
		app, err = findMirror(ctx, agent.apps)
		if err == nil && len(app.Windows) > 0 {
			break
		}
		if time.Now().After(deadline) {
			if err != nil {
				return App{}, fmt.Errorf("iPhone Mirroring did not open: %w", err)
			}
			return App{}, ErrMirrorNoWindow
		}
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return App{}, ctx.Err()
		}
	}
	if app.Active && before != 0 && before != app.PID {
		if raiser, ok := agent.apps.(appRaiser); ok {
			_ = raiser.RaiseApp(ctx, before)
			if again, err := findMirror(ctx, agent.apps); err == nil {
				app = again
			}
		}
	}
	return app, nil
}

// MirrorShot is the mirror window's picture, in its own pixels (the units
// of every point the other Mirror actions take), not scaled down.
func (s *Service) MirrorShot(ctx context.Context, worker string) (*image.RGBA, error) {
	var shot *image.RGBA
	err := s.withMirror(ctx, worker, func(t mirrorTarget) error {
		if t.parked {
			parker := t.agent.apps.(appParker)
			img, err := parker.ParkedImage(ctx, t.window.Bounds)
			if err != nil {
				return fmt.Errorf("the iPhone's picture isn't available: %w", err)
			}
			shot = img
			return nil
		}
		a := t.agent
		a.mu.Lock()
		defer a.mu.Unlock()
		r := t.window.Bounds.Intersect(a.latest.Rect)
		if r.Empty() {
			return ErrMirrorNoWindow
		}
		out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
		for y := 0; y < r.Dy(); y++ {
			copy(out.Pix[y*out.Stride:y*out.Stride+r.Dx()*4], a.latest.Pix[a.latest.PixOffset(r.Min.X, r.Min.Y+y):])
		}
		shot = out
		return nil
	})
	return shot, err
}

// mirrorPoint is a point of the window's picture in framebuffer pixels,
// refused when it is off the window.
func mirrorPoint(t mirrorTarget, x, y int) (image.Point, error) {
	p := t.window.Bounds.Min.Add(image.Pt(x, y))
	if !p.In(t.window.Bounds) {
		return image.Point{}, fmt.Errorf("(%d, %d) is outside the iPhone's %dx%d picture", x, y, t.window.Bounds.Dx(), t.window.Bounds.Dy())
	}
	return p, nil
}

func (t mirrorTarget) input() (mirrorInput, appClicker, error) {
	clicker, ok := t.agent.session.(appClicker)
	input, ok2 := t.agent.session.(mirrorInput)
	if !ok || !ok2 {
		return nil, nil, ErrNoMirror
	}
	if can, why := clicker.BackgroundClicks(); !can {
		return nil, nil, fmt.Errorf("this Mac can't send input to iPhone Mirroring behind other windows: %s", why)
	}
	return input, clicker, nil
}

// noteAppInput records input sent to the mirror, which the Mac may count as
// input, so it is not taken for a person's.
func (t mirrorTarget) noteAppInput(key bool) {
	a := t.agent
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if key {
		a.lastAppKey = now
		a.shared.noteAppKey(now)
	} else {
		a.lastAppClick = now
		a.shared.noteAppClick(now)
	}
}

// MirrorTap taps the iPhone at a point of the window's picture; a hold
// above zero presses that long.
func (s *Service) MirrorTap(ctx context.Context, worker string, x, y int, hold time.Duration) error {
	return s.withMirror(ctx, worker, func(t mirrorTarget) error {
		p, err := mirrorPoint(t, x, y)
		if err != nil {
			return err
		}
		input, clicker, err := t.input()
		if err != nil {
			return err
		}
		defer t.noteAppInput(false)
		if hold > 0 {
			return input.HoldTo(t.app.PID, t.window, p.X, p.Y, hold)
		}
		return clicker.ClickTo(t.app.PID, t.window, p.X, p.Y, 0, 1)
	})
}

// MirrorSwipe moves a finger on the iPhone from x1, y1 to x2, y2 of the
// window's picture.
func (s *Service) MirrorSwipe(ctx context.Context, worker string, x1, y1, x2, y2 int, how SwipeHow) error {
	return s.withMirror(ctx, worker, func(t mirrorTarget) error {
		p, err := mirrorPoint(t, x1, y1)
		if err != nil {
			return err
		}
		if _, err := mirrorPoint(t, x2, y2); err != nil {
			return err
		}
		input, _, err := t.input()
		if err != nil {
			return err
		}
		defer t.noteAppInput(false)
		return input.SwipeTo(t.app.PID, t.window, p.X, p.Y, x2-x1, y2-y1, how)
	})
}

// MirrorType types text into the iPhone's focused field: plain keys for what
// a US keyboard types, the pasteboard for the rest.
func (s *Service) MirrorType(ctx context.Context, worker, text string) error {
	if text == "" {
		return nil
	}
	return s.withMirror(ctx, worker, func(t mirrorTarget) error {
		input, _, err := t.input()
		if err != nil {
			return err
		}
		defer t.noteAppInput(true)
		if strokes, ok := asciiStrokes(text); ok {
			return input.StrokesTo(t.app.PID, t.window, strokes)
		}
		return input.PasteTo(t.app.PID, t.window, text)
	})
}

// MirrorKey presses one of the keys in mirrorKeys, or chooses one of the
// View menu's items (home, app switcher, Spotlight).
func (s *Service) MirrorKey(ctx context.Context, worker, name string) error {
	return s.withMirror(ctx, worker, func(t mirrorTarget) error {
		if char, ok := mirrorMenu[name]; ok {
			if presser, ok := t.agent.apps.(menuPresser); ok {
				var before App
				if apps, err := t.agent.apps.Apps(ctx); err == nil {
					before = t.agent.frontApp(ctx, apps)
				}
				_, found, err := presser.PressMenuKey(ctx, t.app.PID, Shortcut{Char: char})
				// Choosing a menu item can bring the mirror forward: the front goes back to the person's app.
				t.agent.giveFrontBack(ctx, t.app, before)
				if err == nil && found {
					return nil
				}
			}
			// No such menu item here: its shortcut, to the window.
			input, _, err := t.input()
			if err != nil {
				return err
			}
			defer t.noteAppInput(true)
			key := macANSIKeys[rune(char[0])]
			return input.StrokesTo(t.app.PID, t.window, []KeyStroke{{Code: key.code, Flags: macFlagCommand}})
		}
		stroke, ok := mirrorKeys[name]
		if !ok {
			return fmt.Errorf("unknown iPhone key %q", name)
		}
		input, _, err := t.input()
		if err != nil {
			return err
		}
		defer t.noteAppInput(true)
		return input.StrokesTo(t.app.PID, t.window, []KeyStroke{stroke})
	})
}

// MirrorRead reads the text on a picture of the iPhone.
func (s *Service) MirrorRead(ctx context.Context, img *image.RGBA) ([]TextLine, error) {
	reader, ok := s.backend.(textReader)
	if !ok {
		return nil, errors.New("this computer can't read text off a picture")
	}
	return reader.ReadText(ctx, img)
}

// MirrorRelease ends one Worker's use of the iPhone: the mirror goes back
// where it was and is free for another.
func (s *Service) MirrorRelease(worker string) {
	s.agentMu.Lock()
	agent := s.agents[worker]
	s.agentMu.Unlock()
	if agent == nil {
		return
	}
	reader := agent.apps
	if reader == nil {
		return
	}
	app, err := findMirror(context.Background(), reader)
	if err != nil {
		return
	}
	agent.unpark(app.PID)
	s.freeApp(app.PID, worker)
}

// sortLines puts lines in reading order: top to bottom, and left to right
// within a row (lines whose middles are within half a line's height).
func sortLines(lines []TextLine) {
	sort.SliceStable(lines, func(i, j int) bool {
		a, b := lines[i].Box, lines[j].Box
		ay, by := (a.Min.Y+a.Max.Y)/2, (b.Min.Y+b.Max.Y)/2
		tol := max(min(a.Dy(), b.Dy())/2, 1)
		if ay-by > tol || by-ay > tol {
			return ay < by
		}
		return a.Min.X < b.Min.X
	})
}
