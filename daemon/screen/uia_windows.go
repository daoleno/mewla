//go:build windows && !screenfake

package screen

import (
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"math"
	"runtime"
	"slices"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// UI Automation is how Windows apps show their buttons, fields and text to
// assistive technology. Every call runs on one thread of its own in the
// multithreaded apartment, so COM's rules hold whatever goroutine asks.

var (
	clsidCUIAutomation8 = guid("{E22AD333-B25F-460C-83D0-0581107395C9}")
	clsidCUIAutomation  = guid("{FF48DBA4-60EF-4201-AA87-54103EEF594E}")
	iidIUIAutomation2   = guid("{34723AFF-0C9D-49D0-9896-7AB52DF8CD8A}")
	iidIUIAutomation    = guid("{30CBE57D-D9D0-452A-AB13-7AC5AC4825EE}")
)

// Methods, by their place in each interface (mingw's uiautomationclient.h).
const (
	mAutoCompareElements             = 3
	mAutoElementFromHandleBuildCache = 10
	mAutoCreateCacheRequest          = 20
	mAuto2PutConnectionTimeout       = 61
	mAuto2PutTransactionTimeout      = 63

	mElemSetFocus                = 3
	mElemGetCurrentPropertyValue = 10
	mElemGetCachedPropertyValue  = 12
	mElemGetCurrentPattern       = 16
	mElemGetCachedChildren       = 19
	mElemCachedBoundingRectangle = 75
	mCacheAddProperty            = 3
	mCachePutTreeScope           = 7
	mArrayLength                 = 3
	mArrayElement                = 4
	mInvoke                      = 3
	mValueSetValue               = 3
	mValueCurrentValue           = 4
	mToggle                      = 3
	mSelect                      = 3
	mExpand                      = 3
	mCollapse                    = 4
	mExpandState                 = 5
	mRangeSetValue               = 3
	mTextDocumentRange           = 7
	mRangeGetText                = 12
	mLegacyDoDefaultAction       = 4
	mLegacySetValue              = 5
	treeScopeSubtree             = 7
	uiaElementNotAvailable       = 0x80040201
	uiaElementNotEnabled         = 0x80040200
	coinitMultithreaded          = 0x0
	clsctxInprocServer           = 0x1
)

// Patterns and properties.
const (
	patInvoke         = 10000
	patValue          = 10002
	patRangeValue     = 10003
	patExpandCollapse = 10005
	patSelectionItem  = 10010
	patText           = 10014
	patToggle         = 10015
	patLegacy         = 10018

	propRuntimeID         = 30000
	propProcessID         = 30002
	propControlType       = 30003
	propName              = 30005
	propHasKeyboardFocus  = 30008
	propIsEnabled         = 30010
	propIsPassword        = 30019
	propIsOffscreen       = 30022
	propIsExpandCollapse  = 30028
	propIsInvoke          = 30031
	propIsRangeValue      = 30033
	propIsSelectionItem   = 30036
	propIsText            = 30040
	propIsToggle          = 30041
	propIsValue           = 30043
	propValueValue        = 30045
	propValueIsReadOnly   = 30046
	propRangeValueValue   = 30047
	propSelectionSelected = 30079
	propToggleState       = 30086
)

var cachedProps = []int{propRuntimeID, propControlType, propName, propHasKeyboardFocus, propIsEnabled, propIsPassword,
	propIsOffscreen, propIsExpandCollapse, propIsInvoke, propIsRangeValue, propIsSelectionItem, propIsText, propIsToggle,
	propIsValue, propValueValue, propValueIsReadOnly, propRangeValueValue, propSelectionSelected, propToggleState, 30001}

// uia is the UI Automation client and the live elements reads handed out.
type uia struct {
	once  sync.Once
	calls chan func()
	auto  *comObject
	cache *comObject
	err   error

	mu   sync.Mutex
	refs map[uintptr]uiaRef
	next uintptr
}

type uiaRef struct {
	el *comObject
	id []int32 // the runtime id
}

func newUIA() *uia { return &uia{refs: map[uintptr]uiaRef{}} }

// do runs f on the automation thread.
func (u *uia) do(f func() error) error {
	u.once.Do(func() {
		u.calls = make(chan func())
		ready := make(chan struct{})
		go u.thread(ready)
		<-ready
	})
	if u.err != nil {
		return u.err
	}
	done := make(chan error, 1)
	u.calls <- func() { done <- f() }
	return <-done
}

func (u *uia) thread(ready chan struct{}) {
	runtime.LockOSThread()
	if err := windows.CoInitializeEx(0, coinitMultithreaded); err != nil {
		u.err = fmt.Errorf("start COM: %w", err)
		close(ready)
		return
	}
	var auto *comObject
	v2 := true
	r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidCUIAutomation8)), 0, clsctxInprocServer, uintptr(unsafe.Pointer(&iidIUIAutomation2)), uintptr(unsafe.Pointer(&auto)))
	if int32(r) < 0 || auto == nil {
		v2 = false
		r, _, _ = procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, clsctxInprocServer, uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&auto)))
	}
	if int32(r) < 0 || auto == nil {
		u.err = fmt.Errorf("start UI Automation: %w", hresult(uint32(r)))
		close(ready)
		return
	}
	if v2 {
		// A hung app answers no call: give up on it rather than wait.
		_ = auto.call(mAuto2PutConnectionTimeout, 2000)
		_ = auto.call(mAuto2PutTransactionTimeout, 5000)
	}
	var cache *comObject
	if err := auto.call(mAutoCreateCacheRequest, uintptr(unsafe.Pointer(&cache))); err != nil || cache == nil {
		u.err = fmt.Errorf("start UI Automation: %v", err)
		close(ready)
		return
	}
	for _, p := range cachedProps {
		_ = cache.call(mCacheAddProperty, uintptr(p))
	}
	_ = cache.call(mCachePutTreeScope, treeScopeSubtree)
	u.auto, u.cache = auto, cache
	close(ready)
	for f := range u.calls {
		f()
	}
}

