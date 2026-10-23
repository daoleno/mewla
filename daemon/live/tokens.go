package live

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// TokenPrefix marks a widget snapshot token. The token is a bearer for one
// read-only route (GET /widget/snapshot) and nothing else; it is not, and
// can never be exchanged for, a device or pairing credential.
const TokenPrefix = "mwls_"

const tokenFileName = "widget_tokens.json"

var errMalformedToken = errors.New("malformed widget token")

// TokenEntry is what the store keeps about a token. Only the hash of the
// secret is stored; the plaintext exists once, in Issue's return value.
type TokenEntry struct {
	ID        string    `json:"id"`
	DeviceID  string    `json:"device_id"`
	CreatedAt time.Time `json:"created_at"`
	Hash      string    `json:"hash"`
}

type tokenFile struct {
	Version int          `json:"version"`
	Tokens  []TokenEntry `json:"tokens"`
}

// TokenStore persists revocable widget snapshot tokens, one per device.
type TokenStore struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	entries map[string]TokenEntry // by token id
}

// OpenTokenStore loads (or starts) the store in the daemon's state directory.
func OpenTokenStore(dir string) (*TokenStore, error) {
	store := &TokenStore{
		path:    filepath.Join(dir, tokenFileName),
		now:     time.Now,
		entries: map[string]TokenEntry{},
	}
	raw, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	var file tokenFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	for _, entry := range file.Tokens {
		if entry.ID != "" && entry.DeviceID != "" && entry.Hash != "" {
			store.entries[entry.ID] = entry
		}
	}
	return store, nil
}

// Issue creates the device's token, replacing (revoking) any earlier one.
// The returned plaintext is shown once and never stored.
func (s *TokenStore) Issue(deviceID string) (string, TokenEntry, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return "", TokenEntry{}, errors.New("a widget token needs a device")
	}
	id, err := randomHex(8)
	if err != nil {
		return "", TokenEntry{}, err
	}
	secret, err := randomHex(32)
	if err != nil {
		return "", TokenEntry{}, err
	}
	entry := TokenEntry{ID: id, DeviceID: deviceID, CreatedAt: s.now().UTC(), Hash: hashSecret(secret)}

	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]TokenEntry, len(s.entries)+1)
	for key, value := range s.entries {
		if value.DeviceID != deviceID {
			next[key] = value
		}
	}
	next[id] = entry
	if err := s.saveLocked(next); err != nil {
		return "", TokenEntry{}, err
	}
	s.entries = next
	return TokenPrefix + id + "." + secret, entry, nil
}

// Verify checks a presented token in constant time.
func (s *TokenStore) Verify(token string) (TokenEntry, bool) {
	id, secret, err := splitToken(token)
	if err != nil {
		return TokenEntry{}, false
	}
	s.mu.Lock()
	entry, found := s.entries[id]
	s.mu.Unlock()
	want := entry.Hash
	if !found {
		// Same work either way, so a missing id is not a faster answer.
		want = hashSecret("")
	}
	match := subtle.ConstantTimeCompare([]byte(hashSecret(secret)), []byte(want)) == 1
	if !found || !match {
		return TokenEntry{}, false
	}
	return entry, true
}

// Revoke removes one token by id.
func (s *TokenStore) Revoke(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return false
	}
	next := cloneWithout(s.entries, func(entry TokenEntry) bool { return entry.ID == id })
	if err := s.saveLocked(next); err != nil {
		return false
	}
	s.entries = next
	return true
}

// RevokeDevice removes every token the device holds and reports how many.
func (s *TokenStore) RevokeDevice(deviceID string) int {
	deviceID = strings.TrimSpace(deviceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneWithout(s.entries, func(entry TokenEntry) bool { return entry.DeviceID == deviceID })
	removed := len(s.entries) - len(next)
	if removed == 0 {
		return 0
	}
	if err := s.saveLocked(next); err != nil {
		return 0
	}
	s.entries = next
	return removed
}

// DeviceEntry returns the device's token entry, if it has one.
func (s *TokenStore) DeviceEntry(deviceID string) (TokenEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.entries {
		if entry.DeviceID == deviceID {
			return entry, true
		}
	}
	return TokenEntry{}, false
}

func (s *TokenStore) saveLocked(entries map[string]TokenEntry) error {
	file := tokenFile{Version: 1, Tokens: make([]TokenEntry, 0, len(entries))}
	for _, entry := range entries {
		file.Tokens = append(file.Tokens, entry)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(s.path, data, 0o600)
}

func cloneWithout(entries map[string]TokenEntry, drop func(TokenEntry) bool) map[string]TokenEntry {
	next := make(map[string]TokenEntry, len(entries))
	for key, entry := range entries {
		if !drop(entry) {
			next[key] = entry
		}
	}
	return next
}

func splitToken(token string) (id, secret string, err error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, TokenPrefix) {
		return "", "", errMalformedToken
	}
	id, secret, ok := strings.Cut(strings.TrimPrefix(token, TokenPrefix), ".")
	if !ok || len(id) != 16 || len(secret) != 64 {
		return "", "", errMalformedToken
	}
	return id, secret, nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
