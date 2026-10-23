// Package modelfacts owns the one download of the public models.dev catalog.
//
// Several parts of the daemon derive facts from that catalog: reference
// prices for usage stats, context windows and display names for the model
// picker. Each used to fetch and cache its own copy. A Feed fetches it once,
// keeps the raw document in memory and in one file, and hands the same bytes
// to every reader, which parses the part it needs.
package modelfacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

const (
	// URL is the public models.dev feed.
	URL = "https://models.dev/api.json"
	// CacheFile is the file name of the cached catalog in the state directory.
	CacheFile = "models-dev.json"
	// TTL is how long a downloaded catalog counts as fresh.
	TTL = 24 * time.Hour

	// MaxBytes is the largest catalog accepted, downloaded or cached.
	MaxBytes = 32 << 20
	// timeout bounds one download.
	timeout = 15 * time.Second
)

// Config says where a Feed downloads from and keeps its copy.
type Config struct {
	URL string
	// Path is the cache file for the raw document; empty keeps it in memory.
	Path string
	// Client is the HTTP client; nil keeps the one the Feed already has, or
	// http.DefaultClient.
	Client *http.Client
}

// Feed is the models.dev catalog as one process sees it.
type Feed struct {
	url  string
	path string

	mu     sync.Mutex
	client *http.Client
	loaded bool
	raw    []byte
	at     time.Time
	flight *flight
}

// flight is one download and the callers waiting on it. It is cancelled when
// the last of them gives up.
type flight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	err     error
}

var (
	registryMu sync.Mutex
	registry   = map[string]*Feed{}
)

// Open returns the Feed for a URL and cache file. Readers that open the same
// pair share one Feed, so the catalog is downloaded once however many of them
// ask.
func Open(cfg Config) *Feed {
	registryMu.Lock()
	defer registryMu.Unlock()
	key := cfg.URL + "\x00" + cfg.Path
	feed := registry[key]
	if feed == nil {
		feed = &Feed{url: cfg.URL, path: cfg.Path}
		registry[key] = feed
	}
	feed.mu.Lock()
	switch {
	case cfg.Client != nil:
		feed.client = cfg.Client
	case feed.client == nil:
		feed.client = http.DefaultClient
	}
	feed.mu.Unlock()
	return feed
}

// Raw returns the catalog as last downloaded, from this process or from the
// cache file, and when. It is empty when there is none.
func (f *Feed) Raw() ([]byte, time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadLocked()
	return f.raw, f.at
}

// Stale reports a catalog that is missing or older than ttl.
func (f *Feed) Stale(ttl time.Duration) bool {
	_, at := f.Raw()
	return at.IsZero() || time.Since(at) >= ttl
}

func (f *Feed) loadLocked() {
	if f.loaded {
		return
	}
	f.loaded = true
	if f.path == "" {
		return
	}
	info, err := os.Stat(f.path)
	if err != nil || info.Size() > MaxBytes {
		return
	}
	raw, err := os.ReadFile(f.path)
	if err != nil || !json.Valid(raw) {
		return
	}
	f.raw, f.at = raw, info.ModTime().UTC()
}

// Refresh downloads the catalog now, whatever its age. Callers that ask while
// a download is under way share it. A caller that gives up through ctx stops
// waiting; the download is cancelled when no caller is left to want it.
//
// validate, when not nil, judges the document before it is kept: a catalog it
// rejects replaces neither the file nor the copy in memory, so the last good
// one survives a bad day at the source. The caller that starts a download
// supplies the judgement for everyone sharing it.
func (f *Feed) Refresh(ctx context.Context, validate func([]byte) error) ([]byte, time.Time, error) {
	f.mu.Lock()
	f.loadLocked()
	current := f.flight
	if current == nil {
		downloadCtx, cancel := context.WithTimeout(context.Background(), timeout)
		current = &flight{done: make(chan struct{}), cancel: cancel}
		f.flight = current
		go f.download(downloadCtx, current, validate)
	}
	current.waiters++
	f.mu.Unlock()
	select {
	case <-current.done:
	case <-ctx.Done():
		f.mu.Lock()
		current.waiters--
		if current.waiters == 0 {
			current.cancel()
			if f.flight == current {
				f.flight = nil
			}
		}
		f.mu.Unlock()
		return nil, time.Time{}, ctx.Err()
	}
	if current.err != nil {
		return nil, time.Time{}, current.err
	}
	raw, at := f.Raw()
	return raw, at, nil
}

func (f *Feed) download(ctx context.Context, current *flight, validate func([]byte) error) {
	defer current.cancel()
	raw, err := f.fetch(ctx, validate)
	f.mu.Lock()
	if err == nil {
		f.raw, f.at = raw, time.Now().UTC()
	}
	if f.flight == current {
		f.flight = nil
	}
	f.mu.Unlock()
	current.err = err
	close(current.done)
}

func (f *Feed) fetch(ctx context.Context, validate func([]byte) error) ([]byte, error) {
	if f.url == "" {
		return nil, errors.New("models.dev refresh is disabled")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mewla (+https://github.com/daoleno/mewla)")
	f.mu.Lock()
	client := f.client
	f.mu.Unlock()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{Code: resp.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxBytes {
		return nil, fmt.Errorf("models.dev catalog exceeds size limit")
	}
	if !json.Valid(raw) {
		return nil, errors.New("models.dev catalog is not valid JSON")
	}
	if validate != nil {
		if err := validate(raw); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.path != "" {
		if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
			return nil, fmt.Errorf("persist models.dev catalog: %w", err)
		}
		if err := atomicfile.Write(f.path, raw, 0o600); err != nil {
			return nil, fmt.Errorf("persist models.dev catalog: %w", err)
		}
	}
	return raw, nil
}

// StatusError is a non-success answer from the feed.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return http.StatusText(e.Code) }
