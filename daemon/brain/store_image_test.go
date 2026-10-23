package brain

import (
	"encoding/json"
	"testing"
)

// storeImage is the committed durable image of a Store as comparable bytes:
// the presentation rows and the timeline in order. Tests use it to assert that
// a failed or repeated operation changed nothing.
func storeImage(t *testing.T, store *Store) (presentation, timeline []byte) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	database, err := readPresentationRowsForTest(store)
	if err != nil {
		t.Fatal(err)
	}
	presentation, err = json.Marshal(database)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	if err := scanBodies(store.db, `SELECT body FROM timeline ORDER BY seq`, func(body []byte) error {
		rows = append(rows, string(body))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	timeline, err = json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return presentation, timeline
}

// readPresentationRowsForTest reads the presentation image back from the
// database, not from the resident copy, so a test sees what is durable.
func readPresentationRowsForTest(store *Store) (presentationDatabase, error) {
	database, _, err := readPresentationImage(store.db)
	return database, err
}

// dropPresentationRows empties the presentation tables, as if the projection
// had been lost while the lifecycle engine kept the canonical state.
func dropPresentationRows(t *testing.T, store *Store) {
	t.Helper()
	for _, statement := range []string{
		`DELETE FROM pres_work`, `DELETE FROM pres_event`, `DELETE FROM pres_turn`, `DELETE FROM pres_admission`,
		`UPDATE pres_meta SET value = '0' WHERE key = 'next_event_sequence'`,
		`UPDATE pres_meta SET value = '' WHERE key = 'host_foreground_turn'`,
	} {
		if _, err := store.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}
