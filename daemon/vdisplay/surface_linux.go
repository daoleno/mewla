//go:build linux

package vdisplay

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/res"
	"github.com/jezek/xgb/xproto"
)

// ErrSurfaceGone means the agent screen under a surface ended (the display
// stopped, or the Worker's monitor was taken away): start a new surface.
var ErrSurfaceGone = errors.New("the agent's display ended")

// Surface is one Worker's monitor driven as a screen of its own: its picture,
// the pointer and keys on it, the windows on it and the programs it opens.
// Coordinates are the monitor's, from its top-left corner. It is what the
// computer-use tool drives on Linux; the person's Hold and Stop reach it
// through Begin and End, as they reach the browser.
type Surface struct {
	s *Service
	w *workerDisplay

	mu   sync.Mutex
	seq  uint64
	sum  [32]byte
	have bool
}

// Surface gives the Worker's monitor, made if it has none.
func (s *Service) Surface(ctx context.Context, worker string) (*Surface, error) {
	w, err := s.get(ctx, worker, true)
	if err != nil {
		return nil, err
	}
	return &Surface{s: s, w: w}, nil
}

// Rect is the monitor's size, at the origin.
func (f *Surface) Rect() image.Rectangle { return image.Rect(0, 0, f.w.rect.Dx(), f.w.rect.Dy()) }

// alive says whether the monitor is still the Worker's, on a running screen.
func (f *Surface) alive() error {
	if err := f.w.screen.client.Err(); err != nil || !f.w.screen.display.Alive() {
		return ErrSurfaceGone
	}
	f.s.mu.Lock()
	current := f.s.displays[f.w.worker]
	f.s.mu.Unlock()
	if current != f.w {
		return ErrSurfaceGone
	}
	return nil
}

// Frame blocks until the monitor's picture differs from the frame numbered
// after (0 for the first), then returns it with its number.
func (f *Surface) Frame(ctx context.Context, after uint64) (*image.RGBA, uint64, error) {
	c := f.w.screen.client
	changed, stop := c.Watch()
	defer stop()
	for {
		if err := f.alive(); err != nil {
			return nil, 0, err
		}
		pix := c.Crop(f.w.rect)
		sum := sha256.Sum256(pix)
		f.mu.Lock()
		if !f.have || sum != f.sum {
			f.seq++
			f.sum, f.have = sum, true
		}
		seq := f.seq
		f.mu.Unlock()
		if seq > after {
			img := image.NewRGBA(image.Rect(0, 0, f.w.rect.Dx(), f.w.rect.Dy()))
			for i := 0; i+3 < len(pix); i += 4 {
				img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pix[i+2], pix[i+1], pix[i], 255
			}
			return img, seq, nil
		}
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-changed:
		case <-time.After(2 * time.Second):
			// A missed notice, or the display ended quietly: look again.
		}
	}
}

// Pointer moves the pointer to (x, y) on the monitor with the RFB button mask.
func (f *Surface) Pointer(x, y int, buttons uint8) error {
	if err := f.alive(); err != nil {
		return err
	}
	if x < 0 || y < 0 || x >= f.w.rect.Dx() || y >= f.w.rect.Dy() {
		return fmt.Errorf("point %d,%d is outside the %dx%d display", x, y, f.w.rect.Dx(), f.w.rect.Dy())
	}
	f.s.inputMu.Lock()
	defer f.s.inputMu.Unlock()
	f.w.mu.Lock()
	f.w.pointer = image.Pt(x, y)
	f.w.mu.Unlock()
	return f.w.screen.client.Pointer(x+f.w.rect.Min.X, y+f.w.rect.Min.Y, buttons)
}

// Key presses or releases a keysym. With no window manager, keys go to the
// window that has the focus, or else to the one under the pointer, so the
// pointer first goes back to where this agent left it on its own monitor.
func (f *Surface) Key(sym uint32, down bool) error {
	if err := f.alive(); err != nil {
		return err
	}
	f.s.inputMu.Lock()
	defer f.s.inputMu.Unlock()
	f.w.mu.Lock()
	p := f.w.pointer
	f.w.mu.Unlock()
	c := f.w.screen.client
	if down {
		if err := c.Pointer(p.X+f.w.rect.Min.X, p.Y+f.w.rect.Min.Y, 0); err != nil {
			return err
		}
	}
	return c.Key(sym, down)
}

// Launch starts a program on the monitor, as open does: its windows are moved
// there, and it ends with the monitor.
func (f *Surface) Launch(argv ...string) (int, error) {
	if err := f.alive(); err != nil {
		return 0, err
	}
	home := filepath.Join(f.w.screen.display.Dir, "home-"+safeName(f.w.worker))
	if err := os.MkdirAll(home, 0o700); err != nil {
		return 0, err
	}
	app, err := f.w.screen.display.Launch(home, argv...)
	if err != nil {
		return 0, err
	}
	f.w.mu.Lock()
	f.w.apps = append(f.w.apps, app)
	f.w.mu.Unlock()
	return app.Cmd.Process.Pid, nil
}

// A11yAddress is the agent screen's accessibility bus, or "" without one.
func (f *Surface) A11yAddress() string { return f.w.screen.display.A11yAddress() }

// Origin is where the monitor lies on the agent screen, for coordinates that
// accessibility gives on the whole screen.
func (f *Surface) Origin() image.Point { return f.w.rect.Min }

