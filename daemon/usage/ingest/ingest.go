// Package ingest receives Mewla's anonymous usage counts (POST /v1/events
// from computers) and mewla.ai page views (POST /v1/visit), checks them
// against the usage schema and adds them to daily counters in SQLite.
//
// What it keeps: per UTC day, event name, schema properties and the random
// install id; per day, page kind and referrer kind for visits. What it never
// keeps or logs: addresses, user agents, times of day, account or machine
// ids. A client's address is used only in memory, to rate-limit it.
package ingest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/daoleno/mewla/daemon/usage"
)

const (
	// EventsPath and VisitPath are the two endpoints.
	EventsPath = "/v1/events"
	VisitPath  = "/v1/visit"

	// RawDays is how long per-install counters are kept; daily totals
	// (rollups) are kept after that.
	RawDays = 90
	// MaxEventsPerInstallDay caps one install's counted events per day.
	MaxEventsPerInstallDay = 2000
	maxVisitBytes          = 512
	// Requests per address per hour.
	eventsPerAddressHour = 120
	visitsPerAddressHour = 600
)

const schema = `
CREATE TABLE IF NOT EXISTS events (
	day TEXT NOT NULL,
	name TEXT NOT NULL,
	install TEXT NOT NULL,
	props TEXT NOT NULL,
	n INTEGER NOT NULL,
	PRIMARY KEY (day, name, install, props)
);
CREATE TABLE IF NOT EXISTS install_days (
	day TEXT NOT NULL,
	install TEXT NOT NULL,
	n INTEGER NOT NULL,
	PRIMARY KEY (day, install)
);
CREATE TABLE IF NOT EXISTS rollups (
	day TEXT NOT NULL,
	name TEXT NOT NULL,
	props TEXT NOT NULL,
	events INTEGER NOT NULL,
	installs INTEGER NOT NULL,
	PRIMARY KEY (day, name, props)
);
CREATE TABLE IF NOT EXISTS rolled_days (day TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS visits (
	day TEXT NOT NULL,
	page TEXT NOT NULL,
	ref TEXT NOT NULL,
	n INTEGER NOT NULL,
	PRIMARY KEY (day, page, ref)
);
`

// Store is the counters database.
type Store struct{ db *sql.DB }

// OpenStore opens (and creates) the database at path.
func OpenStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("usage database path is empty")
	}
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate usage database: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

var errOverCap = errors.New("over the daily cap")

// canonical is the props as stable JSON (sorted keys).
func canonical(props map[string]string) string {
	if len(props) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(k)
		value, _ := json.Marshal(props[k])
		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return b.String()
}

// AddBatch counts a validated batch, refusing all of it when it would take
// the install past its daily cap.
func (s *Store) AddBatch(ctx context.Context, batch usage.Batch) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var used int
	err = tx.QueryRowContext(ctx, `SELECT n FROM install_days WHERE day = ? AND install = ?`, batch.Day, batch.InstallID).Scan(&used)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if used+len(batch.Events) > MaxEventsPerInstallDay {
		return errOverCap
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO install_days (day, install, n) VALUES (?, ?, ?)
		ON CONFLICT (day, install) DO UPDATE SET n = n + excluded.n`, batch.Day, batch.InstallID, len(batch.Events)); err != nil {
		return err
	}
	for _, e := range batch.Events {
		if _, err := tx.ExecContext(ctx, `INSERT INTO events (day, name, install, props, n) VALUES (?, ?, ?, ?, 1)
			ON CONFLICT (day, name, install, props) DO UPDATE SET n = n + 1`, batch.Day, e.Name, batch.InstallID, canonical(e.Props)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddVisit counts one page view on day.
func (s *Store) AddVisit(ctx context.Context, day string, v usage.Visit) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO visits (day, page, ref, n) VALUES (?, ?, ?, 1)
		ON CONFLICT (day, page, ref) DO UPDATE SET n = n + 1`, day, v.Page, v.Referrer)
	return err
}

