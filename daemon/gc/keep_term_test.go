package gc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A keep gives a term to a path in no Session's scratch: the sweep leaves it
// alone until the date, then it goes to the trash where it is. A keep inside a
// live Session's scratch ends as before: the path stays for its lease.
func TestAKeepOutsideAScratchIsItsTermAndThenItGoesToTheTrash(t *testing.T) {
	saved := readsRecorded
	readsRecorded = func(string) bool { return true }
	t.Cleanup(func() { readsRecorded = saved })
	root := t.TempDir()
	tdir := filepath.Join(root, "t")
	listed := filepath.Join(tdir, "backup-listed")
	if err := os.MkdirAll(tdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(listed, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, _, err := SweepUnmarked(root, Usage{}, now); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(tdir, "backup-new")
	scratch := filepath.Join(tdir, "Zz9yX8")
	inScratch := filepath.Join(scratch, "out")
	if err := os.MkdirAll(inScratch, 0o700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{backup: "x", filepath.Join(scratch, scratchMarker): "mewla-worker-live\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	age(t, backup, 10*24*time.Hour)
	until := now.Add(72 * time.Hour)
	for path, owner := range map[string]string{listed: "brain", backup: "brain", inScratch: "mewla-worker-live"} {
		if _, err := AddKeep(root, path, owner, until, "rollback binary", now); err != nil {
			t.Fatal(err)
		}
	}
	if moved, _, err := SweepUnmarked(root, Usage{}, now.Add(UnmarkedGrace+time.Hour)); err != nil || len(moved) != 0 {
		t.Fatalf("the sweep took kept items: %v %v", moved, err)
	}
	why, err := WhyPath(WhyLookup{Root: root}, backup)
	if err != nil || !strings.HasPrefix(why.Chain[0].State, "kept until ") {
		t.Fatalf("gc why on a kept t/ item = %+v %v", why, err)
	}
	ended, err := ExpireKeeps(root, until.Add(time.Minute))
	if err != nil || len(ended) != 3 {
		t.Fatalf("ExpireKeeps = %v %v", ended, err)
	}
	items, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	trashed := map[string]Item{}
	for _, item := range items {
		trashed[item.Original] = item
	}
	for _, path := range []string{listed, backup} {
		item, ok := trashed[path]
		if !ok || !item.Moved || item.Kind != KindKept || item.Reason != ReasonKeepEnded || item.Owner != "brain" {
			t.Fatalf("%s at its keep's end: %+v (in trash %v)", path, item, ok)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still in place: %v", path, err)
		}
	}
	if _, ok := trashed[inScratch]; ok {
		t.Fatal("a kept path in a live Session's scratch went to the trash at the keep's end")
	}
	if _, err := os.Stat(inScratch); err != nil {
		t.Fatalf("the path in the live scratch: %v", err)
	}
}
