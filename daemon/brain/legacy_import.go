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

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// Before the SQLite store, Brain kept lifecycle/state.json, presentation.json
// and messages.jsonl, each rewritten whole on change. openStateDatabase is the
// one place that still knows those files: it moves their contents into
// brain.db once, proves the move, and sets the old files aside (the protocol
// is sqlitedb.Migration). Nothing reads them afterwards.
//
// The tombstone left at lifecycle/state.json has schema 3. An older binary
// started later reads it as an unsupported lifecycle schema and refuses to
// run, instead of opening an empty Brain beside the real one.
const (
	stateDatabaseName   = "brain.db"
	legacyBackupPrefix  = "legacy-state-"
	tombstoneSchema     = 3
	legacyLifecycleFile = "lifecycle/state.json"
)

// MigrationDetails is what the Brain migration verified: the counts and
// content hashes of everything it moved, as decoded from the old files and as
// read back from the new database.
type MigrationDetails struct {
	Lifecycle    *lifecycle.Digest   `json:"lifecycle,omitempty"`
	Presentation *presentationDigest `json:"presentation,omitempty"`
	Timeline     *timelineDigest     `json:"timeline,omitempty"`
	Archive      *timelineDigest     `json:"archive,omitempty"`
}

// MigrationRecord is the migration record of a Brain database.
type MigrationRecord struct {
	sqlitedb.Record
	MigrationDetails
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
	return sqlitedb.Migration{
		Dir:      stateDir,
		Database: stateDatabaseName,
		Backup: func(at time.Time) string {
			return filepath.Join(filepath.Dir(stateDir), legacyBackupPrefix+at.Format("20060102T150405Z"))
		},
		Primaries: func() ([]string, error) { return legacyPrimaryFiles(stateDir) },
		Artifacts: func() ([]string, error) { return legacyArtifacts(stateDir) },
		Open:      openBrainDatabase,
		Build: func(db *sql.DB, record *sqlitedb.Record) error {
			details, err := buildMigratedDatabase(stateDir, db, record)
			if err != nil {
				return err
			}
			record.Details, err = json.Marshal(details)
			return err
		},
		Tombstone: func() (string, []byte) {
			body, _ := json.Marshal(map[string]any{
				"schema":   tombstoneSchema,
				"moved_to": "../" + stateDatabaseName,
				"note":     "Brain state moved to state/brain.db; an older Mewla must not run on this directory",
			})
			return legacyLifecycleFile, append(body, '\n')
		},
	}.Run()
}

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

// buildMigratedDatabase fills db from the old files and verifies each part
// against what was decoded from them.
func buildMigratedDatabase(stateDir string, db *sql.DB, record *sqlitedb.Record) (MigrationDetails, error) {
	var details MigrationDetails
	if err := sqlitedb.Migrate(db, brainSchemaComponent, brainSchemaSteps); err != nil {
		return details, err
	}

	// Lifecycle.
	legacyLifecycle, found, err := lifecycle.ReadLegacy(filepath.Join(stateDir, legacyLifecycleFile))
	if err != nil {
		return details, err
	}
	if found {
		want, err := lifecycle.LegacyDigest(legacyLifecycle)
		if err != nil {
			return details, err
		}
		if err := lifecycle.ImportLegacy(db, legacyLifecycle); err != nil {
			return details, err
		}
		got, err := lifecycle.ReadDigest(db)
		if err != nil {
			return details, err
		}
		if got != want {
			return details, fmt.Errorf("lifecycle verification failed: wrote %+v, read back %+v", want, got)
		}
		details.Lifecycle = &want
	}

	// Presentation.
	if raw, err := os.ReadFile(filepath.Join(stateDir, "presentation.json")); err == nil {
		decoded, err := decodePresentationDatabase(raw)
		if err != nil {
			return details, fmt.Errorf("decode Brain presentation database: %w", err)
		}
		sortPresentation(&decoded)
		if _, err := commitPresentation(db, 0, presentationDatabase{}, decoded); err != nil {
			return details, err
		}
		image, _, err := readPresentationImage(db)
		if err != nil {
			return details, err
		}
		want, err := digestPresentation(decoded)
		if err != nil {
			return details, err
		}
		got, err := digestPresentation(image)
		if err != nil {
			return details, err
		}
		if got != want {
			return details, fmt.Errorf("presentation verification failed: wrote %+v, read back %+v", want, got)
		}
		details.Presentation = &want
	} else if !errors.Is(err, os.ErrNotExist) {
		return details, err
	}

	// Timeline and its archives.
	live, note, err := readLegacyTimeline(filepath.Join(stateDir, "messages.jsonl"))
	if err != nil {
		return details, err
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
		return details, fmt.Errorf("write timeline: %w", err)
	}
	wantTimeline, err := digestTimeline(live)
	if err != nil {
		return details, err
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
		return details, err
	}
	gotTimeline, err := digestTimeline(stored)
	if err != nil {
		return details, err
	}
	if gotTimeline != wantTimeline {
		return details, fmt.Errorf("timeline verification failed: wrote %+v, read back %+v", wantTimeline, gotTimeline)
	}
	details.Timeline = &wantTimeline

	archived, notes, err := readLegacyArchives(stateDir, live)
	if err != nil {
		return details, err
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
		return details, fmt.Errorf("write timeline archive: %w", err)
	}
	wantArchive, err := digestArchive(archived)
	if err != nil {
		return details, err
	}
	var storedArchive []archivedItem
	rows, err := db.Query(`SELECT origin, body FROM timeline_archive ORDER BY seq`)
	if err != nil {
		return details, err
	}
	for rows.Next() {
		var origin, body string
		if err := rows.Scan(&origin, &body); err != nil {
			_ = rows.Close()
			return details, err
		}
		var item TimelineItem
		if err := json.Unmarshal([]byte(body), &item); err != nil {
			_ = rows.Close()
			return details, err
		}
		storedArchive = append(storedArchive, archivedItem{origin: origin, item: item})
	}
	if err := rows.Close(); err != nil {
		return details, err
	}
	gotArchive, err := digestArchive(storedArchive)
	if err != nil {
		return details, err
	}
	if gotArchive != wantArchive {
		return details, fmt.Errorf("timeline archive verification failed: wrote %+v, read back %+v", wantArchive, gotArchive)
	}
	details.Archive = &wantArchive

	return details, nil
}

// MigrationRecord reports what the migration of this Store's database moved,
// when the database was built by one.
func (s *Store) MigrationRecord() (MigrationRecord, bool, error) {
	record, found, err := sqlitedb.ReadRecord(s.db)
	if err != nil || !found {
		return MigrationRecord{}, false, err
	}
	out := MigrationRecord{Record: record}
	if len(record.Details) > 0 {
		if err := json.Unmarshal(record.Details, &out.MigrationDetails); err != nil {
			return MigrationRecord{}, false, err
		}
	}
	return out, true, nil
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
