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
			return
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
