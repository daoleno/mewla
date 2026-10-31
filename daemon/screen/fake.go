package screen

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"time"
)

// FakeBackend is a scripted screen for tests and the `screenfake` build. Its
// frame shows the pointer, a square at every click and a block per key, so a
// viewer can see its input arrive.
type FakeBackend struct {
	Width, Height int
	Unsupported   bool
	Missing       []string // ids of permissions not granted
	Answer        bool     // Confirm's answer
	Locked        bool
	// Tree is what Elements reads; nil reads as no accessibility.
	Tree *ElementTree

	mu        sync.Mutex
	changed   chan struct{}
	version   uint64
	pointer   image.Point
	clicks    []image.Point
	wheels    int
	keys      []uint32
	Pointers  []FakePointer
	Keys      []FakeKey
	Opened    []string
	Confirmed []string
	Requested int
	input     [inputKinds]time.Time
	// AppEventsSeen makes keys and clicks sent to one app count as input
	// too, as a Mac might.
	AppEventsSeen bool
	// sleeps counts SleepDisplay; a capture started before the last one
	// has stopped, and starting a new capture wakes the display.
	sleeps      int
	woken       int
	DisplayHeld bool
	// RunningApps are the apps an agent can read; Retained counts the
	// element handles read and not yet released.
	RunningApps    []FakeApp
	Retained       int
	ElementActions []FakeElementAction
	// OnElementAction, when set, runs after an element action on a fake app,
	// to change its elements as the app would: a sheet opening, a field
	// clearing. It runs holding the backend's lock.
	OnElementAction func(app *FakeApp, index int, action string)
	// Reads and Prints count the reads and fingerprints of fake apps.
	Reads, Prints int
	AppKeys       []FakeAppKey
	// SkyLight lets clicks reach an app behind others; AppClicks are those
	// clicks and Raised the apps brought to the front.
	SkyLight  bool
	HitTest   bool
	AppClicks []FakeAppClick
	Raised    []int
	// InstalledApps are apps OpenApp can start; Launched is what it was asked.
	InstalledApps []FakeInstalledApp
	Launched      []string
	// MenuPresses are the menu items chosen by shortcut.
	MenuPresses []string
	// Scroll, when above zero, shows a page of text and pictures that
	// scrolls this many pixels at every ScrollTick, for measuring a link.
	Scroll   int
	offset   int
	document *image.RGBA
}

// FakeAppClick is a click sent to one app's window behind others.
type FakeAppClick struct {
	PID           int
	Window        uint32
	X, Y          int
	Button, Count int
}

// SleepDisplay turns the display off, which stops every running capture.
func (f *FakeBackend) SleepDisplay() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sleeps++
	f.bump()
}

// ScrollTick moves the scrolling page one step, as a finger flicking a long
// page would, and counts as a change of the screen.
func (f *FakeBackend) ScrollTick() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offset += f.Scroll
	f.bump()
}

func (f *FakeBackend) HoldDisplay(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DisplayHeld = on
}

// Presence reports the fake's lock and its last input, which every injected
// event updates too, the way a real system may count an agent's events.
func (f *FakeBackend) Presence() Presence {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Presence{Locked: f.Locked, Input: f.input}
}

// Elements returns the scripted tree, as a Mac reads the focused window.
func (f *FakeBackend) Elements(context.Context, int) (ElementTree, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Tree == nil {
		return ElementTree{}, ErrNoElements
	}
	return *f.Tree, nil
}

// PersonInput is someone typing on the computer's own keyboard.
func (f *FakeBackend) PersonInput() {
	f.PersonInputOf(InputKey)
}

// PersonInputOf is someone making one kind of input.
func (f *FakeBackend) PersonInputOf(kind InputKind) {
	f.mu.Lock()
	f.input[kind] = time.Now()
	f.mu.Unlock()
}

// ForgetInput clears the input seen so far.
func (f *FakeBackend) ForgetInput() {
	f.mu.Lock()
	f.input = [inputKinds]time.Time{}
	f.mu.Unlock()
}

// SetLocked locks or unlocks the fake screen.
func (f *FakeBackend) SetLocked(locked bool) {
	f.mu.Lock()
	f.Locked = locked
	f.mu.Unlock()
}

