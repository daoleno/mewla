package gc

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/daoleno/mewla/daemon/logonce"
	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// The ledger (gc/ledger.db) keeps what the collector knows that has no other
// home: what it did (events), which Session a resource unit was and why a
// Session is held, and a daily size of each kind of thing it collects. The
// trash, keep and lease records stay in their own files; the ledger names
// them. A ledger that cannot be written never stops a collection: the
// failure is reported once.

// Event actions.
const (
	ActionTrashed      = "trashed"
	ActionRestored     = "restored"
	ActionDeleted      = "deleted"
	ActionKept         = "kept"
	ActionKeepEnded    = "keep_ended"
	ActionReleased     = "released"
	ActionKeptMoved    = "kept_moved"
	ActionEvicted      = "evicted"
	ActionSessionEnded = "session_ended"
	ActionNoteArchived = "note_archived"
)

// Event is one thing the collector did.
type Event struct {
	Seq     int64     `json:"seq"`
	At      time.Time `json:"at"`
	Action  string    `json:"action"`
	Kind    string    `json:"kind,omitempty"`
	Subject string    `json:"subject"`
	Owner   string    `json:"owner,omitempty"` // the resource unit it was
	Bytes   int64     `json:"bytes,omitempty"`
	Detail  string    `json:"detail,omitempty"`
}

// LedgerSession is what the ledger knows of a Session by its resource unit.
type LedgerSession struct {
	Unit      string    `json:"unit"`
	SessionID string    `json:"session_id"`
	BoundAt   time.Time `json:"bound_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	// HeldBecause is why Mewla keeps a Session it would otherwise end
	// (its Work closed, but it is still busy).
	HeldBecause string    `json:"held_because,omitempty"`
	HeldSince   time.Time `json:"held_since,omitempty"`
}

// SizeSample is one kind's count and bytes on a day.
type SizeSample struct {
	Day   string `json:"day"` // YYYY-MM-DD, local
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

const ledgerComponent = "gc"

var ledgerSteps = []string{
	`CREATE TABLE event (
		seq     INTEGER PRIMARY KEY AUTOINCREMENT,
		at      TEXT NOT NULL,
		action  TEXT NOT NULL,
		kind    TEXT NOT NULL DEFAULT '',
		subject TEXT NOT NULL,
		owner   TEXT NOT NULL DEFAULT '',
		bytes   INTEGER NOT NULL DEFAULT 0,
		detail  TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX event_at ON event(at);
	CREATE TABLE session (
		unit         TEXT PRIMARY KEY,
		session_id   TEXT NOT NULL,
		bound_at     TEXT NOT NULL,
		ended_at     TEXT NOT NULL DEFAULT '',
		held_because TEXT NOT NULL DEFAULT '',
		held_since   TEXT NOT NULL DEFAULT ''
	) WITHOUT ROWID;
	CREATE INDEX session_id ON session(session_id);
	CREATE TABLE size (
		day   TEXT NOT NULL,
		kind  TEXT NOT NULL,
		count INTEGER NOT NULL,
		bytes INTEGER NOT NULL,
		PRIMARY KEY (day, kind)
	) WITHOUT ROWID;`,
}

var ledgerLog logonce.Log

// LedgerPath is the ledger under a state root.
func LedgerPath(stateRoot string) string { return filepath.Join(stateRoot, "gc", "ledger.db") }

// withLedger runs fn on the state root's ledger, opened for the call.
func withLedger(stateRoot string, fn func(*sql.DB) error) error {
	db, err := sqlitedb.Open(LedgerPath(stateRoot))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := sqlitedb.Migrate(db, ledgerComponent, ledgerSteps); err != nil {
		return err
	}
	return fn(db)
}

// readLedger is withLedger for readers: no ledger yet reads as empty.
func readLedger(stateRoot string, fn func(*sql.DB) error) error {
	if _, err := os.Stat(LedgerPath(stateRoot)); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return withLedger(stateRoot, fn)
}

func noteLedger(err error) {
	ledgerLog.Note("gc ledger", err)
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func unstamp(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// Record adds e to the ledger. A failure is reported, never returned: what
// the collector did stands whether or not it was written down.
func Record(stateRoot string, e Event) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	noteLedger(withLedger(stateRoot, func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO event(at, action, kind, subject, owner, bytes, detail) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			stamp(e.At), e.Action, e.Kind, e.Subject, e.Owner, e.Bytes, e.Detail)
		return err
	}))
}

// Events lists what the collector did since since, oldest first.
func Events(stateRoot string, since time.Time) ([]Event, error) {
	var events []Event
	err := readLedger(stateRoot, func(db *sql.DB) error {
		rows, err := db.Query(`SELECT seq, at, action, kind, subject, owner, bytes, detail FROM event WHERE at >= ? ORDER BY seq`, stamp(since))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Event
			var at string
			if err := rows.Scan(&e.Seq, &at, &e.Action, &e.Kind, &e.Subject, &e.Owner, &e.Bytes, &e.Detail); err != nil {
				return err
			}
			e.At = unstamp(at)
			events = append(events, e)
		}
		return rows.Err()
	})
	return events, err
}

// BindSession records that unit is sessionID's resource unit.
func BindSession(stateRoot, unit, sessionID string, at time.Time) {
	noteLedger(withLedger(stateRoot, func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO session(unit, session_id, bound_at) VALUES(?, ?, ?)
			ON CONFLICT(unit) DO UPDATE SET session_id = excluded.session_id, ended_at = '', held_because = '', held_since = ''
			WHERE session.session_id != excluded.session_id OR session.ended_at != ''`,
			unit, sessionID, stamp(at))
		return err
	}))
}

