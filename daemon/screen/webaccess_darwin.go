//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"time"

	"github.com/ebitengine/purego/objc"
)

var webAX webAccess

// manualAccessibility is the attribute Chromium (and so Electron) answers by
// building its web accessibility tree.
const manualAccessibility = "AXManualAccessibility"

func manualAXName() uintptr {
	if axLib.attrs[manualAccessibility] == 0 {
		axLib.attrs[manualAccessibility] = axLib.createString(0, manualAccessibility, cfStringEncodingUTF8)
	}
	return axLib.attrs[manualAccessibility]
}

var macWebOps = webAccessOps{
	state: func(pid int) (on, supported bool) {
		app := appsLib.createApplication(int32(pid))
		if app == 0 {
			return false, false
		}
		defer axLib.release(app)
		axLib.setTimeout(app, 1)
		var value uintptr
		if code := axLib.copyAttribute(app, manualAXName(), &value); code != 0 || value == 0 {
			return false, false
		}
		defer axLib.release(value)
		return cfBoolOf(value, false), true
	},
	enable: func(pid int) error {
		app := appsLib.createApplication(int32(pid))
		if app == 0 {
			return errors.New("no app")
		}
		defer axLib.release(app)
		axLib.setTimeout(app, 1)
		yes := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithBool:"), true)
		if code := appsLib.setAttribute(app, manualAXName(), uintptr(yes)); code != 0 {
			return errors.New("refused")
		}
		return nil
	},
	size: func(ctx context.Context, pid int) int {
		app := appsLib.createApplication(int32(pid))
		if app == 0 {
			return 0
		}
		defer axLib.release(app)
		axLib.setTimeout(app, 1)
		windows, code := axCopy(app, "AXWindows")
		if code != 0 || windows == 0 {
			return 0
		}
		defer axLib.release(windows)
		if axLib.typeID(windows) != axLib.arrayTypeID() {
			return 0
		}
		var tree ElementTree
		deadline := time.Now().Add(500 * time.Millisecond)
		for i, n := int64(0), axLib.arrayCount(windows); i < n; i++ {
			walkElements(ctx, &tree, axLib.arrayAt(windows, i), 400, deadline, nil)
		}
		return len(tree.Elements)
	},
}
