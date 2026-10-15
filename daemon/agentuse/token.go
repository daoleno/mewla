package agentuse

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	_ "modernc.org/sqlite"
)

// ExtensionID is the Playwright Extension in the Chrome Web Store. Its
// manifest carries a fixed key, so an unpacked build has the same ID.
const ExtensionID = "mmlmfjhmonkocbjadbfplnigmagldckm"

const tokenItem = "auth-token"

// errNoToken means the extension has not made its key yet: it does so the
// first time its status or connect page opens.
var errNoToken = errors.New("the extension has not made its key yet")

// readExtensionToken reads the key the extension keeps in its own
// localStorage. Chrome is moving localStorage from leveldb to SQLite and a
// profile can hold both, each with a different key. While Chrome runs, the
// store it has open (it holds a lock on it) is the only one that counts;
// otherwise the store written last does.
func readExtensionToken(profileDir string) (string, error) {
	sqliteOpen := lockedByAnotherProcess(filepath.Join(profileDir, "LocalStorage"), sqliteLockOffset, sqliteLockLength)
	legacyOpen := lockedByAnotherProcess(filepath.Join(profileDir, "Local Storage", "leveldb", "LOCK"), 0, 0)
	readers := []func(string) (string, time.Time, error){sqliteToken, legacyToken}
	switch {
	case sqliteOpen && !legacyOpen:
		readers = readers[:1]
	case legacyOpen && !sqliteOpen:
		readers = readers[1:]
	}
	type candidate struct {
		token    string
		modified time.Time
	}
	var found []candidate
	var errs []error
	for _, read := range readers {
		token, modified, err := read(profileDir)
		switch {
		case err == nil:
			found = append(found, candidate{token, modified})
		case !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errNoToken):
			errs = append(errs, err)
		}
	}
	if len(found) == 0 {
		if len(errs) > 0 {
			return "", errors.Join(errs...)
		}
		return "", errNoToken
	}
	best := found[0]
	for _, c := range found[1:] {
		if c.modified.After(best.modified) {
			best = c
		}
	}
	return best.token, nil
}

// SQLite's unix locks live in a byte range at 1 GiB, past any real data.
const (
	sqliteLockOffset = 0x40000000
	sqliteLockLength = 512
)

// sqliteToken reads Chrome's SQLite localStorage. The database is copied
// first (with its write-ahead log) so Chrome's open handle is never touched.
func sqliteToken(profileDir string) (string, time.Time, error) {
	source := filepath.Join(profileDir, "LocalStorage")
	info, err := os.Stat(source)
	if err != nil {
		return "", time.Time{}, err
	}
	modified := latestModified(info.ModTime(), source+"-wal")
	scratch, err := os.MkdirTemp("", "mewla-chrome-storage-")
	if err != nil {
		return "", time.Time{}, err
	}
	defer os.RemoveAll(scratch)
	for _, suffix := range []string{"", "-wal"} {
		if err := copyPrivate(source+suffix, filepath.Join(scratch, "LocalStorage"+suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", time.Time{}, err
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(scratch, "LocalStorage"))
	if err != nil {
		return "", time.Time{}, err
	}
	defer db.Close()
	var raw []byte
	err = db.QueryRow(`SELECT e.value FROM map_entries e JOIN maps m ON m.row_id = e.map_id
		WHERE CAST(m.storage_key AS TEXT) = ? AND e.key = ? AND e.value_compression_type = 0`,
		"chrome-extension://"+ExtensionID+"/", append([]byte{1}, tokenItem...)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, errNoToken
	}
	if err != nil {
		return "", time.Time{}, err
	}
	token, err := decodeStorageString(raw)
	return token, modified, err
}

// legacyToken reads the older leveldb localStorage.
func legacyToken(profileDir string) (string, time.Time, error) {
	dir := filepath.Join(profileDir, "Local Storage", "leveldb")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}, err
	}
	var modified time.Time
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && info.ModTime().After(modified) {
			modified = info.ModTime()
		}
	}
	key := append([]byte("_chrome-extension://"+ExtensionID+"\x00\x01"), tokenItem...)
	raw, found, err := leveldbLookup(dir, key)
	if err != nil && !found {
		return "", time.Time{}, err
	}
	if !found {
		return "", time.Time{}, errNoToken
	}
	token, err := decodeStorageString(raw)
	return token, modified, err
}

// decodeStorageString undoes Chrome's localStorage value encoding: a leading
// 1 marks Latin-1, a leading 0 marks UTF-16LE.
func decodeStorageString(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("empty storage value")
	}
	var token string
	switch raw[0] {
	case 1:
		runes := make([]rune, len(raw)-1)
		for i, b := range raw[1:] {
			runes[i] = rune(b)
		}
		token = string(runes)
	case 0:
		body := raw[1:]
		if len(body)%2 != 0 {
			return "", errors.New("bad UTF-16 storage value")
		}
		units := make([]uint16, len(body)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(body[2*i:])
		}
		token = string(utf16.Decode(units))
	default:
		return "", errors.New("unknown storage value encoding")
	}
	token = strings.TrimSpace(token)
	if !validToken(token) {
		return "", errors.New("the extension's key has an unexpected shape")
	}
	return token, nil
}

// validToken accepts the extension's base64url keys and nothing that could
// smuggle other text into an environment variable.
func validToken(token string) bool {
	if len(token) < 16 || len(token) > 256 {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '=') {
			return false
		}
	}
	return true
}

func latestModified(base time.Time, paths ...string) time.Time {
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.ModTime().After(base) {
			base = info.ModTime()
		}
	}
	return base
}

func copyPrivate(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
