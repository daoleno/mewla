package sqlitedb

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func open(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func TestMigrateAppliesStepsOnceInOrderAndRefusesANewerDatabase(t *testing.T) {
	db, path := open(t)
	steps := []string{`CREATE TABLE a(x INTEGER)`, `ALTER TABLE a ADD COLUMN y INTEGER`}
	if err := Migrate(db, "part", steps[:1]); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db, "part", steps); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db, "part", steps); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO a(x, y) VALUES(1, 2)`); err != nil {
		t.Fatal(err)
	}
	// A component is versioned on its own.
	if err := Migrate(db, "other", []string{`CREATE TABLE b(z INTEGER)`}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := Migrate(reopened, "part", steps[:1]); err == nil || !strings.Contains(err.Error(), "newer than this build") {
		t.Fatalf("older build opened a newer database: %v", err)
	}
}

func TestFailedMigrationStepChangesNothing(t *testing.T) {
	db, _ := open(t)
	if err := Migrate(db, "part", []string{`CREATE TABLE a(x INTEGER); INSERT INTO nowhere VALUES(1)`}); err == nil {
		t.Fatal("broken step succeeded")
	}
	if _, err := db.Exec(`SELECT * FROM a`); err == nil {
		t.Fatal("half a step was applied")
	}
	var version int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version WHERE component = 'part'`).Scan(&version); err != nil || version != 0 {
		t.Fatalf("version=%d err=%v", version, err)
	}
}

func TestTxRollsBackOnErrorAndWritersQueueInsteadOfFailing(t *testing.T) {
	db, _ := open(t)
	if _, err := db.Exec(`CREATE TABLE n(v INTEGER)`); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if err := Tx(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO n VALUES(1)`); err != nil {
			return err
		}
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Tx(db, func(tx *sql.Tx) error {
				var count int
				if err := tx.QueryRow(`SELECT COUNT(*) FROM n`).Scan(&count); err != nil {
					return err
				}
				_, err := tx.Exec(`INSERT INTO n VALUES(?)`, count)
				return err
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var count, distinct int
	if err := db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT v) FROM n`).Scan(&count, &distinct); err != nil {
		t.Fatal(err)
	}
	// Each writer read the count inside its own transaction: no lost update,
	// and the rolled-back row never appeared.
	if count != 16 || distinct != 16 {
		t.Fatalf("count=%d distinct=%d", count, distinct)
	}
}
