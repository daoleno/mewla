package gc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A restore of a t/ item starts it over as an ordinary unmarked item: no
// Session marker, not in the first sweep's list, and its 48 h start at the
// restore, not at its files' old times.
func TestARestoredTRootItemIsOrdinaryAndItsGraceStartsAtTheRestore(t *testing.T) {
	saved := readsRecorded
	readsRecorded = func(string) bool { return true }
	t.Cleanup(func() { readsRecorded = saved })
	root := t.TempDir()
	tdir := filepath.Join(root, "t")
	scratch := filepath.Join(tdir, "Ab3dE6")
	binary := filepath.Join(tdir, "old.bin")
	backup := filepath.Join(root, "executors.toml.before")
	if err := os.MkdirAll(filepath.Join(scratch, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(scratch, scratchMarker): "mewla-worker-abc\n",
		filepath.Join(scratch, "sub", "f"):    "x",
		binary:                                "x",
		backup:                                "x",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The first sweep lists both t/ items as there before the rule.
	if moved, _, err := SweepUnmarked(root, Usage{}, time.Now()); err != nil || len(moved) != 0 {
		t.Fatalf("first sweep = %v, %v", moved, err)
	}
	var ids []string
	for _, original := range []string{scratch, binary, backup} {
		item, err := Trash(root, KindScratch, original, "mewla-worker-abc", "test", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		age(t, item.Path, 10*24*time.Hour)
		ids = append(ids, item.ID)
	}
	backupTime := time.Now().Add(-10 * 24 * time.Hour)
	for _, id := range ids {
		if _, err := Restore(root, id); err != nil {
			t.Fatal(err)
		}
	}
	restored := time.Now()
	if _, err := os.Lstat(filepath.Join(scratch, scratchMarker)); !os.IsNotExist(err) {
		t.Fatalf("the restored scratch still has its marker: %v", err)
	}
	if raw, err := os.ReadFile(snapshotPath(root)); err != nil || strings.Contains(string(raw), `"old.bin"`) || strings.Contains(string(raw), `"Ab3dE6"`) {
		t.Fatalf("the first sweep's list still names a restored item: %s %v", raw, err)
	}
	if info, err := os.Stat(backup); err != nil || info.ModTime().After(backupTime.Add(time.Minute)) {
		t.Fatalf("an item restored outside t/ was changed: %v %v", info.ModTime(), err)
	}
	if moved, _, err := SweepUnmarked(root, Usage{}, restored.Add(time.Hour)); err != nil || len(moved) != 0 {
		t.Fatalf("an hour after the restore the sweep took %v (%v)", moved, err)
	}
	moved, _, err := SweepUnmarked(root, Usage{}, restored.Add(UnmarkedGrace+time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, item := range moved {
		got[item.Original] = item.Reason
	}
	if len(moved) != 2 || got[scratch] != ReasonUnmarked || got[binary] != ReasonUnmarked {
		t.Fatalf("49 h after the restore the sweep took %v", got)
	}
}
