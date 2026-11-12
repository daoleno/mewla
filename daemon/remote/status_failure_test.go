package remote

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/link"
)

// The newest failed stretch keeps when it began ("Relay unreachable ·
// 13:40") through retries that fail again, takes the newest cause, survives
// the recovery and starts over only after the connection was up.
func TestStatusKeepsWhenTheNewestFailureBegan(t *testing.T) {
	at := func(minute int) time.Time { return time.Date(2026, 11, 12, 13, minute, 0, 0, time.UTC) }
	var status Status
	steps := []link.ConnectorState{
		{Phase: "connecting", ChangedAt: at(39)},
		{Phase: "offline", LastError: "relay unreachable", ChangedAt: at(40)},
		{Phase: "connecting", ChangedAt: at(41)},
		{Phase: "offline", LastError: "relay refused the grant", ChangedAt: at(42)},
	}
	for _, step := range steps {
		status.applyConnectorState(step)
	}
	if status.LastFailureAt == nil || !status.LastFailureAt.Equal(at(40)) || status.LastFailure != "relay refused the grant" {
		t.Fatalf("failing stretch: %+v", status)
	}
	status.applyConnectorState(link.ConnectorState{Phase: "connected", Relay: "tokyo", ChangedAt: at(45)})
	if status.ConnectedAt == nil || !status.ConnectedAt.Equal(at(45)) || !status.LastFailureAt.Equal(at(40)) || status.LastError != "" {
		t.Fatalf("recovered: %+v", status)
	}
	status.applyConnectorState(link.ConnectorState{Phase: "offline", ChangedAt: at(50)})
	if !status.LastFailureAt.Equal(at(40)) {
		t.Fatalf("an offline with no error is not a failure: %+v", status)
	}
	status.applyConnectorState(link.ConnectorState{Phase: "offline", LastError: "relay unreachable", ChangedAt: at(55)})
	if !status.LastFailureAt.Equal(at(55)) || status.LastFailure != "relay unreachable" {
		t.Fatalf("new stretch: %+v", status)
	}
}

// The status file carries the failure, and a change is heard after it is
// written.
func TestStatusWriterReportsFailureChanges(t *testing.T) {
	dir := t.TempDir()
	var heard []Status
	writer := &statusWriter{stateDir: dir, onChange: func(s Status) { heard = append(heard, s) }}
	writer.update(func(s *Status) { s.Phase = "connecting" })
	writer.update(func(s *Status) {
		s.applyConnectorState(link.ConnectorState{Phase: "offline", LastError: "relay unreachable", ChangedAt: time.Date(2026, 11, 12, 13, 40, 0, 0, time.UTC)})
	})
	writer.update(func(s *Status) { s.CertError = "" })
	loaded, ok := LoadStatus(dir)
	if !ok || loaded.LastFailure != "relay unreachable" || loaded.LastFailureAt == nil || loaded.LastFailureAt.Minute() != 40 {
		t.Fatalf("file = %+v %v", loaded, ok)
	}
	if len(heard) != 2 || heard[1].LastFailure != "relay unreachable" {
		t.Fatalf("heard = %+v", heard)
	}
}
