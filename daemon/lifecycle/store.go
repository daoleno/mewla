package lifecycle

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// The durable model is two tables and one counter.
//
//	lc_work   one row per Work: the reduced State as JSON, minus the internal
//	          source-dedupe index (see below).
//	lc_event  the append-only Event log, one row per Event, keyed by Seq.
//	lc_meta   next_seq, the next Event Seq to allocate.
//
// A command commits its new Events, the Work row they reduce into and
// next_seq in one transaction, so a crash leaves either the old Work or the
// new one, never a Work whose log disagrees. The log is never rewritten or
// compacted; indexed lookups keep reads independent of its length.
//
// State.SeenSources is exactly the set of SourceIDs of a Work's Events. The
// unique index on (work_id, source_id) is that set's durable form: it makes
// exactly-once source admission a database constraint, and Open rebuilds the
// in-memory index from it instead of storing it a second time in the row.
const schemaComponent = "lifecycle"

var schemaSteps = []string{`
CREATE TABLE lc_work (
	id    TEXT PRIMARY KEY,
	state TEXT NOT NULL
) WITHOUT ROWID;
CREATE TABLE lc_event (
	seq        INTEGER PRIMARY KEY,
	work_id    TEXT NOT NULL,
	kind       TEXT NOT NULL,
	turn_token TEXT NOT NULL DEFAULT '',
	source_id  TEXT NOT NULL DEFAULT '',
	body       TEXT NOT NULL
);
CREATE INDEX lc_event_work_kind ON lc_event(work_id, kind, turn_token);
CREATE UNIQUE INDEX lc_event_source ON lc_event(work_id, source_id) WHERE source_id <> '';
CREATE TABLE lc_meta (
	key   TEXT PRIMARY KEY,
	value INTEGER NOT NULL
) WITHOUT ROWID;
INSERT INTO lc_meta(key, value) VALUES('next_seq', 1);
`}

const metaNextSeq = "next_seq"

// loadWorks reads every Work row, restores each Work's source index from the
// log and checks the invariants a reader relies on.
func loadWorks(db *sql.DB) (map[WorkID]*State, uint64, error) {
	works := map[WorkID]*State{}
	rows, err := db.Query(`SELECT id, state FROM lc_work`)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			_ = rows.Close()
			return nil, 0, err
		}
		st := &State{}
		if err := json.Unmarshal([]byte(raw), st); err != nil {
			_ = rows.Close()
			return nil, 0, fmt.Errorf("decode lifecycle Work %q: %w", id, err)
		}
		if st.ID != WorkID(id) {
			_ = rows.Close()
			return nil, 0, fmt.Errorf("lifecycle: Work row %q has mismatched identity", id)
		}
		works[st.ID] = st
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	sources, err := db.Query(`SELECT work_id, source_id FROM lc_event WHERE source_id <> ''`)
	if err != nil {
		return nil, 0, err
	}
	for sources.Next() {
		var id, source string
		if err := sources.Scan(&id, &source); err != nil {
			_ = sources.Close()
			return nil, 0, err
		}
		st := works[WorkID(id)]
		if st == nil {
			_ = sources.Close()
			return nil, 0, fmt.Errorf("lifecycle: Event for unknown Work %q", id)
		}
		if st.SeenSources == nil {
			st.SeenSources = map[string]bool{}
		}
		st.SeenSources[source] = true
	}
	if err := sources.Close(); err != nil {
		return nil, 0, err
	}
	if err := sources.Err(); err != nil {
		return nil, 0, err
	}
	var nextSeq, maxSeq uint64
	if err := db.QueryRow(`SELECT value FROM lc_meta WHERE key = ?`, metaNextSeq).Scan(&nextSeq); err != nil {
		return nil, 0, err
	}
	if err := db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM lc_event`).Scan(&maxSeq); err != nil {
		return nil, 0, err
	}
	if err := validateWorks(works, nextSeq, maxSeq); err != nil {
		return nil, 0, err
	}
	return works, nextSeq, nil
}

func validateWorks(works map[WorkID]*State, nextSeq, maxSeq uint64) error {
	if nextSeq <= maxSeq {
		return fmt.Errorf("lifecycle: next event sequence is stale")
	}
	for id, work := range works {
		if work == nil || work.ID != id {
			return fmt.Errorf("lifecycle: Work row %q has mismatched identity", id)
		}
		if work.Status.Terminal() && work.Wake != nil {
			return fmt.Errorf("lifecycle: terminal Work %q retains a wake", id)
		}
		if work.Attempt != nil && (work.Attempt.SessionID == "" || work.Attempt.TurnToken == "" || work.Attempt.Generation == 0) {
			return fmt.Errorf("lifecycle: Work %q has incomplete active Attempt identity", id)
		}
		prepared := 0
		acceptedSequences := make(map[uint64]bool)
		for token, admission := range work.Admissions {
			if admission == nil || token == "" || token != admission.TurnToken || admission.SessionID == "" || admission.AttemptedAt.IsZero() {
				return fmt.Errorf("lifecycle: Work %q has incomplete Attempt admission", id)
			}
			// Ambiguous outcomes remain evidence across model-directed retries.
			// Only a prepared transaction still owns submission serialization.
			if admission.Status == AdmissionPrepared {
				prepared++
			}
			if admission.Status == AdmissionAccepted {
				if admission.AcceptedSeq == 0 || admission.AcceptedSeq >= nextSeq || acceptedSequences[admission.AcceptedSeq] {
					return fmt.Errorf("lifecycle: Work %q has invalid admission acceptance sequence", id)
				}
				acceptedSequences[admission.AcceptedSeq] = true
			}
		}
		if prepared > 1 {
			return fmt.Errorf("lifecycle: Work %q has multiple prepared admissions", id)
		}
	}
	return nil
}

// commit makes one command durable: its new Events, the Work they reduce
// into and the advanced sequence counter.
func commit(db *sql.DB, next *State, events []Event, nextSeq uint64) error {
	row, err := json.Marshal(next.cloneView())
	if err != nil {
		return err
	}
	return sqlitedb.Tx(db, func(tx *sql.Tx) error {
		for _, event := range events {
			body, err := json.Marshal(event)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(
				`INSERT INTO lc_event(seq, work_id, kind, turn_token, source_id, body) VALUES(?,?,?,?,?,?)`,
				event.Seq, string(event.WorkID), string(event.Kind), string(event.TurnToken), event.SourceID, string(body),
			); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(
			`INSERT INTO lc_work(id, state) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET state = excluded.state`,
			string(next.ID), string(row),
		); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE lc_meta SET value = ? WHERE key = ?`, nextSeq, metaNextSeq)
		return err
	})
}