// cachedVariant reads one cached property.
func cachedVariant(el *comObject, prop int) variant {
	var v variant
	if el.call(mElemGetCachedPropertyValue, uintptr(prop), uintptr(unsafe.Pointer(&v))) != nil {
		return variant{}
	}
	return v
}

func cachedBool(el *comObject, prop int) bool {
	v := cachedVariant(el, prop)
	defer v.clear()
	return v.boolean()
}

func cachedInt(el *comObject, prop int) int {
	v := cachedVariant(el, prop)
	defer v.clear()
	return v.int()
}

func cachedString(el *comObject, prop int) string {
	v := cachedVariant(el, prop)
	defer v.clear()
	return v.str()
}

// read reads the windows of an app, each with its whole subtree in one
// round trip, and lists them in reading order. origin is where the captured
// screen begins.
func (u *uia) read(hwnds []windows.HWND, limit int) (ElementTree, error) {
	var tree ElementTree
	err := u.do(func() error {
		count := 0
		for _, hwnd := range hwnds {
			var root *comObject
			if err := u.auto.call(mAutoElementFromHandleBuildCache, uintptr(hwnd), uintptr(unsafe.Pointer(u.cache)), uintptr(unsafe.Pointer(&root))); err != nil || root == nil {
				if len(tree.Elements) == 0 && len(hwnds) == 1 {
					return fmt.Errorf("the app didn't answer UI Automation: %v", err)
				}
				continue
			}
			u.walk(root, 0, limit, &count, &tree)
		}
		return nil
	})
	return tree, err
}

// walk adds el and what lies under it; it owns el's reference.
func (u *uia) walk(el *comObject, depth, limit int, count *int, tree *ElementTree) {
	if *count >= limit {
		tree.Truncated = true
		el.release()
		return
	}
	if depth > 0 && cachedBool(el, propIsOffscreen) {
		el.release()
		return
	}
	*count++
	tree.Elements = append(tree.Elements, u.element(el, depth))
	var children *comObject
	if el.call(mElemGetCachedChildren, uintptr(unsafe.Pointer(&children))) != nil || children == nil {
		return
	}
	defer children.release()
	var n int32
	_ = children.call(mArrayLength, uintptr(unsafe.Pointer(&n)))
	for i := int32(0); i < n; i++ {
		var child *comObject
		if children.call(mArrayElement, uintptr(i), uintptr(unsafe.Pointer(&child))) == nil && child != nil {
			u.walk(child, depth+1, limit, count, tree)
		}
	}
}

