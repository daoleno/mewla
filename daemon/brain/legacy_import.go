package brain

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// Before the SQLite store, Brain kept lifecycle/state.json, presentation.json
// and messages.jsonl, each rewritten whole on change. openStateDatabase is the
// one place that still knows those files: it moves their contents into
// brain.db once, proves the move, and sets the old files aside. Nothing reads
// them afterwards.
//
// The migration is crash-safe and rerunnable:
//
//  1. Take an exclusive lock on the state directory.
//  2. Build the new database as brain.db.migrating, apart from the live name.
//  3. Re-read it and compare row counts and content hashes with what was
//     decoded from the old files.
//  4. Rename it to brain.db. This is the commit point.
//  5. Move the old files into one dated backup directory and leave a schema 3
//     tombstone at lifecycle/state.json, so an older binary that is started
//     later refuses to run instead of opening an empty Brain.
//
// A crash before 4 leaves the old files untouched and the next start rebuilds
// from scratch. A crash after 4 leaves a database with a migration record and
// old files still in place; the next start only finishes step 5.
const (
	stateDatabaseName   = "brain.db"
	migratingSuffix     = ".migrating"
	migrationLockName   = ".migrate.lock"
	legacyBackupPrefix  = "legacy-state-"
	tombstoneSchema     = 3
	legacyLifecycleFile = "lifecycle/state.json"
)

// MigrationRecord is what a migrated database remembers about the move: where
// the old files went and the counts and hashes the move was checked against.
type MigrationRecord struct {
	At           time.Time           `json:"at"`
	BackupDir    string              `json:"backup_dir"`
	Lifecycle    *lifecycle.Digest   `json:"lifecycle,omitempty"`
	Presentation *presentationDigest `json:"presentation,omitempty"`
	Timeline     *timelineDigest     `json:"timeline,omitempty"`
	Archive      *timelineDigest     `json:"archive,omitempty"`
	Files        []MigratedFile      `json:"files"`
	Notes        []string            `json:"notes,omitempty"`
}

// MigratedFile is one old file set aside, relative to the state directory.
type MigratedFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type presentationDigest struct {
	Works             int    `json:"works"`
	Events            int    `json:"events"`
	Turns             int    `json:"turns"`
	Admissions        int    `json:"admissions"`
	NextEventSequence uint64 `json:"next_event_sequence"`
	HostForeground    bool   `json:"host_foreground"`
	Sha256            string `json:"sha256"`
}

type timelineDigest struct {
	Rows   int    `json:"rows"`
	Sha256 string `json:"sha256"`
}

