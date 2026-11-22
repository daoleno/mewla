package server

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

type chatOutageWatcher struct {
	killTrackingWatcher
	claude, codex watcher.ProviderOutage
	codexDown     atomic.Bool
}

func (w *chatOutageWatcher) ProviderOutages() []watcher.ProviderOutage {
	if w.codexDown.Load() {
		return []watcher.ProviderOutage{w.claude, w.codex}
	}
	return []watcher.ProviderOutage{w.claude}
}

func (w *chatOutageWatcher) ProviderOutage(family string) (watcher.ProviderOutage, bool) {
	for _, outage := range w.ProviderOutages() {
		if outage.Provider == family {
			return outage, true
		}
	}
	return watcher.ProviderOutage{}, false
}

func (*chatOutageWatcher) ProviderProbe(string, time.Time) (bool, time.Time) {
	panic("a snapshot spent a probe")
}

func TestChatHostOutageUsesObservedExecutorForOpaqueCommands(t *testing.T) {
	for _, tc := range []struct{ kind, family string }{
		{"claude", "claude"}, {"codex", "codex"}, {"cursor", "cursor-agent"}, {"agy", "agy-session"},
	} {
		srv := &Server{execs: work.NewExecutorConfig(map[string]work.Executor{
			"alias": {Command: "/bin/launcher", Kind: tc.kind},
		})}
		host := &classifier.Worker{ID: "host", ExecutorID: "alias", Command: "/bin/launcher", Hidden: true}
		srv.getWorkerOverride = func(string) *classifier.Worker { return host }
		snapshot := brain.Snapshot{
			HostWorker:      &brain.WorkerRef{ID: "host"},
			ProviderOutages: []watcher.ProviderOutage{{Provider: tc.family}, {Provider: "pi"}},
		}
		if got := srv.chatHostOutages(snapshot); len(got) != 1 || got[0].Provider != tc.family {
			t.Fatalf("%s launcher outages = %v", tc.kind, got)
		}
		host = nil
		if got := srv.chatHostOutages(snapshot); len(got) != 0 {
			t.Fatalf("unobserved host outages = %v", got)
		}
	}
}

func TestBrainChatOutageAcrossHostSwitchAndReconnect(t *testing.T) {
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "")
	t.Cleanup(brain.SetHostLookPath(func(name string) (string, error) { return "/bin/" + name, nil }))
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("old-host", "studio"); err != nil {
		t.Fatal(err)
	}
	fw := &chatOutageWatcher{
		claude: watcher.ProviderOutage{Provider: "claude", Since: time.Now().UTC().Add(-time.Hour), Kind: "rate limit", Cause: "HTTP 429"},
		codex:  watcher.ProviderOutage{Provider: "codex", Since: time.Now().UTC(), Kind: "overloaded", Cause: "HTTP 529"},
	}
	fw.sessions = map[string]*classifier.Worker{
		"old-host": {ID: "old-host", Name: "Brain", Command: "/bin/cc", ExecutorID: "studio", State: classifier.StateRunning, Hidden: true},
		"worker":   {ID: "worker", Name: "Independent Worker", Command: "claude", State: classifier.StateBlocked, Delegated: true},
	}
	execs := work.NewExecutorConfig(map[string]work.Executor{
		"studio": {Command: "/bin/cc", Kind: "claude"},
		"desk":   {Command: "codex", Kind: "codex"},
	})
	service := brain.NewService(store, fw, execs)
	manager, key := enrolledTestDevice(t)
	srv := New(manager, nil, nil, nil, nil, execs, service)
	srv.getWorkerOverride = fw.GetWorker
	host := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer host.Close()
	connect := func(app bool) *websocket.Conn {
		t.Helper()
		signed := calendarAuthHeader(key, manager.DaemonID(), "device-direct", "mewla-connect")
		protocols := []string{authSubprotocolPrefix + base64.RawURLEncoding.EncodeToString([]byte(signed))}
		if app {
			protocols = append(protocols, "mewla.app.android.0.4.21")
		}
		conn, _, err := (&websocket.Dialer{Subprotocols: protocols}).Dial("ws"+strings.TrimPrefix(host.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	check := func(stage string, conn *websocket.Conn, want ...string) {
		t.Helper()
		frame := readWebSocketType(t, conn, "brain_snapshot")["brain"].(map[string]any)
		rows, _ := frame["provider_outages"].([]any)
		var got []string
		for _, row := range rows {
			got = append(got, row.(map[string]any)["provider"].(string))
		}
		t.Logf("%s: outages %v", stage, got)
		if !slices.Equal(got, want) {
			t.Fatalf("%s: got %v, want %v", stage, got, want)
		}
	}
	phone := connect(true)
	check("initial", phone, "claude")
	if err := phone.WriteJSON(clientMessage{Type: "brain_set_executor", ExecutorID: "missing"}); err != nil {
		t.Fatal(err)
	}
	readWebSocketType(t, phone, "error")
	if err := phone.WriteJSON(clientMessage{Type: "brain_snapshot"}); err != nil {
		t.Fatal(err)
	}
	check("rejected switch", phone, "claude")
	if err := phone.WriteJSON(clientMessage{Type: "brain_set_executor", ExecutorID: "desk"}); err != nil {
		t.Fatal(err)
	}
	check("selection reply with old host still serving", phone, "claude")
	check("selection broadcast", phone, "claude")
	if err := srv.reconcileBrainHostContinuity(); err != nil {
		t.Fatal(err)
	}
	check("switched to healthy Codex", phone)
	_ = phone.Close()
	phone = connect(true)
	check("reconnected", phone)
	diagnostic := connect(false)
	check("diagnostics after switch", diagnostic, "claude")
	fw.codexDown.Store(true)
	srv.broadcastBrainSnapshot()
	check("both down", phone, "codex")
	check("diagnostics while both down", diagnostic, "claude", "codex")
	fw.codexDown.Store(false)
	srv.broadcastBrainSnapshot()
	check("Codex recovered", phone)
	check("diagnostics after recovery", diagnostic, "claude")
	if fw.GetWorker("worker").State != classifier.StateBlocked {
		t.Fatal("Claude Worker did not stay held")
	}
}
