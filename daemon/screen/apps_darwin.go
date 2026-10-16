//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Reading any app loads apart from the rest: if this macOS lacks a function,
// agents lose only these actions.
var appsLib struct {
	once sync.Once
	ok   bool

	windowList        func(option, relativeTo uint32) uintptr
	dictValue         func(dict, key uintptr) uintptr
	retain            func(ref uintptr) uintptr
	createApplication func(pid int32) uintptr
	getPid            func(element uintptr, pid *int32) int32
	copyActionNames   func(element uintptr, names *uintptr) int32
	rectFromDict      func(dict uintptr, rect *cgRect) bool
	performAction     func(element, action uintptr) int32
	setAttribute      func(element, attribute, value uintptr) int32
	hash              func(ref uintptr) uintptr
	elementAt         func(app uintptr, x, y float32, element *uintptr) int32
	equal             func(a, b uintptr) bool

	keys map[string]uintptr
}

type cgRect struct{ X, Y, W, H float64 }

const (
	cgWindowListOnScreenOnly   = 1 << 0
	cgWindowListExcludeDesktop = 1 << 4
	cfNumberSInt64             = 4
)

func loadApps() bool {
	if !loadAX() {
		return false
	}
	appsLib.once.Do(func() {
		const frameworks = "/System/Library/Frameworks/"
		libs := map[string]uintptr{}
		for _, name := range []string{"ApplicationServices", "CoreFoundation", "CoreGraphics"} {
			lib, err := purego.Dlopen(frameworks+name+".framework/"+name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				log.Printf("screen: reading apps unavailable: %v", err)
				return
			}
			libs[name] = lib
		}
		for _, fn := range []struct {
			ptr  any
			lib  string
			name string
		}{
			{&appsLib.windowList, "CoreGraphics", "CGWindowListCopyWindowInfo"},
			{&appsLib.dictValue, "CoreFoundation", "CFDictionaryGetValue"},
			{&appsLib.retain, "CoreFoundation", "CFRetain"},
			{&appsLib.createApplication, "ApplicationServices", "AXUIElementCreateApplication"},
			{&appsLib.getPid, "ApplicationServices", "AXUIElementGetPid"},
			{&appsLib.copyActionNames, "ApplicationServices", "AXUIElementCopyActionNames"},
			{&appsLib.rectFromDict, "CoreGraphics", "CGRectMakeWithDictionaryRepresentation"},
			{&appsLib.performAction, "ApplicationServices", "AXUIElementPerformAction"},
			{&appsLib.setAttribute, "ApplicationServices", "AXUIElementSetAttributeValue"},
			{&appsLib.hash, "CoreFoundation", "CFHash"},
			{&appsLib.elementAt, "ApplicationServices", "AXUIElementCopyElementAtPosition"},
			{&appsLib.equal, "CoreFoundation", "CFEqual"},
		} {
			if _, err := purego.Dlsym(libs[fn.lib], fn.name); err != nil {
				log.Printf("screen: reading apps unavailable: no %s", fn.name)
				return
			}
			purego.RegisterLibFunc(fn.ptr, libs[fn.lib], fn.name)
		}
		// The window list's keys are strings equal to their names.
		appsLib.keys = map[string]uintptr{}
		for _, name := range []string{"kCGWindowOwnerPID", "kCGWindowOwnerName", "kCGWindowLayer", "kCGWindowIsOnscreen", "kCGWindowBounds", "kCGWindowNumber"} {
			ref := axLib.createString(0, name, cfStringEncodingUTF8)
			if ref == 0 {
				return
			}
			appsLib.keys[name] = ref
		}
		appsLib.ok = true
	})
	return appsLib.ok
}

// Apps lists the apps that have a normal window, on screen or not, from the
// window list, which needs no run loop in this process.
func (b *macBackend) Apps(context.Context) ([]App, error) {
	if !loadApps() {
		return nil, ErrNoApps
	}
	var apps []App
	var err error
	withPool(func() { apps, err = runningApps() })
	return apps, err
}

