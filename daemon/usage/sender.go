package usage

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// Files in the state directory.
const (
	// SettingsFile holds the switch and the random install id. The CLI, the
	// daemon's settings route and install.sh write it.
	SettingsFile = "usage.json"
	// SpoolFile is where any Mewla process appends events (one JSON line
	// each); only the running daemon reads it, at a flush.
	SpoolFile = "usage-spool.jsonl"
	// StateFile is the daemon's own: one-shot flags and the batches that
	// still wait to be sent.
	StateFile = "usage-state.json"

	// DefaultEndpoint receives the counts (Mewla's account service).
	DefaultEndpoint = "https://connect.mewla.ai/v1/events"
	// EndpointEnv points a proof run at its own receiver; with it set, a
	// development build sends too.
	EndpointEnv = "MEWLA_USAGE_URL"
	// OffEnv set to 0, false, off or no turns counts off for that process
	// (and, in the daemon's environment, for the computer).
	OffEnv = "MEWLA_TELEMETRY"

	maxSpoolBytes = 256 << 10
	maxPending    = 1000
	keepDays      = 7
)

// Settings is usage.json.
type Settings struct {
	// Enabled is nil until someone chose; counts are on by default.
	Enabled   *bool  `json:"enabled,omitempty"`
	InstallID string `json:"install_id,omitempty"`
	// NoticeShown: the first-run line was printed in a terminal.
	NoticeShown bool `json:"notice_shown,omitempty"`
}

// ReadSettings reads usage.json; a missing or broken file is the default.
func ReadSettings(stateDir string) Settings {
	var s Settings
	raw, err := os.ReadFile(filepath.Join(stateDir, SettingsFile))
	if err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	if !ValidID(s.InstallID) {
		s.InstallID = ""
	}
	return s
}

// WriteSettings saves usage.json (0600).
func WriteSettings(stateDir string, s Settings) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(stateDir, SettingsFile), append(raw, '\n'), 0o600)
}

// SetEnabled turns counts on or off for this computer. Off also discards
// whatever waits to be sent.
func SetEnabled(stateDir string, on bool) error {
	s := ReadSettings(stateDir)
	s.Enabled = &on
	if err := WriteSettings(stateDir, s); err != nil {
		return err
	}
	if !on {
		discardQueued(stateDir)
	}
	return nil
}

// ResetID gives this computer a new random install id.
func ResetID(stateDir string) (string, error) {
	s := ReadSettings(stateDir)
	s.InstallID = NewID()
	return s.InstallID, WriteSettings(stateDir, s)
}

// ensureID returns this computer's install id, making one when there is none.
func ensureID(stateDir string) string {
	settings := ReadSettings(stateDir)
	if settings.InstallID == "" {
		settings.InstallID = NewID()
		_ = WriteSettings(stateDir, settings)
	}
	return settings.InstallID
}

// NewID is a random 128-bit id as 32 hex.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Why counts are off, or "" when they are on.
const (
	OffBySetting = "setting"     // mewla telemetry off, Settings, install --no-telemetry
	OffByEnv     = "environment" // MEWLA_TELEMETRY=0 or DO_NOT_TRACK=1
	OffInCI      = "ci"          // CI is set: test and release runs never count
	OffDevBuild  = "development" // not a release build
)

// Release is set by the daemon at start: true only in release binaries.
// Development builds (go run, the dev runner, a local go build) never send
// unless EndpointEnv points them at a receiver.
var Release bool

// OffReason says why counts are off for this process, or "" when on.
func OffReason(stateDir string) string {
	if s := ReadSettings(stateDir); s.Enabled != nil && !*s.Enabled {
		return OffBySetting
	}
	return envOffReason()
}

func envOffReason() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(OffEnv))) {
	case "0", "false", "off", "no":
		return OffByEnv
	}
	if v := strings.TrimSpace(os.Getenv("DO_NOT_TRACK")); v != "" && v != "0" {
		return OffByEnv
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("CI"))); v != "" && v != "false" && v != "0" {
		return OffInCI
	}
	if !Release && os.Getenv(EndpointEnv) == "" {
		return OffDevBuild
	}
	return ""
}

type spooled struct {
	Day   string `json:"day"`
	Event Event  `json:"event"`
}

var spoolMu sync.Mutex

