//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ebitengine/purego"
)

// A window Mewla opened in another app (the Chrome window it adds its
// extension in) is closed by its window number, straight through
// accessibility: no agent's hold on the app stands between Mewla and closing
// what it opened.

var ownWindowLib struct {
	once      sync.Once
	ok        bool
	getWindow func(element uintptr, id *uint32) int32
}

func loadOwnWindow() bool {
	if !loadApps() {
		return false
	}
	ownWindowLib.once.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			log.Printf("screen: window numbers unavailable: %v", err)
			return
		}
		// Private, and what every window manager on macOS uses: the window
		// number of an accessibility window.
		if _, err := purego.Dlsym(lib, "_AXUIElementGetWindow"); err != nil {
			log.Printf("screen: window numbers unavailable: no _AXUIElementGetWindow")
			return
		}
		purego.RegisterLibFunc(&ownWindowLib.getWindow, lib, "_AXUIElementGetWindow")
		ownWindowLib.ok = true
	})
	return ownWindowLib.ok
}

// WindowNumber is the system's number of a window of an earlier read.
func (b *macBackend) WindowNumber(e Element) (uint32, error) {
	if !loadOwnWindow() {
		return 0, errors.New("window numbers are unavailable")
	}
	if e.ref == 0 {
		return 0, ErrStaleElement
	}
	var id uint32
	var code int32
	withPool(func() { code = ownWindowLib.getWindow(e.ref, &id) })
	if code != 0 || id == 0 {
		return 0, fmt.Errorf("the window has no number (accessibility error %d)", code)
	}
	return id, nil
}

// CloseWindow presses the close button of the app's window with that number,
// then waits until the window is gone. A window already gone is closed.
func (b *macBackend) CloseWindow(ctx context.Context, pid int, number uint32) error {
	if !loadOwnWindow() {
		return errors.New("window numbers are unavailable")
	}
	found := false
	var err error
	withPool(func() {
		app := appsLib.createApplication(int32(pid))
		if app == 0 {
			err = fmt.Errorf("couldn't reach the app with pid %d", pid)
			return
		}
		defer axLib.release(app)
		axLib.setTimeout(app, 1)
		windows, code := axCopy(app, "AXWindows")
		if code == axErrorAPIDisabled {
			err = ErrPermissions
			return
		}
		if code != 0 || windows == 0 {
			err = fmt.Errorf("the app's windows can't be read (accessibility error %d)", code)
			return
		}
		defer axLib.release(windows)
		if axLib.typeID(windows) != axLib.arrayTypeID() {
			err = errors.New("the app's windows can't be read")
			return
		}
		for i, n := int64(0), axLib.arrayCount(windows); i < n; i++ {
			window := axLib.arrayAt(windows, i)
			var id uint32
			if ownWindowLib.getWindow(window, &id) != 0 || id != number {
				continue
			}
			found = true
			button, code := axCopy(window, "AXCloseButton")
			if code != 0 || button == 0 {
				err = fmt.Errorf("the window has no close button (accessibility error %d)", code)
				return
			}
			defer axLib.release(button)
			press := axLib.createString(0, "AXPress", cfStringEncodingUTF8)
			if press == 0 {
				err = errors.New("couldn't name the press")
				return
			}
			defer axLib.release(press)
			if code := appsLib.performAction(button, press); code != 0 {
				err = fmt.Errorf("its close button refused (accessibility error %d)", code)
			}
			return
		}
	})
	if err != nil {
		return err
	}
	listed, err := windowListed(pid, number)
	if err != nil {
		return err
	}
	if !found {
		if listed {
			return fmt.Errorf("window %d is not among the app's accessibility windows", number)
		}
		return nil
	}
	deadline := time.Now().Add(3 * time.Second)
	for listed {
		if time.Now().After(deadline) {
			return fmt.Errorf("window %d was asked to close and is still there", number)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		if listed, err = windowListed(pid, number); err != nil {
			return err
		}
	}
	return nil
}

// windowListed says whether the app still has the window with that number.
func windowListed(pid int, number uint32) (bool, error) {
	all, err := windowInfos(cgWindowListExcludeDesktop)
	if err != nil {
		return false, err
	}
	for _, w := range all {
		if w.PID == pid && w.ID == number {
			return true, nil
		}
	}
	return false, nil
}
