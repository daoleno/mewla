package brain

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"

	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// Brain's durable state beyond the lifecycle engine lives in one SQLite
// database, state/brain.db, next to the lifecycle tables:
//
//	pres_work       one row per Work projection (presentation of lifecycle state)
//	pres_event      one row per Work Event, indexed by work_id
//	pres_turn       one row per provider Turn, keyed (session_id, turn_id)
//	pres_admission  one row per Brain input admission, keyed (request_id, thread_id)
//	pres_meta       the host foreground Turn, next_event_sequence and the
//	                commit generation
//	timeline        the Brain chat and Work-card ledger, ordered by seq
//	timeline_archive  chat rows that earlier repairs removed from the live
//	                timeline, kept only so memory search can still find them
//	migration       one row, present only when the database was built from
//	                the pre-SQLite files: where they went and what was checked
//	                (created by the migration, see legacy_import.go)
//
// Every row is the JSON of the same struct the code already used, so no
// reader or writer changed shape. A commit rewrites only the rows that
// differ from the last committed image, in one transaction; nothing is ever
// rewritten as a whole file. The committed image stays resident, so a read is
// a memory copy, not a decode.
const brainSchemaComponent = "brain"

var brainSchemaSteps = []string{`
CREATE TABLE pres_work (
	id   TEXT PRIMARY KEY,
	body TEXT NOT NULL
) WITHOUT ROWID;
CREATE TABLE pres_event (
	id      TEXT PRIMARY KEY,
	work_id TEXT NOT NULL,
	body    TEXT NOT NULL
) WITHOUT ROWID;
CREATE INDEX pres_event_work ON pres_event(work_id);
CREATE TABLE pres_turn (
	session_id TEXT NOT NULL,
	turn_id    TEXT NOT NULL,
	body       TEXT NOT NULL,
	PRIMARY KEY (session_id, turn_id)
) WITHOUT ROWID;
CREATE TABLE pres_admission (
	request_id TEXT NOT NULL,
	thread_id  TEXT NOT NULL,
	body       TEXT NOT NULL,
	PRIMARY KEY (request_id, thread_id)
) WITHOUT ROWID;
CREATE TABLE pres_meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
) WITHOUT ROWID;
INSERT INTO pres_meta(key, value) VALUES ('generation', '0'), ('next_event_sequence', '0'), ('host_foreground_turn', '');
CREATE TABLE timeline (
	seq       INTEGER PRIMARY KEY AUTOINCREMENT,
	id        TEXT NOT NULL UNIQUE,
	thread_id TEXT NOT NULL,
	body      TEXT NOT NULL
);
CREATE INDEX timeline_thread ON timeline(thread_id, seq);
CREATE TABLE timeline_archive (
	seq    INTEGER PRIMARY KEY AUTOINCREMENT,
	origin TEXT NOT NULL,
	id     TEXT NOT NULL,
	body   TEXT NOT NULL,
	UNIQUE (origin, id)
);
`}

const (
	metaGeneration   = "generation"
	metaNextEventSeq = "next_event_sequence"
	metaForegound    = "host_foreground_turn"
)

// errConcurrentWriter reports that another process committed to the database
// since this Store last read it. The daemon is the sole owner of a Brain
// state directory, so this means a second process opened the same one; the
// Store refuses to overwrite what it never saw.
var errConcurrentWriter = errors.New("brain: database was changed by another process")