func runningApps() ([]App, error) {
	front := 0
	if app, code := axCopy(axLib.system, "AXFocusedApplication"); code == 0 && app != 0 {
		var pid int32
		if appsLib.getPid(app, &pid) == 0 {
			front = int(pid)
		}
		axLib.release(app)
	}
	// Only the list of windows on the screen runs from front to back; the
	// list of all windows, which also names apps with none showing, doesn't.
	onScreen, err := windowInfos(cgWindowListOnScreenOnly | cgWindowListExcludeDesktop)
	if err != nil {
		return nil, err
	}
	all, err := windowInfos(cgWindowListExcludeDesktop)
	if err != nil {
		return nil, err
	}
	return appsFromWindows(onScreen, all, front, os.Getpid(), appBundle), nil
}

func windowInfos(option uint32) ([]windowInfo, error) {
	list := appsLib.windowList(option, 0)
	if list == 0 {
		return nil, errors.New("couldn't list this computer's windows")
	}
	defer axLib.release(list)
	var out []windowInfo
	for i, n := int64(0), axLib.arrayCount(list); i < n; i++ {
		info := axLib.arrayAt(list, i)
		w := windowInfo{Name: dictString(info, "kCGWindowOwnerName"), OnScreen: dictBool(info, "kCGWindowIsOnscreen"), Bounds: dictRect(info)}
		layer, ok := dictInt(info, "kCGWindowLayer")
		if !ok {
			continue
		}
		pid, _ := dictInt(info, "kCGWindowOwnerPID")
		id, _ := dictInt(info, "kCGWindowNumber")
		w.Layer, w.PID, w.ID = int(layer), int(pid), uint32(id)
		out = append(out, w)
	}
	return out, nil
}

// dictRect is a window's bounds, in global points, which are the main
// display's framebuffer pixels.
func dictRect(dict uintptr) image.Rectangle {
	value := appsLib.dictValue(dict, appsLib.keys["kCGWindowBounds"])
	var r cgRect
	if value == 0 || !appsLib.rectFromDict(value, &r) {
		return image.Rectangle{}
	}
	return image.Rect(int(math.Round(r.X)), int(math.Round(r.Y)), int(math.Round(r.X+r.W)), int(math.Round(r.Y+r.H)))
}

func dictInt(dict uintptr, key string) (int64, bool) {
	value := appsLib.dictValue(dict, appsLib.keys[key])
	if value == 0 || axLib.typeID(value) != axLib.numberTypeID() {
		return 0, false
	}
	var n int64
	return n, axLib.numberValue(value, cfNumberSInt64, unsafe.Pointer(&n))
}

func dictString(dict uintptr, key string) string {
	value := appsLib.dictValue(dict, appsLib.keys[key])
	if value == 0 || axLib.typeID(value) != axLib.stringTypeID() {
		return ""
	}
	return cfString(value)
}

func dictBool(dict uintptr, key string) bool {
	value := appsLib.dictValue(dict, appsLib.keys[key])
	return value != 0 && axLib.typeID(value) == axLib.booleanTypeID() && axLib.boolValue(value)
}

// ReadApp reads every window of one app, in front or not. Each element is
// retained until Release, so an id names the same element later.
func (b *macBackend) ReadApp(ctx context.Context, pid int, limit int) (ElementTree, error) {
	if !loadApps() {
		return ElementTree{}, ErrNoApps
	}
	var tree ElementTree
	var err error
	withPool(func() { tree, err = readApp(ctx, pid, limit) })
	return tree, err
}

