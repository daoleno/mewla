//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// How a pop-up button is chosen from without a model turn in between.
//
// An AppKit pop-up lists its items (an AXMenu with AXMenuItem children) only
// while its menu is open, and a press on the pop-up does not return until the
// app's menu tracking ends or the call times out. So the press runs on its own
// goroutine with a short messaging timeout, the items are read as soon as they
// show, the wanted one is pressed (which ends the tracking), and a menu that is
// still open after that is cancelled. The menu is open for tens to hundreds of
// milliseconds instead of a whole model turn. This is the technique cua-driver
// documents for its set_value on a pop-up; the code is Mewla's own.
const (
	popupOpenTimeout  = 2500 * time.Millisecond
	popupPoll         = 20 * time.Millisecond
	popupStablePolls  = 3
	popupPressTimeout = 0.5 // seconds the opening press may block the call
	popupCloseWait    = 400 * time.Millisecond
)

// popupItems lists the items the app shows for a pop-up right now: those in
// its AXMenu, or directly under it (Chromium). The elements are borrowed from
// arrays that release frees.
func popupItems(popup uintptr) (titles []string, items []uintptr, menus []uintptr, release func()) {
	var releases []func()
	list, code := axCopy(popup, "AXChildren")
	if code != 0 || list == 0 || axLib.typeID(list) != axLib.arrayTypeID() {
		if list != 0 {
			axLib.release(list)
		}
		return nil, nil, nil, func() {}
	}
	releases = append(releases, func() { axLib.release(list) })
	add := func(item uintptr) {
		if axString(item, "AXRole") != "AXMenuItem" {
			return
		}
		titles = append(titles, axString(item, "AXTitle"))
		items = append(items, item)
	}
	for i, n := int64(0), axLib.arrayCount(list); i < n; i++ {
		kid := axLib.arrayAt(list, i)
		switch axString(kid, "AXRole") {
		case "AXMenu":
			menus = append(menus, kid)
			inner, code := axCopy(kid, "AXChildren")
			if code == 0 && inner != 0 {
				if axLib.typeID(inner) == axLib.arrayTypeID() {
					for j, m := int64(0), axLib.arrayCount(inner); j < m; j++ {
						add(axLib.arrayAt(inner, j))
					}
				}
				releases = append(releases, func() { axLib.release(inner) })
			}
		case "AXMenuItem":
			add(kid)
		}
	}
	return titles, items, menus, func() {
		for _, r := range releases {
			r()
		}
	}
}

func (b *macBackend) ChoosePopupItem(ctx context.Context, e Element, title string) (PopupChoice, error) {
	if !loadApps() {
		return PopupChoice{}, ErrNoApps
	}
	if e.ref == 0 {
		return PopupChoice{}, ErrStaleElement
	}
	var choice PopupChoice
	var err error
	withPool(func() { choice, err = choosePopupItem(ctx, e, title) })
	return choice, err
}

func axAction(ref uintptr, action string) int32 {
	name := axLib.createString(0, action, cfStringEncodingUTF8)
	if name == 0 {
		return -1
	}
	defer axLib.release(name)
	return appsLib.performAction(ref, name)
}

// cancelPopupMenus closes the pop-up's menu if it is open.
func cancelPopupMenus(popup uintptr) {
	_, _, menus, release := popupItems(popup)
	defer release()
	for _, m := range menus {
		axAction(m, "AXCancel")
	}
}

func choosePopupItem(ctx context.Context, e Element, title string) (PopupChoice, error) {
	if err := stillRead(e); err != nil {
		return PopupChoice{}, err
	}
	popup := e.ref
	if !axBool(popup, "AXEnabled", true) {
		return PopupChoice{}, errors.New("this pop-up is turned off right now")
	}
	closed, _, _, releaseClosed := popupItems(popup)
	closed = slices.Clone(closed)
	releaseClosed()

	started := time.Now()
	axLib.setTimeout(popup, popupPressTimeout)
	pressed := make(chan int32, 2)
	press := func(action string) {
		go withPool(func() { pressed <- axAction(popup, action) })
	}
	// A menu an earlier press left open already lists its items (a closed
	// Chromium pop-up lists only the selected one); pressing again would
	// close it.
	if len(closed) > 1 {
		closed = nil
	} else {
		press("AXPress")
	}
	alternate := false
	defer func() {
		// No AX call may outlive this one, and the timeout goes back to the default.
		select {
		case <-pressed:
		case <-time.After(popupCloseWait):
		}
		axLib.setTimeout(popup, 0)
	}()

	var last []string
	stable := 0
	deadline := started.Add(popupOpenTimeout)
	for {
		titles, items, _, release := popupItems(popup)
		if at := matchPopupItem(titles, title); at >= 0 {
			chosen := titles[at]
			code := axAction(items[at], "AXPress")
			release()
			open := time.Since(started)
			if code != 0 && code != axErrorCannotComplete {
				cancelPopupMenus(popup)
				return PopupChoice{}, fmt.Errorf("the app refused the item %q (accessibility error %d)", chosen, code)
			}
			// Pressing the item ends the tracking; a menu still showing is cancelled.
			for end := time.Now().Add(popupCloseWait); time.Now().Before(end); time.Sleep(popupPoll) {
				if open, _, _, rel := popupItems(popup); len(open) == 0 {
					rel()
					break
				} else {
					rel()
				}
			}
			cancelPopupMenus(popup)
			return PopupChoice{Title: chosen, Open: open}, nil
		}
		showing := len(titles) > 0 && !slices.Equal(titles, closed)
		if showing {
			if slices.Equal(titles, last) {
				stable++
			} else {
				stable = 0
			}
		}
		last = slices.Clone(titles)
		release()
		if stable >= popupStablePolls {
			cancelPopupMenus(popup)
			return PopupChoice{}, &PopupItemError{Want: title, Options: dropEmpty(last)}
		}
		select {
		case code := <-pressed:
			switch {
			case code == 0, code == axErrorCannotComplete:
				// The menu may open just after the press returns; keep looking.
				pressed <- code
				pressed = make(chan int32, 2)
				pressed <- code
			case code == axErrorActionUnsupported && !alternate:
				alternate = true
				press("AXShowMenu")
			case code == axErrorAPIDisabled:
				return PopupChoice{}, ErrPermissions
			default:
				if len(last) == 0 || slices.Equal(last, closed) {
					return PopupChoice{}, fmt.Errorf("this pop-up would not open (accessibility error %d); click it and choose with a key or a click on the item", code)
				}
			}
		case <-ctx.Done():
			cancelPopupMenus(popup)
			return PopupChoice{}, ctx.Err()
		case <-time.After(popupPoll):
		}
		if time.Now().After(deadline) {
			cancelPopupMenus(popup)
			return PopupChoice{}, &PopupItemError{Want: title, Options: dropEmpty(last)}
		}
	}
}

func dropEmpty(titles []string) []string {
	var out []string
	for _, t := range titles {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

var _ popupChooser = (*macBackend)(nil)
