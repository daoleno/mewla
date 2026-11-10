package screen

import (
	"fmt"
	"slices"
	"strings"
)

// Some shortcuts end an app or the person's session, and an agent that hits
// one by mistake loses unsaved work. They are refused on an agent's say-so
// alone, the way agent-desktop's macOS policy does (it needs --force):
// the agent asks the person first, and does it in the app's own menus with
// the menu action when they said yes.
var guardedCombos = []struct{ combo, what string }{
	{"cmd+q", "quit the app"},
	{"cmd+shift+q", "log the person out"},
	{"cmd+alt+shift+q", "log the person out at once"},
	{"cmd+alt+esc", "force quit apps"},
	{"ctrl+cmd+q", "lock the screen"},
	{"cmd+shift+delete", "empty the Trash"},
	{"cmd+alt+shift+delete", "empty the Trash without asking"},
}

// comboSet is a key combo's parts, without case, order or aliases.
func comboSet(combo string) []string {
	var set []string
	for _, part := range strings.Split(combo, "+") {
		name := strings.ToLower(strings.TrimSpace(part))
		switch name {
		case "command", "super", "meta", "win":
			name = "cmd"
		case "option", "opt":
			name = "alt"
		case "control":
			name = "ctrl"
		case "escape":
			name = "esc"
		case "backspace":
			name = "delete"
		}
		set = append(set, name)
	}
	slices.Sort(set)
	return set
}

// guardedKeys refuses a key combo that quits an app or ends the session.
func guardedKeys(combo string) error {
	set := comboSet(combo)
	for _, g := range append(guardedCombos, systemGuarded...) {
		if slices.Equal(set, comboSet(g.combo)) {
			return fmt.Errorf("%s would %s, which the person may lose work to. Ask them first; %s", combo, g.what, guardedAfterYes)
		}
	}
	return nil
}

// guardedMenuItem refuses a menu item that quits, logs out, shuts down or
// empties the Trash, for the same reason as guardedKeys.
func guardedMenuItem(title string) error {
	t := strings.ToLower(strings.TrimSpace(title))
	whole := strings.TrimRight(t, "….")
	refused := slices.Contains([]string{"quit", "log out", "shut down", "restart", "sleep", "force quit", "empty trash"}, whole)
	for _, prefix := range []string{"quit ", "log out ", "force quit ", "empty trash"} {
		refused = refused || strings.HasPrefix(t, prefix)
	}
	if refused {
		return fmt.Errorf("%q ends an app or the session, which the person may lose work to: ask them first, and when they say yes, set confirm_user_asked on the menu action", title)
	}
	return nil
}