func readApp(ctx context.Context, pid int, limit int) (ElementTree, error) {
	app := appsLib.createApplication(int32(pid))
	if app == 0 {
		return ElementTree{}, fmt.Errorf("couldn't reach the app with pid %d", pid)
	}
	defer axLib.release(app)
	axLib.setTimeout(app, 1)
	tree := ElementTree{App: axString(app, "AXTitle"), PID: pid}
	windows, code := axCopy(app, "AXWindows")
	switch {
	case code == axErrorAPIDisabled:
		return ElementTree{}, ErrPermissions
	case code == axErrorNoValue || (code == 0 && windows == 0):
		return tree, nil
	case code != 0:
		return ElementTree{}, fmt.Errorf("the app didn't answer (accessibility error %d); take a screenshot", code)
	}
	defer axLib.release(windows)
	if axLib.typeID(windows) != axLib.arrayTypeID() {
		return tree, nil
	}
	deadline := time.Now().Add(axBudget)
	keep := func(element uintptr, e *Element) bool {
		e.ref = appsLib.retain(element)
		e.Actions = axActionNames(element)
		return true
	}
	for i, n := int64(0), axLib.arrayCount(windows); i < n; i++ {
		walkElements(ctx, &tree, axLib.arrayAt(windows, i), limit, deadline, keep)
	}
	return tree, nil
}

func axActionNames(element uintptr) []string {
	var names uintptr
	if appsLib.copyActionNames(element, &names) != 0 || names == 0 {
		return nil
	}
	defer axLib.release(names)
	if axLib.typeID(names) != axLib.arrayTypeID() {
		return nil
	}
	var out []string
	for i, n := int64(0), axLib.arrayCount(names); i < n; i++ {
		if name := axLib.arrayAt(names, i); axLib.typeID(name) == axLib.stringTypeID() {
			out = append(out, cfString(name))
		}
	}
	return out
}

// ElementHash and SameElement compare the live accessibility elements: two
// reads' handles to one element are equal, whatever its text now says.
func (b *macBackend) ElementHash(e Element) uintptr {
	if e.ref == 0 || !loadApps() {
		return 0
	}
	return appsLib.hash(e.ref)
}

func (b *macBackend) SameElement(x, y Element) bool {
	return x.ref != 0 && y.ref != 0 && loadApps() && appsLib.equal(x.ref, y.ref)
}

func (b *macBackend) Release(tree ElementTree) {
	for _, e := range tree.Elements {
		if e.ref != 0 {
			axLib.release(e.ref)
		}
	}
}

// AX errors an element action can meet.
const (
	axErrorInvalidElement       = -25202
	axErrorAttributeUnsupported = -25205
	axErrorActionUnsupported    = -25206
)

// ActOnElement acts on an element of an earlier read, in the background: an
// accessibility action or attribute never moves the pointer or raises the
// window unless the action itself does.
func (b *macBackend) ActOnElement(ctx context.Context, e Element, action, value string) error {
	if !loadApps() {
		return ErrNoApps
	}
	if e.ref == 0 {
		return ErrStaleElement
	}
	var err error
	withPool(func() { err = actOnElement(e, action, value) })
	return err
}

// stillRead checks that e's live element is still the one that was read:
// nothing is done to, or read from, an element that can't be shown to be.
func stillRead(e Element) error {
	role, code := axCopy(e.ref, "AXRole")
	switch {
	case code == axErrorInvalidElement || (code == 0 && role == 0):
		return ErrStaleElement
	case code != 0:
		return fmt.Errorf("the app didn't answer (accessibility error %d); read it again", code)
	}
	same := axLib.typeID(role) == axLib.stringTypeID() && cfString(role) == e.Role
	axLib.release(role)
	title := axString(e.ref, "AXTitle")
	if title == "" {
		title = axString(e.ref, "AXDescription")
	}
	if !same || title != e.Title {
		return ErrStaleElement
	}
	return nil
}

func actOnElement(e Element, action, value string) error {
	if err := stillRead(e); err != nil {
		return err
	}
	var code int32
	switch action {
	case "focus":
		yes := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithBool:"), true)
		code = appsLib.setAttribute(e.ref, axLib.attrs["AXFocused"], uintptr(yes))
	case "set_value":
		text := axLib.createString(0, value, cfStringEncodingUTF8)
		if text == 0 {
			return errors.New("that value can't be set")
		}
		code = appsLib.setAttribute(e.ref, axLib.attrs["AXValue"], text)
		axLib.release(text)
	default:
		name := axLib.createString(0, action, cfStringEncodingUTF8)
		if name == 0 {
			return errors.New("unknown action")
		}
		code = appsLib.performAction(e.ref, name)
		axLib.release(name)
	}
	switch code {
	case 0:
		return nil
	case axErrorInvalidElement:
		return ErrStaleElement
	case axErrorAPIDisabled:
		return ErrPermissions
	case axErrorActionUnsupported, axErrorAttributeUnsupported:
		return fmt.Errorf("this element doesn't allow %s; try another action, or click its point", strings.TrimPrefix(action, "AX"))
	case axErrorCannotComplete:
		return errors.New("the app didn't answer in time; it may be busy")
	}
	return fmt.Errorf("the app refused it (accessibility error %d)", code)
}

