package screen

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// menuPather is a backend that can list and choose an app's menu items by
// their names, through accessibility, in the background: no key and no click.
type menuPather interface {
	// MenuItems lists what is directly under path in pid's menus, with the
	// menu bar's own items for an empty path: each item's title, its
	// shortcut and whether it is turned off.
	MenuItems(ctx context.Context, pid int, path []string) ([]MenuItem, error)
	// PressMenuPath chooses the item at path and gives its place in the menus.
	PressMenuPath(ctx context.Context, pid int, path []string) (string, error)
}

// MenuItem is one entry of a menu, as listed.
type MenuItem struct {
	Title    string
	Shortcut string
	Disabled bool
	// Submenu says choosing it opens more items.
	Submenu bool
}

// ErrNoMenus means the app has no menus to read, as an app with none of its own, or that hasn't answered.
var ErrNoMenus = errors.New("this app shows no menus to accessibility; use its keys or take a screenshot")

// menuPath splits "File > Save As…" into its titles. A trailing ">" (or no
// text) means the items under the menu are wanted, not a choice.
func menuPath(text string) (path []string, list bool) {
	text = strings.TrimSpace(text)
	list = text == "" || strings.HasSuffix(text, ">")
	for _, part := range strings.Split(strings.TrimSuffix(text, ">"), ">") {
		if part = strings.TrimSpace(part); part != "" {
			path = append(path, part)
		}
	}
	return path, list
}

// sameMenuTitle compares titles as a person writes them: without case, and
// without the ellipsis a menu puts after an item that asks something.
func sameMenuTitle(a, b string) bool {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		return strings.TrimSpace(strings.TrimRight(s, "…."))
	}
	return norm(a) == norm(b)
}

func formatMenuItems(app App, path []string, items []MenuItem) string {
	var b strings.Builder
	where := "the menu bar"
	if len(path) > 0 {
		where = strings.Join(path, " > ")
	}
	fmt.Fprintf(&b, "%s's %s:\n", oneLine(app.Name), where)
	for _, it := range items {
		b.WriteString("  " + oneLine(it.Title))
		if it.Submenu {
			b.WriteString(" >")
		}
		if it.Shortcut != "" {
			b.WriteString(" (" + it.Shortcut + ")")
		}
		if it.Disabled {
			b.WriteString(" (turned off)")
		}
		b.WriteByte('\n')
	}
	if len(items) == 0 {
		b.WriteString("  (nothing)\n")
	}
	b.WriteString("Choose one with the menu action: text \"File > Save As…\" (case and the … don't matter); end with > to list a menu.\n")
	return b.String()
}

// menuAction lists or chooses a menu item of an app by name, in the
// background, and gives the front back when choosing brought the app forward.
func (a *agentSession) menuAction(ctx context.Context, action AgentAction) (AgentResult, error) {
	pather, ok := a.apps.(menuPather)
	if !ok || a.apps == nil {
		return AgentResult{}, ErrNoApps
	}
	apps, err := a.apps.Apps(ctx)
	if err != nil {
		return AgentResult{}, err
	}
	app, err := resolveApp(apps, action.App)
	if err != nil {
		return AgentResult{}, err
	}
	path, list := menuPath(action.Text)
	if list {
		items, err := pather.MenuItems(ctx, app.PID, path)
		if err != nil {
			return AgentResult{}, a.gone(ctx, app, err)
		}
		return AgentResult{Text: formatMenuItems(app, path, items)}, nil
	}
	if !action.ConfirmUserAsked {
		for _, title := range path {
			if err := guardedMenuItem(title); err != nil {
				return AgentResult{}, err
			}
		}
	}
	before := a.frontApp(ctx, apps)
	where, err := pather.PressMenuPath(ctx, app.PID, path)
	if err != nil {
		return AgentResult{}, a.gone(ctx, app, err)
	}
	a.mu.Lock()
	a.lastAct = time.Now()
	a.mu.Unlock()
	return AgentResult{Text: fmt.Sprintf("Chose %s in %s's menus, without keys or clicks.", where, oneLine(app.Name)) + a.giveFrontBack(ctx, app, before) + " Read it with app_state to see the result."}, nil
}
