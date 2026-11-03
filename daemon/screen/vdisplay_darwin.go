//go:build darwin && !screenfake

package screen

import (
	"errors"
	"fmt"
	"image"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// A virtual display is a display the window server believes in with nothing
// plugged in, made with CoreGraphics' CGVirtualDisplay (a private API that
// DeskPad and BetterDisplay use). It is the Mac counterpart of the hidden
// display Phone use gives an agent: apps moved onto it are out of the way of
// the person's own screen. This file only makes and describes one; whether
// input reaches a window on it without touching the person's cursor or focus
// is what the spike test measures.

type cgSize struct{ W, H float64 }

var vdLib struct {
	once sync.Once
	err  error

	queueCreate  func(label string, attr uintptr) uintptr
	activeList   func(max uint32, ids *uint32, count *uint32) int32
	displayBound func(id uint32) cgRect
	isMain       func(id uint32) bool
	valueCreate  func(kind uint32, p unsafe.Pointer) uintptr
	eventCreate  func(source uintptr) uintptr
	eventAt      func(event uintptr) cgPoint
}

func loadVDisplay() error {
	vdLib.once.Do(func() {
		if !loadApps() {
			vdLib.err = ErrNoApps
			return
		}
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			vdLib.err = err
			return
		}
		sys, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			vdLib.err = err
			return
		}
		as, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			vdLib.err = err
			return
		}
		for _, fn := range []struct {
			ptr  any
			lib  uintptr
			name string
		}{
			{&vdLib.queueCreate, sys, "dispatch_queue_create"},
			{&vdLib.activeList, cg, "CGGetActiveDisplayList"},
			{&vdLib.displayBound, cg, "CGDisplayBounds"},
			{&vdLib.isMain, cg, "CGDisplayIsMain"},
			{&vdLib.valueCreate, as, "AXValueCreate"},
			{&vdLib.eventCreate, cg, "CGEventCreate"},
			{&vdLib.eventAt, cg, "CGEventGetLocation"},
		} {
			if _, err := purego.Dlsym(fn.lib, fn.name); err != nil {
				vdLib.err = fmt.Errorf("no %s", fn.name)
				return
			}
			purego.RegisterLibFunc(fn.ptr, fn.lib, fn.name)
		}
		for _, class := range []string{"CGVirtualDisplayDescriptor", "CGVirtualDisplaySettings", "CGVirtualDisplayMode", "CGVirtualDisplay"} {
			if objc.GetClass(class) == 0 {
				vdLib.err = fmt.Errorf("this macOS has no %s", class)
				return
			}
		}
	})
	return vdLib.err
}

// virtualDisplay is one made display, alive until Close.
type virtualDisplay struct {
	display objc.ID
	// ID is the window server's number for it.
	ID uint32
}

func sel(name string) objc.SEL { return objc.RegisterName(name) }

// newVirtualDisplay makes a display of w x h points that refreshes at hz.
func newVirtualDisplay(name string, w, h int, hz float64, hiDPI bool) (*virtualDisplay, error) {
	if err := loadVDisplay(); err != nil {
		return nil, err
	}
	desc := objc.ID(objc.GetClass("CGVirtualDisplayDescriptor")).Send(sel("new"))
	if desc == 0 {
		return nil, errors.New("couldn't make a display descriptor")
	}
	defer desc.Send(sel("release"))
	queue := vdLib.queueCreate("mewla.virtual-display", 0)
	desc.Send(sel("setDispatchQueue:"), queue)
	desc.Send(sel("setName:"), nsString(name))
	desc.Send(sel("setMaxPixelsWide:"), uint32(w))
	desc.Send(sel("setMaxPixelsHigh:"), uint32(h))
	// About 110 pixels per inch.
	desc.Send(sel("setSizeInMillimeters:"), cgSize{float64(w) * 25.4 / 110, float64(h) * 25.4 / 110})
	desc.Send(sel("setProductID:"), uint32(0x4d45))
	desc.Send(sel("setVendorID:"), uint32(0x4d57))
	desc.Send(sel("setSerialNum:"), uint32(1))

	display := objc.ID(objc.GetClass("CGVirtualDisplay")).Send(sel("alloc")).Send(sel("initWithDescriptor:"), desc)
	if display == 0 {
		return nil, errors.New("the window server refused the virtual display")
	}
	mode := objc.ID(objc.GetClass("CGVirtualDisplayMode")).Send(sel("alloc")).Send(sel("initWithWidth:height:refreshRate:"), uint32(w), uint32(h), hz)
	modes := objc.ID(objc.GetClass("NSArray")).Send(sel("arrayWithObject:"), mode)
	settings := objc.ID(objc.GetClass("CGVirtualDisplaySettings")).Send(sel("new"))
	defer settings.Send(sel("release"))
	hi := uint32(0)
	if hiDPI {
		hi = 1
	}
	settings.Send(sel("setHiDPI:"), hi)
	settings.Send(sel("setModes:"), modes)
	if objc.Send[bool](display, sel("applySettings:"), settings) == false {
		display.Send(sel("release"))
		return nil, errors.New("the virtual display refused its mode")
	}
	return &virtualDisplay{display: display, ID: objc.Send[uint32](display, sel("displayID"))}, nil
}

