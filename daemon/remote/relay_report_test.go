package remote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

func TestClientReportsItsRelayAndDeclaresTheFeature(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var method, path, features, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, features = r.Method, r.URL.Path, r.Header.Get("X-Mewla-Features")
		raw := make([]byte, 64)
		n, _ := r.Body.Read(raw)
		body = string(raw[:n])
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "", manager)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ReportRelay(context.Background(), "hk"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPut || path != "/v1/machine/relay" || features != "relay-dns" || body != `{"relay":"hk"}` {
		t.Fatalf("request = %s %s features=%q body=%s", method, path, features, body)
	}
}

type scriptedReports struct {
	mu    sync.Mutex
	calls []string
	fail  int // fail this many calls first
	err   error
}

func (s *scriptedReports) ReportRelay(_ context.Context, relay string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, relay)
	if s.fail > 0 {
		s.fail--
		return s.err
	}
	return nil
}

func (s *scriptedReports) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRelayReportIsRetriedUntilTheServiceAcceptsIt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &scriptedReports{fail: 3, err: errors.New("service unreachable")}
	reporter := &relayReporter{ctx: ctx, client: client, logf: t.Logf, backoff: func(int) time.Duration { return time.Millisecond }}
	reporter.report("hk")
	// The loop ends with the report the service took.
	reporter.mu.Lock()
	done := reporter.done
	reporter.mu.Unlock()
	<-done
	if n := len(client.seen()); n != 4 {
		t.Fatalf("%d calls, want 3 failures and the one taken", n)
	}
}

func TestANewerConnectionSupersedesAnUnfinishedReport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &scriptedReports{fail: 1000, err: errors.New("service unreachable")}
	reporter := &relayReporter{ctx: ctx, client: client, logf: t.Logf, backoff: func(int) time.Duration { return 2 * time.Millisecond }}
	reporter.report("hk")
	eventually(t, func() bool { return len(client.seen()) >= 3 })
	reporter.report("main")
	client.mu.Lock()
	client.fail = 0
	client.mu.Unlock()
	// The newer report's loop ends once the service took it; the old one ended before it began.
	reporter.mu.Lock()
	done := reporter.done
	reporter.mu.Unlock()
	<-done
	calls := client.seen()
	first := slices.Index(calls, "main")
	if first < 0 || calls[len(calls)-1] != "main" || slices.Contains(calls[first:], "hk") {
		t.Fatalf("the old relay was reported after the newer one: %v", calls)
	}
}

// landingReports records the reports in the order the service would take them (when a call returns), and holds the
// second report of "hk" in flight until released, as a request already on its way.
type landingReports struct {
	mu       sync.Mutex
	landed   []string
	hkCalls  int
	inFlight chan struct{}
	release  chan struct{}
	main     chan struct{}
}

func (l *landingReports) ReportRelay(_ context.Context, relay string) error {
	l.mu.Lock()
	l.hkCalls += map[bool]int{true: 1}[relay == "hk"]
	hold := relay == "hk" && l.hkCalls == 2
	l.mu.Unlock()
	if hold {
		close(l.inFlight)
		<-l.release
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.landed = append(l.landed, relay)
	if relay == "main" {
		close(l.main)
		return nil
	}
	return errors.New("service unreachable")
}

// A report of the old relay already in flight when a newer connection comes lands before the newer report, never
// after it: the newer one waits for the old one to end (the service would otherwise keep the old relay).
func TestAnOldReportInFlightNeverLandsAfterTheNewerOne(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &landingReports{inFlight: make(chan struct{}), release: make(chan struct{}), main: make(chan struct{})}
	waiting := make(chan struct{})
	reporter := &relayReporter{ctx: ctx, client: client, logf: t.Logf, backoff: func(int) time.Duration { return 0 },
		superseded: func() { close(waiting) }}
	reporter.report("hk")
	<-client.inFlight
	reporter.report("main")
	select {
	case <-waiting:
	case <-client.main:
		t.Fatal("the newer report landed while the old one was still in flight")
	}
	close(client.release)
	<-client.main
	client.mu.Lock()
	defer client.mu.Unlock()
	if n := len(client.landed); n < 2 || client.landed[n-1] != "main" || slices.Contains(client.landed[:n-1], "main") {
		t.Fatalf("landed %v, want the old relay's reports before the newer one", client.landed)
	}
}

func TestRelayReportStopsWhenSignedOutAndIsSafeWithoutAReporter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &scriptedReports{fail: 1000, err: ErrSignedOut}
	reporter := &relayReporter{ctx: ctx, client: client, logf: t.Logf, backoff: func(int) time.Duration { return time.Millisecond }}
	reporter.report("hk")
	time.Sleep(50 * time.Millisecond)
	if n := len(client.seen()); n != 1 {
		t.Fatalf("retried a signed-out report %d times", n)
	}
	var none *relayReporter
	none.report("hk") // a computer with one relay has no reporter
}
