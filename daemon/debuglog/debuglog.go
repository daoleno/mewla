// Package debuglog gates routine daemon log lines behind MEWLA_LOG_VERBOSE.
//
// The daemon's terminal output is for diagnosing problems: failures, retries,
// degraded states and rejected requests go through the standard log package
// unconditionally. Routine success and heartbeat lines (client connect,
// stats refresh, upload complete) go through Printf here and stay silent
// unless MEWLA_LOG_VERBOSE is set to a true value.
package debuglog

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

var enabled atomic.Bool

func init() {
	enabled.Store(parse(os.Getenv("MEWLA_LOG_VERBOSE")))
}

func parse(value string) bool {
	on, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && on
}

// Enabled reports whether routine lines are printed.
func Enabled() bool { return enabled.Load() }

// SetEnabled overrides the environment setting; tests use it.
func SetEnabled(on bool) { enabled.Store(on) }

// Printf logs a routine line only when verbose logging is enabled.
func Printf(format string, args ...any) {
	if enabled.Load() {
		log.Printf(format, args...)
	}
}
