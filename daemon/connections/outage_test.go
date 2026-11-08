package connections

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/diag"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOutageBackoffDoublesToItsCap(t *testing.T) {
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second, 300 * time.Second, 300 * time.Second}
	for i, w := range want {
		if got := outageBackoff(i + 1); got != w {
			t.Fatalf("after %d failures: %s, want %s", i+1, got, w)
		}
	}
}

func TestNetworkCauseKeepsNoAddressOrURL(t *testing.T) {
	for err, want := range map[error]string{
		errors.New("dial tcp 140.82.113.21:443: i/o timeout"): "connection timed out",
		io.EOF: "connection closed by the other side",
		errors.New("read tcp 1.2.3.4:5: connection reset"): "connection closed by the other side",
		errors.New("something else with token gho_secret"): "network error",
	} {
		got := networkError(err).Error()
		if !strings.Contains(got, want) || strings.Contains(got, "gho_") || strings.Contains(got, "140.82") {
			t.Fatalf("%v -> %q, want cause %q", err, got, want)
		}
		if !errors.Is(networkError(err), errNetwork) {
			t.Fatal("cause must stay an errNetwork")
		}
	}
}

// statefulFixture is an MCP service that keeps sessions, as GitHub's does, so
// the Mcp-Session-Id header means something.
func statefulFixture(t *testing.T) (*Manager, string, *[]http.Header, *sync.Mutex) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "Fixture", Version: "1"}, nil)
	var calls atomic.Int32
	addFixtureTool(server, "read", "Reads", &calls)
	ts := httptest.NewTLSServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(ts.Close)
	m := testManager(t, ts.Client().Transport.RoundTrip)
	var mu sync.Mutex
	var headers []http.Header
	base := m.http.Transport
	var failed atomic.Bool
	m.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		headers = append(headers, r.Header.Clone())
		mu.Unlock()
		if r.Body != nil {
			raw, _ := r.GetBody()
			if strings.Contains(readAll(raw), "notifications/initialized") && !failed.Swap(true) {
				// The service answers the notification with a bare 400, as
				// GitHub did (Bad Request) in the daemon's log.
				return &http.Response{StatusCode: 400, Status: "400 Bad Request", Body: http.NoBody, Header: http.Header{}, Request: r}, nil
			}
		}
		return base.RoundTrip(r)
	})
	a := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "mcp", Name: "Fixture", Endpoint: ts.URL}}).Account
	return m, a.ID, &headers, &mu
}

// A 400 on notifications/initialized is a setup failure, not a verdict on the
// endpoint: setup runs again, quietly, and every request after initialize
// carries the session and protocol headers the service issued.
func TestHandshakeSurvivesAnAnswerOnNotificationsInitialized(t *testing.T) {
	before := diag.Last()
	m, id, headers, mu := statefulFixture(t)
	if got := mustHandle(t, m, Request{Action: "get", ID: id}).Account; got == nil || len(got.Tools) != 1 || got.Status == "error" {
		t.Fatalf("account after the handshake hiccup: %+v", got)
	}
	if events, _ := diag.After(before); len(events) != 0 {
		t.Fatalf("a recovered setup reported %d events: %+v", len(events), events)
	}
	mu.Lock()
	defer mu.Unlock()
	sessions := 0
	for _, h := range (*headers)[1:] {
		if h.Get("Mcp-Session-Id") != "" {
			sessions++
			if h.Get("Mcp-Protocol-Version") == "" {
				t.Fatalf("session request without a protocol version: %v", h)
			}
		}
	}
	if sessions < 3 {
		t.Fatalf("requests after initialize carried a session %d times of %d", sessions, len(*headers))
	}
}