// Close removes the display; windows on it move back to the others.
func (v *virtualDisplay) Close() {
	if v != nil && v.display != 0 {
		v.display.Send(sel("release"))
		v.display = 0
	}
}

// displayInfo is one active display.
type displayInfo struct {
	ID     uint32
	Main   bool
	Bounds image.Rectangle
}

// activeDisplays lists the displays the window server has, in global points.
func activeDisplays() ([]displayInfo, error) {
	if err := loadVDisplay(); err != nil {
		return nil, err
	}
	var ids [16]uint32
	var n uint32
	if code := vdLib.activeList(uint32(len(ids)), &ids[0], &n); code != 0 {
		return nil, fmt.Errorf("couldn't list displays (%d)", code)
	}
	var out []displayInfo
	for _, id := range ids[:n] {
		r := vdLib.displayBound(id)
		out = append(out, displayInfo{ID: id, Main: vdLib.isMain(id), Bounds: image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))})
	}
	return out, nil
}

// moveFrontWindow puts the app's first window with its top left at p, in
// global points, through accessibility, which needs no input event.
func moveFrontWindow(pid int, p image.Point) error {
	if err := loadVDisplay(); err != nil {
		return err
	}
	app := appsLib.createApplication(int32(pid))
	if app == 0 {
		return errors.New("no such app")
	}
	defer axLib.release(app)
	axLib.setTimeout(app, 2)
	windows, code := axCopy(app, "AXWindows")
	if code != 0 || windows == 0 {
		return fmt.Errorf("no windows (accessibility error %d)", code)
	}
	defer axLib.release(windows)
	if axLib.arrayCount(windows) == 0 {
		return errors.New("no windows")
	}
	at := cgPoint{float64(p.X), float64(p.Y)}
	value := vdLib.valueCreate(axValueCGPoint, unsafe.Pointer(&at))
	if value == 0 {
		return errors.New("couldn't make a position")
	}
	defer axLib.release(value)
	if code := appsLib.setAttribute(axLib.arrayAt(windows, 0), axLib.attrs["AXPosition"], value); code != 0 {
		return fmt.Errorf("the app refused to move (accessibility error %d)", code)
	}
	return nil
}

// pointerAt is where the pointer is now, in global points.
func pointerAt() cgPoint {
	if loadVDisplay() != nil {
		return cgPoint{}
	}
	event := vdLib.eventCreate(0)
	if event == 0 {
		return cgPoint{}
	}
	defer axLib.release(event)
	return vdLib.eventAt(event)
}

// frontWindowPosition is where the app's first window's top left is, in
// global points, to put it back after a spike.
func frontWindowPosition(pid int) (image.Point, bool) {
	if loadVDisplay() != nil {
		return image.Point{}, false
	}
	app := appsLib.createApplication(int32(pid))
	if app == 0 {
		return image.Point{}, false
	}
	defer axLib.release(app)
	axLib.setTimeout(app, 2)
	windows, code := axCopy(app, "AXWindows")
	if code != 0 || windows == 0 {
		return image.Point{}, false
	}
	defer axLib.release(windows)
	if axLib.arrayCount(windows) == 0 {
		return image.Point{}, false
	}
	value, code := axCopy(axLib.arrayAt(windows, 0), "AXPosition")
	if code != 0 || value == 0 {
		return image.Point{}, false
	}
	defer axLib.release(value)
	var at cgPoint
	if !axLib.valueGetValue(value, axValueCGPoint, unsafe.Pointer(&at)) {
		return image.Point{}, false
	}
	return image.Pt(int(at.X), int(at.Y)), true
}
