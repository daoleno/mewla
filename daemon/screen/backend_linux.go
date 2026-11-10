//go:build linux && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/vdisplay"
)

// On Linux an agent never uses the person's own screen: each one gets a
// display of its own, a monitor on the agent screen (package vdisplay), and
// drives apps there the way it drives a Mac's. X11 shows and takes input with
// no permission, and AT-SPI, on the agent screen's own accessibility bus,
// gives the apps' elements. The person watches, holds or stops each agent
// from Screen, through the agent screen's own views; their Hold and Stop and
// what they did reach every action through the display's Begin and End.

// errLinuxViewer is the answer to a viewer of the whole screen, which Linux
// does not have: the person views each agent's display instead.
var errLinuxViewer = errors.New(errLinuxViewerText)

func newBackend() Backend { return linuxBackend{} }

func platformBackend(displays *vdisplay.Service) Backend { return linuxBackend{displays: displays} }

type linuxBackend struct{ displays *vdisplay.Service }

func (b linuxBackend) Capability(context.Context) Capability {
	if b.displays == nil {
		return Capability{Reason: "Agents' displays are not running on this computer"}
	}
	if err := vdisplay.Capability(); err != nil {
		return Capability{Reason: "Agents use a display of their own on Linux, which needs TigerVNC: " + err.Error()}
	}
	return Capability{Supported: true}
}

func (linuxBackend) RequestPermissions(context.Context) {}

func (linuxBackend) OpenSettings(context.Context, string) error {
	return errors.New("Agents need no permission on Linux")
}

func (linuxBackend) Confirm(context.Context, string) (bool, error) { return false, errLinuxViewer }

func (linuxBackend) Start(context.Context) (Session, error) { return nil, errLinuxViewer }

// StartFor gives the Worker its own display as the screen it drives.
func (b linuxBackend) StartFor(ctx context.Context, worker string) (Session, error) {
	if b.displays == nil {
		return nil, ErrUnsupported
	}
	surface, err := b.displays.Surface(ctx, worker)
	if err != nil {
		return nil, err
	}
	return &linuxSession{surface: surface, names: map[int]string{}}, nil
}

// Begin and End wrap every action of the Worker's: the person's Hold and
// Stop refuse it, and End tells what they did on its display meanwhile.
func (b linuxBackend) Begin(worker string) error {
	if b.displays == nil {
		return nil
	}
	return b.displays.Begin(worker)
}

func (b linuxBackend) End(worker string) []string {
	if b.displays == nil {
		return nil
	}
	return b.displays.End(worker)
}

func computerName() string {
	name, _ := os.Hostname()
	return name
}

// ProcessPermissions has nothing to report: Linux asks for none.
func ProcessPermissions() map[string]bool { return nil }

// linuxSession is one agent's display, its apps and their elements.
type linuxSession struct {
	surface *vdisplay.Surface

	mu    sync.Mutex
	bus   *atspi
	names map[int]string
}

func (s *linuxSession) Frame(ctx context.Context, after uint64) (Frame, error) {
	img, seq, err := s.surface.Frame(ctx, after)
	if err != nil {
		return Frame{}, err
	}
	return Frame{Seq: seq, Image: img, At: time.Now()}, nil
}

func (s *linuxSession) Pointer(x, y int, buttons uint8) error {
	return s.surface.Pointer(x, y, buttons)
}

func (s *linuxSession) Key(keysym uint32, down bool) error { return s.surface.Key(keysym, down) }

func (s *linuxSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bus != nil {
		s.bus.close()
		s.bus = nil
	}
	return nil
}

// a11y is the connection to the display's accessibility bus, made on first
// use.
func (s *linuxSession) a11y() (*atspi, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bus != nil {
		return s.bus, nil
	}
	bus, err := dialATSPI(s.surface.A11yAddress())
	if err != nil {
		return nil, err
	}
	s.bus = bus
	return bus, nil
}

// name is an app's name: what it calls itself to accessibility, or its
// program's name.
func (s *linuxSession) name(ctx context.Context, pid int, apps map[int]atspiRef, bus *atspi) string {
	s.mu.Lock()
	name, ok := s.names[pid]
	s.mu.Unlock()
	if ok {
		return name
	}
	if ref, found := apps[pid]; found && bus != nil {
		name = bus.appName(ctx, ref)
	}
	if name == "" {
		comm, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		name = strings.TrimSpace(string(comm))
	}
	if name == "" {
		return ""
	}
	s.mu.Lock()
	s.names[pid] = name
	s.mu.Unlock()
	return name
}