// EndSession records that unit's Session ended at at.
func EndSession(stateRoot, unit string, at time.Time) {
	noteLedger(withLedger(stateRoot, func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE session SET ended_at = ?, held_because = '', held_since = '' WHERE unit = ? AND ended_at = ''`, stamp(at), unit)
		return err
	}))
}

// HoldSession records why Mewla keeps sessionID although it would end it;
// an empty because clears it.
func HoldSession(stateRoot, sessionID, because string, since time.Time) {
	if because == "" {
		since = time.Time{}
	}
	noteLedger(withLedger(stateRoot, func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE session SET held_because = ?, held_since = ? WHERE session_id = ? AND ended_at = '' AND held_because != ?`,
			because, stamp(since), sessionID, because)
		return err
	}))
}

// SessionOfUnit is what the ledger knows of unit's Session.
func SessionOfUnit(stateRoot, unit string) (LedgerSession, bool, error) {
	var s LedgerSession
	found := false
	err := readLedger(stateRoot, func(db *sql.DB) error {
		var bound, ended, since string
		err := db.QueryRow(`SELECT unit, session_id, bound_at, ended_at, held_because, held_since FROM session WHERE unit = ?`, unit).
			Scan(&s.Unit, &s.SessionID, &bound, &ended, &s.HeldBecause, &since)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		s.BoundAt, s.EndedAt, s.HeldSince, found = unstamp(bound), unstamp(ended), unstamp(since), true
		return nil
	})
	return s, found, err
}

// HeldSessions lists the live Sessions Mewla keeps although it would end
// them, longest held first.
func HeldSessions(stateRoot string) ([]LedgerSession, error) {
	var held []LedgerSession
	err := readLedger(stateRoot, func(db *sql.DB) error {
		rows, err := db.Query(`SELECT unit, session_id, bound_at, held_because, held_since FROM session
			WHERE ended_at = '' AND held_because != '' ORDER BY held_since`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s LedgerSession
			var bound, since string
			if err := rows.Scan(&s.Unit, &s.SessionID, &bound, &s.HeldBecause, &since); err != nil {
				return err
			}
			s.BoundAt, s.HeldSince = unstamp(bound), unstamp(since)
			held = append(held, s)
		}
		return rows.Err()
	})
	return held, err
}

// RecordSizes stores the day's size of each kind, once per day: the first
// call of a day measures, later ones return at once. It reports whether it
// measured.
func RecordSizes(stateRoot string, now time.Time) (bool, error) {
	day := now.Local().Format("2006-01-02")
	measured := false
	err := withLedger(stateRoot, func(db *sql.DB) error {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM size WHERE day = ?`, day).Scan(&n); err != nil || n > 0 {
			return err
		}
		samples := measureKinds(stateRoot, day)
		return sqlitedb.Tx(db, func(tx *sql.Tx) error {
			for _, s := range samples {
				if _, err := tx.Exec(`INSERT INTO size(day, kind, count, bytes) VALUES(?, ?, ?, ?) ON CONFLICT(day, kind) DO NOTHING`,
					s.Day, s.Kind, s.Count, s.Bytes); err != nil {
					return err
				}
			}
			measured = true
			return nil
		})
	})
	return measured, err
}

// Sizes lists the samples of the days since since (a YYYY-MM-DD), oldest
// first.
func Sizes(stateRoot, since string) ([]SizeSample, error) {
	var samples []SizeSample
	err := readLedger(stateRoot, func(db *sql.DB) error {
		rows, err := db.Query(`SELECT day, kind, count, bytes FROM size WHERE day >= ? ORDER BY day, kind`, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s SizeSample
			if err := rows.Scan(&s.Day, &s.Kind, &s.Count, &s.Bytes); err != nil {
				return err
			}
			samples = append(samples, s)
		}
		return rows.Err()
	})
	return samples, err
}

// Size kinds: what RecordSizes measures.
const (
	SizeScratch  = "scratch"  // marked t/ directories: live Sessions' TMPDIRs
	SizeUnmarked = "unmarked" // everything else in t/
	SizeTrash    = "trash"
	SizeKept     = "kept"
	SizeCache    = "cache"
)

func measureKinds(stateRoot, day string) []SizeSample {
	sample := func(kind string, paths []string) SizeSample {
		s := SizeSample{Day: day, Kind: kind, Count: len(paths)}
		for _, p := range paths {
			s.Bytes += Size(p)
		}
		return s
	}
	children := func(dir string) []string {
		entries, _ := os.ReadDir(dir)
		var paths []string
		for _, e := range entries {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
		return paths
	}
	var scratch, unmarked []string
	for _, p := range children(filepath.Join(stateRoot, "t")) {
		if _, ok := markerUnit(p); ok {
			scratch = append(scratch, p)
		} else {
			unmarked = append(unmarked, p)
		}
	}
	var trash []string
	for _, dayDir := range children(TrashRoot(stateRoot)) {
		for _, p := range children(dayDir) {
			if filepath.Ext(p) != ".json" {
				trash = append(trash, p)
			}
		}
	}
	return []SizeSample{
		sample(SizeScratch, scratch),
		sample(SizeUnmarked, unmarked),
		sample(SizeTrash, trash),
		sample(SizeKept, children(filepath.Join(stateRoot, "gc", "kept"))),
		sample(SizeCache, children(CacheDir(stateRoot))),
	}
}
