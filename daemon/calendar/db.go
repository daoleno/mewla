package calendar

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// The calendar lives in calendar.db, beside where calendar.json was:
//
//	cal_item  one row per Item, as JSON without its Runs
//	cal_run   one row per Run, keyed (item_id, seq) where seq is its place in
//	          the Item's Runs. Runs only grow at the end and finish in place,
//	          so a commit writes the Run it touched, not the history.
//
// The Store keeps every Item resident (a calendar is small) and a copy of what
// was last committed; a commit writes the rows that differ between the two in
// one transaction.
const (
	databaseName    = "calendar.db"
	legacyFile      = "calendar.json"
	backupPrefix    = "legacy-calendar-"
	tombstoneSchema = 3
)

var schemaSteps = []string{`
CREATE TABLE cal_item (
	id   TEXT PRIMARY KEY,
	body TEXT NOT NULL
) WITHOUT ROWID;
CREATE TABLE cal_run (
	item_id TEXT NOT NULL,
	seq     INTEGER NOT NULL,
	id      TEXT NOT NULL,
	body    TEXT NOT NULL,
	PRIMARY KEY (item_id, seq)
) WITHOUT ROWID;
CREATE UNIQUE INDEX cal_run_id ON cal_run(item_id, id);
`}

const schemaComponent = "calendar"

func openDatabase(path string) (*sql.DB, error) {
	db, err := sqlitedb.Open(path)
	if err != nil {
		return nil, err
	}
	if err := sqlitedb.Migrate(db, schemaComponent, schemaSteps); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// openRoot opens root/calendar.db, first moving calendar.json into it when it
// is still there.
func openRoot(root string) (*sql.DB, error) {
	return sqlitedb.Migration{
		Dir:      root,
		Database: databaseName,
		Backup: func(at time.Time) string {
			return filepath.Join(filepath.Dir(root), backupPrefix+at.Format("20060102T150405Z"))
		},
		Primaries: func() ([]string, error) { return legacyPrimaries(root) },
		Artifacts: func() ([]string, error) { return legacyArtifacts(root) },
		Open:      openDatabase,
		Build: func(db *sql.DB, record *sqlitedb.Record) error {
			details, err := importLegacy(root, db)
			if err != nil {
				return err
			}
			record.Details, err = json.Marshal(details)
			return err
		},
		Tombstone: func() (string, []byte) {
			body, _ := json.Marshal(map[string]any{
				"schema_version": tombstoneSchema,
				"moved_to":       databaseName,
				"note":           "the calendar moved to calendar.db; an older Mewla must not run on this directory",
			})
			return legacyFile, append(body, '\n')
		},
	}.Run()
}

// legacyPrimaries reports calendar.json when it still holds the calendar. The
// tombstone left by a migration is not one.
func legacyPrimaries(root string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(root, legacyFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var header struct {
		SchemaVersion int    `json:"schema_version"`
		MovedTo       string `json:"moved_to"`
	}
	if json.Unmarshal(raw, &header) == nil && header.SchemaVersion == tombstoneSchema && header.MovedTo != "" {
		return nil, nil
	}
	return []string{legacyFile}, nil
}

// legacyArtifacts lists calendar.json, its schema backups and half-written
// temp files.
func legacyArtifacts(root string) ([]string, error) {
	primaries, err := legacyPrimaries(root)
	if err != nil {
		return nil, err
	}
	files := append([]string(nil), primaries...)
	for _, pattern := range []string{"calendar.json.*", ".calendar.json.tmp-*"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if info, err := os.Lstat(match); err == nil && info.Mode().IsRegular() {
				files = append(files, filepath.Base(match))
			}
		}
	}
	sort.Strings(files)
	return files, nil
}

// MigrationDetails is what the calendar migration verified.
type MigrationDetails struct {
	Items  int    `json:"items"`
	Runs   int    `json:"runs"`
	Sha256 string `json:"sha256"`
}

func importLegacy(root string, db *sql.DB) (MigrationDetails, error) {
	if err := sqlitedb.Migrate(db, schemaComponent, schemaSteps); err != nil {
		return MigrationDetails{}, err
	}
	raw, err := os.ReadFile(filepath.Join(root, legacyFile))
	if err != nil {
		return MigrationDetails{}, err
	}
	doc, err := decodeDocument(raw)
	if err != nil {
		return MigrationDetails{}, fmt.Errorf("decode calendar store: %w", err)
	}
	items := map[string]Item{}
	for _, item := range doc.Items {
		if item.ID != "" {
			items[item.ID] = item
		}
	}
	want, err := digestItems(items)
	if err != nil {
		return MigrationDetails{}, err
	}
	if err := commit(db, map[string]Item{}, items); err != nil {
		return MigrationDetails{}, err
	}
	stored, err := loadItems(db)
	if err != nil {
		return MigrationDetails{}, err
	}
	got, err := digestItems(stored)
	if err != nil {
		return MigrationDetails{}, err
	}
	if got != want {
		return MigrationDetails{}, fmt.Errorf("calendar verification failed: wrote %+v, read back %+v", want, got)
	}
	return want, nil
}

func digestItems(items map[string]Item) (MigrationDetails, error) {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	hasher := sha256.New()
	details := MigrationDetails{Items: len(items)}
	for _, id := range ids {
		body, err := json.Marshal(items[id])
		if err != nil {
			return MigrationDetails{}, err
		}
		hasher.Write(body)
		hasher.Write([]byte{'\n'})
		details.Runs += len(items[id].Runs)
	}
	details.Sha256 = hex.EncodeToString(hasher.Sum(nil))
	return details, nil
}

// loadItems reads every Item with its Runs.
func loadItems(db *sql.DB) (map[string]Item, error) {
	items := map[string]Item{}
	rows, err := db.Query(`SELECT body FROM cal_item`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			_ = rows.Close()
			return nil, err
		}
		var item Item
		if err := json.Unmarshal([]byte(body), &item); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode calendar item: %w", err)
		}
		items[item.ID] = item
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	runs, err := db.Query(`SELECT item_id, body FROM cal_run ORDER BY item_id, seq`)
	if err != nil {
		return nil, err
	}
	defer runs.Close()
	for runs.Next() {
		var itemID, body string
		if err := runs.Scan(&itemID, &body); err != nil {
			return nil, err
		}
		var run Run
		if err := json.Unmarshal([]byte(body), &run); err != nil {
			return nil, fmt.Errorf("decode calendar run: %w", err)
		}
		item, ok := items[itemID]
		if !ok {
			return nil, fmt.Errorf("calendar run %q belongs to unknown item %q", run.ID, itemID)
		}
		item.Runs = append(item.Runs, run)
		items[itemID] = item
	}
	return items, runs.Err()
}

