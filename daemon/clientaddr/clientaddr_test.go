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

func TestLocalIsOnlyABrowserOnThisComputer(t *testing.T) {
	type req struct {
		remote, host string
		header       map[string]string
		relayed      bool
		relayIP      string
	}
	for name, tc := range map[string]struct {
		req  req
		want bool
	}{
		"ipv4 loopback":         {req{remote: "127.0.0.1:5", host: "127.0.0.1:9876"}, true},
		"ipv6 loopback":         {req{remote: "[::1]:5", host: "[::1]:9876"}, true},
		"localhost name":        {req{remote: "127.0.0.1:5", host: "localhost:9876"}, true},
		"tailnet phone":         {req{remote: "100.73.112.9:5", host: "100.73.112.8:9877"}, false},
		"lan device":            {req{remote: "192.168.1.20:5", host: "192.168.1.8:9877"}, false},
		"relayed, no client ip": {req{remote: "127.0.0.1:5", host: "127.0.0.1:9876", relayed: true}, false},
		"relayed with client":   {req{remote: "127.0.0.1:5", host: "127.0.0.1:9876", relayIP: "203.0.113.7"}, false},
		"tunnel keeps its host": {req{remote: "127.0.0.1:5", host: "mac.example.com"}, false},
		"proxy rewrites host":   {req{remote: "127.0.0.1:5", host: "127.0.0.1:9876", header: map[string]string{"X-Forwarded-For": "203.0.113.7"}}, false},
		"cloudflare":            {req{remote: "127.0.0.1:5", host: "127.0.0.1:9876", header: map[string]string{"Cf-Connecting-Ip": "203.0.113.7"}}, false},
		"forwarded":             {req{remote: "127.0.0.1:5", host: "127.0.0.1:9876", header: map[string]string{"Forwarded": "for=203.0.113.7"}}, false},
		"tailscale serve":       {req{remote: "127.0.0.1:5", host: "127.0.0.1:9876", header: map[string]string{"Tailscale-User-Login": "a@b"}}, false},
	} {
		request := httptest.NewRequest("GET", "/screen", nil)
		request.RemoteAddr, request.Host = tc.req.remote, tc.req.host
		for k, v := range tc.req.header {
			request.Header.Set(k, v)
		}
		if tc.req.relayed {
			request = request.WithContext(MarkRelayed(request.Context()))
		}
		if tc.req.relayIP != "" {
			request = request.WithContext(With(request.Context(), net.ParseIP(tc.req.relayIP)))
		}
		if got := Local(request); got != tc.want {
			t.Errorf("%s: Local = %v, want %v", name, got, tc.want)
		}
	}
}
