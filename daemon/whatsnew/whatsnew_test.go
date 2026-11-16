package whatsnew

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var t0 = time.UnixMilli(1_700_000_000_000)

func open(t *testing.T, dir, version string, existing bool, at time.Time) *Store {
	t.Helper()
	s, err := Open(dir, version, existing, at)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAFreshInstallIsItsOwnBaselineWithNoUpdate(t *testing.T) {
	dir := t.TempDir()
	r := open(t, dir, "0.4.9", false, t0).Record()
	if r.Baseline != "0.4.9" || len(r.Updates) != 0 || len(r.Seen) != 0 {
		t.Fatalf("record = %+v", r)
	}
}

func TestAMewlaInUseBeforeTheRecordCountsEveryReleaseAndLeavesItsLine(t *testing.T) {
	r := open(t, t.TempDir(), "0.4.9", true, t0).Record()
	if r.Baseline != "" || !reflect.DeepEqual(r.Updates, []Update{{Version: "0.4.9", At: t0.UnixMilli()}}) {
		t.Fatalf("record = %+v", r)
	}
}

func TestTheComputersOwnUpdateIsRecordedOnceAtStartup(t *testing.T) {
	dir := t.TempDir()
	open(t, dir, "0.4.9", false, t0)
	later := t0.Add(time.Hour)
	open(t, dir, "0.4.10", true, later)
	r := open(t, dir, "0.4.10", true, later.Add(time.Hour)).Record()
	want := []Update{{Version: "0.4.10", At: later.UnixMilli()}}
	if r.Baseline != "0.4.9" || !reflect.DeepEqual(r.Updates, want) {
		t.Fatalf("record = %+v", r)
	}
	// Going back to an older build is no update.
	r = open(t, dir, "0.4.9", true, later.Add(2*time.Hour)).Record()
	if !reflect.DeepEqual(r.Updates, want) {
		t.Fatalf("a downgrade was recorded: %+v", r.Updates)
	}
}

func TestSeenAddsIdsAndNeverReplacesThem(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, "0.4.9", true, t0)
	if _, err := s.MarkSeen([]string{"0.4.9-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkSeen([]string{"0.4.9-b", "0.4.9-a", ""}); err != nil {
		t.Fatal(err)
	}
	r := open(t, dir, "0.4.9", true, t0).Record()
	if !reflect.DeepEqual(r.Seen, []string{"0.4.9-a", "0.4.9-b"}) {
		t.Fatalf("seen = %v", r.Seen)
	}
}

func TestTheAppsUpdateIsNotedOnceAndNotBelowTheBaseline(t *testing.T) {
	s := open(t, t.TempDir(), "0.4.8", false, t0)
	if r, _ := s.NoteUpdate("0.4.8", t0); len(r.Updates) != 0 {
		t.Fatalf("the installed version is no update: %+v", r.Updates)
	}
	s.NoteUpdate("0.4.9", t0)
	r, _ := s.NoteUpdate("0.4.9", t0.Add(time.Minute))
	if !reflect.DeepEqual(r.Updates, []Update{{Version: "0.4.9", At: t0.UnixMilli()}}) {
		t.Fatalf("updates = %+v", r.Updates)
	}
	if _, err := s.NoteUpdate("not a version", t0); err == nil {
		t.Fatal("a malformed version was taken")
	}
}

func TestReplayMakesEverythingNewsUntilSeen(t *testing.T) {
	s := open(t, t.TempDir(), "0.4.9", false, t0)
	s.MarkSeen([]string{"0.4.9-a"})
	r, err := s.Replay()
	if err != nil || !r.Replay || r.Baseline != "" || len(r.Seen) != 0 {
		t.Fatalf("replay = %+v, %v", r, err)
	}
	r, _ = s.MarkSeen([]string{"0.4.9-a"})
	if r.Replay {
		t.Fatal("replay outlived the sheet")
	}
}

func TestAnUnreadableRecordStartsAgainAsAnExistingInstall(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := open(t, dir, "0.4.9", false, t0).Record(); r.Baseline != "" {
		t.Fatalf("record = %+v", r)
	}
}

func TestTheBundledNotesAreTheReleases(t *testing.T) {
	var notes struct {
		Releases []struct {
			Version string `json:"version"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(Notes(), &notes); err != nil || len(notes.Releases) == 0 {
		t.Fatalf("notes = %v, %v", notes, err)
	}
}
