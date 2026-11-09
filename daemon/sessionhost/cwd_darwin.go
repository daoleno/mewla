package sessionhost

import "os"

// processCwd has no cheap answer on macOS; the program's own report (OSC 7)
// is used instead.
func processCwd(*os.File, int) string { return "" }
