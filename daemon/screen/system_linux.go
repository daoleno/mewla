//go:build linux

package screen

// systemName is what agents are told runs this computer.
const systemName = "Linux"

// systemGuarded are this system's own shortcuts that end an app or the
// session, beyond guardedCombos. An agent's display has no window manager,
// so nothing closes or locks it but the apps' own shortcuts.
var systemGuarded = []struct{ combo, what string }{
	{"ctrl+q", "quit the app"},
}

// guardedAfterYes is how to do a guarded thing once the person said yes.
const guardedAfterYes = "if they say yes, press the app's own Quit or Close button by its element id"

// ReadySummary is what the app says when agents can use this computer.
const ReadySummary = "Agents use apps on displays of their own, off your screen. Watch or stop them from Screen."