// ReadValue reads an element's whole value, up to limit characters, past
// the cut a read of the app makes.
func (b *macBackend) ReadValue(ctx context.Context, e Element, limit int) (string, int, error) {
	if !loadApps() {
		return "", 0, ErrNoApps
	}
	if e.ref == 0 {
		return "", 0, ErrStaleElement
	}
	var value string
	var length int
	var err error
	withPool(func() { value, length, err = readValue(e, limit) })
	return value, length, err
}

func readValue(e Element, limit int) (string, int, error) {
	if err := stillRead(e); err != nil {
		return "", 0, err
	}
	value, code := axCopy(e.ref, "AXValue")
	switch {
	case code == axErrorNoValue || code == axErrorAttributeUnsupported || (code == 0 && value == 0):
		return "", 0, nil
	case code != 0:
		return "", 0, fmt.Errorf("the app didn't answer (accessibility error %d); read it again", code)
	}
	defer axLib.release(value)
	if axLib.typeID(value) != axLib.stringTypeID() {
		text := axValueText(e.ref)
		return text, len([]rune(text)), nil
	}
	length := int(axLib.stringLength(value))
	str := objc.ID(value)
	if length > limit {
		str = str.Send(selSubstringToIndex, uint64(limit))
	}
	return objc.Send[string](str, selUTF8String), length, nil
}

// PressAt presses, through accessibility, what an app has at a point in
// global points, which are framebuffer pixels: the element there, or the
// nearest of its three closest ancestors that can be pressed. It asks the
// app itself; whether a window over the point still lets it answer is for a
// real Mac to show. Nothing to press there, or the point in another app's
// element, is no error and no press: the click is sent instead.
func (b *macBackend) PressAt(_ context.Context, pid, x, y int) (string, bool, error) {
	if !loadApps() {
		return "", false, ErrNoApps
	}
	var what string
	var pressed bool
	var err error
	withPool(func() { what, pressed, err = pressAt(pid, x, y) })
	return what, pressed, err
}

var pressContainers = []string{"AXApplication", "AXWindow", "AXSheet", "AXGroup", "AXScrollArea", "AXSplitGroup",
	"AXList", "AXOutline", "AXTable", "AXBrowser", "AXToolbar", "AXTabGroup", "AXLayoutArea", "AXWebArea"}

func pressAt(pid, x, y int) (string, bool, error) {
	app := appsLib.createApplication(int32(pid))
	if app == 0 {
		return "", false, nil
	}
	defer axLib.release(app)
	var element uintptr
	if appsLib.elementAt(app, float32(x), float32(y), &element) != 0 || element == 0 {
		return "", false, nil
	}
	defer func() { axLib.release(element) }()
	for depth := 0; depth < 4; depth++ {
		var owner int32
		if appsLib.getPid(element, &owner) != 0 || int(owner) != pid {
			return "", false, nil
		}
		// A container holds more than the point: pressing it is no click.
		if slices.Contains(pressContainers, axString(element, "AXRole")) {
			return "", false, nil
		}
		if slices.Contains(axActionNames(element), "AXPress") {
			what := strings.TrimPrefix(axString(element, "AXRole"), "AX")
			title := axString(element, "AXTitle")
			if title == "" {
				title = axString(element, "AXDescription")
			}
			if title != "" {
				what += fmt.Sprintf(" %q", oneLine(title))
			}
			err := actOnElement(Element{Role: axString(element, "AXRole"), Title: title, ref: element}, "AXPress", "")
			return what, err == nil, err
		}
		parent, code := axCopy(element, "AXParent")
		if code != 0 || parent == 0 {
			return "", false, nil
		}
		axLib.release(element)
		element = parent
	}
	return "", false, nil
}

