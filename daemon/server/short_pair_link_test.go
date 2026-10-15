package server

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/watcher"
)

func TestShortPairLinkRedirectsThisComputerToThePairingPage(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	handler := srv.Handler()
	token := strings.Repeat("\x07", 32)
	path := "/p/" + base64.RawURLEncoding.EncodeToString([]byte(token))

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9876"+path, nil)
	request.RemoteAddr = "127.0.0.1:50000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}
	location := response.Header().Get("Location")
	fragment, ok := strings.CutPrefix(location, "/#")
	if !ok {
		t.Fatalf("location = %q", location)
	}
	values, err := url.ParseQuery(fragment)
	if err != nil || !strings.HasPrefix(values.Get("pair"), "mewla://settings?p=") {
		t.Fatalf("pair link = %q (%v)", values.Get("pair"), err)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control = %q", response.Header().Get("Cache-Control"))
	}

	for name, mutate := range map[string]func(*http.Request){
		"remote client":     func(r *http.Request) { r.RemoteAddr = "192.168.1.9:50000" },
		"non-loopback host": func(r *http.Request) { r.Host = "quiet-otter.m.mewla.ai:443" },
		"short token":       func(r *http.Request) { r.URL.Path = "/p/abc" },
	} {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9876"+path, nil)
		request.RemoteAddr = "127.0.0.1:50000"
		mutate(request)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", name, response.Code)
		}
	}
}
