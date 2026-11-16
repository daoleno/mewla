package remote

import (
	"context"
	"encoding/json"
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

// relayService is the service's relay endpoint as the real Client reaches it: it keeps the relay it last applied,
// and holds the first "hk" request after it arrived, before it applies it, until released.
type relayService struct {
	mu         sync.Mutex
	relay      string
	held       bool
	hkArrived  chan struct{}
	release    chan struct{}
	hkApplied  chan struct{}
	mainLanded chan struct{}
}

func (s *relayService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct{ Relay string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	hold := body.Relay == "hk" && !s.held
	s.held = s.held || hold
	s.mu.Unlock()
	if hold {
		close(s.hkArrived)
		<-s.release // the request is on the service, being applied, whatever the client does meanwhile
	}
	s.mu.Lock()
	s.relay = body.Relay
	s.mu.Unlock()
	switch {
	case hold:
		close(s.hkApplied)
	case body.Relay == "main":
		close(s.mainLanded)
	}
	w.WriteHeader(http.StatusNoContent)
}

// With the real Client: a report of the old relay the service is already applying when a newer connection comes is
// not abandoned for the newer one; the newer report goes once the service answered the old, so the service keeps the
// newer relay. (Cancelling the old request ended it at the client only; the service still applied it, last.)
func TestTheServiceKeepsTheNewerRelayWhenAnOldReportIsBeingApplied(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := &relayService{hkArrived: make(chan struct{}), release: make(chan struct{}), hkApplied: make(chan struct{}), mainLanded: make(chan struct{})}
	server := httptest.NewServer(service)
	t.Cleanup(server.Close)
	// Whatever happens, the held request is let go before the server closes, which waits for it: a failure ends.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(service.release) }) }
	t.Cleanup(release)
	client, err := NewClient(server.URL, "", manager)
	if err != nil {
		t.Fatal(err)
	}
	obs := &observedReports{inner: client, hkReturned: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan struct{})
	reporter := &relayReporter{ctx: ctx, client: obs, logf: t.Logf, backoff: func(int) time.Duration { return 0 },
		superseded: func() { close(waiting) }}
	reporter.report("hk")
	<-service.hkArrived
	reporter.report("main")
	// The newer report goes ahead as far as it can before the service finishes the old one: if the old request was
	// given up at the client, the newer one lands now; if not, the newer one waits for it.
	select {
	case <-obs.hkReturned:
		t.Fatalf("the old report ended at the client while the service was still applying it: %v", obs.hkErr)
	case <-waiting:
		// A newer report must not cancel the old one's request: the service may be applying it whatever the client
		// does. (Cancelled in report() itself, its context is already done by the time the newer report waits.)
		if err := obs.hkContext().Err(); err != nil {
			t.Fatalf("the newer report cancelled the old one's request in flight: %v", err)
		}
	}
	release()
	<-service.hkApplied
	<-service.mainLanded
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.relay != "main" {
		t.Fatalf("the service keeps relay %q, want the newer %q", service.relay, "main")
	}
}

// observedReports passes reports to the real Client, keeps the context of the first "hk" call and says when that
// call returned at the client.
type observedReports struct {
	inner      relayReporterClient
	hkReturned chan struct{}
	mu         sync.Mutex
	hkCtx      context.Context
	hkErr      error
}

func (o *observedReports) ReportRelay(ctx context.Context, relay string) error {
	o.mu.Lock()
	first := relay == "hk" && o.hkCtx == nil
	if first {
		o.hkCtx = ctx
	}
	o.mu.Unlock()
	err := o.inner.ReportRelay(ctx, relay)
	if first {
		o.mu.Lock()
		o.hkErr = err
		o.mu.Unlock()
		close(o.hkReturned)
	}
	return err
}

func (o *observedReports) hkContext() context.Context {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.hkCtx
}
