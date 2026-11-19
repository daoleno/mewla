//go:build linux

package browser

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/diag"
)

func chromeTime(t time.Time) int64 { return t.UnixMicro() - chromeEpochUnixMicros }

// writeCookies makes the profile's Cookies database hold exactly cookies, as
// Chrome keeps it (host_key, expires_utc since 1601, is_persistent), each
// HttpOnly unless a fourth value says 0.
func writeCookies(t *testing.T, profile string, cookies [][]any) {
	t.Helper()
	path := filepath.Join(profile, "Default", "Cookies")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS cookies (host_key TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', expires_utc INTEGER NOT NULL, is_persistent INTEGER NOT NULL, is_httponly INTEGER NOT NULL); DELETE FROM cookies`); err != nil {
		t.Fatal(err)
	}
	for _, c := range cookies {
		httpOnly := 1
		if len(c) > 3 {
			httpOnly = c[3].(int)
		}
		if _, err := db.Exec(`INSERT INTO cookies (host_key, expires_utc, is_persistent, is_httponly) VALUES (?, ?, ?, ?)`, c[0], c[1], c[2], httpOnly); err != nil {
			t.Fatal(err)
		}
	}
}

func signInEvents(since uint64) []diag.Event {
	found, _ := diag.After(since)
	var events []diag.Event
	for _, e := range found {
		if e.Kind == "browser.signins_lost" {
			events = append(events, e)
		}
	}
	return events
}

// A site whose cookies all went before they expired is named, in the Watch
// report and to the hook that tells Chat; one whose cookies expired, or that
// still has one, is not. The 11-18 19:48 case: an agent's unscoped
// clearCookies() took every site's sign-in at once.
func TestCheckSignInsSaysWhichSitesLostTheirSession(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "profile")
	now := time.Now().UTC()
	since := diag.Last()
	b := &LinuxBackend{}
	var told [][]string
	b.SetSignInsLost(func(sites []string, at time.Time) { told = append(told, sites) })

	// A profile Chrome never ran on has nothing to compare.
	b.noteSignIns(root)
	if events := signInEvents(since); len(events) != 0 || len(told) != 0 {
		t.Fatalf("a new profile reported %+v %v", events, told)
	}

	writeCookies(t, profile, [][]any{
		{".github.com", chromeTime(now.Add(30 * 24 * time.Hour)), 1},
		{"github.com", chromeTime(now.Add(time.Hour)), 1},
		{"app.freeride.ai", 0, 0}, // a session cookie
		{".google.com", chromeTime(now.Add(400 * 24 * time.Hour)), 1},
		{"short.example", chromeTime(now.Add(-time.Minute)), 1},               // expired since
		{".tracker.example", chromeTime(now.Add(400 * 24 * time.Hour)), 1, 0}, // no sign-in
		{"localhost", 0, 0}, // a Worker's own app
	})
	b.noteSignIns(root)
	if events := signInEvents(since); len(events) != 0 || len(told) != 0 {
		t.Fatalf("the first record reported %+v %v", events, told)
	}

	// short.example's cookie expired on its own, google kept a sign-in
	// cookie on one of its hosts, github and the session sign-in (of
	// freeride.ai, its registrable site) were removed, and so were the
	// cookies that are no sign-in.
	writeCookies(t, profile, [][]any{
		{"accounts.google.com", chromeTime(now.Add(24 * time.Hour)), 1},
	})
	b.noteSignIns(root)
	events := signInEvents(since)
	if len(events) != 1 {
		t.Fatalf("want one report, got %+v", events)
	}
	e := events[0]
	if e.Severity != diag.SeverityError || e.Subject != "agents' Chrome" {
		t.Fatalf("report: %+v", e)
	}
	if !strings.Contains(e.Detail, "2 site(s)") || !strings.HasSuffix(e.Detail, ": freeride.ai, github.com") {
		t.Fatalf("detail does not name exactly the lost sites: %q", e.Detail)
	}
	if !reflect.DeepEqual(told, [][]string{{"freeride.ai", "github.com"}}) {
		t.Fatalf("hook told %v", told)
	}

	// The record moved on: the same profile checked again reports nothing.
	since = diag.Last()
	b.noteSignIns(root)
	if events := signInEvents(since); len(events) != 0 || len(told) != 1 {
		t.Fatalf("a second check repeated %+v %v", events, told)
	}
	info, err := os.Stat(filepath.Join(root, signedInSitesFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("record: %v %v", info, err)
	}
}

// A wipe of a profile with weeks of browsing names every signed-in site, not
// the first few in the alphabet.
func TestSignInsLostNamesEverySite(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "profile")
	until := chromeTime(time.Now().Add(30 * 24 * time.Hour))
	var cookies [][]any
	for i := 0; i < 20; i++ {
		cookies = append(cookies, []any{fmt.Sprintf(".ads%02d.example", i), until, 1})
	}
	cookies = append(cookies, []any{".github.com", until, 1}, []any{"vercel.com", until, 1})
	writeCookies(t, profile, cookies)
	b := &LinuxBackend{}
	var told []string
	b.SetSignInsLost(func(sites []string, at time.Time) { told = sites })
	b.noteSignIns(root)
	since := diag.Last()
	writeCookies(t, profile, nil)
	b.noteSignIns(root)
	if len(told) != 22 || !slices.Contains(told, "github.com") || !slices.Contains(told, "vercel.com") {
		t.Fatalf("hook told %v", told)
	}
	events := signInEvents(since)
	if len(events) != 1 || !strings.Contains(events[0].Detail, "github.com") || !strings.Contains(events[0].Detail, "vercel.com") {
		t.Fatalf("report %+v", events)
	}
}

func TestSiteOf(t *testing.T) {
	for host, want := range map[string]string{
		".github.com": "github.com", "accounts.google.com": "google.com", ".google.co.jp": "google.co.jp",
		"0g-app-test.pages.dev": "0g-app-test.pages.dev", "localhost": "", "app.localhost": "", "127.0.0.1": "", "[::1]": "", "intranet": "",
	} {
		if got := siteOf(host); got != want {
			t.Errorf("siteOf(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestLostSignInsSkipsExpiredAndKept(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	before := map[string]time.Time{"a.example": now.Add(time.Hour), "b.example": now.Add(-time.Second), "c.example": sessionCookieExpiry}
	after := map[string]time.Time{"c.example": sessionCookieExpiry}
	if got := lostSignIns(before, after, now); !reflect.DeepEqual(got, []string{"a.example"}) {
		t.Fatalf("lost %v", got)
	}
}

// The check reads Chrome's database and never changes it (no journal mode or
// schema of Mewla's own).
func TestSignedInSitesLeavesChromesDatabaseAlone(t *testing.T) {
	profile := t.TempDir()
	writeCookies(t, profile, [][]any{{"github.com", 0, 0}})
	path := filepath.Join(profile, "Default", "Cookies")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sites, err := signedInSites(profile)
	if err != nil || len(sites) != 1 || !sites["github.com"].Equal(sessionCookieExpiry) {
		t.Fatalf("sites %v err %v", sites, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("reading the sign-ins changed Chrome's Cookies database")
	}
	if _, err := os.Stat(path + "-wal"); err == nil {
		t.Fatal("reading the sign-ins left a WAL beside Chrome's database")
	}
}