// openStateDatabase opens stateDir/brain.db, migrating pre-SQLite files into
// it first when they are present.
func openStateDatabase(stateDir string) (*sql.DB, error) {
	path := filepath.Join(stateDir, stateDatabaseName)
	legacy, err := legacyPrimaryFiles(stateDir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil && len(legacy) == 0 {
		return openBrainDatabase(path)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	unlock, err := lockMigration(filepath.Join(stateDir, migrationLockName))
	if err != nil {
		return nil, err
	}
	defer unlock()
	// Another process may have finished while this one waited for the lock.
	if legacy, err = legacyPrimaryFiles(stateDir); err != nil {
		return nil, err
	}
	_, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		db, err := openBrainDatabase(path)
		if err != nil {
			return nil, err
		}
		if len(legacy) == 0 {
			return db, nil
		}
		record, found, err := readMigrationRecord(db)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		if !found {
			_ = db.Close()
			return nil, fmt.Errorf("brain: %s and %s both exist and the database has no migration record; "+
				"refusing to guess which holds the state", stateDatabaseName, strings.Join(legacy, ", "))
		}
		// The database was committed but the old files were not yet set aside.
		if err := setAsideLegacy(stateDir, record.BackupDir); err != nil {
			_ = db.Close()
			return nil, err
		}
		return db, nil
	case !errors.Is(statErr, os.ErrNotExist):
		return nil, statErr
	case len(legacy) == 0:
		return openBrainDatabase(path)
	}
	return migrateLegacyState(stateDir, path)
}

// legacyPrimaryFiles lists the pre-SQLite state files that still carry live
// state. A tombstone left by an earlier migration is not one.
func legacyPrimaryFiles(stateDir string) ([]string, error) {
	var found []string
	for _, rel := range []string{"presentation.json", "messages.jsonl"} {
		if _, err := os.Stat(filepath.Join(stateDir, rel)); err == nil {
			found = append(found, rel)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if _, legacy, err := lifecycle.ReadLegacy(filepath.Join(stateDir, legacyLifecycleFile)); err != nil {
		return nil, err
	} else if legacy {
		found = append(found, legacyLifecycleFile)
	}
	sort.Strings(found)
	return found, nil
}

func migrateLegacyState(stateDir, path string) (*sql.DB, error) {
	building := path + migratingSuffix
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(building + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	now := time.Now().UTC()
	record := MigrationRecord{
		At:        now,
		BackupDir: filepath.Join(filepath.Dir(stateDir), legacyBackupPrefix+now.Format("20060102T150405Z")),
	}
	if err := buildMigratedDatabase(stateDir, building, &record); err != nil {
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			_ = os.Remove(building + suffix)
		}
		return nil, fmt.Errorf("migrate Brain state to %s: %w", stateDatabaseName, err)
	}
	if err := os.Rename(building, path); err != nil {
		return nil, err
	}
	if err := atomicfile.SyncDir(stateDir); err != nil {
		return nil, err
	}
	if err := setAsideLegacy(stateDir, record.BackupDir); err != nil {
		return nil, err
	}
	return openBrainDatabase(path)
}

// buildMigratedDatabase writes the new database next to its final name and
// verifies it against the decoded old files before returning. The database is
// sealed to a single file, so renaming it moves everything.
func buildMigratedDatabase(stateDir, building string, record *MigrationRecord) error {
	db, err := sqlitedb.Open(building)
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
	if err := sqlitedb.Migrate(db, brainSchemaComponent, brainSchemaSteps); err != nil {
		return err
	}

	// Lifecycle.
	legacyLifecycle, found, err := lifecycle.ReadLegacy(filepath.Join(stateDir, legacyLifecycleFile))
	if err != nil {
		return err
	}
	if found {
		want, err := lifecycle.LegacyDigest(legacyLifecycle)
		if err != nil {
			return err
		}
		if err := lifecycle.ImportLegacy(db, legacyLifecycle); err != nil {
			return err
		}
		got, err := lifecycle.ReadDigest(db)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("lifecycle verification failed: wrote %+v, read back %+v", want, got)
		}
		record.Lifecycle = &want
	}

	// Presentation.
	if raw, err := os.ReadFile(filepath.Join(stateDir, "presentation.json")); err == nil {
		decoded, err := decodePresentationDatabase(raw)
		if err != nil {
			return fmt.Errorf("decode Brain presentation database: %w", err)
		}
		sortPresentation(&decoded)
		if err := commitPresentation(db, 0, presentationDatabase{}, decoded); err != nil {
			return err
		}
		image, _, err := readPresentationImage(db)
		if err != nil {
			return err
		}
		want, err := digestPresentation(decoded)
		if err != nil {
			return err
		}
		got, err := digestPresentation(image)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("presentation verification failed: wrote %+v, read back %+v", want, got)
		}
		record.Presentation = &want
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Timeline and its archives.
	live, note, err := readLegacyTimeline(filepath.Join(stateDir, "messages.jsonl"))
	if err != nil {
		return err
	}
	record.Notes = append(record.Notes, note...)
	if err := sqlitedb.Tx(db, func(tx *sql.Tx) error {
		for _, item := range live {
			body, err := json.Marshal(item)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO timeline(id, thread_id, body) VALUES(?, ?, ?)`,
				strings.TrimSpace(item.ID), strings.TrimSpace(item.ThreadID), string(body)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("write timeline: %w", err)
	}
	wantTimeline, err := digestTimeline(live)
	if err != nil {
		return err
	}
	var stored []TimelineItem
	if err := scanBodies(db, `SELECT body FROM timeline ORDER BY seq`, func(body []byte) error {
		var item TimelineItem
		if err := json.Unmarshal(body, &item); err != nil {
			return err
		}
		stored = append(stored, item)
		return nil
	}); err != nil {
		return err
	}
	gotTimeline, err := digestTimeline(stored)
	if err != nil {
		return err
	}
	if gotTimeline != wantTimeline {
		return fmt.Errorf("timeline verification failed: wrote %+v, read back %+v", wantTimeline, gotTimeline)
	}
	record.Timeline = &wantTimeline

	archived, notes, err := readLegacyArchives(stateDir, live)
	if err != nil {
		return err
	}
	record.Notes = append(record.Notes, notes...)
	if err := sqlitedb.Tx(db, func(tx *sql.Tx) error {
		for _, row := range archived {
			body, err := json.Marshal(row.item)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO timeline_archive(origin, id, body) VALUES(?, ?, ?)`,
				row.origin, strings.TrimSpace(row.item.ID), string(body)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("write timeline archive: %w", err)
	}
	wantArchive, err := digestArchive(archived)
	if err != nil {
		return err
	}
	var storedArchive []archivedItem
	rows, err := db.Query(`SELECT origin, body FROM timeline_archive ORDER BY seq`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var origin, body string
		if err := rows.Scan(&origin, &body); err != nil {
			_ = rows.Close()
			return err
		}
		var item TimelineItem
		if err := json.Unmarshal([]byte(body), &item); err != nil {
			_ = rows.Close()
			return err
		}
		storedArchive = append(storedArchive, archivedItem{origin: origin, item: item})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	gotArchive, err := digestArchive(storedArchive)
	if err != nil {
		return err
	}
	if gotArchive != wantArchive {
		return fmt.Errorf("timeline archive verification failed: wrote %+v, read back %+v", wantArchive, gotArchive)
	}
	record.Archive = &wantArchive

	// What gets set aside, recorded before the commit point.
	files, err := legacyArtifacts(stateDir)
	if err != nil {
		return err
	}
	for _, rel := range files {
		info, err := os.Stat(filepath.Join(stateDir, rel))
		if err != nil {
			return err
		}
		record.Files = append(record.Files, MigratedFile{Path: rel, Bytes: info.Size()})
	}
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO migration(id, body) VALUES(1, ?)`, string(body)); err != nil {
		return err
	}
	if err := sqlitedb.Seal(db); err != nil {
		return err
	}
	closed = true
	return db.Close()
}

func readMigrationRecord(db *sql.DB) (MigrationRecord, bool, error) {
	var body string
	err := db.QueryRow(`SELECT body FROM migration WHERE id = 1`).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return MigrationRecord{}, false, nil
	}
	if err != nil {
		return MigrationRecord{}, false, err
	}
	var record MigrationRecord
	if err := json.Unmarshal([]byte(body), &record); err != nil {
		return MigrationRecord{}, false, err
	}
	return record, true, nil
}

// MigrationRecordOf reports what the migration of this Store's state
// directory moved, when it happened here.
func (s *Store) MigrationRecord() (MigrationRecord, bool, error) {
	return readMigrationRecord(s.db)
}

// legacyArtifacts lists every pre-SQLite file under stateDir, relative to it:
// the live files, their repair copies and backups, and half-written temp
// files. Anything else in the directory is not Brain's former store.
func legacyArtifacts(stateDir string) ([]string, error) {
	var files []string
	add := func(pattern string) error {
		matches, err := filepath.Glob(filepath.Join(stateDir, pattern))
		if err != nil {
			return err
		}
		for _, match := range matches {
			info, err := os.Lstat(match)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				continue
			}
			rel, err := filepath.Rel(stateDir, match)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	}
	for _, pattern := range []string{
		"lifecycle/*", "lifecycle/.*",
		"presentation.json*", ".presentation.json.tmp-*",
		"messages.jsonl*", "messages-*.jsonl", ".messages.jsonl.tmp-*",
	} {
		if err := add(pattern); err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	out := files[:0]
	for index, file := range files {
		if index == 0 || file != files[index-1] {
			out = append(out, file)
		}
	}
	// A tombstone from an earlier migration is not an old file.
	kept := out[:0]
	for _, file := range out {
		if file == legacyLifecycleFile {
			if _, legacy, err := lifecycle.ReadLegacy(filepath.Join(stateDir, file)); err != nil {
				return nil, err
			} else if !legacy {
				continue
			}
		}
		kept = append(kept, file)
	}
	return kept, nil
}

// setAsideLegacy moves every old file into backupDir, keeping its relative
// path, then writes the tombstone. Rerunning after a crash skips what already
// moved. Nothing is deleted.
func setAsideLegacy(stateDir, backupDir string) error {
	files, err := legacyArtifacts(stateDir)
	if err != nil {
		return err
	}
	for _, rel := range files {
		from := filepath.Join(stateDir, filepath.FromSlash(rel))
		to := filepath.Join(backupDir, "state", filepath.FromSlash(rel))
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
	tombstone, err := json.Marshal(map[string]any{
		"schema":   tombstoneSchema,
		"moved_to": "../" + stateDatabaseName,
		"note":     "Brain state moved to state/brain.db; an older Mewla must not run on this directory",
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "lifecycle"), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(stateDir, legacyLifecycleFile), append(tombstone, '\n'), 0o600)
}

// decodePresentationDatabase accepts only the current projection schema.
// Older scheduler documents must reset; legacy fact rows are never promoted
// back into writable lifecycle authority.
func decodePresentationDatabase(raw []byte) (presentationDatabase, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return presentationDatabase{}, fmt.Errorf("document must be a JSON object")
	}
	var header struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(trimmed, &header); err != nil {
		return presentationDatabase{}, err
	}
	if header.SchemaVersion == nil {
		return presentationDatabase{}, fmt.Errorf("schema_version is required")
	}
	version := *header.SchemaVersion
	if version != presentationSchemaVersion {
		return presentationDatabase{}, fmt.Errorf(
			"%w: schema_version %d (expected %d)",
			ErrSchedulerStateReset,
			version,
			presentationSchemaVersion,
		)
	}
	var record presentationDatabaseRecord
	if err := json.Unmarshal(trimmed, &record); err != nil {
		return presentationDatabase{}, err
	}
	if record.BrainWork == nil || record.BrainWorkEvents == nil || record.BrainInputAdmissions == nil {
		return presentationDatabase{}, fmt.Errorf("brain_input_admissions, brain_work, and brain_work_events are required arrays")
	}
	database := presentationDatabase{
		SchemaVersion:        presentationSchemaVersion,
		NextEventSequence:    record.NextEventSequence,
		HostForegroundTurn:   record.HostForegroundTurn,
		BrainWork:            worksFromRecords(record.BrainWork),
		BrainWorkEvents:      record.BrainWorkEvents,
		BrainTurns:           record.BrainTurns,
		BrainInputAdmissions: record.BrainInputAdmissions,
	}
	if database.BrainTurns == nil {
		database.BrainTurns = []TurnRecord{}
	}
	if err := validatePresentationDatabase(database); err != nil {
		return presentationDatabase{}, err
	}
	return database, nil
}

// readLegacyTimeline decodes a messages.jsonl file in order. A repeated id
// keeps its first row, which is the one every old reader returned. A final
// line without a newline that does not parse is a write the crash cut short,
// and is skipped; any other bad line fails the migration.
func readLegacyTimeline(path string) ([]TimelineItem, []string, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	name := filepath.Base(path)
	var items []TimelineItem
	var notes []string
	seen := map[string]bool{}
	reader := bufio.NewReaderSize(file, 1<<20)
	for lineNumber := 1; ; lineNumber++ {
		line, readErr := reader.ReadBytes('\n')
		terminated := bytes.HasSuffix(line, []byte{'\n'})
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			var item TimelineItem
			switch err := json.Unmarshal(trimmed, &item); {
			case err != nil && !terminated:
				notes = append(notes, fmt.Sprintf("%s: skipped a torn final line of %d bytes", name, len(trimmed)))
			case err != nil:
				return nil, nil, fmt.Errorf("decode %s line %d: %w", name, lineNumber, err)
			case strings.TrimSpace(item.ID) == "":
				return nil, nil, fmt.Errorf("decode %s line %d: id is required", name, lineNumber)
			case seen[strings.TrimSpace(item.ID)]:
				notes = append(notes, fmt.Sprintf("%s: dropped a repeated row for id %q", name, strings.TrimSpace(item.ID)))
			default:
				seen[strings.TrimSpace(item.ID)] = true
				items = append(items, item)
			}
		}
		if readErr != nil {
			break
		}
	}
	return items, notes, nil
}

type archivedItem struct {
	origin string
	item   TimelineItem
}

// readLegacyArchives collects the rows of the old repair copies and archives
// of the chat log that the live timeline does not have.
func readLegacyArchives(stateDir string, live []TimelineItem) ([]archivedItem, []string, error) {
	inLive := make(map[string]bool, len(live))
	for _, item := range live {
		inLive[strings.TrimSpace(item.ID)] = true
	}
	var names []string
	for _, pattern := range []string{"messages-*.jsonl", "messages.jsonl.*"} {
		matches, err := filepath.Glob(filepath.Join(stateDir, pattern))
		if err != nil {
			return nil, nil, err
		}
		names = append(names, matches...)
	}
	sort.Strings(names)
	var out []archivedItem
	var notes []string
	for _, name := range names {
		items, fileNotes, err := readLegacyTimeline(name)
		if err != nil {
			return nil, nil, err
		}
		notes = append(notes, fileNotes...)
		origin := filepath.Base(name)
		for _, item := range items {
			if !inLive[strings.TrimSpace(item.ID)] {
				out = append(out, archivedItem{origin: origin, item: item})
			}
		}
	}
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].origin != out[right].origin {
			return out[left].origin < out[right].origin
		}
		return strings.TrimSpace(out[left].item.ID) < strings.TrimSpace(out[right].item.ID)
	})
	return out, notes, nil
}

func digestPresentation(database presentationDatabase) (presentationDigest, error) {
	hasher := sha256.New()
	write := func(table, key string, value any) error {
		body, err := json.Marshal(value)
		if err != nil {
			return err
		}
		hasher.Write([]byte(table + "\x00" + key + "\x00"))
		hasher.Write(body)
		hasher.Write([]byte{'\n'})
		return nil
	}
	works := append([]Work(nil), database.BrainWork...)
	sort.Slice(works, func(i, j int) bool { return works[i].ID < works[j].ID })
	for _, item := range works {
		if err := write("work", item.ID, item); err != nil {
			return presentationDigest{}, err
		}
	}
	events := append([]WorkEvent(nil), database.BrainWorkEvents...)
	sort.Slice(events, func(i, j int) bool { return events[i].ID < events[j].ID })
	for _, event := range events {
		if err := write("event", event.ID, event); err != nil {
			return presentationDigest{}, err
		}
	}
	turns := append([]TurnRecord(nil), database.BrainTurns...)
	sort.Slice(turns, func(i, j int) bool {
		return turnRecordKey(turns[i].SessionID, turns[i].TurnID) < turnRecordKey(turns[j].SessionID, turns[j].TurnID)
	})
	for _, turn := range turns {
		if err := write("turn", turnRecordKey(turn.SessionID, turn.TurnID), turn); err != nil {
			return presentationDigest{}, err
		}
	}
	admissions := append([]BrainInputAdmission(nil), database.BrainInputAdmissions...)
	sort.Slice(admissions, func(i, j int) bool {
		return admissions[i].RequestID+"\x00"+admissions[i].ThreadID < admissions[j].RequestID+"\x00"+admissions[j].ThreadID
	})
	for _, admission := range admissions {
		if err := write("admission", admission.RequestID+"\x00"+admission.ThreadID, admission); err != nil {
			return presentationDigest{}, err
		}
	}
	if database.HostForegroundTurn != nil {
		if err := write("foreground", "", database.HostForegroundTurn); err != nil {
			return presentationDigest{}, err
		}
	}
	return presentationDigest{
		Works: len(works), Events: len(events), Turns: len(turns), Admissions: len(admissions),
		NextEventSequence: database.NextEventSequence, HostForeground: database.HostForegroundTurn != nil,
		Sha256: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

func digestTimeline(items []TimelineItem) (timelineDigest, error) {
	hasher := sha256.New()
	for _, item := range items {
		body, err := json.Marshal(item)
		if err != nil {
			return timelineDigest{}, err
		}
		hasher.Write(body)
		hasher.Write([]byte{'\n'})
	}
	return timelineDigest{Rows: len(items), Sha256: hex.EncodeToString(hasher.Sum(nil))}, nil
}

func digestArchive(rows []archivedItem) (timelineDigest, error) {
	hasher := sha256.New()
	for _, row := range rows {
		body, err := json.Marshal(row.item)
		if err != nil {
			return timelineDigest{}, err
		}
		hasher.Write([]byte(row.origin + "\x00"))
		hasher.Write(body)
		hasher.Write([]byte{'\n'})
	}
	return timelineDigest{Rows: len(rows), Sha256: hex.EncodeToString(hasher.Sum(nil))}, nil
}
