package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/usage"
)

const install = "0123456789abcdef0123456789abcdef"

var clock = time.Date(2030, 3, 14, 15, 0, 0, 0, time.UTC)

func newTestHandler(t *testing.T) (*Handler, *Store, string, *[]string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "usage.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var logs []string
	h := New(Config{Store: store, Now: func() time.Time { return clock }, Logf: func(f string, a ...any) {
		logs = append(logs, fmt.Sprintf(f, a...))
	}})
	return h, store, path, &logs
}

func post(t *testing.T, h *Handler, path, body, remote string) int {
	t.Helper()
	mux := http.NewServeMux()
	h.Routes(mux)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code
}

func batchJSON(t *testing.T, b usage.Batch) string {
	t.Helper()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func countRows(t *testing.T, store *Store, table string) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEventsAreCountedPerDayNameInstallAndProps(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	b := usage.Batch{InstallID: install, Day: "2030-03-14", Events: []usage.Event{
		{Name: "login_started"},
		{Name: "pair_ok", Props: map[string]string{"transport": "relay", "platform": "android"}},
		{Name: "pair_ok", Props: map[string]string{"platform": "android", "transport": "relay"}},
	}}
	if code := post(t, h, EventsPath, batchJSON(t, b), "203.0.113.7:4000"); code != http.StatusNoContent {
		t.Fatalf("code = %d", code)
	}
	var n int
	if err := store.db.QueryRow(`SELECT n FROM events WHERE name = 'pair_ok' AND props = '{"platform":"android","transport":"relay"}'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("pair_ok n = %d, %v", n, err)
	}
}

// A path, a prompt or an address in any field is refused whole and never
// reaches the database or the log.
func TestContentIsRefusedAndNeverStored(t *testing.T) {
	sentinels := []string{"/home/alice/secret-project/plan.md", "please refactor the billing module", "10.1.2.3", "alice@example.com"}
	for _, sentinel := range sentinels {
		h, store, path, logs := newTestHandler(t)
		bodies := []string{
			batchJSON(t, usage.Batch{InstallID: install, Day: "2030-03-14", Events: []usage.Event{{Name: "pair_failed", Props: map[string]string{"reason": sentinel}}}}),
			batchJSON(t, usage.Batch{InstallID: install, Day: "2030-03-14", Events: []usage.Event{{Name: sentinel}}}),
			batchJSON(t, usage.Batch{InstallID: install, Day: "2030-03-14", Events: []usage.Event{{Name: "login_started", Props: map[string]string{sentinel: "true"}}}}),
			batchJSON(t, usage.Batch{InstallID: install, Day: "2030-03-14", Events: []usage.Event{{Name: "daily", Props: map[string]string{"agents": "claude," + sentinel}}}}),
			batchJSON(t, usage.Batch{InstallID: sentinel, Day: "2030-03-14", Events: []usage.Event{{Name: "login_started"}}}),
			`{"install_id":"` + install + `","day":"2030-03-14","events":[{"name":"login_started"}],"note":"` + sentinel + `"}`,
		}
		for i, body := range bodies {
			if code := post(t, h, EventsPath, body, "203.0.113.7:4000"); code != http.StatusBadRequest {
				t.Fatalf("%q body %d: code = %d, want 400", sentinel, i, code)
			}
		}
		if code := post(t, h, VisitPath, `{"page":"`+sentinel+`","ref":"direct"}`, "203.0.113.7:4000"); code != http.StatusBadRequest {
			t.Fatalf("visit code = %d", code)
		}
		if countRows(t, store, "events")+countRows(t, store, "visits")+countRows(t, store, "install_days") != 0 {
			t.Fatal("a refused batch was stored")
		}
		assertNotOnDisk(t, path, sentinel)
		for _, line := range *logs {
			if strings.Contains(line, sentinel) {
				t.Fatalf("log carries the sentinel: %s", line)
			}
		}
	}
}

// The client's address is used to rate-limit and is never written.
func TestAddressIsNeverStored(t *testing.T) {
	h, store, path, _ := newTestHandler(t)
	b := usage.Batch{InstallID: install, Day: "2030-03-14", Events: []usage.Event{{Name: "login_started"}}}
	if code := post(t, h, EventsPath, batchJSON(t, b), "198.51.100.23:5555"); code != http.StatusNoContent {
		t.Fatalf("code = %d", code)
	}
	if code := post(t, h, VisitPath, `{"page":"home","ref":"github"}`, "198.51.100.23:5555"); code != http.StatusNoContent {
		t.Fatalf("visit code = %d", code)
	}
	_ = store.Close()
	assertNotOnDisk(t, path, "198.51.100.23")
}

func assertNotOnDisk(t *testing.T, dbPath, needle string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		raw, err := os.ReadFile(dbPath + suffix)
		if err != nil {
			continue
		}
		if bytes.Contains(raw, []byte(needle)) {
			t.Fatalf("%s holds %q", filepath.Base(dbPath+suffix), needle)
		}
	}
}

func TestBodyIsCapped(t *testing.T) {
	h, _, _, _ := newTestHandler(t)
	big := `{"install_id":"` + install + `","day":"2030-03-14","events":[` + strings.Repeat(`{"name":"login_started"},`, 4000) + `{"name":"login_started"}]}`
	if code := post(t, h, EventsPath, big, "203.0.113.7:1"); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d", code)
	}
	if code := post(t, h, VisitPath, `{"page":"home","ref":"`+strings.Repeat("x", 600)+`"}`, "203.0.113.7:1"); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("visit code = %d", code)
	}
}

func TestInstallDailyCap(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	events := make([]usage.Event, usage.MaxEventsPerBatch)
	for i := range events {
		events[i] = usage.Event{Name: "login_started"}
	}
	b := usage.Batch{InstallID: install, Day: "2030-03-14", Events: events}
	accepted := 0
	for i := 0; i < MaxEventsPerInstallDay/usage.MaxEventsPerBatch+2; i++ {
		switch code := post(t, h, EventsPath, batchJSON(t, b), fmt.Sprintf("203.0.113.%d:1", i)); code {
		case http.StatusNoContent:
			accepted++
		case http.StatusTooManyRequests:
		default:
			t.Fatalf("code = %d", code)
		}
	}
	if accepted*usage.MaxEventsPerBatch != MaxEventsPerInstallDay {
		t.Fatalf("accepted %d batches", accepted)
	}
	var n int
	_ = store.db.QueryRow(`SELECT n FROM install_days`).Scan(&n)
	if n != MaxEventsPerInstallDay {
		t.Fatalf("counted %d", n)
	}
}

func TestAddressRateLimit(t *testing.T) {
	h, _, _, _ := newTestHandler(t)
	limited := 0
	for i := 0; i < eventsPerAddressHour+5; i++ {
		if post(t, h, EventsPath, `{}`, "[2001:db8:1:2::7]:1") == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited != 5 {
		t.Fatalf("limited %d", limited)
	}
	// Another address in the same /64 shares the limit; another /64 does not.
	if post(t, h, EventsPath, `{}`, "[2001:db8:1:2::9]:1") != http.StatusTooManyRequests {
		t.Fatal("same /64 was not limited")
	}
	if post(t, h, EventsPath, `{}`, "[2001:db8:1:3::9]:1") != http.StatusBadRequest {
		t.Fatal("another /64 was limited")
	}
}

func TestDaysOutsideTheWindowAreDropped(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	for _, day := range []string{"2030-03-01", "2030-03-16"} {
		b := usage.Batch{InstallID: install, Day: day, Events: []usage.Event{{Name: "login_started"}}}
		if code := post(t, h, EventsPath, batchJSON(t, b), "203.0.113.7:1"); code != http.StatusNoContent {
			t.Fatalf("%s code = %d", day, code)
		}
	}
	if countRows(t, store, "events") != 0 {
		t.Fatal("an out-of-window day was counted")
	}
}

func TestMaintainRollsUpDaysThatCanNoLongerChangeAndPurgesOldRows(t *testing.T) {
	_, store, _, _ := newTestHandler(t)
	ctx := context.Background()
	add := func(day, id string) {
		if err := store.AddBatch(ctx, usage.Batch{InstallID: id, Day: day, Events: []usage.Event{{Name: "daily", Props: map[string]string{"os": "linux"}}}}); err != nil {
			t.Fatal(err)
		}
	}
	other := strings.Repeat("f", 32)
	add("2029-11-01", install)
	add("2030-03-01", install)
	add("2030-03-10", install)
	if err := store.Maintain(ctx, clock); err != nil {
		t.Fatal(err)
	}
	// 03-10 is still inside the days a batch may name: not rolled yet, so a
	// late batch for it still counts once it is.
	var n int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM rollups WHERE day = '2030-03-10'`).Scan(&n)
	if n != 0 {
		t.Fatal("a day that can still change was rolled up")
	}
	add("2030-03-10", other)
	if err := store.Maintain(ctx, clock.AddDate(0, 0, 10)); err != nil {
		t.Fatal(err)
	}
	var events, installs int
	if err := store.db.QueryRow(`SELECT events, installs FROM rollups WHERE day = '2030-03-10'`).Scan(&events, &installs); err != nil || events != 2 || installs != 2 {
		t.Fatalf("rollup = %d/%d, %v", events, installs, err)
	}
	if countRows(t, store, "rollups") != 3 {
		t.Fatalf("rollups = %d, want 11-01, 03-01 and 03-10", countRows(t, store, "rollups"))
	}
	var old int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM events WHERE day = '2029-11-01'`).Scan(&old)
	if old != 0 {
		t.Fatal("raw rows past RawDays were kept")
	}
	// Running again keeps the rollups as they are.
	if err := store.Maintain(ctx, clock.AddDate(0, 0, 10)); err != nil {
		t.Fatal(err)
	}
	if countRows(t, store, "rollups") != 3 {
		t.Fatal("maintain is not idempotent")
	}
}

func TestVisitCounts(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	for i := 0; i < 3; i++ {
		if code := post(t, h, VisitPath, `{"page":"docs","ref":"hackernews"}`, "203.0.113.7:1"); code != http.StatusNoContent {
			t.Fatalf("code = %d", code)
		}
	}
	var n int
	if err := store.db.QueryRow(`SELECT n FROM visits WHERE day = '2030-03-14' AND page = 'docs' AND ref = 'hackernews'`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("n = %d, %v", n, err)
	}
}