// Maintain rolls every finished day (before yesterday, UTC) up into daily
// totals and deletes per-install counters older than RawDays.
func (s *Store) Maintain(ctx context.Context, now time.Time) error {
	yesterday := now.UTC().AddDate(0, 0, -1).Format(time.DateOnly)
	rawCut := now.UTC().AddDate(0, 0, -RawDays).Format(time.DateOnly)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO rollups (day, name, props, events, installs)
		SELECT day, name, props, SUM(n), COUNT(DISTINCT install) FROM events
		WHERE day < ? AND day NOT IN (SELECT day FROM rolled_days)
		GROUP BY day, name, props`, yesterday); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO rolled_days (day)
		SELECT DISTINCT day FROM events WHERE day < ?`, yesterday); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE day < ?`, rawCut); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM install_days WHERE day < ?`, yesterday); err != nil {
		return err
	}
	return tx.Commit()
}

// Config is the receiver's setup.
type Config struct {
	Store *Store
	// Now is the clock (tests); time.Now when nil.
	Now func() time.Time
	// Logf reports store failures, never request contents; log.Printf when nil.
	Logf func(string, ...any)
}

// Handler serves EventsPath and VisitPath.
type Handler struct {
	config Config
	events *limiter
	visits *limiter
}

// New returns the receiver.
func New(config Config) *Handler {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Logf == nil {
		config.Logf = func(string, ...any) {}
	}
	return &Handler{
		config: config,
		events: newLimiter(eventsPerAddressHour, time.Hour),
		visits: newLimiter(visitsPerAddressHour, time.Hour),
	}
}

// Routes mounts the two endpoints on mux.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST "+EventsPath, h.handleEvents)
	mux.HandleFunc("POST "+VisitPath, h.handleVisit)
	mux.HandleFunc("OPTIONS "+VisitPath, h.handleVisitPreflight)
}

func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	now := h.config.Now()
	if !h.events.allow(addressKey(r.RemoteAddr), now) {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, usage.MaxBatchBytes))
	if err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	var batch usage.Batch
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&batch); err != nil {
		http.Error(w, "invalid batch", http.StatusBadRequest)
		return
	}
	if err := usage.ValidateBatch(batch); err != nil {
		// The reason names the schema rule, never the refused value.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !dayInWindow(batch.Day, now) {
		// A queue that waited too long, or a wrong clock: not counted, not retried.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch err := h.config.Store.AddBatch(r.Context(), batch); {
	case errors.Is(err, errOverCap):
		w.WriteHeader(http.StatusTooManyRequests)
	case err != nil:
		h.config.Logf("usage: store failed: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// The site sends visits with navigator.sendBeacon as text/plain, which needs
// no preflight; a preflight from anything else gets no CORS grant.
func (h *Handler) handleVisitPreflight(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleVisit(w http.ResponseWriter, r *http.Request) {
	now := h.config.Now()
	if !h.visits.allow(addressKey(r.RemoteAddr), now) {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxVisitBytes))
	if err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	var visit usage.Visit
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&visit) != nil || usage.ValidateVisit(visit) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := h.config.Store.AddVisit(r.Context(), now.UTC().Format(time.DateOnly), visit); err != nil {
		h.config.Logf("usage: store visit failed: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// dayInWindow accepts days from a week ago to tomorrow (UTC), so a queue
// that waited out a weekend counts and a wrong clock does not.
func dayInWindow(day string, now time.Time) bool {
	d, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return false
	}
	today, _ := time.Parse(time.DateOnly, now.UTC().Format(time.DateOnly))
	return !d.Before(today.AddDate(0, 0, -7)) && !d.After(today.AddDate(0, 0, 1))
}

// addressKey is the rate-limit key: an IPv4 address or an IPv6 /64. It
// lives only in the limiter's memory.
func addressKey(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "?"
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String()
}

// limiter allows max requests per key per window (fixed windows), in memory.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	start  time.Time
	counts map[string]int
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, counts: map[string]int{}}
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.start) >= l.window {
		l.start = now
		clear(l.counts)
	}
	if l.counts[key] >= l.max {
		return false
	}
	l.counts[key]++
	return true
}

// MaintainEvery runs Maintain now and then every interval until ctx ends.
func (s *Store) MaintainEvery(ctx context.Context, every time.Duration, logf func(string, ...any)) {
	for {
		if err := s.Maintain(ctx, time.Now()); err != nil && ctx.Err() == nil && logf != nil {
			logf("usage: maintenance failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