// Record adds one event for today (UTC) to this computer's spool. It does
// nothing when counts are off, when the event is not a schema event, or
// when the spool is full. It never fails the caller.
func Record(stateDir, name string, props map[string]string) {
	if stateDir == "" || OffReason(stateDir) != "" {
		return
	}
	e := Event{Name: name, Props: props}
	if Validate(e) != nil {
		return
	}
	line, err := json.Marshal(spooled{Day: time.Now().UTC().Format(time.DateOnly), Event: e})
	if err != nil {
		return
	}
	spoolMu.Lock()
	defer spoolMu.Unlock()
	path := filepath.Join(stateDir, SpoolFile)
	if info, err := os.Stat(path); err == nil && info.Size() > maxSpoolBytes {
		return
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// State is usage-state.json, the daemon's own.
type State struct {
	InstalledAt    time.Time `json:"installed_at"`
	InstallSent    bool      `json:"install_sent,omitempty"`
	FirstReplySent bool      `json:"first_reply_sent,omitempty"`
	LastDaily      string    `json:"last_daily,omitempty"`
	// LastVersion is the version that last started: a different one now is an update.
	LastVersion string    `json:"last_version,omitempty"`
	Pending     []spooled `json:"pending,omitempty"`
}

func readState(stateDir string) State {
	var s State
	if raw, err := os.ReadFile(filepath.Join(stateDir, StateFile)); err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

func writeState(stateDir string, s State) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(stateDir, StateFile), raw, 0o600)
}

func discardQueued(stateDir string) {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	_ = os.Remove(filepath.Join(stateDir, SpoolFile))
	s := readState(stateDir)
	s.Pending = nil
	_ = writeState(stateDir, s)
}

// Queued lists what waits to be sent (spool and pending), for
// `mewla telemetry show`.
func Queued(stateDir string) []Batch {
	all := append([]spooled(nil), readState(stateDir).Pending...)
	all = append(all, readSpool(filepath.Join(stateDir, SpoolFile))...)
	return group(ReadSettings(stateDir).InstallID, all)
}

func readSpool(path string) []spooled {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []spooled
	scanner := bufio.NewScanner(io.LimitReader(f, maxSpoolBytes*2))
	for scanner.Scan() {
		var s spooled
		if json.Unmarshal(scanner.Bytes(), &s) == nil && ValidDay(s.Day) && Validate(s.Event) == nil {
			out = append(out, s)
		}
	}
	return out
}

// group splits events into batches by day, at most MaxEventsPerBatch each,
// oldest day first.
func group(installID string, events []spooled) []Batch {
	byDay := map[string][]Event{}
	var days []string
	for _, s := range events {
		if _, ok := byDay[s.Day]; !ok {
			days = append(days, s.Day)
		}
		byDay[s.Day] = append(byDay[s.Day], s.Event)
	}
	sort.Strings(days)
	var out []Batch
	for _, day := range days {
		list := byDay[day]
		for len(list) > 0 {
			n := min(len(list), MaxEventsPerBatch)
			out = append(out, Batch{InstallID: installID, Day: day, Events: list[:n]})
			list = list[n:]
		}
	}
	return out
}

// Daily describes this computer for the once-a-day event.
type Daily struct {
	OS, Arch, Version string
	SignedIn          bool
	Devices           int
	Agents            []string
	Features          []string
}

// Sender is the daemon's flusher: it sends what any process spooled.
type Sender struct {
	StateDir string
	Version  string
	OS, Arch string
	// Via is how this install came ("script", "mac_app", "source", "other").
	Via string
	// Daily is asked once a UTC day for the daily event; nil skips it.
	Daily func() Daily
	// Endpoint is DefaultEndpoint unless EndpointEnv or a test says otherwise.
	Endpoint string
	Client   *http.Client
	Now      func() time.Time
	// First is the wait before the first flush after start, Every the wait
	// between flushes.
	First, Every time.Duration

	mu sync.Mutex
	// replied: the first reply is counted (saves a state read per Brain turn).
	replied bool
}

func (s *Sender) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Start prepares this install (its id, the install event on first start)
// and returns; Run sends.
func (s *Sender) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	ensureID(s.StateDir)
	state := readState(s.StateDir)
	if state.InstalledAt.IsZero() {
		state.InstalledAt = s.now().UTC()
		_ = writeState(s.StateDir, state)
	}
	if !state.InstallSent && OffReason(s.StateDir) == "" {
		Record(s.StateDir, "install", map[string]string{
			"os": OneOf(s.OS, OSes), "arch": OneOf(s.Arch, Arches), "version": versionOrDev(s.Version), "via": OneOf(s.Via, InstallVia),
		})
		state.InstallSent = true
		_ = writeState(s.StateDir, state)
	}
	if current := versionOrDev(s.Version); state.LastVersion != current {
		if state.LastVersion != "" && OffReason(s.StateDir) == "" {
			Record(s.StateDir, "update", map[string]string{"from": state.LastVersion, "to": current, "result": "ok"})
		}
		state.LastVersion = current
		_ = writeState(s.StateDir, state)
	}
}

