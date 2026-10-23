package clientaddr

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
)

func TestIPPrefersTheRelayReportedClient(t *testing.T) {
	request := httptest.NewRequest("POST", "/enrollment/request", nil)
	request.RemoteAddr = "10.0.0.9:443"
	if got := IP(request).String(); got != "10.0.0.9" {
		t.Fatalf("without relay address IP = %s", got)
	}
	request = request.WithContext(With(context.Background(), net.ParseIP("203.0.113.7")))
	if got := IP(request).String(); got != "203.0.113.7" {
		t.Fatalf("with relay address IP = %s", got)
	}
}

func TestKeyGroupsIPv6By64(t *testing.T) {
	a := Key(net.ParseIP("2001:db8:1:2::1"))
	b := Key(net.ParseIP("2001:db8:1:2:ffff::9"))
	c := Key(net.ParseIP("2001:db8:1:3::1"))
	if a != b || a == c {
		t.Fatalf("keys a=%s b=%s c=%s", a, b, c)
	}
	if got := Key(net.ParseIP("::ffff:198.51.100.4")); got != "198.51.100.4" {
		t.Fatalf("mapped IPv4 key = %s", got)
	}
}

func TestParseRejectsAnythingButAnIP(t *testing.T) {
	for _, value := range []string{"", "1.2.3.4:80", "2001:db8::/64", "example.com", "1.2.3.4\n"} {
		if Parse(value) != nil {
			t.Fatalf("Parse(%q) accepted", value)
		}
	}
	if Parse("2001:db8::1") == nil || Parse("198.51.100.4") == nil {
		t.Fatal("Parse rejected a valid IP")
	}
}

func TestPathNamesHowARequestArrived(t *testing.T) {
	for remote, want := range map[string]string{
		"127.0.0.1:1":           "loopback",
		"[::1]:1":               "loopback",
		"100.73.112.8:1":        "tailscale",
		"[fd7a:115c:a1e0::1]:1": "tailscale",
		"192.168.1.20:1":        "lan",
		"10.0.0.4:1":            "lan",
		"[fe80::1]:1":           "lan",
		"203.0.113.7:1":         "direct",
		"not an address":        "direct",
	} {
		request := httptest.NewRequest("GET", "/screen/viewer", nil)
		request.RemoteAddr = remote
		if got := Path(request); got != want {
			t.Errorf("Path(%s) = %s, want %s", remote, got, want)
		}
	}
	relayed := httptest.NewRequest("GET", "/screen/viewer", nil)
	relayed.RemoteAddr = "192.168.1.20:1"
	if got := Path(relayed.WithContext(MarkRelayed(relayed.Context()))); got != "relay" {
		t.Errorf("a marked request is %s", got)
	}
	if got := Path(relayed.WithContext(With(relayed.Context(), net.ParseIP("203.0.113.7")))); got != "relay" {
		t.Errorf("a request with a relay-reported client is %s", got)
	}
}