// element turns a cached element into the AX vocabulary the tools speak,
// and keeps its reference until the read is released.
func (u *uia) element(el *comObject, depth int) Element {
	controlType := cachedInt(el, propControlType)
	password := cachedBool(el, propIsPassword)
	editable := cachedBool(el, propIsValue) && !cachedBool(el, propValueIsReadOnly)
	role := uiaRole(controlType, password, editable || cachedBool(el, propIsText))
	e := Element{
		Depth:   depth,
		Role:    role,
		Title:   cachedString(el, propName),
		Enabled: cachedBool(el, propIsEnabled),
		Focused: cachedBool(el, propHasKeyboardFocus),
	}
	if role == "AXSecureTextField" {
		e.Subrole = "AXSecureTextField"
	} else if cachedBool(el, propIsValue) {
		e.Value = cachedString(el, propValueValue)
	} else if cachedBool(el, propIsToggle) {
		e.Value = fmt.Sprint(cachedInt(el, propToggleState))
	} else if cachedBool(el, propIsRangeValue) {
		v := cachedVariant(el, propRangeValueValue)
		if f, ok := v.float(); ok {
			e.Value = fmt.Sprint(math.Round(f*1000) / 1000)
		}
		v.clear()
	} else if cachedBool(el, propIsSelectionItem) && (role == "AXRadioButton" || role == "AXTab") {
		if cachedBool(el, propSelectionSelected) {
			e.Value = "1"
		} else {
			e.Value = "0"
		}
	}
	var r rect
	if el.call(mElemCachedBoundingRectangle, uintptr(unsafe.Pointer(&r))) == nil && r.Right > r.Left && r.Bottom > r.Top {
		e.Frame = image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom))
	}
	if cachedBool(el, propIsInvoke) || cachedBool(el, propIsToggle) || cachedBool(el, propIsSelectionItem) {
		e.Actions = append(e.Actions, "AXPress")
	}
	if cachedBool(el, propIsExpandCollapse) {
		e.Actions = append(e.Actions, "AXShowMenu")
		if !slices.Contains(e.Actions, "AXPress") {
			e.Actions = append(e.Actions, "AXPress")
		}
	}
	id := cachedVariant(el, propRuntimeID)
	ref := uiaRef{el: el, id: id.ints()}
	id.clear()
	u.mu.Lock()
	u.next++
	u.refs[u.next] = ref
	e.ref = u.next
	u.mu.Unlock()
	return e
}

// uiaRole names a control type as the tools do on every computer.
func uiaRole(controlType int, password, editable bool) string {
	switch controlType {
	case 50000:
		return "AXButton"
	case 50002:
		return "AXCheckBox"
	case 50003:
		return "AXComboBox"
	case 50004:
		if password {
			return "AXSecureTextField"
		}
		return "AXTextField"
	case 50030:
		if password {
			return "AXSecureTextField"
		}
		if editable {
			return "AXTextArea"
		}
		return "AXGroup"
	case 50005:
		return "AXLink"
	case 50006:
		return "AXImage"
	case 50007, 50024, 50029:
		return "AXRow"
	case 50008:
		return "AXList"
	case 50009:
		return "AXMenu"
	case 50010:
		return "AXMenuBar"
	case 50011:
		return "AXMenuItem"
	case 50012:
		return "AXProgressIndicator"
	case 50013:
		return "AXRadioButton"
	case 50014:
		return "AXScrollBar"
	case 50015:
		return "AXSlider"
	case 50016:
		return "AXIncrementor"
	case 50018:
		return "AXTabGroup"
	case 50019:
		return "AXTab"
	case 50020:
		return "AXStaticText"
	case 50021, 50040:
		return "AXToolbar"
	case 50022:
		return "AXHelpTag"
	case 50023:
		return "AXOutline"
	case 50028, 50036:
		return "AXTable"
	case 50031:
		return "AXMenuButton"
	case 50032:
		return "AXWindow"
	case 50035:
		return "AXColumnHeader"
	case 50037:
		return "AXTitleBar"
	case 50038:
		return "AXSplitter"
	}
	return "AXGroup"
}

func (u *uia) ref(e Element) (uiaRef, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	r, ok := u.refs[e.ref]
	return r, ok
}

func (u *uia) release(tree ElementTree) {
	var gone []*comObject
	u.mu.Lock()
	for _, e := range tree.Elements {
		if r, ok := u.refs[e.ref]; ok {
			gone = append(gone, r.el)
			delete(u.refs, e.ref)
		}
	}
	u.mu.Unlock()
	if len(gone) > 0 {
		_ = u.do(func() error {
			for _, el := range gone {
				el.release()
			}
			return nil
		})
	}
}

// live checks that an element still answers, as one that went away doesn't.
func live(el *comObject) error {
	var v variant
	err := el.call(mElemGetCurrentPropertyValue, propProcessID, uintptr(unsafe.Pointer(&v)))
	v.clear()
	var hr hresult
	if errors.As(err, &hr) && (uint32(hr) == uiaElementNotAvailable) {
		return ErrStaleElement
	}
	return err
}

// pattern gets one of an element's control patterns, or nil.
func pattern(el *comObject, id int) *comObject {
	var p *comObject
	if el.call(mElemGetCurrentPattern, uintptr(id), uintptr(unsafe.Pointer(&p))) != nil {
		return nil
	}
	return p
}