// Window is one top-level window on the monitor, in monitor pixels.
type Window struct {
	ID     uint32
	PID    int
	Title  string
	Bounds image.Rectangle
}

// Windows lists the top-level windows that lie on the monitor, front to back,
// and the window that has the keyboard focus (0 when none of them does).
func (f *Surface) Windows() ([]Window, uint32, error) {
	if err := f.alive(); err != nil {
		return nil, 0, err
	}
	conn, err := f.s.query(f.w.screen)
	if err != nil {
		return nil, 0, err
	}
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	tree, err := xproto.QueryTree(conn, root).Reply()
	if err != nil {
		f.s.dropQuery(f.w.screen)
		return nil, 0, err
	}
	atoms := f.s.atoms(conn)
	// QueryTree lists the children bottom to top.
	attrs := make([]xproto.GetWindowAttributesCookie, len(tree.Children))
	geoms := make([]xproto.GetGeometryCookie, len(tree.Children))
	for i, win := range tree.Children {
		attrs[i] = xproto.GetWindowAttributes(conn, win)
		geoms[i] = xproto.GetGeometry(conn, xproto.Drawable(win))
	}
	var out []Window
	for i := len(tree.Children) - 1; i >= 0; i-- {
		win := tree.Children[i]
		a, err := attrs[i].Reply()
		g, gerr := geoms[i].Reply()
		if err != nil || gerr != nil || a.MapState != xproto.MapStateViewable || a.OverrideRedirect || g.Width <= 1 || g.Height <= 1 {
			continue
		}
		bounds := image.Rect(int(g.X), int(g.Y), int(g.X)+int(g.Width), int(g.Y)+int(g.Height))
		if !bounds.Overlaps(f.w.rect) {
			continue
		}
		out = append(out, Window{
			ID:     uint32(win),
			PID:    processOf(conn, win, atoms.pid),
			Title:  windowTitle(conn, win, atoms),
			Bounds: bounds.Sub(f.w.rect.Min),
		})
	}
	focus := uint32(0)
	if reply, err := xproto.GetInputFocus(conn).Reply(); err == nil {
		focus = topLevel(conn, root, reply.Focus, out)
	}
	return out, focus, nil
}

// Raise brings a window of the monitor to the top and gives it the keyboard.
func (f *Surface) Raise(id uint32) error {
	if err := f.alive(); err != nil {
		return err
	}
	conn, err := f.s.query(f.w.screen)
	if err != nil {
		return err
	}
	win := xproto.Window(id)
	if err := xproto.ConfigureWindowChecked(conn, win, xproto.ConfigWindowStackMode, []uint32{xproto.StackModeAbove}).Check(); err != nil {
		return err
	}
	return xproto.SetInputFocusChecked(conn, xproto.InputFocusPointerRoot, win, xproto.TimeCurrentTime).Check()
}

// topLevel is the window of list that holds focus, or 0.
func topLevel(conn *xgb.Conn, root, focus xproto.Window, list []Window) uint32 {
	for i := 0; i < 32 && focus != 0 && focus != root && focus != xproto.Window(1); i++ { // 1 is PointerRoot
		for _, w := range list {
			if xproto.Window(w.ID) == focus {
				return w.ID
			}
		}
		tree, err := xproto.QueryTree(conn, focus).Reply()
		if err != nil {
			return 0
		}
		focus = tree.Parent
	}
	return 0
}

type xAtoms struct{ pid, netName, utf8 xproto.Atom }

// query is the screen's connection for questions about its windows, opened on
// first use.
func (s *Service) query(scr *sharedScreen) (*xgb.Conn, error) {
	scr.queryMu.Lock()
	defer scr.queryMu.Unlock()
	if scr.query != nil {
		return scr.query, nil
	}
	nc, err := net.Dial("unix", fmt.Sprintf("/tmp/.X11-unix/X%d", scr.display.Number))
	if err != nil {
		return nil, err
	}
	conn, err := xgb.NewConnNetWithCookieHex(nc, scr.display.cookie)
	if err != nil {
		_ = nc.Close()
		return nil, err
	}
	_ = res.Init(conn)
	scr.query = conn
	return conn, nil
}

// dropQuery closes a connection that failed, so the next question opens a new one.
func (s *Service) dropQuery(scr *sharedScreen) {
	scr.queryMu.Lock()
	defer scr.queryMu.Unlock()
	if scr.query != nil {
		scr.query.Close()
		scr.query = nil
	}
}

func (s *Service) atoms(conn *xgb.Conn) xAtoms {
	intern := func(name string) xproto.Atom {
		reply, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			return 0
		}
		return reply.Atom
	}
	return xAtoms{pid: intern("_NET_WM_PID"), netName: intern("_NET_WM_NAME"), utf8: intern("UTF8_STRING")}
}

// windowTitle is a window's title: its UTF-8 name, or else its plain one.
func windowTitle(conn *xgb.Conn, win xproto.Window, atoms xAtoms) string {
	if atoms.netName != 0 {
		if p, err := xproto.GetProperty(conn, false, win, atoms.netName, atoms.utf8, 0, 256).Reply(); err == nil && len(p.Value) > 0 && utf8.Valid(p.Value) {
			return string(p.Value)
		}
	}
	if p, err := xproto.GetProperty(conn, false, win, xproto.AtomWmName, xproto.AtomString, 0, 256).Reply(); err == nil && len(p.Value) > 0 {
		return string(p.Value)
	}
	return ""
}
