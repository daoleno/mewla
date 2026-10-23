package telegram

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
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// Telegram's durable state is one struct, durableState: a few dozen scalars
// plus maps and lists (processed updates, the outbox, topic projections and
// the like). telegram.db keeps it as
//
//	tg_core  one row: the scalars, as JSON of the struct with its maps and
//	         lists left empty
//	tg_entry one row per map entry or list element: the field it belongs to,
//	         its key (the map key, or the list index) and its value as JSON
//
// A mutation writes the entries that changed, in one transaction, so a state
// that has grown to tens of thousands of entries costs the same to update as
// a small one. Fields are found by reflection on durableState's own json
// tags, so a field added to the struct is stored without touching this file.
const (
	databaseName    = "state.db"
	legacyFile      = "state.json"
	backupPrefix    = "legacy-telegram-"
	tombstoneSchema = 99
	schemaComponent = "telegram"
)

var schemaSteps = []string{`
CREATE TABLE tg_core (
	id   INTEGER PRIMARY KEY CHECK (id = 1),
	body TEXT NOT NULL
);
CREATE TABLE tg_entry (
	field TEXT NOT NULL,
	key   TEXT NOT NULL,
	body  TEXT NOT NULL,
	PRIMARY KEY (field, key)
) WITHOUT ROWID;
`}

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

// openRoot opens dir/state.db, first moving state.json into it when it is
// still there.
func openRoot(dir string) (*sql.DB, error) {
	return sqlitedb.Migration{
		Dir:      dir,
		Database: databaseName,
		Backup: func(at time.Time) string {
			return filepath.Join(filepath.Dir(dir), backupPrefix+at.Format("20060102T150405Z"))
		},
		Primaries: func() ([]string, error) { return legacyPrimaries(dir) },
		Artifacts: func() ([]string, error) { return legacyArtifacts(dir) },
		Open:      openDatabase,
		Build: func(db *sql.DB, record *sqlitedb.Record) error {
			details, err := importLegacy(dir, db)
			if err != nil {
				return err
			}
			record.Details, err = json.Marshal(details)
			return err
		},
		Tombstone: func() (string, []byte) {
			body, _ := json.Marshal(map[string]any{
				"schema":   tombstoneSchema,
				"moved_to": databaseName,
				"note":     "Telegram state moved to state.db; an older Mewla must not run on this directory",
			})
			return legacyFile, append(body, '\n')
		},
	}.Run()
}

// legacyPrimaries reports state.json when it still holds the state. The
// tombstone left by a migration is not one.
func legacyPrimaries(dir string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, legacyFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var header struct {
		Schema  int    `json:"schema"`
		MovedTo string `json:"moved_to"`
	}
	if json.Unmarshal(raw, &header) == nil && header.Schema == tombstoneSchema && header.MovedTo != "" {
		return nil, nil
	}
	return []string{legacyFile}, nil
}

