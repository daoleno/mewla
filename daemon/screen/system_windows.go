//go:build windows

package screen

// systemName is what agents are told runs this computer.
const systemName = "Windows"

// systemGuarded are this system's own shortcuts that end an app or the
// session, beyond guardedCombos (win is cmd there).
var systemGuarded = []struct{ combo, what string }{
	{"alt+f4", "close the app or shut down Windows"},
	{"cmd+l", "lock the screen"},
	{"ctrl+alt+delete", "open the security screen"},
}

// guardedAfterYes is how to do a guarded thing once the person said yes.
const guardedAfterYes = "if they say yes, press the app's own Close button by its element id"

// ReadySummary is what the app says when agents can use this computer.
const ReadySummary = "Agents can see and use this screen. Watch or stop them from Screen."
