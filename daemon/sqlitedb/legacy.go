package sqlitedb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// A Migration moves one state directory from whole-file JSON to a database,
// once. It is crash-safe and rerunnable:
//
//  1. Take an exclusive lock on the directory.
//  2. Build the new database under a temporary name, apart from the live one.
//  3. Let the owner verify it against the decoded old files (Build does this).
//  4. Rename it into place. This is the commit point.
//  5. Move the old files into one dated backup directory and leave a
//     tombstone, so an older binary started later refuses to run instead of
//     opening an empty store.
//
// A crash before 4 leaves the old files untouched and the next start rebuilds
// from scratch. A crash after 4 leaves a database that carries a migration
// record and old files still in place; the next start only finishes step 5.
// Nothing is ever deleted, and the old files are never read again.
type Migration struct {
	// Dir holds the database and the old files; Database is the file name.
	Dir      string
	Database string
	// Backup returns the directory the old files are moved into.
	Backup func(at time.Time) string
	// Primaries lists the old files (relative to Dir) that still carry live
	// state. Empty means there is nothing to migrate.
	Primaries func() ([]string, error)
	// Artifacts lists every old file to set aside, relative to Dir: the live
	// files, repair copies, backups and half-written temp files.
	Artifacts func() ([]string, error)
	// Open opens the database at path and brings its schema current.
	Open func(path string) (*sql.DB, error)
	// Build fills a database opened with Open's schema from the old files and
	// verifies it. It records what it checked in record.Details.
	Build func(db *sql.DB, record *Record) error
	// Tombstone returns the file written after the old files are set aside, if
	// any: its path relative to Dir and its content.
	Tombstone func() (rel string, content []byte)
}

// Record is what a migrated database remembers about the move.
type Record struct {
	At        time.Time       `json:"at"`
	BackupDir string          `json:"backup_dir"`
	Files     []File          `json:"files"`
	Notes     []string        `json:"notes,omitempty"`
	Details   json.RawMessage `json:"details,omitempty"`
}

// File is one old file set aside, relative to the migrated directory.
type File struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

const (
	// LockName is the lock file a migration holds in its directory.
	LockName = ".migrate.lock"
	// MigratingSuffix names the database while it is being built.
	MigratingSuffix = ".migrating"
)

var sidecars = []string{"", "-wal", "-shm", "-journal"}

// Run opens the database, migrating the old files first when they are
// present.
func (m Migration) Run() (*sql.DB, error) {
	path := filepath.Join(m.Dir, m.Database)
	primaries, err := m.Primaries()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil && len(primaries) == 0 {
		return m.Open(path)
	}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return nil, err
	}
	unlock, err := lockFile(filepath.Join(m.Dir, LockName))
	if err != nil {
		return nil, err
	}
	defer unlock()
	// Another process may have finished while this one waited for the lock.
	if primaries, err = m.Primaries(); err != nil {
		return nil, err
	}
	_, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		db, err := m.Open(path)
		if err != nil {
			return nil, err
		}
		if len(primaries) == 0 {
			return db, nil
		}
		record, found, err := ReadRecord(db)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		if !found {
			_ = db.Close()
			return nil, fmt.Errorf("%s and %s both exist and the database has no migration record; "+
				"refusing to guess which holds the state", m.Database, strings.Join(primaries, ", "))
		}
		// The database was committed but the old files were not yet set aside.
		if err := m.setAside(record.BackupDir); err != nil {
			_ = db.Close()
			return nil, err
		}
		return db, nil
	case !errors.Is(statErr, os.ErrNotExist):
		return nil, statErr
	case len(primaries) == 0:
		return m.Open(path)
	}
	return m.migrate(path)
}

func (m Migration) migrate(path string) (*sql.DB, error) {
	building := path + MigratingSuffix
	removeDatabase(building)
	now := time.Now().UTC()
	record := Record{At: now, BackupDir: m.Backup(now)}
	if err := m.build(building, &record); err != nil {
		removeDatabase(building)
		return nil, fmt.Errorf("migrate %s to %s: %w", m.Dir, m.Database, err)
	}
	if err := os.Rename(building, path); err != nil {
		return nil, err
	}
	if err := atomicfile.SyncDir(m.Dir); err != nil {
		return nil, err
	}
	if err := m.setAside(record.BackupDir); err != nil {
		return nil, err
	}
	return m.Open(path)
}

func removeDatabase(path string) {
	for _, suffix := range sidecars {
		_ = os.Remove(path + suffix)
	}
}

func (m Migration) build(building string, record *Record) error {
	db, err := Open(building)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	closed := false
	defer func() {
		if !closed {
			_ = db.Close()
		}
	}()
	if err := m.Build(db, record); err != nil {
		return err
	}
	files, err := m.Artifacts()
	if err != nil {
		return err
	}
	for _, rel := range files {
		info, err := os.Stat(filepath.Join(m.Dir, rel))
		if err != nil {
			return err
		}
		record.Files = append(record.Files, File{Path: rel, Bytes: info.Size()})
	}
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS migration (
		id INTEGER PRIMARY KEY CHECK (id = 1), body TEXT NOT NULL)`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO migration(id, body) VALUES(1, ?)`, string(body)); err != nil {
		return err
	}
	if err := Seal(db); err != nil {
		return err
	}
	closed = true
	return db.Close()
}

// ReadRecord returns the migration record of db, if it was built by one.
func ReadRecord(db *sql.DB) (Record, bool, error) {
	var tables int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'migration'`,
	).Scan(&tables); err != nil || tables == 0 {
		return Record{}, false, err
	}
	var body string
	switch err := db.QueryRow(`SELECT body FROM migration WHERE id = 1`).Scan(&body); {
	case errors.Is(err, sql.ErrNoRows):
		return Record{}, false, nil
	case err != nil:
		return Record{}, false, err
	}
	var record Record
	if err := json.Unmarshal([]byte(body), &record); err != nil {
		return Record{}, false, err
	}
	return record, true, nil
}

// setAside moves every old file into backupDir, keeping its relative path,
// then writes the tombstone. Rerunning after a crash skips what already
// moved. Nothing is deleted.
func (m Migration) setAside(backupDir string) error {
	files, err := m.Artifacts()
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, rel := range files {
		from := filepath.Join(m.Dir, filepath.FromSlash(rel))
		to := filepath.Join(backupDir, filepath.Base(m.Dir), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return err
		}
		if _, err := os.Lstat(to); err == nil {
			return fmt.Errorf("backup %s already holds %s; refusing to overwrite it", backupDir, rel)
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
	}
	if len(files) > 0 {
		for _, dir := range []string{filepath.Dir(backupDir), backupDir} {
			if err := atomicfile.SyncDir(dir); err != nil {
				return err
			}
		}
	}
	if m.Tombstone == nil {
		return nil
	}
	rel, content := m.Tombstone()
	target := filepath.Join(m.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(target, content, 0o600)
}
