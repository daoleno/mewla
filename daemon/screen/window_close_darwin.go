//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// CloseKeyWindow presses the close button of the app's key window through
// accessibility. Nothing is raised or typed, so the app can stay behind other
// windows, where its Close menu item is off. The key window is the app's
// focused window: with none, no other window is guessed at (it could be a
// document of the person's).
func (b *macBackend) CloseKeyWindow(ctx context.Context, pid int) (CloseResult, error) {
	if !loadApps() {
		return CloseResult{}, ErrNoApps
	}
	var result CloseResult
	var err error
	withPool(func() { result, err = closeKeyWindow(ctx, pid) })
	return result, err
}

func closeKeyWindow(ctx context.Context, pid int) (CloseResult, error) {
	app := appsLib.createApplication(int32(pid))
	if app == 0 {
		return CloseResult{}, fmt.Errorf("couldn't reach the app with pid %d", pid)
	}
	defer axLib.release(app)
	axLib.setTimeout(app, 1)
	window, code := axCopy(app, "AXFocusedWindow")
	switch {
	case code == axErrorAPIDisabled:
		return CloseResult{}, ErrPermissions
	case code != 0 || window == 0:
		return CloseResult{}, errors.New("the app has no key window to close")
	}
	defer axLib.release(window)
	result := CloseResult{Title: axString(window, "AXTitle")}
	button, code := axCopy(window, "AXCloseButton")
	if code != 0 || button == 0 {
		return result, errors.New("its key window has no close button")
	}
	defer axLib.release(button)
	if !axBool(button, "AXEnabled", true) {
		return result, errors.New("its close button is turned off")
	}
	press := axLib.createString(0, "AXPress", cfStringEncodingUTF8)
	if press == 0 {
		return result, errors.New("couldn't press the close button")
	}
	defer axLib.release(press)
	switch code := appsLib.performAction(button, press); code {
	case 0:
	case axErrorAPIDisabled:
		return result, ErrPermissions
	case axErrorCannotComplete:
		// Closing can run a while (a sheet opening): look for the sheet anyway.
	default:
		return result, fmt.Errorf("the app refused the close button (accessibility error %d)", code)
	}
	// A document with unsaved changes asks first, with a sheet on its window.
	deadline := time.Now().Add(600 * time.Millisecond)
	for {
		if sheet := sheetButtons(window); len(sheet) > 0 {
			result.Sheet = sheet
			return result, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return result, nil
		}
		time.Sleep(60 * time.Millisecond)
	}
}

// sheetButtons are the titles of the buttons of a sheet on a window.
func sheetButtons(window uintptr) []string {
	kids, release := menuKids(window)
	defer release()
	var titles []string
	for _, k := range kids {
		if axString(k, "AXRole") != "AXSheet" {
			continue
		}
		inner, releaseInner := menuKids(k)
		for _, c := range inner {
			if axString(c, "AXRole") == "AXButton" {
				if title := axString(c, "AXTitle"); title != "" {
					titles = append(titles, title)
				}
			}
		}
		releaseInner()
	}
	return titles
}

var _ windowCloser = (*macBackend)(nil)
