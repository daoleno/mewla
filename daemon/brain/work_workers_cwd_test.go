package brain

import (
	"os"
	"testing"
)

// A Worker cwd recorded from a raw OSC 7 report (a Mac Session before the
// report was decoded) reads back as its path, so a stored list heals itself.
func TestWorkWorkerRecordHealsAnOSC7Cwd(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	line := `{"work_id":"w1","session_id":"s1","cwd":"file:///Users/x/a%20b","joined_at":"2026-11-14T00:00:00Z"}` + "\n"
	if err := os.WriteFile(store.workWorkersPath(), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	records, err := store.workWorkerRecords()
	if err != nil {
		t.Fatal(err)
	}
	if got := records[workWorkerKey("w1", "s1")].Cwd; got != "/Users/x/a b" {
		t.Fatalf("stored cwd reads as %q, want /Users/x/a b", got)
	}
}