type FakePointer struct {
	X, Y    int
	Buttons uint8
}

type FakeKey struct {
	Keysym uint32
	Down   bool
}

// FakeAppKey is a key sent to one app.
type FakeAppKey struct {
	PID int
	FakeKey
}

func NewFakeBackend(width, height int) *FakeBackend {
	return &FakeBackend{Width: width, Height: height, Answer: true, changed: make(chan struct{})}
}

func (f *FakeBackend) Capability(context.Context) Capability {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Unsupported {
		return Capability{Reason: ErrUnsupported.Error()}
	}
	perms := []Permission{
		{ID: "screen_recording", Name: "Screen Recording", Granted: true, CanOpen: true},
		{ID: "accessibility", Name: "Accessibility", Granted: true, CanOpen: true},
	}
	for i := range perms {
		for _, id := range f.Missing {
			if perms[i].ID == id {
				perms[i].Granted = false
				perms[i].Guidance = "Allow Mewla in " + perms[i].Name
			}
		}
	}
	return Capability{Supported: true, Permissions: perms}
}

func (f *FakeBackend) RequestPermissions(context.Context) {
	f.mu.Lock()
	f.Requested++
	f.mu.Unlock()
}

func (f *FakeBackend) OpenSettings(_ context.Context, permission string) error {
	f.mu.Lock()
	f.Opened = append(f.Opened, permission)
	f.mu.Unlock()
	return nil
}

func (f *FakeBackend) Confirm(_ context.Context, device string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Confirmed = append(f.Confirmed, device)
	return f.Answer, nil
}

func (f *FakeBackend) Start(context.Context) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sleeps != f.woken {
		f.woken = f.sleeps
		displayWoke.Store(time.Now().UnixNano())
	}
	return &fakeSession{backend: f, image: image.NewRGBA(image.Rect(0, 0, f.Width, f.Height)), sleeps: f.sleeps}, nil
}

// bump marks a change; callers hold f.mu.
func (f *FakeBackend) bump() {
	f.version++
	close(f.changed)
	f.changed = make(chan struct{})
}

type fakeSession struct {
	backend *FakeBackend
	image   *image.RGBA
	sleeps  int
}

func (s *fakeSession) Frame(ctx context.Context, after uint64) (Frame, error) {
	f := s.backend
	for {
		f.mu.Lock()
		if f.sleeps != s.sleeps {
			f.mu.Unlock()
			return Frame{}, fmt.Errorf("%w: no display to capture", ErrCaptureStopped)
		}
		if f.version+1 > after {
			s.draw()
			frame := Frame{Seq: f.version + 1, Image: s.image, At: time.Now()}
			f.mu.Unlock()
			return frame, nil
		}
		wait := f.changed
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return Frame{}, ctx.Err()
		case <-wait:
		}
	}
}