// openBrainDatabase opens state/brain.db and brings the Brain tables to the
// current schema. The lifecycle engine adds its own tables to the same file.
func openBrainDatabase(path string) (*sql.DB, error) {
	db, err := sqlitedb.Open(path)
	if err != nil {
		return nil, err
	}
	if err := sqlitedb.Migrate(db, brainSchemaComponent, brainSchemaSteps); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// readPresentationImage loads the committed presentation image and its
// generation. The image is validated exactly as every commit validates it.
func readPresentationImage(db *sql.DB) (presentationDatabase, int64, error) {
	database := presentationDatabase{SchemaVersion: presentationSchemaVersion}
	records := []workRecord{}
	if err := scanBodies(db, `SELECT body FROM pres_work`, func(body []byte) error {
		var record workRecord
		if err := json.Unmarshal(body, &record); err != nil {
			return err
		}
		records = append(records, record)
		return nil
	}); err != nil {
		return presentationDatabase{}, 0, fmt.Errorf("read Brain Work: %w", err)
	}
	database.BrainWork = worksFromRecords(records)
	database.BrainWorkEvents = []WorkEvent{}
	if err := scanBodies(db, `SELECT body FROM pres_event`, func(body []byte) error {
		var event WorkEvent
		if err := json.Unmarshal(body, &event); err != nil {
			return err
		}
		database.BrainWorkEvents = append(database.BrainWorkEvents, event)
		return nil
	}); err != nil {
		return presentationDatabase{}, 0, fmt.Errorf("read Brain Work events: %w", err)
	}
	database.BrainTurns = []TurnRecord{}
	if err := scanBodies(db, `SELECT body FROM pres_turn`, func(body []byte) error {
		var turn TurnRecord
		if err := json.Unmarshal(body, &turn); err != nil {
			return err
		}
		database.BrainTurns = append(database.BrainTurns, turn)
		return nil
	}); err != nil {
		return presentationDatabase{}, 0, fmt.Errorf("read Brain turns: %w", err)
	}
	database.BrainInputAdmissions = []BrainInputAdmission{}
	if err := scanBodies(db, `SELECT body FROM pres_admission`, func(body []byte) error {
		var admission BrainInputAdmission
		if err := json.Unmarshal(body, &admission); err != nil {
			return err
		}
		database.BrainInputAdmissions = append(database.BrainInputAdmissions, admission)
		return nil
	}); err != nil {
		return presentationDatabase{}, 0, fmt.Errorf("read Brain input admissions: %w", err)
	}
	meta := map[string]string{}
	if err := scanPairs(db, `SELECT key, value FROM pres_meta`, func(key, value string) { meta[key] = value }); err != nil {
		return presentationDatabase{}, 0, err
	}
	next, err := strconv.ParseUint(meta[metaNextEventSeq], 10, 64)
	if err != nil {
		return presentationDatabase{}, 0, fmt.Errorf("read next_event_sequence: %w", err)
	}
	database.NextEventSequence = next
	if raw := meta[metaForegound]; raw != "" {
		var foreground HostForegroundTurn
		if err := json.Unmarshal([]byte(raw), &foreground); err != nil {
			return presentationDatabase{}, 0, fmt.Errorf("read host foreground turn: %w", err)
		}
		database.HostForegroundTurn = &foreground
	}
	generation, err := strconv.ParseInt(meta[metaGeneration], 10, 64)
	if err != nil {
		return presentationDatabase{}, 0, fmt.Errorf("read generation: %w", err)
	}
	sortPresentation(&database)
	if err := validatePresentationDatabase(database); err != nil {
		return presentationDatabase{}, 0, fmt.Errorf("validate Brain presentation database: %w", err)
	}
	return database, generation, nil
}

func scanBodies(db *sql.DB, query string, each func(body []byte) error) error {
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return err
		}
		if err := each([]byte(body)); err != nil {
			return err
		}
	}
	return rows.Err()
}

func scanPairs(db *sql.DB, query string, each func(key, value string)) error {
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		each(key, value)
	}
	return rows.Err()
}

