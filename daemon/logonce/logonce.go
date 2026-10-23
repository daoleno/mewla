// Package logonce reports a repeating failure once instead of once per tick.
package logonce

import (
	"errors"
	"log"
	"sync"
)

// Log remembers the last failure of each scope. The zero value is ready.
type Log struct {
	mu   sync.Mutex
	last map[string]string
}

// Note logs "WARN <scope>: <err>" when err differs from the scope's previous
// failure. Failures are compared by their root cause, so paths and temp-file
// names inside a wrapping message do not make every repeat look new. A nil err
// clears the scope, so the next failure is logged again. It reports whether it
// wrote a line.
func (l *Log) Note(scope string, err error) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err == nil {
		delete(l.last, scope)
		return false
	}
	text := rootCause(err)
	if previous, ok := l.last[scope]; ok && previous == text {
		return false
	}
	if l.last == nil {
		l.last = map[string]string{}
	}
	l.last[scope] = text
	log.Printf("WARN %s: %v", scope, err)
	return true
}

func rootCause(err error) string {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err.Error()
		}
		err = next
	}
}