// act performs action on a live element.
func (u *uia) act(e Element, action, value string) error {
	r, ok := u.ref(e)
	if !ok {
		return ErrStaleElement
	}
	return u.do(func() error {
		if err := live(r.el); err != nil {
			return err
		}
		switch action {
		case "focus":
			return r.el.call(mElemSetFocus)
		case "set_value":
			if p := pattern(r.el, patValue); p != nil {
				defer p.release()
				b, err := allocBSTR(value)
				if err != nil {
					return err
				}
				defer freeBSTR(b)
				if err := p.call(mValueSetValue, uintptr(unsafe.Pointer(b))); err != nil {
					return fmt.Errorf("the app refused the value (%v); focus the element and type instead", err)
				}
				return nil
			}
			if p := pattern(r.el, patRangeValue); p != nil {
				defer p.release()
				var f float64
				if _, err := fmt.Sscan(value, &f); err != nil {
					return fmt.Errorf("this element takes a number, not %q", value)
				}
				return p.call(mRangeSetValue, uintptr(math.Float64bits(f)))
			}
			if p := pattern(r.el, patLegacy); p != nil {
				defer p.release()
				b, err := allocBSTR(value)
				if err != nil {
					return err
				}
				defer freeBSTR(b)
				if p.call(mLegacySetValue, uintptr(unsafe.Pointer(b))) == nil {
					return nil
				}
			}
			return errors.New("this element's value can't be set here; focus it and type instead")
		case "AXPress":
			if p := pattern(r.el, patInvoke); p != nil {
				defer p.release()
				return p.call(mInvoke)
			}
			if p := pattern(r.el, patToggle); p != nil {
				defer p.release()
				return p.call(mToggle)
			}
			if p := pattern(r.el, patSelectionItem); p != nil {
				defer p.release()
				return p.call(mSelect)
			}
			if p := pattern(r.el, patExpandCollapse); p != nil {
				defer p.release()
				var state int32
				_ = p.call(mExpandState, uintptr(unsafe.Pointer(&state)))
				if state == 1 { // expanded
					return p.call(mCollapse)
				}
				return p.call(mExpand)
			}
			if p := pattern(r.el, patLegacy); p != nil {
				defer p.release()
				return p.call(mLegacyDoDefaultAction)
			}
			return errors.New("this element can't be pressed through UI Automation; click it instead")
		case "AXShowMenu":
			if p := pattern(r.el, patExpandCollapse); p != nil {
				defer p.release()
				return p.call(mExpand)
			}
			return errors.New("this element has no menu to show")
		}
		return fmt.Errorf("this element doesn't do %s", action)
	})
}

// value reads up to limit characters of an element's text and its length.
func (u *uia) value(e Element, limit int) (string, int, error) {
	r, ok := u.ref(e)
	if !ok {
		return "", 0, ErrStaleElement
	}
	var text string
	err := u.do(func() error {
		if err := live(r.el); err != nil {
			return err
		}
		if p := pattern(r.el, patValue); p != nil {
			defer p.release()
			var b *uint16
			if err := p.call(mValueCurrentValue, uintptr(unsafe.Pointer(&b))); err == nil {
				text = bstrString(b)
				return nil
			}
		}
		if p := pattern(r.el, patText); p != nil {
			defer p.release()
			var rng *comObject
			if err := p.call(mTextDocumentRange, uintptr(unsafe.Pointer(&rng))); err != nil || rng == nil {
				return fmt.Errorf("read the text: %v", err)
			}
			defer rng.release()
			var b *uint16
			if err := rng.call(mRangeGetText, uintptr(limit+1), uintptr(unsafe.Pointer(&b))); err != nil {
				return fmt.Errorf("read the text: %v", err)
			}
			text = bstrString(b)
			return nil
		}
		text = e.Value
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	runes := []rune(text)
	n := len(runes)
	if n > limit {
		runes = runes[:limit]
	}
	return string(runes), n, nil
}

func (u *uia) hash(e Element) uintptr {
	r, ok := u.ref(e)
	if !ok || len(r.id) == 0 {
		return 0
	}
	h := fnv.New64a()
	for _, x := range r.id {
		h.Write([]byte{byte(x), byte(x >> 8), byte(x >> 16), byte(x >> 24)})
	}
	return uintptr(h.Sum64())
}

func (u *uia) same(a, b Element) bool {
	ra, oka := u.ref(a)
	rb, okb := u.ref(b)
	return oka && okb && len(ra.id) > 0 && slices.Equal(ra.id, rb.id)
}
