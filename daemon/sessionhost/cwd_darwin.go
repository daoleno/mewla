package sessionhost

import "os"

// processCwd has no cheap answer on macOS; the program's own report (OSC 7)
// is used instead.
func processCwd(*os.File, int) string { return "" }

// processCommand is not read on macOS yet (P3).
func processCommand(*os.File, int) string { return "" }