func TestUnreachableServiceIsOneOutageWithBackoff(t *testing.T) {
	f := newGitHubFixture(t, "octo")
	m := f.manager(t)
	a := addGitHub(t, m, "work", "gh-token")
	other := addGitHub(t, m, "other", "gh-token-2")
	mustHandle(t, m, Request{Action: "permissions", ID: a.ID, Group: "read", Allowed: true})

	now := time.Date(2026, 11, 8, 4, 0, 0, 0, time.UTC)
	m.outages.now = func() time.Time { return now }
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(io.Discard) })

	base := m.http.Transport
	var down atomic.Bool
	var requests atomic.Int32
	m.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			requests.Add(1) // the SDK also sends a DELETE when it closes
		}
		if down.Load() {
			return nil, errors.New(`dial tcp 140.82.113.21:443: i/o timeout`)
		}
		return base.RoundTrip(r)
	})
	resync := func(id string) error { return m.syncRemoteTools(context.Background(), id, true, 5*time.Second) }
	before := diag.Last()
	down.Store(true)

	// The first failure retries quietly once, then opens the outage.
	if err := resync(a.ID); !errors.Is(err, errNetwork) {
		t.Fatalf("first failure: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("first failure made %d requests, want the attempt and one retry", got)
	}
	// During backoff nothing goes out and nothing is logged, for either
	// account of the same service.
	requests.Store(0)
	for i := 0; i < 20; i++ {
		for _, id := range []string{a.ID, other.ID} {
			err := resync(id)
			var refused errUnavailable
			if !errors.As(err, &refused) || !strings.Contains(err.Error(), "connection timed out") {
				t.Fatalf("during backoff: %v", err)
			}
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("%d requests during backoff", requests.Load())
	}
	// A person's own call still goes out.
	if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: a.ID, Tool: "get_me"}); err == nil || requests.Load() == 0 {
		t.Fatalf("invoke during backoff: err %v, requests %d", err, requests.Load())
	}
	// Each due backoff lets exactly one probe through.
	for i := 0; i < 2; i++ {
		now = now.Add(outageBackoffMax + time.Second)
		requests.Store(0)
		_ = resync(a.ID)
		_ = resync(a.ID)
		if requests.Load() != 1 {
			t.Fatalf("probe %d: %d requests, want one", i+1, requests.Load())
		}
	}
	if events, _ := diag.After(before); len(events) != 1 || events[0].Kind != "plugins.mcp.connect" || !strings.Contains(events[0].Detail, "connection timed out") {
		t.Fatalf("diag events for the outage: %+v", events)
	}
	// The unavailable state carries the real reason, and no address or URL.
	m.mu.Lock()
	shown := m.projection(m.records[a.ID])
	m.mu.Unlock()
	if shown.Unavailable != "connection timed out" || shown.Status != "error" || shown.UnavailableSince == nil {
		t.Fatalf("projection: %+v", shown)
	}
	if strings.Contains(logs.String(), "140.82") || strings.Contains(logs.String(), "gh-token") {
		t.Fatalf("log carries an address or secret:\n%s", logs.String())
	}

	// The network returns: the next probe recovers by itself.
	down.Store(false)
	now = now.Add(time.Hour)
	if err := resync(a.ID); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if err := resync(other.ID); err != nil {
		t.Fatalf("second account after recovery: %v", err)
	}
	if strings.Count(logs.String(), "reachable again") != 1 {
		t.Fatalf("recovery lines:\n%s", logs.String())
	}
	if events, _ := diag.After(before); len(events) != 1 {
		t.Fatalf("diag events after recovery: %+v", events)
	}
	// The mark a failed call left is cleared by the recovery itself.
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.mu.Lock()
		shown = m.projection(m.records[a.ID])
		m.mu.Unlock()
		if shown.Status != "error" || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if shown.Unavailable != "" || shown.Status != "connected" {
		t.Fatalf("still shown unavailable: status %s, %q", shown.Status, shown.Unavailable)
	}
}

// A refused token is the service answering: never an outage, never backed off.
func TestRefusedTokenIsNotAnOutage(t *testing.T) {
	f := newGitHubFixture(t, "octo")
	m := f.manager(t)
	a := addGitHub(t, m, "work", "gh-token")
	base := m.http.Transport
	var refuse atomic.Bool
	m.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if refuse.Load() {
			return &http.Response{StatusCode: 401, Status: "401 Unauthorized", Body: http.NoBody, Header: http.Header{}, Request: r}, nil
		}
		return base.RoundTrip(r)
	})
	refuse.Store(true)
	for i := 0; i < 3; i++ {
		if err := m.syncRemoteTools(context.Background(), a.ID, true, 5*time.Second); !errors.Is(err, errAuth) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if m.outages.open(&record{Account: Account{Endpoint: githubMCP}}) {
		t.Fatal("a refused token opened an outage")
	}
}
