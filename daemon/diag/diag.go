// Package diag carries the daemon's own problems as typed events.
//
// An error path that Watch should know about calls Report with a stable kind,
// a severity and a subject. Report writes the human line to the daemon log and
// keeps the structured event for the Watch runtime collector, which reads
// events instead of grading log text. The log file stays for people: wording
// a log line any way you like changes nothing Watch sees.
package diag

import (
	"log"
	"strings"
	"sync"
	"time"
)

// Severity grades an event. The reporter decides it.
type Severity string

const (
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// Event is one reported problem.
type Event struct {
	// Seq numbers events from 1 in the order they were reported.
	Seq  uint64
	Time time.Time
	// Kind names the failing mechanism ("plugins.mcp.connect"). Kind and
	// Subject together identify the problem: Watch counts repeats of the pair,
	// so neither may carry ids, paths or numbers.
	Kind     string
	Severity Severity
	// Subject names what failed within the Kind (a plugin, a source); may be empty.
	Subject string
	// Detail is the human explanation, free to vary between repeats.
	Detail string
}

// capacity bounds the events kept between reads. A reader that falls behind
// loses the oldest and is told how many.
const capacity = 512

var (
	mu     sync.Mutex
	events []Event
	next   uint64 = 1
	now           = time.Now
)

// Report records a problem and logs it. The log line is
// "WARN|ERROR kind: subject: detail" (subject omitted when empty).
func Report(kind string, severity Severity, subject, detail string) {
	kind, subject, detail = strings.TrimSpace(kind), strings.TrimSpace(subject), strings.TrimSpace(detail)
	if severity != SeverityError {
		severity = SeverityWarn
	}
	mu.Lock()
	events = append(events, Event{Seq: next, Time: now().UTC(), Kind: kind, Severity: severity, Subject: subject, Detail: detail})
	next++
	if len(events) > capacity {
		events = append(events[:0], events[len(events)-capacity:]...)
	}
	mu.Unlock()

	level := "WARN"
	if severity == SeverityError {
		level = "ERROR"
	}
	if subject != "" {
		log.Printf("%s %s: %s: %s", level, kind, subject, detail)
	} else {
		log.Printf("%s %s: %s", level, kind, detail)
	}
}

// Last is the Seq of the newest event, 0 before the first. A reader that wants
// only what is reported from now on passes it to After.
func Last() uint64 {
	mu.Lock()
	defer mu.Unlock()
	return next - 1
}

// After returns the events reported after seq, oldest first, and how many
// after seq were already dropped to stay within capacity. Start from 0.
func After(seq uint64) (found []Event, dropped uint64) {
	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 {
		return nil, 0
	}
	first := events[0].Seq
	if seq+1 < first {
		dropped = first - seq - 1
		seq = first - 1
	}
	for _, event := range events {
		if event.Seq > seq {
			found = append(found, event)
		}
	}
	return found, dropped
}