func legacyArtifacts(dir string) ([]string, error) {
	primaries, err := legacyPrimaries(dir)
	if err != nil {
		return nil, err
	}
	files := append([]string(nil), primaries...)
	for _, pattern := range []string{"state.json.*", ".state.json.tmp-*"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
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

// MigrationDetails is what the Telegram migration verified.
type MigrationDetails struct {
	Entries int    `json:"entries"`
	Sha256  string `json:"sha256"`
}

func importLegacy(dir string, db *sql.DB) (MigrationDetails, error) {
	if err := sqlitedb.Migrate(db, schemaComponent, schemaSteps); err != nil {
		return MigrationDetails{}, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, legacyFile))
	if err != nil {
		return MigrationDetails{}, err
	}
	state, _, err := decodeLegacy(raw)
	if err != nil {
		return MigrationDetails{}, err
	}
	want, err := digestState(state)
	if err != nil {
		return MigrationDetails{}, err
	}
	if err := commitState(db, durableState{}, state); err != nil {
		return MigrationDetails{}, err
	}
	stored, _, err := loadState(db)
	if err != nil {
		return MigrationDetails{}, err
	}
	got, err := digestState(stored)
	if err != nil {
		return MigrationDetails{}, err
	}
	if got != want {
		return MigrationDetails{}, fmt.Errorf("Telegram verification failed: wrote %+v, read back %+v", want, got)
	}
	return want, nil
}

// decodeLegacy decodes a state.json, bringing older schemas to the current
// one. migrated reports that the schema changed.
func decodeLegacy(data []byte) (durableState, bool, error) {
	state := newDurableState()
	if err := json.Unmarshal(data, &state); err != nil {
		return durableState{}, false, fmt.Errorf("decode Telegram state: %w", err)
	}
	migrated := false
	switch state.Schema {
	case 1:
		for index := range state.Outbox {
			state.Outbox[index].PlainText = state.Outbox[index].Text
			state.Outbox[index].Variant = "plain"
		}
		state.Schema = stateSchema
		migrated = true
	case 2:
		// Schema 2 -> 3 only introduces topic state maps/slices, which
		// did not exist before; every existing row is preserved as-is.
		state.Schema = stateSchema
		migrated = true
	case 3:
		// Schema 3 already contains durable topic state. Schema 4 adds the
		// non-topic private-chat recipient and a non-fatal capability notice.
		state.Schema = stateSchema
		migrated = true
	case stateSchema:
	default:
		return durableState{}, false, fmt.Errorf("unsupported Telegram state schema")
	}
	ensureDurableMaps(&state)
	return state, migrated, nil
}

// digestState hashes the whole state's canonical JSON and counts its entries.
func digestState(state durableState) (MigrationDetails, error) {
	ensureDurableMaps(&state)
	body, err := json.Marshal(state)
	if err != nil {
		return MigrationDetails{}, err
	}
	sum := sha256.Sum256(body)
	entries := 0
	for _, field := range collectionFields() {
		entries += reflect.ValueOf(state).Field(field.index).Len()
	}
	return MigrationDetails{Entries: entries, Sha256: hex.EncodeToString(sum[:])}, nil
}

type collectionField struct {
	index int
	name  string
}

// collectionFields lists the maps and lists of durableState, by json name.
func collectionFields() []collectionField {
	t := reflect.TypeOf(durableState{})
	var out []collectionField
	for index := 0; index < t.NumField(); index++ {
		field := t.Field(index)
		if field.Type.Kind() != reflect.Map && field.Type.Kind() != reflect.Slice {
			continue
		}
		out = append(out, collectionField{index: index, name: strings.Split(field.Tag.Get("json"), ",")[0]})
	}
	return out
}

// loadState reads the stored state. found is false when nothing has been
// stored yet. The scalars are written only once they differ from a new
// state's, so a database can hold entries without a core row.
func loadState(db *sql.DB) (durableState, bool, error) {
	state := newDurableState()
	var core string
	found := true
	switch err := db.QueryRow(`SELECT body FROM tg_core WHERE id = 1`).Scan(&core); {
	case errors.Is(err, sql.ErrNoRows):
		found = false
	case err != nil:
		return durableState{}, false, err
	default:
		if err := json.Unmarshal([]byte(core), &state); err != nil {
			return durableState{}, false, fmt.Errorf("decode Telegram state: %w", err)
		}
	}
	value := reflect.ValueOf(&state).Elem()
	for _, field := range collectionFields() {
		target := value.Field(field.index)
		rows, err := db.Query(`SELECT key, body FROM tg_entry WHERE field = ?`, field.name)
		if err != nil {
			return durableState{}, false, err
		}
		type entry struct {
			index int
			key   string
			body  string
		}
		var entries []entry
		for rows.Next() {
			var e entry
			if err := rows.Scan(&e.key, &e.body); err != nil {
				_ = rows.Close()
				return durableState{}, false, err
			}
			if target.Kind() == reflect.Slice {
				index, err := strconv.Atoi(e.key)
				if err != nil {
					_ = rows.Close()
					return durableState{}, false, fmt.Errorf("Telegram %s entry %q: %w", field.name, e.key, err)
				}
				e.index = index
			}
			entries = append(entries, e)
		}
		if err := rows.Close(); err != nil {
			return durableState{}, false, err
		}
		if target.Kind() == reflect.Slice {
			sort.Slice(entries, func(i, j int) bool { return entries[i].index < entries[j].index })
			list := reflect.MakeSlice(target.Type(), 0, len(entries))
			for position, e := range entries {
				if e.index != position {
					return durableState{}, false, fmt.Errorf("Telegram %s has a gap at %d", field.name, position)
				}
				element := reflect.New(target.Type().Elem())
				if err := json.Unmarshal([]byte(e.body), element.Interface()); err != nil {
					return durableState{}, false, fmt.Errorf("decode Telegram %s[%d]: %w", field.name, position, err)
				}
				list = reflect.Append(list, element.Elem())
			}
			target.Set(list)
			continue
		}
		table := reflect.MakeMapWithSize(target.Type(), len(entries))
		for _, e := range entries {
			key, err := parseMapKey(target.Type().Key(), e.key)
			if err != nil {
				return durableState{}, false, fmt.Errorf("Telegram %s key %q: %w", field.name, e.key, err)
			}
			element := reflect.New(target.Type().Elem())
			if err := json.Unmarshal([]byte(e.body), element.Interface()); err != nil {
				return durableState{}, false, fmt.Errorf("decode Telegram %s[%s]: %w", field.name, e.key, err)
			}
			table.SetMapIndex(key, element.Elem())
		}
		target.Set(table)
	}
	ensureDurableMaps(&state)
	var entries int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tg_entry`).Scan(&entries); err != nil {
		return durableState{}, false, err
	}
	return state, found || entries > 0, nil
}

func parseMapKey(t reflect.Type, key string) (reflect.Value, error) {
	value := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.String:
		value.SetString(key)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			return reflect.Value{}, err
		}
		value.SetInt(n)
	default:
		return reflect.Value{}, fmt.Errorf("unsupported map key type %s", t)
	}
	return value, nil
}

func formatMapKey(key reflect.Value) string {
	if key.Kind() == reflect.String {
		return key.String()
	}
	return strconv.FormatInt(key.Int(), 10)
}

// coreOf is the state with every map and list emptied: the scalars alone.
func coreOf(state durableState) ([]byte, error) {
	value := reflect.ValueOf(&state).Elem()
	for _, field := range collectionFields() {
		value.Field(field.index).Set(reflect.Zero(value.Field(field.index).Type()))
	}
	return json.Marshal(state)
}

// commitState writes the difference between two states in one transaction.
func commitState(db *sql.DB, prev, next durableState) error {
	return sqlitedb.Tx(db, func(tx *sql.Tx) error {
		before, err := coreOf(prev)
		if err != nil {
			return err
		}
		after, err := coreOf(next)
		if err != nil {
			return err
		}
		if string(before) != string(after) {
			if _, err := tx.Exec(`INSERT INTO tg_core(id, body) VALUES(1, ?)
				ON CONFLICT(id) DO UPDATE SET body = excluded.body`, string(after)); err != nil {
				return err
			}
		}
		prevValue, nextValue := reflect.ValueOf(prev), reflect.ValueOf(next)
		for _, field := range collectionFields() {
			old, current := prevValue.Field(field.index), nextValue.Field(field.index)
			if current.Kind() == reflect.Slice {
				if err := diffList(tx, field.name, old, current); err != nil {
					return err
				}
				continue
			}
			if err := diffMap(tx, field.name, old, current); err != nil {
				return err
			}
		}
		return nil
	})
}

func upsertEntry(tx *sql.Tx, field, key string, value reflect.Value) error {
	body, err := json.Marshal(value.Interface())
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO tg_entry(field, key, body) VALUES(?, ?, ?)
		ON CONFLICT(field, key) DO UPDATE SET body = excluded.body`, field, key, string(body))
	return err
}

