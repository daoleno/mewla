package gc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedScratch(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, "t", name)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "f"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTrashListRestore(t *testing.T) {
	root := t.TempDir()
	dir := seedScratch(t, root, "abc123")
	now := time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC)
	item, err := Trash(root, KindScratch, dir, "unit-a", "Session closed", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("original still there: %v", err)
	}
	items, err := List(root)
	if err != nil || len(items) != 1 || items[0].ID != item.ID || !items[0].Moved || items[0].Owner != "unit-a" {
		t.Fatalf("list = %+v, %v", items, err)
	}
	if got := Size(items[0].Path); got != 4 {
		t.Fatalf("size = %d", got)
	}
	// Restore refuses an occupied original path.
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(root, item.ID); err == nil {
		t.Fatal("restored over an existing path")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(root, item.ID); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "sub", "f")); err != nil || string(raw) != "data" {
		t.Fatalf("restored content = %q, %v", raw, err)
	}
	if items, _ := List(root); len(items) != 0 {
		t.Fatalf("record kept after restore: %+v", items)
	}
	if _, err := Restore(root, "../../etc"); err == nil {
		t.Fatal("restored an invalid id")
	}
}

func TestFinalizeDeletesOnlyPastGrace(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC)
	old, err := Trash(root, KindScratch, seedScratch(t, root, "old"), "u", "gone", now.Add(-TrashGrace-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	young, err := Trash(root, KindScratch, seedScratch(t, root, "young"), "u", "gone", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Restricted nested dirs (a Go module cache) are still deleted.
	if err := os.Chmod(filepath.Join(old.Path, "sub"), 0o500); err != nil {
		t.Fatal(err)
	}
	deleted, err := Finalize(root, now)
	if err != nil || len(deleted) != 1 || deleted[0].ID != old.ID {
		t.Fatalf("finalize = %+v, %v", deleted, err)
	}
	if _, err := os.Lstat(old.Path); !os.IsNotExist(err) {
		t.Fatalf("old item still there: %v", err)
	}
	if _, err := os.Lstat(young.Path); err != nil {
		t.Fatalf("young item deleted: %v", err)
	}
}

func TestARecordWhoseMoveNeverHappenedIsDropped(t *testing.T) {
	root := t.TempDir()
	dir := seedScratch(t, root, "stay")
	now := time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC)
	item, err := Trash(root, KindScratch, dir, "u", "gone", now)
	if err != nil {
		t.Fatal(err)
	}
	// A crash after the record and before the move: put it back by hand.
	if err := os.Rename(item.Path, dir); err != nil {
		t.Fatal(err)
	}
	items, _ := List(root)
	if len(items) != 1 || items[0].Moved {
		t.Fatalf("list = %+v", items)
	}
	if _, err := Restore(root, item.ID); err == nil {
		t.Fatal("restored an item that was never moved")
	}
	if _, err := Finalize(root, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if items, _ := List(root); len(items) != 0 {
		t.Fatalf("stale record kept: %+v", items)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "f")); err != nil {
		t.Fatalf("the never-moved original was touched: %v", err)
	}
}

func TestTrashRefusesASymlink(t *testing.T) {
	root := t.TempDir()
	target := seedScratch(t, root, "real")
	link := filepath.Join(root, "t", "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash(root, KindScratch, link, "u", "gone", time.Now()); err == nil {
		t.Fatal("trashed a symlink")
	}
}
