//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
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
			return fmt.Errorf("refused (accessibility error %d)", code)
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

// enhancedUIName is the attribute VoiceOver sets on an app it reads.
const enhancedUIName = "AXEnhancedUserInterface"

// setEnhancedUI sets AXEnhancedUserInterface on the app with pid: what
// VoiceOver sets, on which Chrome builds its pages' tree (it no longer answers
// AXManualAccessibility). Chrome then runs as for a screen reader, so it is set
// only while Mewla needs a page and put back after; never off while VoiceOver
// runs, whose setting it is too. Chrome answers a set it made with "not
// implemented": the value read back is the answer.
func setEnhancedUI(pid int, on bool) error {
	if !loadApps() {
		return ErrNoApps
	}
	var err error
	withPool(func() {
		if !on && voiceOverRuns() {
			return
		}
		app := appsLib.createApplication(int32(pid))
		if app == 0 {
			err = errors.New("no app")
			return
		}
		defer axLib.release(app)
		axLib.setTimeout(app, 1)
		if axLib.attrs[enhancedUIName] == 0 {
			axLib.attrs[enhancedUIName] = axLib.createString(0, enhancedUIName, cfStringEncodingUTF8)
		}
		if axBool(app, enhancedUIName, !on) == on {
			return
		}
		value := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithBool:"), on)
		if code := appsLib.setAttribute(app, axLib.attrs[enhancedUIName], uintptr(value)); code != 0 && axBool(app, enhancedUIName, !on) != on {
			err = fmt.Errorf("refused (accessibility error %d)", code)
		}
	})
	return err
}

// voiceOverRuns says whether VoiceOver is running. It runs inside an
// autorelease pool.
func voiceOverRuns() bool {
	apps := objc.ID(objc.GetClass("NSRunningApplication")).Send(objc.RegisterName("runningApplicationsWithBundleIdentifier:"), nsString("com.apple.VoiceOver"))
	return apps != 0 && objc.Send[uint64](apps, objc.RegisterName("count")) > 0
}

// SetEnhancedUI is setEnhancedUI for the extension flow.
func (b *macBackend) SetEnhancedUI(pid int, on bool) error { return setEnhancedUI(pid, on) }
