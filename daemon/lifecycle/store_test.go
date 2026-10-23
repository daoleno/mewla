package lifecycle

import (
	"encoding/json"
	"testing"
)

// allEvents reads the whole Event log in Seq order. Tests only: production
// code reads the log by index.
func allEvents(t *testing.T, e *Engine) []Event {
	if t != nil {
		t.Helper()
	}
	rows, err := e.db.Query(`SELECT body FROM lc_event ORDER BY seq`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			panic(err)
		}
		var event Event
		if err := json.Unmarshal([]byte(body), &event); err != nil {
			panic(err)
		}
		out = append(out, event)
	}
	return out
}
