//go:build darwin

package screen

// systemName is what agents are told runs this computer.
const systemName = "macOS"

// systemGuarded are this system's own shortcuts that end an app or the
// session, beyond guardedCombos.
var systemGuarded []struct{ combo, what string }

// guardedAfterYes is how to do a guarded thing once the person said yes.
const guardedAfterYes = "if they say yes, choose the item in the app's menus with the menu action"

// ReadySummary is what the app says when agents can use this computer.
const ReadySummary = "Agents can see and use this screen. Watch or stop them from Screen."
