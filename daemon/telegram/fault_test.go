package telegram

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// failWrites makes every write to the store's database fail until the
// returned function is called. Reads keep working.
func failWrites(t *testing.T, s *store) (restore func()) {
	t.Helper()
	triggers := map[string]string{
		"refuse_core_insert":  "BEFORE INSERT ON tg_core",
		"refuse_core_update":  "BEFORE UPDATE ON tg_core",
		"refuse_entry_insert": "BEFORE INSERT ON tg_entry",
		"refuse_entry_update": "BEFORE UPDATE ON tg_entry",
		"refuse_entry_delete": "BEFORE DELETE ON tg_entry",
	}
	for name, when := range triggers {
		if _, err := s.db.Exec("CREATE TRIGGER " + name + " " + when + " BEGIN SELECT RAISE(ABORT, 'injected write failure'); END"); err != nil {
			t.Fatal(err)
		}
	}
	return func() {
		t.Helper()
		for name := range triggers {
			if _, err := s.db.Exec("DROP TRIGGER " + name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// countWrites counts the rows the store writes from now on.
func countWrites(t *testing.T, s *store) (count func() int) {
	t.Helper()
	if _, err := s.db.Exec(`CREATE TABLE writes(what TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{
		`CREATE TRIGGER count_core_insert AFTER INSERT ON tg_core BEGIN INSERT INTO writes VALUES('core'); END`,
		`CREATE TRIGGER count_core_update AFTER UPDATE ON tg_core BEGIN INSERT INTO writes VALUES('core'); END`,
		`CREATE TRIGGER count_entry_insert AFTER INSERT ON tg_entry BEGIN INSERT INTO writes VALUES('entry'); END`,
		`CREATE TRIGGER count_entry_update AFTER UPDATE ON tg_entry BEGIN INSERT INTO writes VALUES('entry'); END`,
		`CREATE TRIGGER count_entry_delete AFTER DELETE ON tg_entry BEGIN INSERT INTO writes VALUES('entry'); END`,
	} {
		if _, err := s.db.Exec(trigger); err != nil {
			t.Fatal(err)
		}
	}
	return func() int {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM writes`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
}

// storedState reads the durable state of the store under root the way a
// restarted daemon would, as the state and as JSON.
func storedState(t *testing.T, root string) (durableState, []byte) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(root, "telegram", databaseName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	state, found, err := loadState(db)
	if err != nil || !found {
		t.Fatalf("stored state found=%v err=%v", found, err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return state, raw
}
