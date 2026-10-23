package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/daoleno/mewla/daemon/diag"
)

// Runtime signals are the daemon's own problems, as the error paths reported
// them through diag (kind, severity, subject). Watch never reads the log file:
// wording a log line differently changes nothing here. A problem's signature
// is its kind and subject, so the repeated-error rule counts one failure
// however its detail varies.

const (
	kindRuntimeError = "runtime_error"
	kindRuntimeWarn  = "runtime_warn"
)

// collectRuntime turns the diag events reported since the last pass into
// signals. It runs inside a pass, which holds c.pass.
func (c *Collector) collectRuntime(ctx context.Context) (int, error) {
	events, dropped := diag.After(c.runtimeSeq)
	if len(events) == 0 {
		return 0, ctx.Err()
	}
	projectID := c.store.RuntimeProject()
	signals := make([]Signal, 0, len(events))
	for _, event := range events {
		signal := runtimeSignal(event)
		signal.ProjectID = projectID
		signals = append(signals, signal)
	}
	added, err := c.store.Append(signals)
	if err != nil {
		// The next pass offers the same events again; Append is idempotent by ID.
		return added, err
	}
	c.runtimeSeq = events[len(events)-1].Seq
	if dropped > 0 {
		// More problems were reported between passes than diag keeps.
		return added, fmt.Errorf("%d daemon problem reports were dropped before Watch read them", dropped)
	}
	return added, ctx.Err()
}

func runtimeSignal(event diag.Event) Signal {
	kind := kindRuntimeWarn
	if event.Severity == diag.SeverityError {
		kind = kindRuntimeError
	}
	pattern := event.Kind
	if event.Subject != "" {
		pattern += " " + event.Subject
	}
	message := event.Kind + ": " + event.Detail
	if event.Subject != "" {
		message = event.Kind + ": " + event.Subject + ": " + event.Detail
	}
	return Signal{
		// Identity: when and what, plus the process-local sequence so two
		// identical reports in the same instant stay two signals.
		ID:      signalID(SourceRuntime, event.Time.Format("2006-01-02T15:04:05.000000000Z")+"|"+strconv.FormatUint(event.Seq, 10)+"|"+pattern),
		Source:  SourceRuntime,
		Time:    event.Time,
		Kind:    kind,
		Excerpt: excerpt(message, 500),
		Refs:    map[string]string{"signature": signatureID(pattern), "pattern": excerpt(pattern, 200)},
	}
}

func signatureID(signature string) string {
	sum := sha256.Sum256([]byte(signature))
	return hex.EncodeToString(sum[:6])
}
