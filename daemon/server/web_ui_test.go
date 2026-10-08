package server

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func webUITestServer(t *testing.T, origins ...string) *Server {
	t.Helper()
	var index bytes.Buffer
	writer := gzip.NewWriter(&index)
	_, _ = writer.Write([]byte("<html>mewla</html>"))
	_ = writer.Close()
	s := &Server{webUIFiles: fstest.MapFS{"index.html.gz": {Data: index.Bytes()}}}
	s.SetWebOrigins(origins)
	return s
}

func serveWebUI(s *Server, host, remoteAddr string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = host
	request.RemoteAddr = remoteAddr
	recorder := httptest.NewRecorder()
	s.webUIHandler().ServeHTTP(recorder, request)
	return recorder
}

func TestWebUIServesLoopbackClientsOfLoopbackHosts(t *testing.T) {
	s := webUITestServer(t)
	for _, host := range []string{"127.0.0.1:9876", "localhost:9876", "[::1]:9876"} {
		response := serveWebUI(s, host, "127.0.0.1:50000")
		if response.Code != http.StatusOK || response.Body.String() != "<html>mewla</html>" {
			t.Fatalf("%s: status=%d body=%q", host, response.Code, response.Body.String())
		}
		policy := response.Header().Get("Content-Security-Policy")
		if !strings.Contains(policy, "connect-src 'self' ws://"+host+" wss://"+host) ||
			!strings.Contains(policy, "frame-ancestors 'none'") {
			t.Fatalf("%s: policy = %q", host, policy)
		}
	}
}

func TestWebUIIsNotServedToTheNetworkByDefault(t *testing.T) {
	s := webUITestServer(t)
	cases := []struct{ host, remote string }{
		{"192.168.1.20:9876", "192.168.1.30:50000"},
		// A LAN peer cannot claim a loopback Host header.
		{"127.0.0.1:9876", "192.168.1.30:50000"},
		// DNS rebinding presents a foreign name to a loopback socket.
		{"attacker.example:9876", "127.0.0.1:50000"},
	}
	for _, tc := range cases {
		if response := serveWebUI(s, tc.host, tc.remote); response.Code != http.StatusNotFound {
			t.Fatalf("host=%s remote=%s status=%d", tc.host, tc.remote, response.Code)
		}
	}
}

func TestWebUIServesExplicitHTTPSOrigins(t *testing.T) {
	origin, err := ParseWebOrigin("https://Mewla.Example.ts.net:443/")
	if err != nil {
		t.Fatal(err)
	}
	if origin != "https://mewla.example.ts.net" {
		t.Fatalf("origin = %q", origin)
	}
	s := webUITestServer(t, origin)
	if response := serveWebUI(s, "mewla.example.ts.net", "127.0.0.1:50000"); response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if response := serveWebUI(s, "other.example.ts.net", "127.0.0.1:50000"); response.Code != http.StatusNotFound {
		t.Fatalf("other origin status = %d", response.Code)
	}
}

func TestParseWebOriginRequiresBareHTTPSOrigins(t *testing.T) {
	for _, raw := range []string{
		"http://mewla.example.ts.net",
		"https://",
		"https://mewla.example.ts.net/app",
		"https://user@mewla.example.ts.net",
		"https://mewla.example.ts.net?x=1",
	} {
		if _, err := ParseWebOrigin(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