// draw renders the scene; callers hold backend.mu.
func (s *fakeSession) draw() {
	f := s.backend
	img := s.image
	if f.Scroll > 0 {
		f.drawPage(img)
		return
	}
	for y := 0; y < f.Height; y++ {
		for x := 0; x < f.Width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 255 / f.Width), G: 96, B: uint8(y * 255 / f.Height), A: 255})
		}
	}
	square := func(p image.Point, size int, c color.RGBA) {
		r := image.Rect(p.X-size, p.Y-size, p.X+size, p.Y+size).Intersect(img.Rect)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	for _, c := range f.clicks {
		square(c, 12, color.RGBA{R: 255, G: 220, B: 0, A: 255})
	}
	for i, k := range f.keys {
		p := image.Pt(20+(i%40)*18, 20+(i/40)*18)
		square(p, 7, color.RGBA{R: uint8(k * 37), G: 255, B: uint8(k * 91), A: 255})
	}
	for i := 0; i < f.wheels && i < 60; i++ {
		square(image.Pt(f.Width-12, 12+i*12), 5, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	}
	square(f.pointer, 4, color.RGBA{A: 255})
}

func (s *fakeSession) Pointer(x, y int, buttons uint8) error {
	f := s.backend
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Pointers = append(f.Pointers, FakePointer{X: x, Y: y, Buttons: buttons})
	kind := InputPointer
	if buttons&(8|16|32|64) != 0 {
		kind = InputScroll
	} else if buttons&7 != 0 || image.Pt(x, y) == f.pointer {
		kind = InputClick
	}
	f.input[kind] = time.Now()
	f.pointer = image.Pt(x, y)
	if buttons&1 != 0 {
		f.clicks = append(f.clicks, f.pointer)
	}
	if buttons&(8|16|32|64) != 0 {
		f.wheels++
	}
	f.bump()
	return nil
}

func (s *fakeSession) Key(keysym uint32, down bool) error {
	f := s.backend
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Keys = append(f.Keys, FakeKey{Keysym: keysym, Down: down})
	f.input[InputKey] = time.Now()
	if down {
		f.keys = append(f.keys, keysym)
		f.bump()
	}
	return nil
}

// KeyTo sends a key to one app. It goes to the app's own queue, not
// through the computer's input system, so it is not taken for a person's.
func (s *fakeSession) KeyTo(pid int, keysym uint32, down bool) error {
	f := s.backend
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AppKeys = append(f.AppKeys, FakeAppKey{pid, FakeKey{keysym, down}})
	if f.AppEventsSeen {
		f.input[InputKey] = time.Now()
	}
	return nil
}

func (s *fakeSession) BackgroundClicks() (bool, string) {
	f := s.backend
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.SkyLight, "SkyLight isn't available on this Mac"
}

func (s *fakeSession) ClickTo(pid int, window uint32, x, y, button, count int) error {
	f := s.backend
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AppClicks = append(f.AppClicks, FakeAppClick{pid, window, x, y, button, count})
	if f.AppEventsSeen {
		f.input[InputClick] = time.Now()
	}
	return nil
}

// RaiseApp brings a fake app to the front.
func (f *FakeBackend) RaiseApp(_ context.Context, pid int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Raised = append(f.Raised, pid)
	for i := range f.RunningApps {
		f.RunningApps[i].Active = f.RunningApps[i].PID == pid
	}
	return nil
}

var (
	_ appKeyer   = (*fakeSession)(nil)
	_ appClicker = (*fakeSession)(nil)
	_ appRaiser  = (*FakeBackend)(nil)
)

func (s *fakeSession) Close() error { return nil }

// Snapshot returns copies of the recorded input, for tests.
func (f *FakeBackend) Snapshot() ([]FakePointer, []FakeKey) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakePointer(nil), f.Pointers...), append([]FakeKey(nil), f.Keys...)
}

// FakeApp is a running app of the fake: its windows' elements in reading
// order.
type FakeApp struct {
	App
	Elements []Element
	Menu     []FakeMenuItem
	// OtherDesktop puts its windows on another desktop: on the screen, and
	// readable, only while it is in front.
	OtherDesktop bool
}

// FakeMenuItem is a fake app's menu item with a shortcut: Char and
// Modifiers as accessibility gives them. Choosing it adds Adds to the app's
// elements, as a new note does, and with Activates brings the app to the
// front, as choosing Notes' New Note through accessibility does on a Mac.
type FakeMenuItem struct {
	Path      string
	Char      string
	Modifiers int64
	Disabled  bool
	Activates bool
	Adds      []Element
}

// FrontPID is the fake's app in front.
func (f *FakeBackend) FrontPID(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, app := range f.RunningApps {
		if app.Active {
			return app.PID, nil
		}
	}
	return 0, nil
}

// PressMenuKey chooses the fake app's menu item with the shortcut.
func (f *FakeBackend) PressMenuKey(_ context.Context, pid int, shortcut Shortcut) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.RunningApps {
		app := &f.RunningApps[i]
		if app.PID != pid {
			continue
		}
		for _, item := range app.Menu {
			if !shortcut.matches(item.Char, item.Modifiers) {
				continue
			}
			if item.Disabled {
				return item.Path, true, menuItemOff{item.Path, shortcut}
			}
			f.MenuPresses = append(f.MenuPresses, item.Path)
			app.Elements = append(app.Elements, item.Adds...)
			if item.Activates {
				for j := range f.RunningApps {
					f.RunningApps[j].Active = f.RunningApps[j].PID == pid
				}
			}
			return item.Path, true, nil
		}
		return "", false, nil
	}
	return "", false, errors.New("the app quit")
}

