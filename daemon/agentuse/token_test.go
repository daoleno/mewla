package agentuse

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const fixtureKey = "_chrome-extension://" + ExtensionID + "\x00\x01auth-token"

// The fixtures were written by goleveldb with snappy tables, the same
// layout Chrome uses for its older localStorage.
func TestLevelDBReaderFindsTheNewestValue(t *testing.T) {
	cases := []struct {
		dir   string
		want  string
		found bool
	}{
		{"leveldb-compacted", "\x01" + tableKey, true},
		{"leveldb-newer", "\x01" + logKey, true},
		{"leveldb-deleted", "", false},
	}
	for _, c := range cases {
		value, found, err := leveldbLookup(filepath.Join("testdata", c.dir), []byte(fixtureKey))
		if err != nil {
			t.Fatalf("%s: %v", c.dir, err)
		}
		if found != c.found || string(value) != c.want {
			t.Fatalf("%s: got %q found=%v, want %q found=%v", c.dir, value, found, c.want, c.found)
		}
	}
}

func TestLevelDBReaderToleratesOtherFiles(t *testing.T) {
	dir := t.TempDir()
	copyDir(t, filepath.Join("testdata", "leveldb-compacted"), dir)
	for _, name := range []string{"CURRENT", "LOCK", "LOG", "MANIFEST-000001"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not a table"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, found, err := leveldbLookup(dir, []byte(fixtureKey)); err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

const (
	validKey = "endAriTgxnY7Ioqr-VLcI61jAkY0E0WUMjueI4p8HXQ"
	tableKey = "OldTokenInTable_aaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	logKey   = "NewerTokenInLog_bbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestTokenFromSQLiteStorage(t *testing.T) {
	profile := t.TempDir()
	writeSQLiteStorage(t, profile, append([]byte{1}, validKey...))
	token, err := readExtensionToken(profile)
	if err != nil || token != validKey {
		t.Fatalf("token %q err %v", token, err)
	}
}

func TestTokenPrefersTheStoreChromeWroteLast(t *testing.T) {
	profile := t.TempDir()
	legacy := filepath.Join(profile, "Local Storage", "leveldb")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	copyDir(t, filepath.Join("testdata", "leveldb-newer"), legacy)
	writeSQLiteStorage(t, profile, append([]byte{1}, validKey...))
	age := func(path string, d time.Duration) {
		when := time.Now().Add(-d)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"000004.ldb", "000005.log"} {
		age(filepath.Join(legacy, name), time.Minute)
	}
	age(filepath.Join(profile, "LocalStorage"), time.Hour)
	if token, err := readExtensionToken(profile); err != nil || token != logKey {
		t.Fatalf("leveldb written last: token %q err %v", token, err)
	}
	age(filepath.Join(profile, "LocalStorage"), 0)
	if token, err := readExtensionToken(profile); err != nil || token != validKey {
		t.Fatalf("SQLite written last: token %q err %v", token, err)
	}
}

func TestTokenMissingUntilTheExtensionMakesIt(t *testing.T) {
	profile := t.TempDir()
	writeSQLiteStorage(t, profile, nil)
	if _, err := readExtensionToken(profile); err != errNoToken {
		t.Fatalf("err %v, want errNoToken", err)
	}
	if _, err := readExtensionToken(t.TempDir()); err != errNoToken {
		t.Fatalf("empty profile: err %v, want errNoToken", err)
	}
}

func TestStorageStringsAndKeyShape(t *testing.T) {
	utf16 := []byte{0}
	for _, r := range validKey {
		utf16 = append(utf16, byte(r), 0)
	}
	if token, err := decodeStorageString(utf16); err != nil || token != validKey {
		t.Fatalf("utf16: %q %v", token, err)
	}
	for _, bad := range [][]byte{nil, {1}, append([]byte{1}, "short"...), append([]byte{1}, validKey+"\nEVIL=1"...)} {
		if _, err := decodeStorageString(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

// writeSQLiteStorage creates Chrome's SQLite localStorage schema (Chromium
// 151) with the extension's map and, when value is set, its key.
func writeSQLiteStorage(t *testing.T, profile string, value []byte) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(profile, "LocalStorage"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	statements := []string{
		`CREATE TABLE maps(row_id INTEGER PRIMARY KEY AUTOINCREMENT,storage_key BLOB NOT NULL,last_accessed INTEGER,last_modified INTEGER,total_size INTEGER)`,
		`CREATE UNIQUE INDEX maps_by_storage_key ON maps(storage_key)`,
		`CREATE TABLE map_entries(map_id INTEGER NOT NULL,value_compression_type INTEGER NOT NULL,key BLOB NOT NULL,value BLOB NOT NULL,PRIMARY KEY(map_id, key)) WITHOUT ROWID`,
		`CREATE TABLE meta(key LONGVARCHAR NOT NULL UNIQUE PRIMARY KEY, value LONGVARCHAR)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO maps(storage_key) VALUES (?)`, []byte("chrome-extension://"+ExtensionID+"/")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO maps(storage_key) VALUES (?)`, []byte("https://example.com/")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO map_entries VALUES (2, 0, ?, ?)`, append([]byte{1}, tokenItem...), append([]byte{1}, "someone-elses-value-xx"...)); err != nil {
		t.Fatal(err)
	}
	if value != nil {
		if _, err := db.Exec(`INSERT INTO map_entries VALUES (1, 0, ?, ?)`, append([]byte{1}, tokenItem...), value); err != nil {
			t.Fatal(err)
		}
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(from, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, entry.Name()), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
