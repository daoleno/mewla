package remote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	eventually(t, func() bool { return len(client.seen()) == 4 })
	time.Sleep(30 * time.Millisecond)
	if n := len(client.seen()); n != 4 {
		t.Fatalf("kept reporting after success: %d calls", n)
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
	eventually(t, func() bool {
		calls := client.seen()
		return calls[len(calls)-1] == "main"
	})
	mark := len(client.seen())
	time.Sleep(40 * time.Millisecond)
	for _, call := range client.seen()[mark-1:] {
		if call == "hk" {
			t.Fatal("the old relay was still being reported after a newer one")
		}
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
