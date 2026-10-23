package calendar

import "testing"

// failWrites makes every write to the store's database fail, as a full or
// failing disk would, until the returned function is called. Reads keep
// working, so a test can check memory and disk agree after the failure.
func failWrites(t *testing.T, store *Store) (restore func()) {
	t.Helper()
	triggers := map[string]string{
		"refuse_item_insert": "BEFORE INSERT ON cal_item",
		"refuse_item_update": "BEFORE UPDATE ON cal_item",
		"refuse_run_insert":  "BEFORE INSERT ON cal_run",
		"refuse_run_update":  "BEFORE UPDATE ON cal_run",
	}
	for name, when := range triggers {
		if _, err := store.db.Exec("CREATE TRIGGER " + name + " " + when + " BEGIN SELECT RAISE(ABORT, 'injected write failure'); END"); err != nil {
			t.Fatal(err)
		}
	}
	return func() {
		t.Helper()
		for name := range triggers {
			if _, err := store.db.Exec("DROP TRIGGER " + name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// snapshot is the store's durable content as it would be read back after a
// restart.
func snapshot(t *testing.T, store *Store) map[string]Item {
	t.Helper()
	items, err := loadItems(store.db)
	if err != nil {
		t.Fatal(err)
	}
	return items
}
