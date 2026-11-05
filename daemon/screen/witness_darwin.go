//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"unsafe"
)

// Witness reads the focus of an app, in front or not.
func (b *macBackend) Witness(ctx context.Context, pid int) (Witness, error) {
	if !loadAX() {
		return Witness{}, ErrNoElements
	}
	var w Witness
	withPool(func() {
		app := appsLib.createApplication(int32(pid))
		if app == 0 {
			return
		}
		defer axLib.release(app)
		axLib.setTimeout(app, 1)
		if window, code := axCopy(app, "AXFocusedWindow"); code == 0 && window != 0 {
			w.Window = axString(window, "AXTitle")
			axLib.release(window)
			w.Readable = true
		} else if windows, code := axCopy(app, "AXWindows"); code == 0 && windows != 0 {
			if axLib.arrayCount(windows) > 0 {
				w.Window = axString(axLib.arrayAt(windows, 0), "AXTitle")
				w.Readable = true
			}
			axLib.release(windows)
		}
		field, code := axCopy(app, "AXFocusedUIElement")
		if code != 0 || field == 0 {
			// An app that isn't in front, or on the agent display, often
			// doesn't give its focused element to a reader asking the
			// application: look in its window for the focused element, or for
			// the text field the click would land in.
			window, wcode := axCopy(app, "AXFocusedWindow")
			if wcode != 0 || window == 0 {
				if windows, c := axCopy(app, "AXWindows"); c == 0 && windows != 0 {
					if axLib.arrayCount(windows) > 0 {
						window = axLib.arrayAt(windows, 0)
						appsLib.retain(window)
					}
					axLib.release(windows)
				}
			}
			if window == 0 {
				return
			}
			field = witnessFieldIn(window)
			axLib.release(window)
			if field == 0 {
				return
			}
		}
		defer axLib.release(field)
		axLib.setTimeout(field, 1)
		role, title := axString(field, "AXRole"), axString(field, "AXTitle")
		w.Field = role + "|" + title
		subrole := axString(field, "AXSubrole")
		if subrole == "AXSecureTextField" {
			return
		}
		w.Text = axValueText(field)
		w.Caret = witnessRange(field)
	})
	if !w.Readable {
		return w, errors.New("the app gave no accessibility reading")
	}
	return w, nil
}

// witnessRange is the element's AXSelectedTextRange as "location,length".
func witnessRange(element uintptr) string {
	value, code := axCopy(element, "AXSelectedTextRange")
	if code != 0 || value == 0 {
		return ""
	}
	defer axLib.release(value)
	var r struct{ Location, Length int64 }
	const axValueCFRange = 4
	if !axLib.valueGetValue(value, axValueCFRange, unsafe.Pointer(&r)) {
		return ""
	}
	return fmt.Sprintf("%d,%d", r.Location, r.Length)
}

var _ appWitness = (*macBackend)(nil)

// witnessFieldIn is the element with the focus in a window, or else its first
// text field or text area: where typing or a click in a text app shows. The
// caller releases it.
func witnessFieldIn(window uintptr) uintptr {
	var firstText uintptr
	visited := 0
	var walk func(element uintptr, depth int) uintptr
	walk = func(element uintptr, depth int) uintptr {
		if depth > 8 || visited > 400 {
			return 0
		}
		visited++
		role := axString(element, "AXRole")
		if axBool(element, "AXFocused", false) && role != "AXWindow" {
			appsLib.retain(element)
			return element
		}
		switch role {
		case "AXTextArea", "AXTextField", "AXComboBox", "AXTextView":
			if firstText == 0 {
				appsLib.retain(element)
				firstText = element
			}
		}
		children, code := axCopy(element, "AXChildren")
		if code != 0 || children == 0 {
			return 0
		}
		defer axLib.release(children)
		for i := int64(0); i < axLib.arrayCount(children); i++ {
			if found := walk(axLib.arrayAt(children, i), depth+1); found != 0 {
				return found
			}
		}
		return 0
	}
	if found := walk(window, 0); found != 0 {
		if firstText != 0 {
			axLib.release(firstText)
		}
		return found
	}
	return firstText
}
