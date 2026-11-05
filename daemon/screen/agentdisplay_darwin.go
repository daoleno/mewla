//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"sync"
	"time"
	"unsafe"
)

// agentDisplay is the virtual display agents' apps wait on. It is made when
// the first app or browser window needs it and let go when none does. The
// windows' places before they moved are kept, to put them back.
type agentDisplay struct {
	mu     sync.Mutex
	vd     *virtualDisplay
	bounds image.Rectangle
	saved  map[int][]image.Point
	holds  map[string]bool
	// capture is a stream of the display, started when a picture of a window
	// on it is first asked for.
	capture Session
}

const (
	agentDisplayW, agentDisplayH = 1600, 1000
	agentDisplayName             = "Mewla Agent"
)

func (b *macBackend) AgentDisplayAvailable() bool { return loadVDisplay() == nil && loadAX() }

// ensureLocked makes the display, or finds it again: a display the Mac's
// sleep took is made anew.
func (d *agentDisplay) ensureLocked() error {
	if d.vd != nil {
		if bounds, ok := listedDisplay(d.vd.ID); ok {
			d.bounds = bounds
			return nil
		}
		d.closeLocked()
	}
	vd, err := newVirtualDisplay(agentDisplayName, agentDisplayW, agentDisplayH, 60, false)
	if err != nil {
		return err
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if bounds, ok := listedDisplay(vd.ID); ok {
			d.vd, d.bounds = vd, bounds
			return nil
		}
	}
	vd.Close()
	return errors.New("the new display never appeared")
}

func listedDisplay(id uint32) (image.Rectangle, bool) {
	displays, err := activeDisplays()
	if err != nil {
		return image.Rectangle{}, false
	}
	for _, d := range displays {
		if d.ID == id {
			return d.Bounds, true
		}
	}
	return image.Rectangle{}, false
}

func (d *agentDisplay) closeLocked() {
	if d.capture != nil {
		_ = d.capture.Close()
		d.capture = nil
	}
	d.vd.Close()
	d.vd = nil
}

// releaseIfIdleLocked lets the display go when nothing is on it.
func (d *agentDisplay) releaseIfIdleLocked() {
	if len(d.saved) == 0 && len(d.holds) == 0 && d.vd != nil {
		d.closeLocked()
	}
}

// windowPositions are the app's windows' top lefts, in AXWindows order, in
// global points; ok is false for a window that has none.
func windowPositions(pid int) (positions []image.Point, ok []bool, err error) {
	if err := loadVDisplay(); err != nil {
		return nil, nil, err
	}
	withPool(func() {
		app := appsLib.createApplication(int32(pid))
		if app == 0 {
			err = errors.New("no such app")
			return
		}
		defer axLib.release(app)
		axLib.setTimeout(app, 2)
		windows, code := axCopy(app, "AXWindows")
		if code != 0 || windows == 0 {
			err = fmt.Errorf("no windows (accessibility error %d)", code)
			return
		}
		defer axLib.release(windows)
		for i, n := int64(0), axLib.arrayCount(windows); i < n; i++ {
			value, code := axCopy(axLib.arrayAt(windows, i), "AXPosition")
			var at cgPoint
			good := code == 0 && value != 0 && axLib.valueGetValue(value, axValueCGPoint, unsafe.Pointer(&at))
			if value != 0 {
				axLib.release(value)
			}
			positions = append(positions, image.Pt(int(at.X), int(at.Y)))
			ok = append(ok, good)
		}
	})
	return positions, ok, err
}

// setWindowPosition moves one window (AXWindows order) through accessibility.
func setWindowPosition(pid, index int, p image.Point) error {
	var err error
	withPool(func() {
		app := appsLib.createApplication(int32(pid))
		if app == 0 {
			err = errors.New("no such app")
			return
		}
		defer axLib.release(app)
		axLib.setTimeout(app, 2)
		windows, code := axCopy(app, "AXWindows")
		if code != 0 || windows == 0 {
			err = fmt.Errorf("no windows (accessibility error %d)", code)
			return
		}
		defer axLib.release(windows)
		if int64(index) >= axLib.arrayCount(windows) {
			err = errors.New("no such window")
			return
		}
		at := cgPoint{float64(p.X), float64(p.Y)}
		value := vdLib.valueCreate(axValueCGPoint, unsafe.Pointer(&at))
		if value == 0 {
			err = errors.New("couldn't make a position")
			return
		}
		defer axLib.release(value)
		if code := appsLib.setAttribute(axLib.arrayAt(windows, int64(index)), axLib.attrs["AXPosition"], value); code != 0 {
			err = fmt.Errorf("the app refused to move (accessibility error %d)", code)
		}
	})
	return err
}