func diffList(tx *sql.Tx, field string, old, current reflect.Value) error {
	for index := 0; index < current.Len(); index++ {
		if index < old.Len() && reflect.DeepEqual(old.Index(index).Interface(), current.Index(index).Interface()) {
			continue
		}
		if err := upsertEntry(tx, field, strconv.Itoa(index), current.Index(index)); err != nil {
			return err
		}
	}
	for index := current.Len(); index < old.Len(); index++ {
		if _, err := tx.Exec(`DELETE FROM tg_entry WHERE field = ? AND key = ?`, field, strconv.Itoa(index)); err != nil {
			return err
		}
	}
	return nil
}

func diffMap(tx *sql.Tx, field string, old, current reflect.Value) error {
	iterator := current.MapRange()
	for iterator.Next() {
		if old.IsValid() && !old.IsNil() {
			if before := old.MapIndex(iterator.Key()); before.IsValid() && reflect.DeepEqual(before.Interface(), iterator.Value().Interface()) {
				continue
			}
		}
		if err := upsertEntry(tx, field, formatMapKey(iterator.Key()), iterator.Value()); err != nil {
			return err
		}
	}
	if old.IsValid() && !old.IsNil() {
		iterator = old.MapRange()
		for iterator.Next() {
			if current.IsNil() || !current.MapIndex(iterator.Key()).IsValid() {
				if _, err := tx.Exec(`DELETE FROM tg_entry WHERE field = ? AND key = ?`, field, formatMapKey(iterator.Key())); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