// RaiseApp brings an app to the front, for a click that has to go on the
// screen.
// OpenApp opens an app by name or bundle id with open -g, which keeps it
// behind the app in front; the name was checked to be no path or option.
func (b *macBackend) OpenApp(ctx context.Context, name string) error {
	by := "-a"
	if bundleID.MatchString(name) {
		by = "-b"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/open", "-g", by, name).CombinedOutput()
	if err != nil {
		why := strings.TrimSpace(string(out))
		if why == "" {
			why = err.Error()
		}
		return fmt.Errorf("macOS couldn't open %s: %s", oneLine(name), oneLine(why))
	}
	return nil
}

func (b *macBackend) RaiseApp(_ context.Context, pid int) error {
	if !loadApps() {
		return ErrNoApps
	}
	var code int32
	withPool(func() {
		app := appsLib.createApplication(int32(pid))
		if app == 0 {
			code = axErrorInvalidElement
			return
		}
		defer axLib.release(app)
		yes := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithBool:"), true)
		code = appsLib.setAttribute(app, axLib.attrs["AXFrontmost"], uintptr(yes))
	})
	switch code {
	case 0:
		return nil
	case axErrorAPIDisabled:
		return ErrPermissions
	}
	return fmt.Errorf("couldn't bring the app forward (accessibility error %d)", code)
}

// Chromium-based apps ship one of these frameworks.
var chromiumFrameworks = []string{"Electron Framework", "Chromium Embedded Framework", "Microsoft Edge Framework",
	"Brave Browser Framework", "Vivaldi Framework", "Opera Framework", "ArcCore"}

var chromium struct {
	once    sync.Once
	pidPath func(pid int32, buf *byte, size uint32) int32
	mu      sync.Mutex
	known   map[int]bundleInfo
}

// appBundle is the app with pid's bundle file name, such as Notes for a
// Notes.app shown as 备忘录, its bundle id, and whether it is built on
// Chromium, from the frameworks in its bundle; remembered per process. It
// runs inside an autorelease pool.
func appBundle(pid int) bundleInfo {
	chromium.once.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			if _, err := purego.Dlsym(lib, "proc_pidpath"); err == nil {
				purego.RegisterLibFunc(&chromium.pidPath, lib, "proc_pidpath")
			}
		}
		chromium.known = map[int]bundleInfo{}
	})
	chromium.mu.Lock()
	defer chromium.mu.Unlock()
	if known, ok := chromium.known[pid]; ok {
		return known
	}
	// Processes come and go on a Mac that runs for weeks.
	if len(chromium.known) > 512 {
		clear(chromium.known)
	}
	var info bundleInfo
	if chromium.pidPath != nil {
		buf := make([]byte, 4096)
		if n := chromium.pidPath(int32(pid), &buf[0], uint32(len(buf))); n > 0 {
			path := string(buf[:n])
			if i := strings.Index(path, ".app/"); i >= 0 {
				info.name = strings.TrimSuffix(filepath.Base(path[:i+4]), ".app")
				info.id = bundleIdentifier(path[:i+4])
				entries, _ := os.ReadDir(path[:i+4] + "/Contents/Frameworks")
				for _, entry := range entries {
					name := strings.TrimSuffix(entry.Name(), ".framework")
					info.chromium = info.chromium || strings.Contains(name, "Chrom") || slices.Contains(chromiumFrameworks, name)
				}
			}
		}
	}
	chromium.known[pid] = info
	return info
}

// bundleIdentifier is the bundle id in the app bundle at path, from its
// Info.plist through Foundation, or "".
func bundleIdentifier(path string) string {
	bundle := objc.ID(objc.GetClass("NSBundle")).Send(objc.RegisterName("bundleWithPath:"), nsString(path))
	if bundle == 0 {
		return ""
	}
	id := bundle.Send(objc.RegisterName("bundleIdentifier"))
	if id == 0 {
		return ""
	}
	return objc.Send[string](id, selUTF8String)
}

