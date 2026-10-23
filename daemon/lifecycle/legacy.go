package lifecycle

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"

	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// legacyStoreSchema is the schema of the whole-file JSON image that
// state/lifecycle/state.json held before the SQLite store. It is read once,
// by ImportLegacy, and never written again.
const legacyStoreSchema = 2

type legacyDatabase struct {
	Schema  int               `json:"schema"`
	NextSeq uint64            `json:"next_seq"`
	Works   map[WorkID]*State `json:"works"`
	Events  []Event           `json:"events"`
}

// ReadLegacy decodes and validates a pre-SQLite lifecycle image. found is
// false when path does not exist or is not a schema 2 image (a tombstone left
// by an earlier import has a different schema).
func ReadLegacy(path string) (database legacyDatabase, found bool, err error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return legacyDatabase{}, false, nil
	}
	if err != nil {
		return legacyDatabase{}, false, err
	}
	var header struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return legacyDatabase{}, false, fmt.Errorf("decode lifecycle store: %w", err)
	}
	if header.Schema != legacyStoreSchema {
		return legacyDatabase{}, false, nil
	}
	if err := json.Unmarshal(raw, &database); err != nil {
		return legacyDatabase{}, false, fmt.Errorf("decode lifecycle store: %w", err)
	}
	if database.Works == nil || database.Events == nil {
		return legacyDatabase{}, false, fmt.Errorf("lifecycle: works and events are required")
	}
	var maxSeq uint64
	for _, event := range database.Events {
		if event.Seq <= maxSeq {
			return legacyDatabase{}, false, errDuplicateSeq
		}
		maxSeq = event.Seq
	}
	if err := validateWorks(database.Works, database.NextSeq, maxSeq); err != nil {
		return legacyDatabase{}, false, err
	}
	return database, true, nil
}

// LegacyDigest is the Digest the legacy image at path must produce once it is
// in the SQLite store.
func LegacyDigest(database legacyDatabase) (Digest, error) {
	events := newEventDigest()
	for _, event := range database.Events {
		if err := events.add(event); err != nil {
			return Digest{}, err
		}
	}
	return digestOf(database.Works, database.NextSeq, events)
}

// ImportLegacy writes a legacy image into the lifecycle tables of an empty
// database in one transaction. The sequence numbers, Works and Events are
// preserved exactly: admissions refer to Event sequences.
func ImportLegacy(db *sql.DB, database legacyDatabase) error {
	if err := sqlitedb.Migrate(db, schemaComponent, schemaSteps); err != nil {
		return err
	}
	return sqlitedb.Tx(db, func(tx *sql.Tx) error {
		var existing int
		if err := tx.QueryRow(`SELECT (SELECT COUNT(*) FROM lc_work) + (SELECT COUNT(*) FROM lc_event)`).Scan(&existing); err != nil {
			return err
		}
		if existing != 0 {
			return fmt.Errorf("lifecycle: import target is not empty")
		}
		for _, id := range sortedWorkIDs(database.Works) {
			row, err := json.Marshal(database.Works[id].cloneView())
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO lc_work(id, state) VALUES(?, ?)`, string(id), string(row)); err != nil {
				return err
			}
		}
		for _, event := range database.Events {
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
		_, err := tx.Exec(`UPDATE lc_meta SET value = ? WHERE key = ?`, database.NextSeq, metaNextSeq)
		return err
	})
}

type eventDigest struct {
	h    hash.Hash
	n    int
	last uint64
}

func newEventDigest() *eventDigest { return &eventDigest{h: sha256.New()} }

func (d *eventDigest) add(event Event) error {
	if event.Seq <= d.last {
		return errDuplicateSeq
	}
	d.last = event.Seq
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	d.h.Write(body)
	d.h.Write([]byte{'\n'})
	d.n++
	return nil
}

func digestOf(works map[WorkID]*State, nextSeq uint64, events *eventDigest) (Digest, error) {
	h := sha256.New()
	for _, id := range sortedWorkIDs(works) {
		body, err := json.Marshal(works[id])
		if err != nil {
			return Digest{}, err
		}
		h.Write([]byte(id))
		h.Write([]byte{'\n'})
		h.Write(body)
		h.Write([]byte{'\n'})
	}
	return Digest{
		Works: len(works), Events: events.n, NextSeq: nextSeq,
		WorksHash:  hex.EncodeToString(h.Sum(nil)),
		EventsHash: hex.EncodeToString(events.h.Sum(nil)),
	}, nil
}