// commit writes the difference between two states of the calendar in one
// transaction. A Run is a row of its own: appending or finishing one writes
// that row and the Item, never the Item's history.
func commit(db *sql.DB, prev, next map[string]Item) error {
	return sqlitedb.Tx(db, func(tx *sql.Tx) error {
		for id, item := range next {
			before, existed := prev[id]
			if !existed || !reflect.DeepEqual(withoutRuns(before), withoutRuns(item)) {
				body, err := json.Marshal(withoutRuns(item))
				if err != nil {
					return err
				}
				if _, err := tx.Exec(`INSERT INTO cal_item(id, body) VALUES(?, ?)
					ON CONFLICT(id) DO UPDATE SET body = excluded.body`, id, string(body)); err != nil {
					return err
				}
			}
			for seq, run := range item.Runs {
				if seq < len(before.Runs) && reflect.DeepEqual(before.Runs[seq], run) {
					continue
				}
				body, err := json.Marshal(run)
				if err != nil {
					return err
				}
				if _, err := tx.Exec(`INSERT INTO cal_run(item_id, seq, id, body) VALUES(?, ?, ?, ?)
					ON CONFLICT(item_id, seq) DO UPDATE SET id = excluded.id, body = excluded.body`,
					id, seq, run.ID, string(body)); err != nil {
					return err
				}
			}
			if len(before.Runs) > len(item.Runs) {
				if _, err := tx.Exec(`DELETE FROM cal_run WHERE item_id = ? AND seq >= ?`, id, len(item.Runs)); err != nil {
					return err
				}
			}
		}
		for id := range prev {
			if _, kept := next[id]; kept {
				continue
			}
			if _, err := tx.Exec(`DELETE FROM cal_run WHERE item_id = ?`, id); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM cal_item WHERE id = ?`, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func withoutRuns(item Item) Item {
	item.Runs = nil
	return item
}