// sortPresentation puts each collection in the order readers rely on.
func sortPresentation(database *presentationDatabase) {
	sort.Slice(database.BrainWork, func(left, right int) bool {
		if database.BrainWork[left].CreatedAt.Equal(database.BrainWork[right].CreatedAt) {
			return database.BrainWork[left].ID < database.BrainWork[right].ID
		}
		return database.BrainWork[left].CreatedAt.Before(database.BrainWork[right].CreatedAt)
	})
	sort.Slice(database.BrainInputAdmissions, func(left, right int) bool {
		if database.BrainInputAdmissions[left].CreatedAt.Equal(database.BrainInputAdmissions[right].CreatedAt) {
			if database.BrainInputAdmissions[left].RequestID == database.BrainInputAdmissions[right].RequestID {
				return database.BrainInputAdmissions[left].ThreadID < database.BrainInputAdmissions[right].ThreadID
			}
			return database.BrainInputAdmissions[left].RequestID < database.BrainInputAdmissions[right].RequestID
		}
		return database.BrainInputAdmissions[left].CreatedAt.Before(database.BrainInputAdmissions[right].CreatedAt)
	})
	sort.Slice(database.BrainWorkEvents, func(left, right int) bool {
		if database.BrainWorkEvents[left].CreatedAt.Equal(database.BrainWorkEvents[right].CreatedAt) {
			return database.BrainWorkEvents[left].ID < database.BrainWorkEvents[right].ID
		}
		return database.BrainWorkEvents[left].CreatedAt.Before(database.BrainWorkEvents[right].CreatedAt)
	})
	sort.Slice(database.BrainTurns, func(left, right int) bool {
		if database.BrainTurns[left].SessionID == database.BrainTurns[right].SessionID {
			return database.BrainTurns[left].TurnID < database.BrainTurns[right].TurnID
		}
		return database.BrainTurns[left].SessionID < database.BrainTurns[right].SessionID
	})
}

