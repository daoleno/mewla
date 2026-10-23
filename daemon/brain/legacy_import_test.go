package brain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/sqlitedb"
	"github.com/daoleno/mewla/daemon/watcher"
)

// richStore is a Store with Work, lifecycle Events, Turns, admissions,
// Work Events and timeline rows, so a migration has something of every kind
// to carry across.
func richStore(t *testing.T, root string) *Store {
	t.Helper()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		sessionID := fmt.Sprintf("mewla-worker-migrate:@%d", i+1)
		item, err := store.CreateWork(Work{
			Title: fmt.Sprintf("Migrated Work %d", i), Objective: "Survive the move to SQLite.",
			Status: WorkRunning, AttemptSessionID: sessionID, SourceThreadID: "thread-migrate",
			CompletionPolicy: CompletionBounded, NextAction: "Wait.", WaitFor: "Session " + sessionID,
		})
		if err != nil {
			t.Fatal(err)
		}
		bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
			SessionID: sessionID, TurnID: sessionID + ":turn:1", AcceptedAt: now,
			ProcessIdentity: "proc", PaneGeneration: "pane", PayloadSHA256: "digest",
		})
		if _, _, err := store.AppendWorkEvent(WorkEvent{
			WorkID: item.ID, Kind: "note", DedupeKey: fmt.Sprintf("note-%d", i), Summary: "kept",
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		role, kind := "user", timelineKindUserMessage
		if i%2 == 1 {
			role, kind = "assistant", timelineKindAssistantMessage
		}
		if _, err := store.AppendTimelineItem(TimelineItem{
			ID: fmt.Sprintf("row-%d", i), ThreadID: "thread-migrate", SessionID: "host", Role: role, Kind: kind,
			Body: fmt.Sprintf("message %d", i), CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

// exportLegacyState writes store's content in the pre-SQLite file formats.
func exportLegacyState(t *testing.T, store *Store, stateDir string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	works := map[string]json.RawMessage{}
	for _, st := range store.fsm.ListStates() {
		raw, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		works[string(st.ID)] = raw
	}
	var events []json.RawMessage
	if err := scanBodies(store.db, `SELECT body FROM lc_event ORDER BY seq`, func(body []byte) error {
		events = append(events, append(json.RawMessage(nil), body...))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var nextSeq uint64
	if err := store.db.QueryRow(`SELECT value FROM lc_meta WHERE key = 'next_seq'`).Scan(&nextSeq); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(stateDir, "lifecycle", "state.json"), map[string]any{
		"schema": 2, "next_seq": nextSeq, "works": works, "events": events,
	})
	image := clonePresentationDatabase(store.presentation)
	if image.BrainInputAdmissions == nil {
		image.BrainInputAdmissions = []BrainInputAdmission{}
	}
	if image.BrainWork == nil {
		image.BrainWork = []Work{}
	}
	if image.BrainWorkEvents == nil {
		image.BrainWorkEvents = []WorkEvent{}
	}
	if image.BrainTurns == nil {
		image.BrainTurns = []TurnRecord{}
	}
	writeJSON(t, filepath.Join(stateDir, "presentation.json"), image)
	var lines []string
	if err := scanBodies(store.db, `SELECT body FROM timeline ORDER BY seq`, func(body []byte) error {
		lines = append(lines, string(body))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "messages.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

type imageDigests struct {
	Lifecycle    lifecycle.Digest
	Presentation presentationDigest
	Timeline     timelineDigest
	Archive      timelineDigest
}

// digestsOf summarizes everything durable in store by what a reader sees.
func digestsOf(t *testing.T, store *Store) imageDigests {
	t.Helper()
	lc, err := lifecycle.ReadDigest(store.db)
	if err != nil {
		t.Fatal(err)
	}
	image, _, err := readPresentationImage(store.db)
	if err != nil {
		t.Fatal(err)
	}
	pd, err := digestPresentation(image)
	if err != nil {
		t.Fatal(err)
	}
	var items []TimelineItem
	if err := scanBodies(store.db, `SELECT body FROM timeline ORDER BY seq`, func(body []byte) error {
		var item TimelineItem
		if err := json.Unmarshal(body, &item); err != nil {
			return err
		}
		items = append(items, item)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	td, err := digestTimeline(items)
	if err != nil {
		t.Fatal(err)
	}
	var archive []archivedItem
	rows, err := store.db.Query(`SELECT origin, body FROM timeline_archive ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var origin, body string
		if err := rows.Scan(&origin, &body); err != nil {
			t.Fatal(err)
		}
		var item TimelineItem
		if err := json.Unmarshal([]byte(body), &item); err != nil {
			t.Fatal(err)
		}
		archive = append(archive, archivedItem{origin: origin, item: item})
	}
	_ = rows.Close()
	ad, err := digestArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	return imageDigests{Lifecycle: lc, Presentation: pd, Timeline: td, Archive: ad}
}

// legacyRoot builds a Brain root holding only pre-SQLite state files. source
// is the image those files hold.
func legacyRoot(t *testing.T) (root string, source imageDigests) {
	t.Helper()
	original := richStore(t, t.TempDir())
	source = digestsOf(t, original)
	root = t.TempDir()
	exportLegacyState(t, original, filepath.Join(root, "state"))
	return root, source
}

// sameShape compares what a restarted daemon must still hold. Startup repairs
// derived projections and stamps them with the wall clock, so hashes of a
// started Store differ from the image that was migrated; the migration record
// (requireRecord) is where exact equality is proved.
func sameShape(a, b imageDigests) bool {
	return a.Lifecycle.Works == b.Lifecycle.Works && a.Lifecycle.Events == b.Lifecycle.Events &&
		a.Lifecycle.NextSeq == b.Lifecycle.NextSeq && a.Lifecycle.EventsHash == b.Lifecycle.EventsHash &&
		a.Presentation.Works == b.Presentation.Works && a.Presentation.Events == b.Presentation.Events &&
		a.Presentation.Turns == b.Presentation.Turns && a.Presentation.Admissions == b.Presentation.Admissions &&
		a.Timeline == b.Timeline
}

// requireRecord checks the migration record holds exactly source's counts and
// hashes: the image the old files held is the image that was written.
func requireRecord(t *testing.T, store *Store, source imageDigests) {
	t.Helper()
	record, found, err := store.MigrationRecord()
	if err != nil || !found {
		t.Fatalf("migration record found=%v err=%v", found, err)
	}
	if *record.Lifecycle != source.Lifecycle || *record.Presentation != source.Presentation || *record.Timeline != source.Timeline {
		t.Fatalf("record differs from the old files:\nwant %+v\ngot  lifecycle=%+v presentation=%+v timeline=%+v",
			source, *record.Lifecycle, *record.Presentation, *record.Timeline)
	}
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files
}

// withoutLock drops the migration lock file, which every attempt leaves behind.
func withoutLock(files []string) []string {
	var out []string
	for _, file := range files {
		if file != sqlitedb.LockName {
			out = append(out, file)
		}
	}
	return out
}

func backupDirs(t *testing.T, root string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, legacyBackupPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestMigrationCarriesEveryRowAndSetsOldFilesAside(t *testing.T) {
	root, source := legacyRoot(t)
	stateDir := filepath.Join(root, "state")
	// Repair leftovers the old store accumulated.
	for name, body := range map[string]string{
		"presentation.json.pre-digest-repair-20261008-153901": `{"old":"copy"}`,
		".presentation.json.tmp-3712114679":                   `{"half":"written"`,
		"messages-110807379.jsonl":                            "",
		"lifecycle/state.json.schema1.bak":                    `{"schema":1}`,
		"lifecycle/events.jsonl":                              "old log\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(stateDir, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	archive := `{"id":"archived-only","thread_id":"thread-migrate","session_id":"host","role":"assistant","kind":"assistant_message","body":"history the live timeline dropped","created_at":"2026-05-01T00:00:00Z"}` + "\n" +
		`{"id":"row-0","thread_id":"thread-migrate","session_id":"host","role":"user","kind":"user_message","body":"message 0","created_at":"2026-09-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(stateDir, "messages-110807379.jsonl"), []byte(archive), 0o600); err != nil {
		t.Fatal(err)
	}
	// Files that are not Brain's former store stay where they are.
	for _, name := range []string{"chat_state.json", "memory.sqlite", "work_transcripts.jsonl"} {
		if err := os.WriteFile(filepath.Join(stateDir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	migrated, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got := digestsOf(t, migrated)
	if !sameShape(got, source) {
		t.Fatalf("migrated image differs:\nwant %+v\ngot  %+v", source, got)
	}
	requireRecord(t, migrated, source)
	if got.Archive.Rows != 1 {
		t.Fatalf("archive rows = %d, want only the row the live timeline lacks", got.Archive.Rows)
	}
	record, found, err := migrated.MigrationRecord()
	if err != nil || !found {
		t.Fatalf("migration record found=%v err=%v", found, err)
	}
	dirs := backupDirs(t, root)
	if len(dirs) != 1 || dirs[0] != record.BackupDir {
		t.Fatalf("backup dirs = %v, record says %s", dirs, record.BackupDir)
	}
	backedUp := listFiles(t, filepath.Join(record.BackupDir, "state"))
	want := []string{
		".presentation.json.tmp-3712114679",
		"lifecycle/events.jsonl", "lifecycle/state.json", "lifecycle/state.json.schema1.bak",
		"messages-110807379.jsonl", "messages.jsonl",
		"presentation.json", "presentation.json.pre-digest-repair-20261008-153901",
	}
	if strings.Join(backedUp, ",") != strings.Join(want, ",") {
		t.Fatalf("backup holds %v, want %v", backedUp, want)
	}
	live := listFiles(t, stateDir)
	for _, file := range live {
		for _, old := range []string{"presentation.json", "messages", ".presentation", "lifecycle/events", "lifecycle/state.json.schema1"} {
			if strings.HasPrefix(file, old) {
				t.Fatalf("old file %s is still in the state directory: %v", file, live)
			}
		}
	}
	for _, kept := range []string{"chat_state.json", "memory.sqlite", "work_transcripts.jsonl", stateDatabaseName} {
		if _, err := os.Stat(filepath.Join(stateDir, kept)); err != nil {
			t.Fatalf("%s: %v", kept, err)
		}
	}
	// The tombstone makes an older binary refuse to start instead of opening
	// an empty Brain.
	raw, err := os.ReadFile(filepath.Join(stateDir, "lifecycle", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tombstone struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(raw, &tombstone); err != nil || tombstone.Schema != tombstoneSchema {
		t.Fatalf("tombstone = %s err=%v", raw, err)
	}
	// Content a reader sees survives: the thread timeline and the Work.
	items, err := migrated.ThreadTimeline("thread-migrate", 0)
	if err != nil || len(items) != 5 || items[4].Body != "message 4" {
		t.Fatalf("timeline = %d items err=%v", len(items), err)
	}
	works, err := migrated.ListWork()
	if err != nil || len(works) != 3 {
		t.Fatalf("works = %d err=%v", len(works), err)
	}
	// A second start finds nothing to migrate and changes nothing.
	again, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if after := digestsOf(t, again); !sameShape(after, got) || after.Archive != got.Archive {
		t.Fatalf("second start changed the image: %+v", after)
	}
	if len(backupDirs(t, root)) != 1 {
		t.Fatalf("second start made another backup: %v", backupDirs(t, root))
	}
}

func TestMigrationResumesAfterCrashBeforeCommitPoint(t *testing.T) {
	root, source := legacyRoot(t)
	stateDir := filepath.Join(root, "state")
	// A previous attempt died while building: a partial database and its
	// sidecars are left under the temporary name.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.WriteFile(filepath.Join(stateDir, stateDatabaseName+sqlitedb.MigratingSuffix+suffix), []byte("not a database"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := listFiles(t, stateDir)

	migrated, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := digestsOf(t, migrated); !sameShape(got, source) {
		t.Fatalf("rerun produced a different image: %+v", got)
	}
	requireRecord(t, migrated, source)
	for _, leftover := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(filepath.Join(stateDir, stateDatabaseName+sqlitedb.MigratingSuffix+leftover)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary database %q survived: %v", leftover, err)
		}
	}
	if len(before) == 0 || len(backupDirs(t, root)) != 1 {
		t.Fatalf("backups = %v", backupDirs(t, root))
	}
}

func TestFailedMigrationLeavesOldFilesUntouchedAndRerunsCleanly(t *testing.T) {
	root, source := legacyRoot(t)
	stateDir := filepath.Join(root, "state")
	good, err := os.ReadFile(filepath.Join(stateDir, "presentation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "presentation.json"), []byte(`{"schema_version":12}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := listFiles(t, stateDir)
	if _, err := NewStore(root); !errors.Is(err, ErrSchedulerStateReset) {
		t.Fatalf("bad presentation was not refused: %v", err)
	}
	if after := listFiles(t, stateDir); strings.Join(before, ",") != strings.Join(withoutLock(after), ",") {
		t.Fatalf("failed migration changed the directory:\nbefore %v\nafter  %v", before, after)
	}
	if len(backupDirs(t, root)) != 0 {
		t.Fatalf("failed migration made a backup: %v", backupDirs(t, root))
	}
	// Fixing the input and starting again completes it.
	if err := os.WriteFile(filepath.Join(stateDir, "presentation.json"), good, 0o600); err != nil {
		t.Fatal(err)
	}
	migrated, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := digestsOf(t, migrated); !sameShape(got, source) {
		t.Fatalf("rerun image differs: %+v", got)
	}
	requireRecord(t, migrated, source)
}

func TestMigrationFinishesSettingAsideAfterCrashPastTheCommitPoint(t *testing.T) {
	root, source := legacyRoot(t)
	stateDir := filepath.Join(root, "state")
	migrated, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := migrated.MigrationRecord()
	if err != nil {
		t.Fatal(err)
	}
	// The database was renamed into place, then the process died partway
	// through moving the old files: some are back where they were, the rest
	// are in the backup.
	moved := filepath.Join(record.BackupDir, "state", "messages.jsonl")
	if err := os.Rename(moved, filepath.Join(stateDir, "messages.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(stateDir, "lifecycle", "state.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(record.BackupDir, "state", "lifecycle", "state.json"), filepath.Join(stateDir, "lifecycle", "state.json")); err != nil {
		t.Fatal(err)
	}

	resumed, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := digestsOf(t, resumed); !sameShape(got, source) {
		t.Fatalf("resumed image differs: %+v", got)
	}
	requireRecord(t, resumed, source)
	for _, rel := range []string{"messages.jsonl", "lifecycle/state.json", "presentation.json"} {
		if _, err := os.Stat(filepath.Join(record.BackupDir, "state", filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s is not in the backup: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(stateDir, "messages.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("messages.jsonl still in the state directory: %v", err)
	}
	if len(backupDirs(t, root)) != 1 {
		t.Fatalf("backups = %v", backupDirs(t, root))
	}
}

func TestDatabaseAndOldFilesWithoutARecordAreRefused(t *testing.T) {
	root, _ := legacyRoot(t)
	stateDir := filepath.Join(root, "state")
	// A database that did not come from a migration, beside old files.
	fresh, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = fresh
	other := filepath.Join(fresh.StateDir(), stateDatabaseName)
	raw, err := os.ReadFile(other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if raw, err = os.ReadFile(other); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, stateDatabaseName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(root); err == nil || !strings.Contains(err.Error(), "refusing to guess") {
		t.Fatalf("ambiguous state was not refused: %v", err)
	}
}

func TestNeitherDatabaseNorOldFilesStartsEmpty(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.MigrationRecord(); err != nil || found {
		t.Fatalf("fresh store has a migration record: found=%v err=%v", found, err)
	}
	if len(backupDirs(t, root)) != 0 {
		t.Fatalf("fresh store made a backup")
	}
	works, err := store.ListWork()
	if err != nil || len(works) != 0 {
		t.Fatalf("works=%d err=%v", len(works), err)
	}
}

func TestMigrationToleratesATornFinalLineAndRepeatedIDs(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	line := func(id, body string) string {
		return fmt.Sprintf(`{"id":%q,"thread_id":"t","session_id":"s","role":"user","kind":"user_message","body":%q,"created_at":"2026-09-01T00:00:00Z"}`+"\n", id, body)
	}
	content := line("a", "first") + line("a", "repeated, ignored") + line("b", "second") + `{"id":"c","thread_id":"t","session_i`
	if err := os.WriteFile(filepath.Join(stateDir, "messages.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline("t", 0)
	if err != nil || len(items) != 2 || items[0].Body != "first" || items[1].ID != "b" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	record, _, _ := store.MigrationRecord()
	if len(record.Notes) != 2 {
		t.Fatalf("notes = %v", record.Notes)
	}
	// A bad line that is not the torn tail is refused, not skipped.
	badRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(badRoot, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badRoot, "state", "messages.jsonl"), []byte("garbage\n"+line("a", "x")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(badRoot); err == nil || !strings.Contains(err.Error(), "decode messages.jsonl line 1") {
		t.Fatalf("corrupt line was skipped: %v", err)
	}
}

func TestConcurrentStartsMigrateExactlyOnce(t *testing.T) {
	root, source := legacyRoot(t)
	var wg sync.WaitGroup
	stores := make([]*Store, 4)
	errs := make([]error, len(stores))
	for index := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stores[index], errs[index] = NewStore(root)
		}()
	}
	wg.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("start %d: %v", index, err)
		}
	}
	if len(backupDirs(t, root)) != 1 {
		t.Fatalf("backups = %v", backupDirs(t, root))
	}
	for _, store := range stores {
		if got := digestsOf(t, store); !sameShape(got, source) {
			t.Fatalf("image differs: %+v", got)
		}
	}
	requireRecord(t, stores[0], source)
}

// TestMigrationOfARealStateCopy migrates a copy of an actual Brain root and
// checks the move the way the daemon does, plus the headline numbers. Set
// MEWLA_REAL_BRAIN_COPY to a directory shaped like ~/.mewla/brain (it must
// contain state/); the test works on a copy of it and never writes to it.
func TestMigrationOfARealStateCopy(t *testing.T) {
	source := os.Getenv("MEWLA_REAL_BRAIN_COPY")
	if source == "" {
		t.Skip("MEWLA_REAL_BRAIN_COPY is not set")
	}
	root := t.TempDir()
	if err := copyTree(filepath.Join(source, "state"), filepath.Join(root, "state")); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	record, found, err := store.MigrationRecord()
	if err != nil || !found {
		t.Fatalf("record found=%v err=%v", found, err)
	}
	recorded, _ := json.MarshalIndent(record, "", "  ")
	t.Logf("migrated in %s\n%s", time.Since(started), recorded)
	// Starting repairs derived projections and restamps them, so after the
	// start the counts, the event log and the timeline must match the record;
	// the record itself holds the exact hashes of the old files.
	want := imageDigests{Lifecycle: *record.Lifecycle, Presentation: *record.Presentation, Timeline: *record.Timeline, Archive: *record.Archive}
	if got := digestsOf(t, store); !sameShape(got, want) || got.Archive != want.Archive {
		t.Fatalf("database no longer matches its record:\nrecord %+v\ngot    %+v", want, got)
	}
	if out := os.Getenv("MEWLA_MIGRATION_RECORD"); out != "" {
		if err := os.WriteFile(out, recorded, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		target := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o600)
	})
}
