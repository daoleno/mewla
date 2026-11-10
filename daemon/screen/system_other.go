//go:build !darwin && !linux && !windows

package screen

// systemName is what agents are told runs this computer.
const systemName = "the system"

var systemGuarded []struct{ combo, what string }

const guardedAfterYes = "if they say yes, do it in the app itself"

// ReadySummary is what the app says when agents can use this computer.
const ReadySummary = "Agents can see and use this screen. Watch or stop them from Screen."
