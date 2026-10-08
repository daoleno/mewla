package server

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/watcher"
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

const browserPageAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"

func TestPrefersHTML(t *testing.T) {
	cases := map[string]bool{
		browserPageAccept:                   true,
		"text/html":                         true,
		"application/json;q=0.5, text/html": true,
		"":                                  false,
		"*/*":                               false,
		"application/json":                  false,
		"application/json, text/html;q=0.5": false,
		"text/html;q=0":                     false,
		"text/plain, */*;q=0.1":             false,
		"TEXT/HTML ; Q=0.9, application/json;q=0.8": true,
	}
	for accept, want := range cases {
		if got := prefersHTML(accept); got != want {
			t.Errorf("prefersHTML(%q) = %v, want %v", accept, got, want)
		}
	}
}

// Reloading /resources or /browser in the web app must reopen the page, while
// the app's own requests to the same paths keep the daemon API.
func TestAppRoutesServeTheWebUIToPageLoadsOnly(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.webUIFiles = webUITestServer(t).webUIFiles
	handler := srv.Handler()
	serve := func(method, path, accept, host, remote string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, nil)
		request.Host = host
		request.RemoteAddr = remote
		if accept != "" {
			request.Header.Set("Accept", accept)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	const loopbackHost, loopbackRemote = "127.0.0.1:9876", "127.0.0.1:50000"
	for _, path := range []string{"/resources", "/browser", "/resources?tab=processes"} {
		response := serve(http.MethodGet, path, browserPageAccept, loopbackHost, loopbackRemote)
		if response.Code != http.StatusOK || response.Body.String() != "<html>mewla</html>" {
			t.Fatalf("page load %s: status=%d body=%q", path, response.Code, response.Body.String())
		}
		if response.Header().Get("Content-Security-Policy") == "" || response.Header().Values("Vary")[0] != "Accept" {
			t.Fatalf("page load %s served without the web UI headers", path)
		}
	}
	apiCases := []struct{ method, path, accept, host, remote string }{
		// fetch() and API clients send */* or JSON.
		{http.MethodGet, "/resources", "", loopbackHost, loopbackRemote},
		{http.MethodGet, "/resources", "*/*", loopbackHost, loopbackRemote},
		{http.MethodGet, "/resources", "application/json", loopbackHost, loopbackRemote},
		// Browser requests are POSTs whatever they accept.
		{http.MethodPost, "/browser", browserPageAccept, loopbackHost, loopbackRemote},
		// Hosts outside the web UI never see the app.
		{http.MethodGet, "/resources", browserPageAccept, "192.168.1.20:9876", "192.168.1.30:50000"},
	}
	for _, tc := range apiCases {
		response := serve(tc.method, tc.path, tc.accept, tc.host, tc.remote)
		if response.Code == http.StatusOK || strings.Contains(response.Body.String(), "<html>") {
			t.Fatalf("%s %s accept=%q host=%s: status=%d body=%q", tc.method, tc.path, tc.accept, tc.host, response.Code, response.Body.String())
		}
	}
}