func (s *linuxSession) Apps(ctx context.Context) ([]App, error) {
	windows, focus, err := s.surface.Windows()
	if err != nil {
		return nil, err
	}
	var accessible map[int]atspiRef
	bus, _ := s.a11y()
	if bus != nil {
		accessible, _ = bus.apps(ctx)
	}
	front := 0
	infos := make([]windowInfo, 0, len(windows))
	for _, w := range windows {
		if w.PID <= 0 {
			continue
		}
		if w.ID == focus {
			front = w.PID
		}
		infos = append(infos, windowInfo{PID: w.PID, Name: s.name(ctx, w.PID, accessible, bus), ID: w.ID, OnScreen: true, Bounds: w.Bounds})
	}
	return appsFromWindows(infos, nil, front, os.Getpid(), func(int) bundleInfo { return bundleInfo{} }), nil
}

func (s *linuxSession) ReadApp(ctx context.Context, pid int, limit int) (ElementTree, error) {
	bus, err := s.a11y()
	if err != nil {
		return ElementTree{PID: pid, Note: "This computer has no accessibility bus for agents' apps (install at-spi2-core); use screenshots.\n"}, nil
	}
	// An app joins the bus a moment after its window shows.
	var ref atspiRef
	found := false
	for deadline := time.Now().Add(2 * time.Second); ; {
		apps, err := bus.apps(ctx)
		if err != nil {
			return ElementTree{}, err
		}
		if ref, found = apps[pid]; found || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ElementTree{}, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !found {
		return ElementTree{PID: pid, Note: "This app doesn't show itself to accessibility; use screenshots.\n"}, nil
	}
	read, cancel := context.WithTimeout(ctx, atspiTimeout)
	defer cancel()
	tree, err := bus.read(read, ref, s.surface.Origin(), limit)
	if err != nil {
		return ElementTree{}, err
	}
	tree.PID = pid
	tree.App = s.name(ctx, pid, map[int]atspiRef{pid: ref}, bus)
	for _, e := range tree.Elements {
		if e.Depth == 0 && e.Title != "" {
			tree.Window = e.Title
			break
		}
	}
	return tree, nil
}

func (s *linuxSession) Release(tree ElementTree) {
	if bus, err := s.a11y(); err == nil {
		bus.release(tree)
	}
}

func (s *linuxSession) ActOnElement(ctx context.Context, e Element, action, value string) error {
	bus, err := s.a11y()
	if err != nil {
		return err
	}
	return bus.act(ctx, e, action, value)
}

func (s *linuxSession) ReadValue(ctx context.Context, e Element, limit int) (string, int, error) {
	bus, err := s.a11y()
	if err != nil {
		return "", 0, err
	}
	return bus.value(ctx, e, limit)
}

func (s *linuxSession) ElementHash(e Element) uintptr {
	if bus, err := s.a11y(); err == nil {
		return bus.hash(e)
	}
	return 0
}

func (s *linuxSession) SameElement(a, b Element) bool {
	if bus, err := s.a11y(); err == nil {
		return bus.same(a, b)
	}
	return false
}

// OpenApp starts an installed app on the agent's display, by the name in its
// desktop entry or its program's name.
func (s *linuxSession) OpenApp(ctx context.Context, name string) error {
	argv, err := linuxAppCommand(name)
	if err != nil {
		return err
	}
	_, err = s.surface.Launch(argv...)
	return err
}

func (s *linuxSession) RaiseApp(ctx context.Context, pid int) error {
	windows, _, err := s.surface.Windows()
	if err != nil {
		return err
	}
	for _, w := range windows {
		if w.PID == pid {
			return s.surface.Raise(w.ID)
		}
	}
	return fmt.Errorf("no window of pid %d is on the display", pid)
}

func (s *linuxSession) FrontPID(context.Context) (int, error) {
	windows, focus, err := s.surface.Windows()
	if err != nil {
		return 0, err
	}
	for _, w := range windows {
		if w.ID == focus {
			return w.PID, nil
		}
	}
	return 0, nil
}

// KeyTo sends a key to one app: its window takes the keyboard first. The
// display is the agent's own, so that disturbs nobody.
func (s *linuxSession) KeyTo(pid int, keysym uint32, down bool) error {
	if down {
		windows, focus, err := s.surface.Windows()
		if err != nil {
			return err
		}
		target := uint32(0)
		for _, w := range windows {
			if w.ID == focus && w.PID == pid {
				target = 0
				break
			}
			if w.PID == pid && target == 0 {
				target = w.ID
			}
		}
		if target != 0 {
			if err := s.surface.Raise(target); err != nil {
				return err
			}
		}
	}
	return s.surface.Key(keysym, down)
}
