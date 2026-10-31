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

	// Batched reads load apart from the rest (see loadBatchAX): without
	// them an element is read one attribute at a time.
	batch          bool
	copyMultiple   func(element, attributes uintptr, options uint32, values *uintptr) int32
	valueType      func(value uintptr) uint32
	arrayCreate    func(alloc uintptr, values *uintptr, n int64, callbacks uintptr) uintptr
	nullTypeID     func() uint64
	arrayCallbacks uintptr // kCFTypeArrayCallBacks, used by address

	system uintptr
	attrs  map[string]uintptr
	// walkSet is what reading an element for the tree asks for.
	walkSet attrSet
	// printAppSet and printWindowSet are what fingerprinting an app asks of
	// it and of its front window.
	printAppSet, printWindowSet attrSet
}

const (
	axErrorAPIDisabled    = -25211
	axErrorNoValue        = -25212
	axErrorCannotComplete = -25204
	axValueCGPoint        = 1
	axValueCGSize         = 2
	axValueAXError        = 5
	cfNumberDouble        = 13
	axMaxDepth            = 30
	axBudget              = 3 * time.Second
	axLongString          = 4096
	// axElementTimeout is how long one request to an element may take, never
	// more than the walk has left.
	axElementTimeout = time.Second
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
			"AXMenuBar", "AXMenuItemCmdChar", "AXMenuItemCmdModifiers", "AXFocusedUIElement", "AXSubrole",
		} {
			ref := axLib.createString(0, name, cfStringEncodingUTF8)
			if ref == 0 {
				return
			}
			axLib.attrs[name] = ref
		}
		loadBatchAX(libs)
		axLib.walkSet = newAttrSet("AXRole", "AXTitle", "AXDescription", "AXPosition", "AXSize", "AXEnabled", "AXFocused", "AXChildren", "AXSubrole")
		axLib.printAppSet = newAttrSet("AXWindows", "AXFocusedWindow", "AXFocusedUIElement")
		axLib.printWindowSet = newAttrSet("AXTitle", "AXChildren")
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

// loadBatchAX loads what reading several attributes in one call needs. It
// follows agent-desktop's approach (github.com/vercel-labs/agent-desktop,
// Apache-2.0): AXUIElementCopyMultipleAttributeValues with a slot per
// attribute, where a slot that failed holds an AXError value instead. Any
// symbol missing leaves batch off, and elements are read one attribute at a
// time as before; it never turns accessibility itself off.
func loadBatchAX(libs map[string]uintptr) {
	for _, fn := range []struct {
		ptr  any
		lib  string
		name string
	}{
		{&axLib.copyMultiple, "ApplicationServices", "AXUIElementCopyMultipleAttributeValues"},
		{&axLib.valueType, "ApplicationServices", "AXValueGetType"},
		{&axLib.arrayCreate, "CoreFoundation", "CFArrayCreate"},
		{&axLib.nullTypeID, "CoreFoundation", "CFNullGetTypeID"},
	} {
		if _, err := purego.Dlsym(libs[fn.lib], fn.name); err != nil {
			log.Printf("screen: reading elements one attribute at a time: no %s", fn.name)
			return
		}
		purego.RegisterLibFunc(fn.ptr, libs[fn.lib], fn.name)
	}
	// A data symbol: its address is the callbacks structure.
	callbacks, err := purego.Dlsym(libs["CoreFoundation"], "kCFTypeArrayCallBacks")
	if err != nil || callbacks == 0 {
		log.Printf("screen: reading elements one attribute at a time: no kCFTypeArrayCallBacks")
		return
	}
	axLib.arrayCallbacks = callbacks
	axLib.batch = true
}

// attrSet is a list of attributes read together, with the CFArray of their
// names a batch call takes, kept for the life of the process.
type attrSet struct {
	names []string
	array uintptr // 0 without batch
}

func newAttrSet(names ...string) attrSet {
	set := attrSet{names: names}
	if !axLib.batch {
		return set
	}
	refs := make([]uintptr, len(names))
	for i, name := range names {
		if refs[i] = axLib.attrs[name]; refs[i] == 0 {
			return set
		}
	}
	set.array = axLib.arrayCreate(0, &refs[0], int64(len(refs)), axLib.arrayCallbacks)
	return set
}