// FirstReply records the first Brain reply this install ever got, once.
func (s *Sender) FirstReply(agent string) {
	if s == nil || OffReason(s.StateDir) != "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replied {
		return
	}
	state := readState(s.StateDir)
	if state.FirstReplySent {
		s.replied = true
		return
	}
	Record(s.StateDir, "first_reply", map[string]string{"after": SinceBucket(s.now().Sub(state.InstalledAt)), "agent": OneOf(agent, AgentKinds)})
	state.FirstReplySent = true
	s.replied = true
	_ = writeState(s.StateDir, state)
}

// SinceBucket puts a duration in its bucket.
func SinceBucket(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < 5*time.Minute:
		return "1-5m"
	case d < 30*time.Minute:
		return "5-30m"
	case d < 2*time.Hour:
		return "30m-2h"
	case d < 24*time.Hour:
		return "2h-1d"
	default:
		return ">1d"
	}
}

func versionOrDev(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if versionPattern.MatchString(v) {
		return v
	}
	return "dev"
}

// Run flushes after First and then every Every until ctx ends.
func (s *Sender) Run(ctx context.Context) {
	wait := s.First
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		_ = s.Flush(ctx)
		wait = s.Every
	}
}

// Flush adds the daily event when due, sends what waits and keeps what
// could not be sent for the next flush (at most a week, at most maxPending).
func (s *Sender) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if OffReason(s.StateDir) != "" {
		discardQueued(s.StateDir)
		return nil
	}
	state := readState(s.StateDir)
	today := s.now().UTC().Format(time.DateOnly)
	if s.Daily != nil && state.LastDaily != today {
		d := s.Daily()
		Record(s.StateDir, "daily", map[string]string{
			"os": OneOf(d.OS, OSes), "arch": OneOf(d.Arch, Arches), "version": versionOrDev(d.Version),
			"signed_in": fmt.Sprint(d.SignedIn), "devices": Bucket(d.Devices),
			"agents": Set(d.Agents, AgentKinds), "features": Set(d.Features, Features),
		})
		state.LastDaily = today
	}
	// Take the spool: rename it so processes appending now start a new one.
	spoolMu.Lock()
	spoolPath := filepath.Join(s.StateDir, SpoolFile)
	taking := spoolPath + ".sending"
	var taken []spooled
	if os.Rename(spoolPath, taking) == nil {
		taken = readSpool(taking)
		_ = os.Remove(taking)
	}
	spoolMu.Unlock()
	queue := append(state.Pending, taken...)
	cut := s.now().UTC().AddDate(0, 0, -keepDays).Format(time.DateOnly)
	kept := queue[:0]
	for _, q := range queue {
		if q.Day >= cut {
			kept = append(kept, q)
		}
	}
	if len(kept) > maxPending {
		kept = kept[len(kept)-maxPending:]
	}
	var unsent []spooled
	var firstErr error
	id := ensureID(s.StateDir)
	for _, batch := range group(id, kept) {
		retry, err := s.send(ctx, batch)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if retry {
			for _, e := range batch.Events {
				unsent = append(unsent, spooled{Day: batch.Day, Event: e})
			}
		}
	}
	state.Pending = unsent
	if err := writeState(s.StateDir, state); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// send posts one batch. retry is true when the batch should be tried again
// (no answer, rate limited, server trouble); a refusal is dropped.
func (s *Sender) send(ctx context.Context, b Batch) (retry bool, err error) {
	if ValidateBatch(b) != nil {
		return false, errors.New("usage: batch is not schema values")
	}
	body, err := json.Marshal(b)
	if err != nil {
		return false, err
	}
	endpoint := s.Endpoint
	if v := os.Getenv(EndpointEnv); v != "" {
		endpoint = v
	}
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "mewla")
	resp, err := client.Do(req)
	if err != nil {
		return true, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode/100 == 2:
		return false, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return true, fmt.Errorf("usage: %s", resp.Status)
	default:
		// 400, 404 (an older service), 413: sending again would not help.
		return false, fmt.Errorf("usage: %s", resp.Status)
	}
}