// menuWalk bounds the search of an app's menus for a shortcut.
const (
	menuItems = 3000
	menuDepth = 6
	menuTime  = 3 * time.Second
)

// PressMenuKey presses the menu item with the shortcut through
// accessibility, which an app behind others acts on without being brought
// forward.
func (b *macBackend) PressMenuKey(ctx context.Context, pid int, shortcut Shortcut) (string, bool, error) {
	if !loadApps() {
		return "", false, ErrNoApps
	}
	var path string
	var found bool
	var err error
	withPool(func() { path, found, err = pressMenuKey(ctx, pid, shortcut) })
	return path, found, err
}

func pressMenuKey(ctx context.Context, pid int, shortcut Shortcut) (string, bool, error) {
	app := appsLib.createApplication(int32(pid))
	if app == 0 {
		return "", false, fmt.Errorf("couldn't reach the app with pid %d", pid)
	}
	defer axLib.release(app)
	axLib.setTimeout(app, 1)
	bar, code := axCopy(app, "AXMenuBar")
	switch {
	case code == axErrorAPIDisabled:
		return "", false, ErrPermissions
	case code != 0 || bar == 0:
		return "", false, nil // no menus to look in: the keys are sent instead
	}
	defer axLib.release(bar)
	deadline := time.Now().Add(menuTime)
	seen := 0
	var item uintptr
	var path []string
	var walk func(element uintptr, titles []string, depth int) bool
	walk = func(element uintptr, titles []string, depth int) bool {
		seen++
		if seen > menuItems || depth > menuDepth || time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		role := axString(element, "AXRole")
		if role == "AXMenuItem" || role == "AXMenuBarItem" {
			titles = append(titles[:len(titles):len(titles)], axString(element, "AXTitle"))
		}
		if role == "AXMenuItem" {
			modifiers := int64(0)
			if value, code := axCopy(element, "AXMenuItemCmdModifiers"); code == 0 && value != 0 {
				if axLib.typeID(value) == axLib.numberTypeID() {
					axLib.numberValue(value, cfNumberSInt64, unsafe.Pointer(&modifiers))
				}
				axLib.release(value)
			}
			if shortcut.matches(axString(element, "AXMenuItemCmdChar"), modifiers) {
				item, path = appsLib.retain(element), titles
				return true
			}
		}
		children, code := axCopy(element, "AXChildren")
		if code != 0 || children == 0 {
			return false
		}
		defer axLib.release(children)
		if axLib.typeID(children) != axLib.arrayTypeID() {
			return false
		}
		for i, n := int64(0), axLib.arrayCount(children); i < n; i++ {
			if walk(axLib.arrayAt(children, i), titles, depth+1) {
				return true
			}
		}
		return false
	}
	if !walk(bar, nil, 0) {
		return "", false, nil
	}
	defer axLib.release(item)
	where := strings.Join(path, " > ")
	if !axBool(item, "AXEnabled", true) {
		return where, true, fmt.Errorf("%s (%s) is turned off in the app's menus right now, so it wasn't chosen", where, shortcut)
	}
	press := axLib.createString(0, "AXPress", cfStringEncodingUTF8)
	if press == 0 {
		return where, true, errors.New("couldn't choose the menu item")
	}
	defer axLib.release(press)
	switch code := appsLib.performAction(item, press); code {
	case 0:
		return where, true, nil
	case axErrorAPIDisabled:
		return where, true, ErrPermissions
	case axErrorCannotComplete:
		// Choosing an item can run a while (a dialog opening): the app may
		// have done it without answering in time.
		return where, true, fmt.Errorf("chose %s, but the app didn't answer in time; read it to see whether it took effect", where)
	default:
		return where, true, fmt.Errorf("the app refused %s (accessibility error %d)", where, code)
	}
}

var (
	_ menuPresser = (*macBackend)(nil)
	_ valueReader = (*macBackend)(nil)
	_ appReader   = (*macBackend)(nil)
	_ appActor    = (*macBackend)(nil)
	_ appRaiser   = (*macBackend)(nil)
)
