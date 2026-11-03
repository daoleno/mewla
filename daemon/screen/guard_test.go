package screen

import "testing"

func TestGuardedKeys(t *testing.T) {
	for _, c := range []string{"cmd+q", "Cmd+Q", "command+q", "q+cmd", "cmd+shift+Q", "cmd+option+Escape", "ctrl+cmd+q", "cmd+shift+backspace"} {
		if guardedKeys(c) == nil {
			t.Errorf("%q should be refused", c)
		}
	}
	for _, c := range []string{"cmd+w", "cmd+c", "q", "cmd+shift+4", "Return", "cmd+alt+c", "cmd+delete"} {
		if err := guardedKeys(c); err != nil {
			t.Errorf("%q should pass: %v", c, err)
		}
	}
}

func TestGuardedMenuItem(t *testing.T) {
	for _, m := range []string{"Quit Notes", "Quit", "Log Out daoleno…", "Shut Down…", "Empty Trash…", "Restart…"} {
		if guardedMenuItem(m) == nil {
			t.Errorf("%q should be refused", m)
		}
	}
	for _, m := range []string{"Quick Look", "Save", "Restart Numbering", "Sleepy"} {
		if err := guardedMenuItem(m); err != nil {
			t.Errorf("%q should pass: %v", m, err)
		}
	}
}
