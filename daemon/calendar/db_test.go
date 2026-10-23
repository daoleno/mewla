package calendar

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// historyDocument is a calendar.json whose recurring action has a long run
// history, as a long-lived one does.
func historyDocument(t *testing.T, runs int) (document, Item) {
	t.Helper()
	at := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	item := Item{
		ID: "daily", Title: "Daily", Kind: KindScheduledAction, Status: StatusScheduled, DueAt: &at,
		NextAt: at.Add(24 * time.Hour * time.Duration(runs)), Timezone: "UTC", Recurrence: RecurrenceDaily,
		ActionInstruction: "Run it", SourceThreadID: "thread-1", CreatedAt: at, UpdatedAt: at, Revision: 1,
	}
	for index := 0; index < runs; index++ {
		finished := at.Add(24*time.Hour*time.Duration(index) + time.Minute)
		item.Runs = append(item.Runs, Run{
			ID: fmt.Sprintf("run-%04d", index), Title: "Daily", SourceThreadID: "thread-1",
			ScheduledFor: at.Add(24 * time.Hour * time.Duration(index)), StartedAt: at, FinishedAt: &finished,
			Status: StatusCompleted, Result: strings.Repeat("result ", 20),
		})
	}
	other := Item{
		ID: "once", Title: "Once", Kind: KindReminder, Status: StatusScheduled, NotifyAt: &at,
		NextAt: at, Timezone: "UTC", Recurrence: RecurrenceNone, CreatedAt: at, UpdatedAt: at, Revision: 1,
	}
	return document{SchemaVersion: SchemaVersion, Items: []Item{item, other}}, item
}

func TestMigrationMovesItemsAndEveryRunAndRecordsWhatItChecked(t *testing.T) {
	root := t.TempDir()
	doc, daily := historyDocument(t, 400)
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeCalendarFixture(t, root, raw)
	// A schema backup and a crash-left temp file go to the backup with it.
	for _, name := range []string{"calendar.json.schema1.bak", ".calendar.json.tmp-123"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("daily")
	if err != nil || !reflect.DeepEqual(got, cloneItem(daily)) {
		t.Fatalf("migrated item differs: err=%v\nwant %d runs\ngot  %d runs", err, len(daily.Runs), len(got.Runs))
	}
	record, found, err := sqlitedb.ReadRecord(store.db)
	if err != nil || !found {
		t.Fatalf("record found=%v err=%v", found, err)
	}
	var details MigrationDetails
	if err := json.Unmarshal(record.Details, &details); err != nil {
		t.Fatal(err)
	}
	want, err := digestItems(map[string]Item{"daily": daily, "once": doc.Items[1]})
	if err != nil || details != want || details.Items != 2 || details.Runs != 400 {
		t.Fatalf("details = %+v, want %+v err=%v", details, want, err)
	}
	for _, name := range []string{"calendar.json", "calendar.json.schema1.bak", ".calendar.json.tmp-123"} {
		if _, err := os.Stat(filepath.Join(record.BackupDir, filepath.Base(root), name)); err != nil {
			t.Fatalf("%s not in the backup: %v", name, err)
		}
	}
	// calendar.json is a tombstone: the strict decoder an older binary used
	// refuses it, so that binary stops instead of starting an empty calendar.
	if _, err := decodeDocument(mustRead(t, filepath.Join(root, legacyFile))); err == nil {
		t.Fatal("tombstone is accepted as a calendar document")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCommitsWriteTheRunTheyTouchNotTheHistory(t *testing.T) {
	root := t.TempDir()
	doc, _ := historyDocument(t, 400)
	raw, _ := json.Marshal(doc)
	writeCalendarFixture(t, root, raw)
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE writes(what TEXT)`,
		`CREATE TRIGGER count_run_insert AFTER INSERT ON cal_run BEGIN INSERT INTO writes VALUES('run'); END`,
		`CREATE TRIGGER count_run_update AFTER UPDATE ON cal_run BEGIN INSERT INTO writes VALUES('run'); END`,
		`CREATE TRIGGER count_item_update AFTER UPDATE ON cal_item BEGIN INSERT INTO writes VALUES('item'); END`,
	} {
		if _, err := store.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2027, 10, 1, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	_, run, err := store.Claim("daily", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun("daily", run.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	var runs, items int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM writes WHERE what = 'run'`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM writes WHERE what = 'item'`).Scan(&items); err != nil {
		t.Fatal(err)
	}
	// Claim appends the run (1), finish updates it (1); the item row is
	// rewritten once per commit. Nothing else in 400 runs is touched.
	if runs != 2 || items != 2 {
		t.Fatalf("claim+finish wrote %d run rows and %d item rows, want 2 and 2", runs, items)
	}
	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted.List(), store.List()) {
		t.Fatal("restart did not read back what was committed")
	}
}
