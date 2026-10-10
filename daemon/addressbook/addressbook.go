// Package addressbook stores the daemon's reachable HTTPS entry points.
//
// The file is intentionally daemon owned.  Readers reload it before each
// admission check, so `mewla address add/remove` takes effect without a daemon
// restart and a second process never needs to select a server for a request.
package addressbook

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const fileName = "addresses.json"

type Source string

const (
	SourceDiscovered Source = "discovered"
	SourceManual     Source = "manual"
	SourceVerified   Source = "verified"
)

type Entry struct {
	URL        string    `json:"url"`
	Source     Source    `json:"source"`
	AddedAt    time.Time `json:"added_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

type persisted struct {
	Entries []Entry `json:"entries"`
}

var ErrInvalidAddress = errors.New("invalid address")

type Store struct {
	path string
	mu   sync.Mutex
}

func New(stateDir string) (*Store, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("address book state directory is required")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(stateDir, fileName)}, nil
}

func Normalize(raw string) (string, error) {
	p, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ErrInvalidAddress
	}
	if p.User != nil || p.Hostname() == "" || p.RawQuery != "" || p.Fragment != "" || (p.Path != "" && p.Path != "/") {
		return "", ErrInvalidAddress
	}
	switch strings.ToLower(p.Scheme) {
	case "https":
		p.Scheme = "https"
	case "http":
		if !isLoopback(p.Hostname()) && !net.ParseIP(strings.Trim(p.Hostname(), "[]")).IsPrivate() {
			return "", ErrInvalidAddress
		}
		p.Scheme = "http"
	default:
		return "", ErrInvalidAddress
	}
	host := strings.ToLower(p.Host)
	if p.Scheme == "https" {
		host = strings.TrimSuffix(host, ":443")
	}
	return p.Scheme + "://" + host, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || net.ParseIP(strings.Trim(host, "[]")).IsLoopback()
}

func (s *Store) List() ([]Entry, error) {
	if s == nil {
		return nil, errors.New("nil address book")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].URL < entries[j].URL })
	return append([]Entry(nil), entries...), nil
}

func (s *Store) Add(raw string, source Source) (Entry, error) {
	value, err := Normalize(raw)
	if err != nil {
		return Entry{}, ErrInvalidAddress
	}
	if source == "" {
		source = SourceManual
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadLocked()
	if err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC()
	for i := range entries {
		if entries[i].URL == value {
			entries[i].Source = source
			entries[i].LastSeenAt = now
			if err := s.saveLocked(entries); err != nil {
				return Entry{}, err
			}
			return entries[i], nil
		}
	}
	entry := Entry{URL: value, Source: source, AddedAt: now, LastSeenAt: now}
	entries = append(entries, entry)
	if err := s.saveLocked(entries); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (s *Store) Remove(raw string) error {
	value, err := Normalize(raw)
	if err != nil {
		return ErrInvalidAddress
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadLocked()
	if err != nil {
		return err
	}
	next := entries[:0]
	for _, entry := range entries {
		if entry.URL != value {
			next = append(next, entry)
		}
	}
	return s.saveLocked(next)
}

// Learn records a host only after the caller has verified the device
// signature. It accepts HTTPS hosts and deliberately ignores HTTP/LAN Hosts.
func (s *Store) Learn(rawHost string) error {
	if strings.TrimSpace(rawHost) == "" {
		return ErrInvalidAddress
	}
	return func() error {
		_, err := s.Add("https://"+strings.TrimSuffix(rawHost, ":443"), SourceVerified)
		return err
	}()
}

func (s *Store) Contains(raw string) bool {
	value, err := Normalize(raw)
	if err != nil {
		return false
	}
	entries, err := s.List()
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.URL == value {
			return true
		}
	}
	return false
}

func (s *Store) loadLocked() ([]Entry, error) {
	value, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var data persisted
	if err := json.Unmarshal(value, &data); err != nil {
		return nil, fmt.Errorf("decode address book: %w", err)
	}
	return data.Entries, nil
}

func (s *Store) saveLocked(entries []Entry) error {
	data, err := json.MarshalIndent(persisted{Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
