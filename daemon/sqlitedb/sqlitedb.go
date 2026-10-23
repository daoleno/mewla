// Package sqlitedb opens the SQLite files Mewla keeps durable state in and
// gives every store the same transaction and schema-version rules.
//
// A file is a write-ahead-logged database owned by one daemon. Every commit
// is fsynced (synchronous=FULL), so a crash leaves the last committed
// transaction and nothing torn, and writers queue on a busy timeout instead of
// failing. Schema changes are numbered steps per component, applied in one
// transaction each.
package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open opens (creating, mode 0600) the database at path.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() +
		"?_txlock=immediate&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Tx runs fn in one write transaction and commits it, or rolls back when fn
// fails. Transactions begin IMMEDIATE, so a second writer waits instead of
// failing on lock upgrade.
func Tx(db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return errors.Join(err, rollbackErr)
		}
		return err
	}
	return tx.Commit()
}

// Migrate brings component's schema to len(steps). Step i (0-based) moves the
// component from version i to i+1, and the new version commits with the step.
// A database written by a newer binary is refused rather than guessed at.
func Migrate(db *sql.DB, component string, steps []string) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		component TEXT PRIMARY KEY,
		version   INTEGER NOT NULL
	) WITHOUT ROWID`); err != nil {
		return err
	}
	var version int
	err := db.QueryRow(`SELECT version FROM schema_version WHERE component = ?`, component).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if version > len(steps) {
		return fmt.Errorf("%s schema version %d is newer than this build (%d)", component, version, len(steps))
	}
	for next := version; next < len(steps); next++ {
		step := steps[next]
		if err := Tx(db, func(tx *sql.Tx) error {
			if _, err := tx.Exec(step); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO schema_version(component, version) VALUES(?, ?)
				ON CONFLICT(component) DO UPDATE SET version = excluded.version`, component, next+1)
			return err
		}); err != nil {
			return fmt.Errorf("migrate %s to schema %d: %w", component, next+1, err)
		}
	}
	return nil
}

// Seal checkpoints the write-ahead log into the main file and switches the
// database to a single-file journal, so the file can be renamed or copied
// alone. The database must be closed after Seal.
func Seal(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return err
	}
	_, err := db.Exec(`PRAGMA journal_mode = DELETE`)
	return err
}