// axReadMany reads set's attributes of element, in one request when it can.
// Slot i is the value of set.names[i], or 0 when there is none: no value,
// an error, or null. The slots are borrowed; done releases them, after the
// caller has finished with them. A request that failed because the app
// didn't answer, or the element is gone, is not retried one attribute at a
// time, which would cost a timeout each.
func axReadMany(element uintptr, set attrSet) (slots []uintptr, done func()) {
	slots = make([]uintptr, len(set.names))
	if set.array != 0 {
		var values uintptr
		code := axLib.copyMultiple(element, set.array, 0, &values)
		if code == 0 && values != 0 && axLib.typeID(values) == axLib.arrayTypeID() && axLib.arrayCount(values) == int64(len(slots)) {
			for i := range slots {
				if v := axLib.arrayAt(values, int64(i)); axSlotUsable(v) {
					slots[i] = v
				}
			}
			return slots, func() { axLib.release(values) }
		}
		if values != 0 {
			axLib.release(values)
		}
		if code == axErrorCannotComplete || code == axErrorInvalidElement || code == axErrorAPIDisabled {
			return slots, func() {}
		}
	}
	for i, name := range set.names {
		slots[i], _ = axCopy(element, name)
	}
	return slots, func() {
		for _, v := range slots {
			if v != 0 {
				axLib.release(v)
			}
		}
	}
}

// axSlotUsable says whether a batch slot holds a value: not nothing, not
// kCFNull and not the AXError value a failed attribute leaves.
func axSlotUsable(v uintptr) bool {
	if v == 0 {
		return false
	}
	switch axLib.typeID(v) {
	case axLib.nullTypeID():
		return false
	case axLib.valueTypeID():
		return axLib.valueType(v) != axValueAXError
	}
	return true
}

// elementTimeout is the messaging timeout for one element: the usual, but
// not past the walk's deadline.
func elementTimeout(deadline time.Time) float32 {
	left := min(axElementTimeout, time.Until(deadline))
	return float32(max(left, 100*time.Millisecond).Seconds())
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
		axLib.setTimeout(element, elementTimeout(deadline))
		slots, done := axReadMany(element, axLib.walkSet)
		defer done()
		const role, title, description, position, size, enabled, focused, children, subrole = 0, 1, 2, 3, 4, 5, 6, 7, 8
		e := Element{Depth: depth, Role: cfStringOf(slots[role]), Subrole: cfStringOf(slots[subrole]), Title: cfStringOf(slots[title]),
			Frame: cfFrame(slots[position], slots[size]), Enabled: cfBoolOf(slots[enabled], true), Focused: cfBoolOf(slots[focused], false)}
		if e.Title == "" {
			e.Title = cfStringOf(slots[description])
		}
		// A password is never read, so it can't be shown or kept: its value
		// is read only now that its role and subrole are known, and not in
		// the batch.
		if !e.secureField() {
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
		list := slots[children]
		if list == 0 || axLib.typeID(list) != axLib.arrayTypeID() {
			return
		}
		// Children belong to the array, which outlives this loop.
		for i, n := int64(0), axLib.arrayCount(list); i < n; i++ {
			walk(axLib.arrayAt(list, i), depth+1)
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
	return cfStringOf(value)
}

// cfStringOf is a string value's text, or "" for anything else.
func cfStringOf(value uintptr) string {
	if value == 0 || axLib.typeID(value) != axLib.stringTypeID() {
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
	return cfValueText(value)
}

func cfValueText(value uintptr) string {
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
	return cfBoolOf(value, missing)
}

// cfBoolOf is a boolean value, or missing when there is none.
func cfBoolOf(value uintptr, missing bool) bool {
	if value == 0 || axLib.typeID(value) != axLib.booleanTypeID() {
		return missing
	}
	return axLib.boolValue(value)
}

// cfFrame is the element's frame from its position and size values, in global
// points, which are the main display's framebuffer pixels: capture runs at
// the display's point size.
func cfFrame(position, size uintptr) image.Rectangle {
	var at cgPoint
	var extent struct{ W, H float64 }
	if !cfGeometry(position, axValueCGPoint, unsafe.Pointer(&at)) || !cfGeometry(size, axValueCGSize, unsafe.Pointer(&extent)) {
		return image.Rectangle{}
	}
	return image.Rect(int(math.Round(at.X)), int(math.Round(at.Y)), int(math.Round(at.X+extent.W)), int(math.Round(at.Y+extent.H)))
}

func cfGeometry(value uintptr, kind uint32, out unsafe.Pointer) bool {
	return value != 0 && axLib.typeID(value) == axLib.valueTypeID() && axLib.valueGetValue(value, kind, out)
}