// eventsOf reads one Work's Events of one kind, oldest first, optionally
// narrowed to one turn token.
func eventsOf(db *sql.DB, id WorkID, kind Kind, token TurnToken) ([]Event, error) {
	query := `SELECT body FROM lc_event WHERE work_id = ? AND kind = ?`
	args := []any{string(id), string(kind)}
	if token != "" {
		query += ` AND turn_token = ?`
		args = append(args, string(token))
	}
	rows, err := db.Query(query+` ORDER BY seq`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var event Event
		if err := json.Unmarshal([]byte(body), &event); err != nil {
			return nil, fmt.Errorf("decode lifecycle Event: %w", err)
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

// Digest summarizes the durable contents of a lifecycle database so a
// migration can prove it moved everything: row counts and a hash over every
// row's canonical JSON, in key order.
type Digest struct {
	Works      int    `json:"works"`
	Events     int    `json:"events"`
	NextSeq    uint64 `json:"next_seq"`
	WorksHash  string `json:"works_sha256"`
	EventsHash string `json:"events_sha256"`
}

// ReadDigest computes the Digest of a lifecycle database from its rows.
func ReadDigest(db *sql.DB) (Digest, error) {
	works, nextSeq, err := loadWorks(db)
	if err != nil {
		return Digest{}, err
	}
	rows, err := db.Query(`SELECT body FROM lc_event ORDER BY seq`)
	if err != nil {
		return Digest{}, err
	}
	defer rows.Close()
	events := newEventDigest()
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return Digest{}, err
		}
		var event Event
		if err := json.Unmarshal([]byte(body), &event); err != nil {
			return Digest{}, err
		}
		if err := events.add(event); err != nil {
			return Digest{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return Digest{}, err
	}
	return digestOf(works, nextSeq, events)
}

var errDuplicateSeq = errors.New("lifecycle: event sequence is not strictly increasing")

func sortedWorkIDs(works map[WorkID]*State) []WorkID {
	ids := make([]WorkID, 0, len(works))
	for id := range works {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
