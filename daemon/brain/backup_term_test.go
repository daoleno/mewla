package brain

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/gc"
)

// The move to SQLite sets Brain's old files aside in a backup that Mewla
// keeps 30 days, in the state root's records (the Brain root is <state>/brain).
func TestBrainsMoveToSQLiteGivesItsBackupATerm(t *testing.T) {
	state := t.TempDir()
	root := filepath.Join(state, "brain")
	if err := os.MkdirAll(filepath.Join(root, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state", "messages.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	backups, _ := filepath.Glob(filepath.Join(root, legacyBackupPrefix+"*"))
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
	keeps, err := gc.ListKeeps(state)
	if err != nil || len(keeps) != 1 || keeps[0].Path != backups[0] || keeps[0].Owner != gc.OwnerMewla ||
		keeps[0].Until.Before(time.Now().Add(gc.BackupTerm-time.Hour)) {
		t.Fatalf("keeps = %+v %v", keeps, err)
	}
}
