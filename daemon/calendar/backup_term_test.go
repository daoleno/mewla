package calendar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/gc"
)

// The move to SQLite sets calendar.json aside in a backup that has its term
// at birth: Mewla keeps it 30 days, then it goes to the trash.
func TestTheMoveToSQLiteGivesItsBackupATerm(t *testing.T) {
	state := t.TempDir()
	root := filepath.Join(state, "calendar")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	doc, _ := historyDocument(t, 3)
	raw, _ := json.Marshal(doc)
	writeCalendarFixture(t, root, raw)
	before := time.Now()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.db.Close()
	backups, _ := filepath.Glob(filepath.Join(state, backupPrefix+"*"))
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
	keeps, err := gc.ListKeeps(state)
	if err != nil || len(keeps) != 1 {
		t.Fatalf("keeps = %+v %v", keeps, err)
	}
	k := keeps[0]
	if k.Path != backups[0] || k.Owner != gc.OwnerMewla || !strings.Contains(k.Why, "SQLite") ||
		k.Until.Before(before.Add(gc.BackupTerm-time.Minute)) || k.Until.After(time.Now().Add(gc.BackupTerm)) {
		t.Fatalf("keep = %+v", k)
	}
	// Opening again migrates nothing and adds no keep.
	store, err = NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.db.Close()
	if keeps, _ := gc.ListKeeps(state); len(keeps) != 1 {
		t.Fatalf("keeps after a second open = %+v", keeps)
	}
	if _, err := gc.ExpireKeeps(state, k.Until.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	items, err := gc.List(state)
	if err != nil || len(items) != 1 || items[0].Original != backups[0] || items[0].Owner != gc.OwnerMewla {
		t.Fatalf("trash after the term = %+v %v", items, err)
	}
}
