package gc

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestUsageFindsAPathAndWhatIsUnderIt(t *testing.T) {
	u := newUsage(map[string]int{"/r/t/a-b": 1, "/r/t/a/x/y": 2, "/r/t/c": 3})
	for path, want := range map[string]int{"/r/t/a": 2, "/r/t/a-b": 1, "/r/t/c": 3, "/r/t/a/x": 2} {
		if pid, ok := u.Uses(path); !ok || pid != want {
			t.Fatalf("Uses(%s) = %d, %v; want %d", path, pid, ok, want)
		}
	}
	for _, path := range []string{"/r/t/b", "/r/t/a/x/y/z", "/r/t/c-"} {
		if _, ok := u.Uses(path); ok {
			t.Fatalf("Uses(%s): nothing holds it", path)
		}
	}
}

func age(t *testing.T, path string, ago time.Duration) {
	t.Helper()
	when := time.Now().Add(-ago)
	_ = filepath.Walk(path, func(p string, _ os.FileInfo, _ error) error {
		return os.Chtimes(p, when, when)
	})
}

func TestSweepUnmarkedTakesOnlyNewUnusedUnchangedItems(t *testing.T) {
	root := t.TempDir()
	tdir := filepath.Join(root, "t")
	mk := func(name string, file bool) string {
		path := filepath.Join(tdir, name)
		if file {
			if err := os.MkdirAll(tdir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		} else if err := os.MkdirAll(filepath.Join(path, "sub"), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	old := mk("before", false)
	age(t, old, 100*time.Hour)
	now := time.Now()
	// The first sweep lists what is there and takes nothing.
	if moved, err := SweepUnmarked(root, Usage{}, now); err != nil || len(moved) != 0 {
		t.Fatalf("first sweep = %v, %v", moved, err)
	}
	aged := mk("aged", false)
	agedFile := mk("aged.log", true)
	lock := mk("0g-heavy.lock", true)
	fresh := mk("fresh", false)
	used := mk("used", false)
	marked := mk("marked", false)
	if err := os.WriteFile(filepath.Join(marked, scratchMarker), []byte("u\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deep := mk("deep", false)
	for _, p := range []string{aged, agedFile, lock, used, marked, deep} {
		age(t, p, 49*time.Hour)
	}
	if err := os.WriteFile(filepath.Join(deep, "sub", "new"), []byte("n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		sock := filepath.Join(tdir, "s.sock")
		l, err := net.Listen("unix", sock)
		if err == nil {
			defer l.Close()
			age(t, sock, 49*time.Hour)
		}
	}
	usage := newUsage(map[string]int{filepath.Join(used, "sub"): 42})
	moved, err := SweepUnmarked(root, usage, now)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, item := range moved {
		got[filepath.Base(item.Original)] = true
		if item.Kind != KindUnmarked || item.Reason != ReasonUnmarked {
			t.Fatalf("trashed %+v", item)
		}
	}
	if len(got) != 2 || !got["aged"] || !got["aged.log"] {
		t.Fatalf("trashed %v, want aged and aged.log only", got)
	}
	for _, kept := range []string{old, lock, fresh, used, marked, deep} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("%s was taken: %v", filepath.Base(kept), err)
		}
	}
	if _, err := Restore(root, moved[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestEvictCacheDeletesTheLeastRecentlyChangedUnusedEntries(t *testing.T) {
	root := t.TempDir()
	dir := CacheDir(root)
	write := func(name string, bytes int, ago time.Duration) string {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "blob"), make([]byte, bytes), 0o600); err != nil {
			t.Fatal(err)
		}
		age(t, path, ago)
		return path
	}
	oldest := write("oldest", 400, 72*time.Hour)
	oldUsed := write("old-used", 400, 48*time.Hour)
	middle := write("middle", 400, 24*time.Hour)
	newest := write("newest", 400, time.Hour)
	usage := newUsage(map[string]int{oldUsed: 7})
	evicted, err := EvictCache(root, 900, usage)
	if err != nil {
		t.Fatal(err)
	}
	if len(evicted) != 2 || evicted[0].Path != oldest || evicted[1].Path != middle {
		t.Fatalf("evicted %+v, want oldest then middle", evicted)
	}
	for _, kept := range []string{oldUsed, newest} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("%s evicted", kept)
		}
	}
	if again, _ := EvictCache(root, 900, usage); len(again) != 0 {
		t.Fatalf("under budget, evicted %+v", again)
	}
}

func TestAKeptPathOutlivesItsSessionUntilItsDate(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, "t", "abcdef")
	kept := filepath.Join(scratch, "out")
	if err := os.MkdirAll(kept, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kept, "artifact"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, bad := range []struct {
		until time.Time
		why   string
		want  string
	}{
		{now.Add(time.Hour), "", "reason"},
		{now.Add(-time.Hour), "x", "not in the future"},
		{now.Add(31 * 24 * time.Hour), "x", "ask Brain"},
	} {
		if _, err := AddKeep(root, kept, "unit-a", bad.until, bad.why, now); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Fatalf("AddKeep(%v, %q) = %v, want %q", bad.until, bad.why, err, bad.want)
		}
	}
	k, err := AddKeep(root, kept, "unit-a", now.Add(48*time.Hour), "the release gate reads it", now)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := AddKeep(root, filepath.Join(scratch, "deleted-later"), "unit-a", now.Add(time.Hour), "x", now)
	if err != nil {
		t.Fatal(err)
	}
	other, err := AddKeep(root, filepath.Join(root, "t", "zzzzzz", "x"), "unit-b", now.Add(time.Hour), "x", now)
	if err != nil {
		t.Fatal(err)
	}

	notes, err := MoveKept(root, "unit-a", scratch)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]bool{}
	for _, note := range notes {
		byID[note.Keep.ID] = note.Gone
	}
	if goneFlag, ok := byID[k.ID]; len(notes) != 2 || !ok || goneFlag || !byID[gone.ID] {
		t.Fatalf("notes = %+v: want the kept path moved and the missing one dropped", notes)
	}
	dest := KeptPath(root, k)
	if _, err := os.Stat(filepath.Join(dest, "artifact")); err != nil {
		t.Fatalf("kept path not moved: %v", err)
	}
	if again, _ := MoveKept(root, "unit-a", scratch); len(again) != 0 {
		t.Fatalf("second move = %+v", again)
	}
	if _, err := ReleaseKeep(root, other.ID, "unit-a", now); err == nil {
		t.Fatal("released another Session's keep")
	}
	if ended, _ := ExpireKeeps(root, now.Add(2*time.Hour)); len(ended) != 1 || ended[0].ID != other.ID {
		t.Fatalf("ended %+v, want only the expired one", ended)
	}
	ended, err := ExpireKeeps(root, now.Add(49*time.Hour))
	if err != nil || len(ended) != 1 || ended[0].ID != k.ID {
		t.Fatalf("ended %+v, %v", ended, err)
	}
	items, _ := List(root)
	if len(items) != 1 || items[0].Kind != KindKept || items[0].Reason != ReasonKeepEnded {
		t.Fatalf("trash %+v", items)
	}
	if keeps, _ := ListKeeps(root); len(keeps) != 0 {
		t.Fatalf("keeps left %+v", keeps)
	}
}
