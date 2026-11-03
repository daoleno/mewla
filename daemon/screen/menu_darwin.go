//go:build darwin && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unsafe"
)

// menuKids are an element's menu items. A menu bar item or an item with a
// submenu holds one AXMenu whose children are the items; a menu bar holds
// its items directly. The elements are borrowed from the array release frees.
func menuKids(el uintptr) (kids []uintptr, release func()) {
	children, code := axCopy(el, "AXChildren")
	if code != 0 || children == 0 {
		return nil, func() {}
	}
	if axLib.typeID(children) != axLib.arrayTypeID() {
		axLib.release(children)
		return nil, func() {}
	}
	n := axLib.arrayCount(children)
	if n == 1 {
		if only := axLib.arrayAt(children, 0); axString(only, "AXRole") == "AXMenu" {
			inner, code := axCopy(only, "AXChildren")
			if code == 0 && inner != 0 && axLib.typeID(inner) == axLib.arrayTypeID() {
				for i, m := int64(0), axLib.arrayCount(inner); i < m; i++ {
					kids = append(kids, axLib.arrayAt(inner, i))
				}
				return kids, func() { axLib.release(inner); axLib.release(children) }
			}
		}
	}
	for i := int64(0); i < n; i++ {
		kids = append(kids, axLib.arrayAt(children, i))
	}
	return kids, func() { axLib.release(children) }
}

func isMenuItem(el uintptr) bool {
	role := axString(el, "AXRole")
	return role == "AXMenuItem" || role == "AXMenuBarItem"
}

// resolveMenu walks path through an app's menus, one level at a time, and
// gives the retained element it ends on: the menu bar itself for no path.
// A title it can't find is an error that lists what is there.
func resolveMenu(app uintptr, path []string) (uintptr, error) {
	cur, code := axCopy(app, "AXMenuBar")
	switch {
	case code == axErrorAPIDisabled:
		return 0, ErrPermissions
	case code != 0 || cur == 0:
		return 0, ErrNoMenus
	}
	for depth, want := range path {
		kids, release := menuKids(cur)
		var next uintptr
		var titles []string
		for _, k := range kids {
			if !isMenuItem(k) {
				continue
			}
			title := axString(k, "AXTitle")
			if title != "" {
				titles = append(titles, title)
			}
			if next == 0 && sameMenuTitle(title, want) {
				next = appsLib.retain(k)
			}
		}
		release()
		axLib.release(cur)
		if next == 0 {
			where := "the menu bar"
			if depth > 0 {
				where = strings.Join(path[:depth], " > ")
			}
			return 0, fmt.Errorf("no menu item %q in %s; it has: %s", want, where, strings.Join(titles, ", "))
		}
		cur = next
	}
	return cur, nil
}

func (b *macBackend) MenuItems(ctx context.Context, pid int, path []string) ([]MenuItem, error) {
	if !loadApps() {
		return nil, ErrNoApps
	}
	var items []MenuItem
	var err error
	withPool(func() { items, err = listMenuItems(pid, path) })
	return items, err
}

func listMenuItems(pid int, path []string) ([]MenuItem, error) {
	app := appsLib.createApplication(int32(pid))
	if app == 0 {
		return nil, fmt.Errorf("couldn't reach the app with pid %d", pid)
	}
	defer axLib.release(app)
	axLib.setTimeout(app, 1)
	at, err := resolveMenu(app, path)
	if err != nil {
		return nil, err
	}
	defer axLib.release(at)
	kids, release := menuKids(at)
	defer release()
	var items []MenuItem
	for _, k := range kids {
		if !isMenuItem(k) {
			continue // separators
		}
		title := axString(k, "AXTitle")
		if title == "" {
			continue
		}
		item := MenuItem{Title: title, Disabled: !axBool(k, "AXEnabled", true)}
		if sub, release := menuKids(k); len(sub) > 0 {
			item.Submenu = true
			release()
		} else {
			release()
		}
		if char := axString(k, "AXMenuItemCmdChar"); char != "" {
			modifiers := int64(0)
			if value, code := axCopy(k, "AXMenuItemCmdModifiers"); code == 0 && value != 0 {
				if axLib.typeID(value) == axLib.numberTypeID() {
					axLib.numberValue(value, cfNumberSInt64, unsafe.Pointer(&modifiers))
				}
				axLib.release(value)
			}
			item.Shortcut = shortcutFrom(char, modifiers).String()
		}
		items = append(items, item)
	}
	return items, nil
}

func (b *macBackend) PressMenuPath(ctx context.Context, pid int, path []string) (string, error) {
	if !loadApps() {
		return "", ErrNoApps
	}
	var where string
	var err error
	withPool(func() { where, err = pressMenuPath(pid, path) })
	return where, err
}

func pressMenuPath(pid int, path []string) (string, error) {
	if len(path) == 0 {
		return "", errors.New("menu needs a path such as \"File > Save As…\"")
	}
	app := appsLib.createApplication(int32(pid))
	if app == 0 {
		return "", fmt.Errorf("couldn't reach the app with pid %d", pid)
	}
	defer axLib.release(app)
	axLib.setTimeout(app, 1)
	item, err := resolveMenu(app, path)
	if err != nil {
		return "", err
	}
	defer axLib.release(item)
	where := strings.Join(path, " > ")
	if !axBool(item, "AXEnabled", true) {
		return where, fmt.Errorf("%s is turned off in the app's menus right now, so it wasn't chosen", where)
	}
	press := axLib.createString(0, "AXPress", cfStringEncodingUTF8)
	if press == 0 {
		return where, errors.New("couldn't choose the menu item")
	}
	defer axLib.release(press)
	switch code := appsLib.performAction(item, press); code {
	case 0:
		return where, nil
	case axErrorAPIDisabled:
		return where, ErrPermissions
	case axErrorCannotComplete:
		// Choosing an item can run a while (a dialog opening): the app may
		// have done it without answering in time.
		return where, fmt.Errorf("chose %s, but the app didn't answer in time; read it to see whether it took effect", where)
	default:
		return where, fmt.Errorf("the app refused %s (accessibility error %d)", where, code)
	}
}

var _ menuPather = (*macBackend)(nil)