// commitPresentation writes the difference between two images in one
// transaction and advances the generation. It fails, writing nothing, when the
// generation on disk is not the one prev was read at. It returns the committed
// image as the next start will read it: every row that was written is the
// decode of what was stored, so the resident image never differs from the
// durable one in representation (nil against empty, omitted fields).
func commitPresentation(db *sql.DB, generation int64, prev, next presentationDatabase) (presentationDatabase, error) {
	committed := clonePresentationDatabase(next)
	err := sqlitedb.Tx(db, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE pres_meta SET value = ? WHERE key = ? AND value = ?`,
			strconv.FormatInt(generation+1, 10), metaGeneration, strconv.FormatInt(generation, 10),
		)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return errConcurrentWriter
		}
		if err := diffTable(tx, prev.BrainWork, committed.BrainWork,
			func(item Work) string { return item.ID },
			func(body []byte) (Work, error) {
				var record workRecord
				if err := json.Unmarshal(body, &record); err != nil {
					return Work{}, err
				}
				return worksFromRecords([]workRecord{record})[0], nil
			},
			`INSERT INTO pres_work(id, body) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET body = excluded.body`,
			func(item Work, body string) []any { return []any{item.ID, body} },
			`DELETE FROM pres_work WHERE id = ?`,
			func(key string) []any { return []any{key} },
		); err != nil {
			return fmt.Errorf("write Brain Work: %w", err)
		}
		if err := diffTable(tx, prev.BrainWorkEvents, committed.BrainWorkEvents,
			func(event WorkEvent) string { return event.ID },
			decodeRow[WorkEvent],
			`INSERT INTO pres_event(id, work_id, body) VALUES(?, ?, ?) ON CONFLICT(id) DO UPDATE SET work_id = excluded.work_id, body = excluded.body`,
			func(event WorkEvent, body string) []any { return []any{event.ID, event.WorkID, body} },
			`DELETE FROM pres_event WHERE id = ?`,
			func(key string) []any { return []any{key} },
		); err != nil {
			return fmt.Errorf("write Brain Work events: %w", err)
		}
		if err := diffTable(tx, prev.BrainTurns, committed.BrainTurns,
			func(turn TurnRecord) string { return turnRecordKey(turn.SessionID, turn.TurnID) },
			decodeRow[TurnRecord],
			`INSERT INTO pres_turn(session_id, turn_id, body) VALUES(?, ?, ?) ON CONFLICT(session_id, turn_id) DO UPDATE SET body = excluded.body`,
			func(turn TurnRecord, body string) []any { return []any{turn.SessionID, turn.TurnID, body} },
			`DELETE FROM pres_turn WHERE session_id = ? AND turn_id = ?`,
			func(key string) []any { session, turn := splitTurnRecordKey(key); return []any{session, turn} },
		); err != nil {
			return fmt.Errorf("write Brain turns: %w", err)
		}
		if err := diffTable(tx, prev.BrainInputAdmissions, committed.BrainInputAdmissions,
			func(admission BrainInputAdmission) string { return admission.RequestID + "\x00" + admission.ThreadID },
			decodeRow[BrainInputAdmission],
			`INSERT INTO pres_admission(request_id, thread_id, body) VALUES(?, ?, ?) ON CONFLICT(request_id, thread_id) DO UPDATE SET body = excluded.body`,
			func(admission BrainInputAdmission, body string) []any {
				return []any{admission.RequestID, admission.ThreadID, body}
			},
			`DELETE FROM pres_admission WHERE request_id = ? AND thread_id = ?`,
			func(key string) []any { request, thread := splitTurnRecordKey(key); return []any{request, thread} },
		); err != nil {
			return fmt.Errorf("write Brain input admissions: %w", err)
		}
		if prev.NextEventSequence != next.NextEventSequence {
			if _, err := tx.Exec(`UPDATE pres_meta SET value = ? WHERE key = ?`,
				strconv.FormatUint(next.NextEventSequence, 10), metaNextEventSeq); err != nil {
				return err
			}
		}
		if !reflect.DeepEqual(prev.HostForegroundTurn, next.HostForegroundTurn) {
			value := ""
			if next.HostForegroundTurn != nil {
				raw, err := json.Marshal(next.HostForegroundTurn)
				if err != nil {
					return err
				}
				value = string(raw)
				var stored HostForegroundTurn
				if err := json.Unmarshal(raw, &stored); err != nil {
					return err
				}
				committed.HostForegroundTurn = &stored
			}
			if _, err := tx.Exec(`UPDATE pres_meta SET value = ? WHERE key = ?`, value, metaForegound); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return presentationDatabase{}, err
	}
	return committed, nil
}

func decodeRow[T any](body []byte) (T, error) {
	var row T
	err := json.Unmarshal(body, &row)
	return row, err
}

// splitTurnRecordKey undoes a "<first>\x00<second>" key.
func splitTurnRecordKey(key string) (string, string) {
	for index := 0; index < len(key); index++ {
		if key[index] == 0 {
			return key[:index], key[index+1:]
		}
	}
	return key, ""
}

// diffTable upserts the rows of next that are new or changed against prev and
// deletes the rows prev has that next lacks. Rows are compared as values, so
// an untouched row costs no write. Each written row of next is replaced by the
// decode of the JSON that was stored.
func diffTable[T any](
	tx *sql.Tx, prev, next []T,
	key func(T) string,
	decode func([]byte) (T, error),
	upsert string, upsertArgs func(T, string) []any,
	remove string, removeArgs func(string) []any,
) error {
	old := make(map[string]*T, len(prev))
	for index := range prev {
		old[key(prev[index])] = &prev[index]
	}
	for index := range next {
		row := next[index]
		k := key(row)
		if before, ok := old[k]; ok {
			delete(old, k)
			if reflect.DeepEqual(*before, row) {
				continue
			}
		}
		body, err := json.Marshal(row)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(upsert, upsertArgs(row, string(body))...); err != nil {
			return err
		}
		stored, err := decode(body)
		if err != nil {
			return err
		}
		next[index] = stored
	}
	for k := range old {
		if _, err := tx.Exec(remove, removeArgs(k)...); err != nil {
			return err
		}
	}
	return nil
}