var _ menuPresser = (*FakeBackend)(nil)

// OpenApp starts one of the fake's InstalledApps, by name or bundle id.
func (f *FakeBackend) OpenApp(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Launched = append(f.Launched, name)
	for i, app := range f.InstalledApps {
		if strings.EqualFold(app.Name, name) || app.Bundle == name {
			running := app.FakeApp
			running.BundleID = app.Bundle
			f.RunningApps = append(f.RunningApps, running)
			f.InstalledApps = append(f.InstalledApps[:i], f.InstalledApps[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("macOS couldn't open %s: Unable to find application named '%s'", name, name)
}

// FakeInstalledApp is an app open_app can start.
type FakeInstalledApp struct {
	FakeApp
	Bundle string
}

// Apps lists the fake's running apps.
func (f *FakeBackend) Apps(context.Context) ([]App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	apps := make([]App, len(f.RunningApps))
	for i, app := range f.RunningApps {
		apps[i] = app.App
		if app.OtherDesktop {
			apps[i].Hidden = !app.Active
		}
	}
	return apps, nil
}

// ReadApp reads one fake app; each element's handle is its app and place.
func (f *FakeBackend) ReadApp(_ context.Context, pid int, limit int) (ElementTree, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Reads++
	for _, app := range f.RunningApps {
		if app.PID != pid {
			continue
		}
		tree := ElementTree{App: app.Name, PID: pid}
		if app.OtherDesktop && !app.Active {
			return tree, nil
		}
		for i, e := range app.Elements {
			if i == limit {
				tree.Truncated = true
				break
			}
			e.ref = uintptr(pid)<<16 | uintptr(i+1)
			tree.Elements = append(tree.Elements, e)
		}
		f.Retained += len(tree.Elements)
		return tree, nil
	}
	return ElementTree{}, errors.New("the app quit")
}

// PrintApp fingerprints a fake app by its top-level elements, those directly
// under the first, the first focused element and the first one's title.
func (f *FakeBackend) PrintApp(_ context.Context, pid int) (AppPrint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Prints++
	for _, app := range f.RunningApps {
		if app.PID != pid {
			continue
		}
		var print AppPrint
		if app.OtherDesktop && !app.Active {
			return print, nil
		}
		for i, e := range app.Elements {
			switch {
			case e.Depth == 0:
				print.Windows++
				if print.Windows == 1 {
					print.Title = e.Title
				}
			case e.Depth == 1 && print.Windows == 1:
				print.Children++
			}
			if e.Focused && print.Focus == 0 {
				print.Focus = uintptr(i + 1)
			}
		}
		return print, nil
	}
	return AppPrint{}, errors.New("the app quit")
}

var _ appPrinter = (*FakeBackend)(nil)

// PressAt presses the last fake element of the app whose frame holds the
// point and that offers AXPress, when HitTest is on.
func (f *FakeBackend) PressAt(_ context.Context, pid, x, y int) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.HitTest {
		return "", false, nil
	}
	for _, app := range f.RunningApps {
		if app.PID != pid {
			continue
		}
		for i := len(app.Elements) - 1; i >= 0; i-- {
			e := app.Elements[i]
			if image.Pt(x, y).In(e.Frame) && slices.Contains(e.Actions, "AXPress") {
				f.ElementActions = append(f.ElementActions, FakeElementAction{PID: pid, Index: i, Action: "AXPress"})
				return fmt.Sprintf("%s %q", strings.TrimPrefix(e.Role, "AX"), e.Title), true, nil
			}
		}
	}
	return "", false, nil
}

// ElementHash and SameElement take a fake element for the same one when it
// is in the same place with the same role; a Mac compares the live element.
func (f *FakeBackend) ElementHash(e Element) uintptr { return e.ref }

func (f *FakeBackend) SameElement(a, b Element) bool { return a.ref == b.ref && a.Role == b.Role }

func (f *FakeBackend) Release(tree ElementTree) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Retained -= len(tree.Elements)
}

var _ appReader = (*FakeBackend)(nil)

// FakeElementAction is one action an agent took on a fake app's element.
type FakeElementAction struct {
	PID, Index    int
	Action, Value string
}

// ActOnElement acts on a fake element, refusing one replaced since it was
// read; set_value changes what the next read shows.
func (f *FakeBackend) ActOnElement(_ context.Context, e Element, action, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	pid, index := int(e.ref>>16), int(e.ref&0xffff)-1
	for i := range f.RunningApps {
		app := &f.RunningApps[i]
		if app.PID != pid || index < 0 || index >= len(app.Elements) {
			continue
		}
		live := &app.Elements[index]
		if live.Role != e.Role || live.Title != e.Title {
			continue
		}
		if action == "set_value" {
			live.Value = value
		}
		f.ElementActions = append(f.ElementActions, FakeElementAction{PID: app.PID, Index: index, Action: action, Value: value})
		if f.OnElementAction != nil {
			f.OnElementAction(app, index, action)
		}
		return nil
	}
	return ErrStaleElement
}

// ReadValue gives a fake element's live value, cut at limit characters.
func (f *FakeBackend) ReadValue(_ context.Context, e Element, limit int) (string, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pid, index := int(e.ref>>16), int(e.ref&0xffff)-1
	for _, app := range f.RunningApps {
		if app.PID != pid || index < 0 || index >= len(app.Elements) {
			continue
		}
		live := app.Elements[index]
		if live.Role != e.Role || live.Title != e.Title {
			break
		}
		runes := []rune(live.Value)
		return string(runes[:min(limit, len(runes))]), len(runes), nil
	}
	return "", 0, ErrStaleElement
}

var (
	_ appActor    = (*FakeBackend)(nil)
	_ valueReader = (*FakeBackend)(nil)
)

// drawPage copies the visible part of a long page of text lines and
// pictures, wrapping, so every scroll step changes nearly every pixel the
// way a real document does, at a compressed size close to a real screen's.
func (f *FakeBackend) drawPage(img *image.RGBA) {
	if f.document == nil {
		f.document = fixturePage(f.Width, f.Height*4)
	}
	doc := f.document
	for y := 0; y < f.Height; y++ {
		src := (f.offset + y) % doc.Rect.Dy()
		copy(img.Pix[y*img.Stride:y*img.Stride+f.Width*4], doc.Pix[src*doc.Stride:src*doc.Stride+f.Width*4])
	}
}

// fixturePage is the same page every run: lines of dark words on white with
// a textured picture now and then.
func fixturePage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 250, 250, 248, 255
	}
	rng := rand.New(rand.NewSource(7))
	fill := func(r image.Rectangle, c func(x, y int) color.RGBA) {
		r = r.Intersect(img.Rect)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, c(x, y))
			}
		}
	}
	for y := 20; y < height-40; {
		if rng.Intn(9) == 0 {
			// A picture: smooth colour with grain.
			h := 140 + rng.Intn(100)
			base := color.RGBA{R: uint8(40 + rng.Intn(180)), G: uint8(40 + rng.Intn(180)), B: uint8(40 + rng.Intn(180)), A: 255}
			fill(image.Rect(40, y, width-40, y+h), func(x, y int) color.RGBA {
				n := rng.Intn(48) - 24
				return color.RGBA{R: clamp8(int(base.R) + n + x/16%40), G: clamp8(int(base.G) + n + y/12%40), B: clamp8(int(base.B) + n), A: 255}
			})
			y += h + 24
			continue
		}
		x := 40
		for x < width-80 {
			w := 14 + rng.Intn(70)
			shade := uint8(20 + rng.Intn(70))
			fill(image.Rect(x, y, x+w, y+11), func(int, int) color.RGBA { return color.RGBA{R: shade, G: shade, B: shade + 10, A: 255} })
			x += w + 9
		}
		y += 20
	}
	return img
}

func clamp8(v int) uint8 { return uint8(min(max(v, 0), 255)) }
