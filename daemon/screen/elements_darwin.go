//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"math"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Accessibility functions load apart from capture and input: if this macOS
// lacks one, agents only lose reading elements, never Screen itself.
var axLib struct {
	once sync.Once
	ok   bool

	createSystemWide func() uintptr
	copyAttribute    func(element, attribute uintptr, value *uintptr) int32
	setTimeout       func(element uintptr, seconds float32) int32
	valueGetValue    func(value uintptr, kind uint32, out unsafe.Pointer) bool
	valueTypeID      func() uint64
	typeID           func(ref uintptr) uint64
	stringTypeID     func() uint64
	arrayTypeID      func() uint64
	booleanTypeID    func() uint64
	numberTypeID     func() uint64
	arrayCount       func(array uintptr) int64
	arrayAt          func(array uintptr, index int64) uintptr
	boolValue        func(ref uintptr) bool
	numberValue      func(number uintptr, kind int64, out unsafe.Pointer) bool
	stringLength     func(ref uintptr) int64
	createString     func(alloc uintptr, text string, encoding uint32) uintptr
	release          func(ref uintptr)

	system uintptr
	attrs  map[string]uintptr
}

const (
	axErrorAPIDisabled    = -25211
	axErrorNoValue        = -25212
	axErrorCannotComplete = -25204
	axValueCGPoint        = 1
	axValueCGSize         = 2
	cfNumberDouble        = 13
	axMaxDepth            = 30
	axBudget              = 3 * time.Second
	axLongString          = 4096
)

var (
	selUTF8String       = objc.RegisterName("UTF8String")
	selSubstringToIndex = objc.RegisterName("substringToIndex:")
)

func loadAX() bool {
	axLib.once.Do(func() {
		const frameworks = "/System/Library/Frameworks/"
		libs := map[string]uintptr{}
		for _, name := range []string{"ApplicationServices", "CoreFoundation"} {
			lib, err := purego.Dlopen(frameworks+name+".framework/"+name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				log.Printf("screen: elements unavailable: %v", err)
				return
			}
			libs[name] = lib
		}
		for _, fn := range []struct {
			ptr  any
			lib  string
			name string
		}{
			{&axLib.createSystemWide, "ApplicationServices", "AXUIElementCreateSystemWide"},
			{&axLib.copyAttribute, "ApplicationServices", "AXUIElementCopyAttributeValue"},
			{&axLib.setTimeout, "ApplicationServices", "AXUIElementSetMessagingTimeout"},
			{&axLib.valueGetValue, "ApplicationServices", "AXValueGetValue"},
			{&axLib.valueTypeID, "ApplicationServices", "AXValueGetTypeID"},
			{&axLib.typeID, "CoreFoundation", "CFGetTypeID"},
			{&axLib.stringTypeID, "CoreFoundation", "CFStringGetTypeID"},
			{&axLib.arrayTypeID, "CoreFoundation", "CFArrayGetTypeID"},
			{&axLib.booleanTypeID, "CoreFoundation", "CFBooleanGetTypeID"},
			{&axLib.numberTypeID, "CoreFoundation", "CFNumberGetTypeID"},
			{&axLib.arrayCount, "CoreFoundation", "CFArrayGetCount"},
			{&axLib.arrayAt, "CoreFoundation", "CFArrayGetValueAtIndex"},
			{&axLib.boolValue, "CoreFoundation", "CFBooleanGetValue"},
			{&axLib.numberValue, "CoreFoundation", "CFNumberGetValue"},
			{&axLib.stringLength, "CoreFoundation", "CFStringGetLength"},
			{&axLib.createString, "CoreFoundation", "CFStringCreateWithCString"},
			{&axLib.release, "CoreFoundation", "CFRelease"},
		} {
			if _, err := purego.Dlsym(libs[fn.lib], fn.name); err != nil {
				log.Printf("screen: elements unavailable: no %s", fn.name)
				return
			}
			purego.RegisterLibFunc(fn.ptr, libs[fn.lib], fn.name)
		}
		// Kept for the life of the process.
		axLib.attrs = map[string]uintptr{}
		for _, name := range []string{
			"AXFocusedApplication", "AXFocusedWindow", "AXChildren", "AXRole", "AXTitle",
			"AXDescription", "AXValue", "AXPosition", "AXSize", "AXEnabled", "AXFocused", "AXWindows", "AXFrontmost", "AXParent",
			"AXMenuBar", "AXMenuItemCmdChar", "AXMenuItemCmdModifiers",
		} {
			ref := axLib.createString(0, name, cfStringEncodingUTF8)
			if ref == 0 {
				return
			}
			axLib.attrs[name] = ref
		}
		axLib.system = axLib.createSystemWide()
		if axLib.system == 0 {
			return
		}
		// Every later request gives up on an app that doesn't answer.
		axLib.setTimeout(axLib.system, 1)
		axLib.ok = true
	})
	return axLib.ok
}

// Elements reads the focused app's focused window. A hung app costs at most
// the messaging timeout per request and the walk at most axBudget.
func (b *macBackend) Elements(ctx context.Context, limit int) (ElementTree, error) {
	if !loadAX() {
		return ElementTree{}, ErrNoElements
	}
	var tree ElementTree
	var err error
	withPool(func() { tree, err = readFocusedWindow(ctx, limit) })
	return tree, err
}