func (b *macBackend) ParkApp(ctx context.Context, pid int) error {
	d := &b.agentDisp
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureLocked(); err != nil {
		return err
	}
	positions, ok, err := windowPositions(pid)
	if err != nil {
		return err
	}
	if len(positions) == 0 {
		return errors.New("the app has no window to move")
	}
	if _, kept := d.saved[pid]; !kept {
		var home []image.Point
		for i, p := range positions {
			if ok[i] && p.In(d.bounds) {
				// Already on the display, by the person's own doing: its
				// home is the main display.
				p = image.Pt(60, 60)
			}
			home = append(home, p)
		}
		if d.saved == nil {
			d.saved = map[int][]image.Point{}
		}
		d.saved[pid] = home
	}
	moved := 0
	for i, p := range positions {
		if !ok[i] {
			continue
		}
		if p.In(d.bounds) {
			moved++
			continue
		}
		target := d.bounds.Min.Add(image.Pt(40+30*i, 60+30*i))
		if err := setWindowPosition(pid, i, target); err != nil {
			log.Printf("screen: agent display: %v", err)
			continue
		}
		moved++
	}
	if moved == 0 {
		delete(d.saved, pid)
		d.releaseIfIdleLocked()
		return errors.New("the app wouldn't let its windows be moved")
	}
	// Check where they are now: an app can accept a move and not make it.
	time.Sleep(150 * time.Millisecond)
	if after, ok, err := windowPositions(pid); err == nil {
		on := false
		for i, p := range after {
			if ok[i] && p.In(d.bounds) {
				on = true
			}
		}
		if !on {
			delete(d.saved, pid)
			d.releaseIfIdleLocked()
			return errors.New("the app took the move but its window did not go")
		}
	}
	return nil
}

func (b *macBackend) RestoreApp(pid int) error {
	d := &b.agentDisp
	d.mu.Lock()
	defer d.mu.Unlock()
	home, kept := d.saved[pid]
	if !kept {
		return nil
	}
	delete(d.saved, pid)
	defer d.releaseIfIdleLocked()
	positions, ok, err := windowPositions(pid)
	if err != nil {
		return nil // the app quit: nothing to put back
	}
	displays, _ := activeDisplays()
	onSomeDisplay := func(p image.Point) bool {
		for _, display := range displays {
			if p.In(display.Bounds) && (d.vd == nil || display.ID != d.vd.ID) {
				return true
			}
		}
		return false
	}
	var failed error
	for i, p := range positions {
		if !ok[i] || !p.In(d.bounds) {
			continue
		}
		target := home[min(i, len(home)-1)]
		if !onSomeDisplay(target) {
			target = image.Pt(60, 60) // the display it came from is gone
		}
		if err := setWindowPosition(pid, i, target); err != nil {
			failed = err
		}
	}
	return failed
}

func (b *macBackend) ParkedOnDisplay(pid int) bool {
	d := &b.agentDisp
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.vd == nil {
		return false
	}
	bounds, listed := listedDisplay(d.vd.ID)
	if !listed {
		return false
	}
	positions, ok, err := windowPositions(pid)
	if err != nil {
		return false
	}
	for i, p := range positions {
		if ok[i] && p.In(bounds) {
			return true
		}
	}
	return false
}

// ParkedImage is the display's picture of rect (global points).
func (b *macBackend) ParkedImage(ctx context.Context, rect image.Rectangle) (*image.RGBA, error) {
	d := &b.agentDisp
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureLocked(); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		if d.capture == nil {
			session, err := b.startOn(ctx, d.vd.ID)
			if err != nil {
				return nil, err
			}
			d.capture = session
		}
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		frame, err := d.capture.Frame(cctx, 0)
		cancel()
		if err != nil {
			// A stream the display's sleep ended is started anew.
			_ = d.capture.Close()
			d.capture = nil
			continue
		}
		r := rect.Sub(d.bounds.Min).Intersect(frame.Image.Rect)
		if r.Empty() {
			return nil, errors.New("the window isn't on the agent display")
		}
		out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
		for y := 0; y < r.Dy(); y++ {
			copy(out.Pix[y*out.Stride:y*out.Stride+r.Dx()*4], frame.Image.Pix[frame.Image.PixOffset(r.Min.X, r.Min.Y+y):])
		}
		return out, nil
	}
	return nil, errors.New("the agent display's picture isn't available")
}

// HoldAgentDisplay makes the display for something other than an app moved
// onto it (a browser window) and returns where it is, in global points.
func (b *macBackend) HoldAgentDisplay(key string) (image.Rectangle, error) {
	d := &b.agentDisp
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureLocked(); err != nil {
		return image.Rectangle{}, err
	}
	if d.holds == nil {
		d.holds = map[string]bool{}
	}
	d.holds[key] = true
	return d.bounds, nil
}

// ReleaseAgentDisplay lets go of a hold; the display goes when nothing is on it.
func (b *macBackend) ReleaseAgentDisplay(key string) {
	d := &b.agentDisp
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.holds, key)
	d.releaseIfIdleLocked()
}

var _ appParker = (*macBackend)(nil)
