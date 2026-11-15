package terminal

import "sync/atomic"

// A Session's history is served at most this many rows and bytes at a time.
const (
	historyLineLimit = 6000
	historyByteLimit = 4 << 20
)

// terminalIDCounter numbers the Terminal views this daemon opens.
var terminalIDCounter atomic.Int64
