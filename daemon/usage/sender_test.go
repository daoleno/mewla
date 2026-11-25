package usage_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/usage"
	"github.com/daoleno/mewla/daemon/usage/ingest"
)

// onByDefault clears every environment switch and marks the process a
// release build, as a released daemon on a person's computer is.
func onByDefault(t *testing.T) {
	t.Helper()
	for _, key := range []string{usage.OffEnv, "DO_NOT_TRACK", "CI", usage.EndpointEnv} {
		t.Setenv(key, "")
	}
	usage.Release = true
	t.Cleanup(func() { usage.Release = false })
}

type receiver struct {
	*httptest.Server
	store *ingest.Store
	hits  atomic.Int32
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	store, err := ingest.OpenStore(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	r := &receiver{store: store}
	mux := http.NewServeMux()
	ingest.New(ingest.Config{Store: store}).Routes(mux)
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.hits.Add(1)
		mux.ServeHTTP(w, req)
	}))
	t.Cleanup(r.Close)
	return r
}

func newSender(t *testing.T, endpoint string) *usage.Sender {
	t.Helper()
	return &usage.Sender{
		StateDir: t.TempDir(), Version: "0.5.0", OS: "linux", Arch: "amd64", Via: "script",
		Endpoint: endpoint + ingest.EventsPath,
		Daily: func() usage.Daily {
			return usage.Daily{OS: "linux", Arch: "amd64", Version: "0.5.0", SignedIn: true, Devices: 2, Agents: []string{"codex", "claude", "my-own-agent"}, Features: []string{"terminal"}}
		},
	}
}

// What a computer spools reaches the receiver as schema events: install
// once, daily once a day, and the funnel events any process recorded.
func TestSpoolReachesTheReceiver(t *testing.T) {
	onByDefault(t)
	r := newReceiver(t)
	s := newSender(t, r.URL)
	s.Start()
	s.Start() // a restart sends no second install
	usage.Record(s.StateDir, "login_started", nil)
	usage.Record(s.StateDir, "pair_ok", map[string]string{"transport": "relay", "platform": "ios"})
	s.FirstReply("claude")
	s.FirstReply("claude")
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(context.Background()); err != nil { // same day: no second daily
		t.Fatal(err)
	}
	if len(usage.Queued(s.StateDir)) != 0 {
		t.Fatal("events still queued after a good flush")
	}
	ctx := context.Background()
	got := map[string]int{}
	rows, err := ingestRows(ctx, r.store)
	if err != nil {
		t.Fatal(err)
	}
	for name, n := range rows {
		got[name] = n
	}
	for name, want := range map[string]int{"install": 1, "daily": 1, "login_started": 1, "pair_ok": 1, "first_reply": 1} {
		if got[name] != want {
			t.Errorf("%s = %d, want %d (all: %v)", name, got[name], want, got)
		}
	}
}

func ingestRows(ctx context.Context, store *ingest.Store) (map[string]int, error) {
	return ingest.CountByName(ctx, store)
}

func TestOffSendsNothingAndDropsTheQueue(t *testing.T) {
	cases := map[string]func(t *testing.T, stateDir string){
		"setting":      func(t *testing.T, dir string) { _ = usage.SetEnabled(dir, false) },
		"env":          func(t *testing.T, _ string) { t.Setenv(usage.OffEnv, "0") },
		"do not track": func(t *testing.T, _ string) { t.Setenv("DO_NOT_TRACK", "1") },
		"ci":           func(t *testing.T, _ string) { t.Setenv("CI", "true") },
		"dev build":    func(t *testing.T, _ string) { usage.Release = false },
	}
	for name, turnOff := range cases {
		t.Run(name, func(t *testing.T) {
			onByDefault(t)
			r := newReceiver(t)
			s := newSender(t, r.URL)
			usage.Record(s.StateDir, "login_started", nil) // queued while on
			turnOff(t, s.StateDir)
			s.Start()
			usage.Record(s.StateDir, "login_started", nil)
			if err := s.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			if r.hits.Load() != 0 {
				t.Fatalf("sent %d requests while off", r.hits.Load())
			}
			if _, err := os.Stat(filepath.Join(s.StateDir, usage.SpoolFile)); !os.IsNotExist(err) {
				t.Fatal("the spool outlived turning off")
			}
		})
	}
}

func TestRetryKeepsAndRefusalDrops(t *testing.T) {
	onByDefault(t)
	var status atomic.Int32
	status.Store(http.StatusServiceUnavailable)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	s := newSender(t, server.URL)
	s.Daily = nil
	usage.Record(s.StateDir, "login_started", nil)
	_ = s.Flush(context.Background())
	if n := len(usage.Queued(s.StateDir)); n != 1 {
		t.Fatalf("after 503 queued %d batches, want 1", n)
	}
	status.Store(http.StatusNotFound) // an older service without the endpoint
	_ = s.Flush(context.Background())
	if n := len(usage.Queued(s.StateDir)); n != 0 {
		t.Fatalf("after 404 queued %d batches, want 0", n)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestOldEventsAgeOut(t *testing.T) {
	onByDefault(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	s := newSender(t, server.URL)
	s.Daily = nil
	usage.Record(s.StateDir, "login_started", nil)
	_ = s.Flush(context.Background())
	s.Now = func() time.Time { return time.Now().AddDate(0, 0, 9) }
	_ = s.Flush(context.Background())
	if n := len(usage.Queued(s.StateDir)); n != 0 {
		t.Fatalf("a nine-day-old event is still queued (%d)", n)
	}
}

func TestRecordRefusesNonSchemaEvents(t *testing.T) {
	onByDefault(t)
	dir := t.TempDir()
	usage.Record(dir, "pair_failed", map[string]string{"reason": "/home/alice/project"})
	usage.Record(dir, "prompt", map[string]string{"text": "hello"})
	raw, _ := os.ReadFile(filepath.Join(dir, usage.SpoolFile))
	if strings.Contains(string(raw), "alice") || strings.Contains(string(raw), "hello") || len(raw) != 0 {
		t.Fatalf("spool = %q", raw)
	}
}

func TestSettingsSurviveAndResetRotatesTheID(t *testing.T) {
	dir := t.TempDir()
	first, err := usage.ResetID(dir)
	if err != nil || !usage.ValidID(first) {
		t.Fatal(first, err)
	}
	_ = usage.SetEnabled(dir, false)
	settings := usage.ReadSettings(dir)
	if settings.InstallID != first || settings.Enabled == nil || *settings.Enabled {
		t.Fatalf("settings = %+v", settings)
	}
	second, _ := usage.ResetID(dir)
	if second == first {
		t.Fatal("reset kept the id")
	}
	info, err := os.Stat(filepath.Join(dir, usage.SettingsFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v", info.Mode(), err)
	}
}

func TestAnUpdateIsCountedOnTheNextStart(t *testing.T) {
	onByDefault(t)
	dir := t.TempDir()
	(&usage.Sender{StateDir: dir, Version: "0.5.0"}).Start()
	(&usage.Sender{StateDir: dir, Version: "0.5.0"}).Start()
	(&usage.Sender{StateDir: dir, Version: "v0.5.1"}).Start()
	raw, _ := os.ReadFile(filepath.Join(dir, usage.SpoolFile))
	if n := strings.Count(string(raw), `"update"`); n != 1 || !strings.Contains(string(raw), `"from":"0.5.0"`) || !strings.Contains(string(raw), `"to":"0.5.1"`) {
		t.Fatalf("spool = %s", raw)
	}
}