func readFocusedWindow(ctx context.Context, limit int) (ElementTree, error) {
	app, code := axCopy(axLib.system, "AXFocusedApplication")
	switch {
	case code == axErrorAPIDisabled:
		return ElementTree{}, ErrPermissions
	case code == axErrorNoValue || app == 0:
		return ElementTree{}, errors.New("no app is in front; take a screenshot")
	case code != 0:
		return ElementTree{}, fmt.Errorf("the app in front didn't answer (accessibility error %d); take a screenshot", code)
	}
	defer axLib.release(app)
	tree := ElementTree{App: axString(app, "AXTitle")}
	window, code := axCopy(app, "AXFocusedWindow")
	if code != 0 || window == 0 {
		return tree, fmt.Errorf("%s has no window in front; take a screenshot", tree.App)
	}
	defer axLib.release(window)
	tree.Window = axString(window, "AXTitle")
	walkElements(ctx, &tree, window, limit, time.Now().Add(axBudget), nil)
	return tree, nil
}

// walkElements appends root and what it contains to tree, in reading order.
// With keep, each element is also retained as its ref and its action names
// are read; keep reports whether the walk may go on.
func walkElements(ctx context.Context, tree *ElementTree, root uintptr, limit int, deadline time.Time, keep func(element uintptr, e *Element) bool) {
	var walk func(element uintptr, depth int)
	walk = func(element uintptr, depth int) {
		if len(tree.Elements) >= limit || time.Now().After(deadline) || ctx.Err() != nil {
			tree.Truncated = true
			return
		}
		e := Element{Depth: depth, Role: axString(element, "AXRole"), Title: axString(element, "AXTitle"),
			Frame: axFrame(element), Enabled: axBool(element, "AXEnabled", true), Focused: axBool(element, "AXFocused", false)}
		if e.Title == "" {
			e.Title = axString(element, "AXDescription")
		}
		// A password is never read, so it can't be shown or kept.
		if e.Role != "AXSecureTextField" {
			e.Value = axValueText(element)
		}
		if keep != nil && !keep(element, &e) {
			tree.Truncated = true
			return
		}
		tree.Elements = append(tree.Elements, e)
		if depth >= axMaxDepth {
			return
		}
		children, code := axCopy(element, "AXChildren")
		if code != 0 || children == 0 {
			return
		}
		defer axLib.release(children)
		if axLib.typeID(children) != axLib.arrayTypeID() {
			return
		}
		// Children belong to the array, which outlives this loop.
		for i, n := int64(0), axLib.arrayCount(children); i < n; i++ {
			walk(axLib.arrayAt(children, i), depth+1)
		}
	}
	walk(root, 0)
}

// axCopy reads one attribute; a non-zero result is the caller's to release.
func axCopy(element uintptr, attribute string) (uintptr, int32) {
	var value uintptr
	code := axLib.copyAttribute(element, axLib.attrs[attribute], &value)
	if code != 0 {
		return 0, code
	}
	return value, 0
}

func axString(element uintptr, attribute string) string {
	value, code := axCopy(element, attribute)
	if code != 0 || value == 0 {
		return ""
	}
	defer axLib.release(value)
	if axLib.typeID(value) != axLib.stringTypeID() {
		return ""
	}
	return cfString(value)
}

// cfString converts through NSString, which CFString is bridged to; callers
// run inside an autorelease pool. A long text is cut before conversion.
func cfString(ref uintptr) string {
	str := objc.ID(ref)
	if axLib.stringLength(ref) > axLongString {
		str = str.Send(selSubstringToIndex, uint64(axLongString))
	}
	return objc.Send[string](str, selUTF8String)
}

// axValueText is an element's value when it reads as text: a string, a
// number (a slider) or a checkbox state.
func axValueText(element uintptr) string {
	value, code := axCopy(element, "AXValue")
	if code != 0 || value == 0 {
		return ""
	}
	defer axLib.release(value)
	switch axLib.typeID(value) {
	case axLib.stringTypeID():
		return cfString(value)
	case axLib.numberTypeID():
		var number float64
		if axLib.numberValue(value, cfNumberDouble, unsafe.Pointer(&number)) && !math.IsNaN(number) {
			return strconv.FormatFloat(number, 'g', 6, 64)
		}
	case axLib.booleanTypeID():
		if axLib.boolValue(value) {
			return "on"
		}
		return "off"
	}
	return ""
}

func axBool(element uintptr, attribute string, missing bool) bool {
	value, code := axCopy(element, attribute)
	if code != 0 || value == 0 {
		return missing
	}
	defer axLib.release(value)
	if axLib.typeID(value) != axLib.booleanTypeID() {
		return missing
	}
	return axLib.boolValue(value)
}

// axFrame is the element's frame in global points, which are the main
// display's framebuffer pixels: capture runs at the display's point size.
func axFrame(element uintptr) image.Rectangle {
	var at cgPoint
	var size struct{ W, H float64 }
	if !axGeometry(element, "AXPosition", axValueCGPoint, unsafe.Pointer(&at)) ||
		!axGeometry(element, "AXSize", axValueCGSize, unsafe.Pointer(&size)) {
		return image.Rectangle{}
	}
	return image.Rect(int(math.Round(at.X)), int(math.Round(at.Y)), int(math.Round(at.X+size.W)), int(math.Round(at.Y+size.H)))
}

func axGeometry(element uintptr, attribute string, kind uint32, out unsafe.Pointer) bool {
	value, code := axCopy(element, attribute)
	if code != 0 || value == 0 {
		return false
	}
	defer axLib.release(value)
	return axLib.typeID(value) == axLib.valueTypeID() && axLib.valueGetValue(value, kind, out)
}
