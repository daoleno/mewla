//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
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
	performAction     func(element, action uintptr) int32
	setAttribute      func(element, attribute, value uintptr) int32

	keys map[string]uintptr
}

const (
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
			{&appsLib.performAction, "ApplicationServices", "AXUIElementPerformAction"},
			{&appsLib.setAttribute, "ApplicationServices", "AXUIElementSetAttributeValue"},
		} {
			if _, err := purego.Dlsym(libs[fn.lib], fn.name); err != nil {
				log.Printf("screen: reading apps unavailable: no %s", fn.name)
				return
			}
			purego.RegisterLibFunc(fn.ptr, libs[fn.lib], fn.name)
		}
		// The window list's keys are strings equal to their names.
		appsLib.keys = map[string]uintptr{}
		for _, name := range []string{"kCGWindowOwnerPID", "kCGWindowOwnerName", "kCGWindowLayer", "kCGWindowIsOnscreen"} {
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
	list := appsLib.windowList(cgWindowListExcludeDesktop, 0)
	if list == 0 {
		return nil, errors.New("couldn't list this computer's windows")
	}
	defer axLib.release(list)
	front := 0
	if app, code := axCopy(axLib.system, "AXFocusedApplication"); code == 0 && app != 0 {
		var pid int32
		if appsLib.getPid(app, &pid) == 0 {
			front = int(pid)
		}
		axLib.release(app)
	}
	byPID := map[int]int{}
	var apps []App
	for i, n := int64(0), axLib.arrayCount(list); i < n; i++ {
		info := axLib.arrayAt(list, i)
		// Layer 0 holds apps' own windows; menus, the Dock and the menu bar
		// sit above it.
		if layer, ok := dictInt(info, "kCGWindowLayer"); !ok || layer != 0 {
			continue
		}
		pid, ok := dictInt(info, "kCGWindowOwnerPID")
		if !ok || pid <= 0 || pid == int64(os.Getpid()) {
			continue
		}
		index, seen := byPID[int(pid)]
		if !seen {
			name := dictString(info, "kCGWindowOwnerName")
			if name == "" {
				continue
			}
			index = len(apps)
			byPID[int(pid)] = index
			apps = append(apps, App{Name: name, PID: int(pid), Active: int(pid) == front, Hidden: true})
		}
		if dictBool(info, "kCGWindowIsOnscreen") {
			apps[index].Hidden = false
		}
	}
	return apps, nil
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

func actOnElement(e Element, action, value string) error {
	// Never act on an element that can't be shown to be the one read.
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

var (
	_ appReader = (*macBackend)(nil)
	_ appActor  = (*macBackend)(nil)
)
