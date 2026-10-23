package diag

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"
)

func reset(t *testing.T) *bytes.Buffer {
	t.Helper()
	mu.Lock()
	events, next = nil, 1
	mu.Unlock()
	var out bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &out
}

func TestReportKeepsTheStructuredEventAndWritesAHumanLine(t *testing.T) {
	out := reset(t)
	Report(" plugins.mcp ", SeverityError, "linear", "connect refused")
	Report("watch.collection", Severity("bogus"), "", "slow")

	found, dropped := After(0)
	if dropped != 0 || len(found) != 2 {
		t.Fatalf("events = %+v dropped=%d", found, dropped)
	}
	first, second := found[0], found[1]
	if first.Seq != 1 || first.Kind != "plugins.mcp" || first.Severity != SeverityError || first.Subject != "linear" || first.Detail != "connect refused" || first.Time.IsZero() {
		t.Errorf("first = %+v", first)
	}
	if second.Seq != 2 || second.Severity != SeverityWarn || second.Subject != "" {
		t.Errorf("second = %+v", second)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "ERROR plugins.mcp: linear: connect refused") || !strings.HasSuffix(lines[1], "WARN watch.collection: slow") {
		t.Errorf("log = %q", out.String())
	}
}

func TestAfterResumesFromACursorAndCountsWhatWasDropped(t *testing.T) {
	reset(t)
	now = func() time.Time { return time.Unix(0, 0) }
	t.Cleanup(func() { now = time.Now })
	for range capacity + 10 {
		Report("k", SeverityWarn, "s", "d")
	}
	found, dropped := After(0)
	if len(found) != capacity || dropped != 10 || found[0].Seq != 11 {
		t.Fatalf("after 0: %d events, dropped %d, first %d", len(found), dropped, found[0].Seq)
	}
	last := found[len(found)-1].Seq
	if again, dropped := After(last); len(again) != 0 || dropped != 0 {
		t.Fatalf("after last: %+v dropped=%d", again, dropped)
	}
	Report("k", SeverityWarn, "s", "d")
	if more, _ := After(last); len(more) != 1 || more[0].Seq != last+1 {
		t.Fatalf("after cursor: %+v", more)
	}
}
