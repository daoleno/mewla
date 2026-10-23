package watch

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestCollectorDecodeReportsAChangedShapeOncePerKind(t *testing.T) {
	buf := captureLog(t)
	c := &Collector{}
	var issues []githubIssue
	if c.decode("github issues", []byte(`[{"number":1}]`), &issues) != true || len(issues) != 1 {
		t.Fatalf("valid body rejected: %v", issues)
	}
	for i := 0; i < 3; i++ {
		if c.decode("github issues", []byte(`{"message":"not a list"}`), &issues) {
			t.Fatal("an object where a list is expected must not decode")
		}
	}
	if got := strings.Count(buf.String(), "watch: github issues"); got != 1 {
		t.Fatalf("shape change logged %d times, want once:\n%s", got, buf.String())
	}
	if c.decode("github issues", []byte(`[]`), &issues) != true {
		t.Fatal("a good body after a bad one must decode")
	}
	c.decode("github issues", []byte(`{}`), &issues)
	if got := strings.Count(buf.String(), "watch: github issues"); got != 2 {
		t.Fatalf("a recovered kind must log its next failure; logged %d times", got)
	}
}

func TestNotifierSaveFailureIsLoggedOnce(t *testing.T) {
	f := newNotifyFixture(t, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC))
	buf := captureLog(t)
	// Replace the state file with a directory so the write cannot succeed.
	path := f.store.dir + "/" + notifyFile
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	f.n.mu.Lock()
	f.n.persistLocked()
	f.n.persistLocked()
	f.n.mu.Unlock()
	if got := strings.Count(buf.String(), "watch: save notify state"); got != 1 {
		t.Fatalf("save failure logged %d times, want once:\n%s", got, buf.String())
	}
}
