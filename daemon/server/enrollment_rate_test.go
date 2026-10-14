package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/clientaddr"
)

func TestEnrollmentRateLimitKeysOnTheRelayReportedClient(t *testing.T) {
	s := &Server{enrollmentRates: make(map[string][]time.Time)}
	// Every request through Link shares the relay connection's RemoteAddr.
	from := func(client string) *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/enrollment/request", nil)
		request.RemoteAddr = "127.0.0.1:40000"
		return request.WithContext(clientaddr.With(request.Context(), net.ParseIP(client)))
	}
	for attempt := range 5 {
		if !s.allowEnrollmentRequest(from("203.0.113.9")) {
			t.Fatalf("attacker request %d refused early", attempt+1)
		}
	}
	if s.allowEnrollmentRequest(from("203.0.113.9")) {
		t.Fatal("attacker exceeded 5 requests a minute")
	}
	if s.allowEnrollmentRequest(from("2001:db8:1:2::1")) == false {
		t.Fatal("another client was blocked by the attacker's limit")
	}
	for range 4 {
		s.allowEnrollmentRequest(from("2001:db8:1:2::1"))
	}
	if s.allowEnrollmentRequest(from("2001:db8:1:2:ffff::7")) {
		t.Fatal("an address in the same IPv6 /64 got a fresh allowance")
	}
	direct := httptest.NewRequest(http.MethodPost, "/enrollment/request", nil)
	direct.RemoteAddr = "192.0.2.5:5000"
	if !s.allowEnrollmentRequest(direct) {
		t.Fatal("a direct request without a relay-reported client was refused")
	}
}

func TestRelayReportedLoopbackClientIsNotTrustedAsLocal(t *testing.T) {
	s := &Server{}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	request.RemoteAddr = "192.0.2.5:5000"
	request = request.WithContext(clientaddr.With(request.Context(), net.ParseIP("127.0.0.1")))
	if s.webUIAdmitted(request) {
		t.Fatal("loopback trust must come from RemoteAddr, not the relay-reported client")
	}
}
