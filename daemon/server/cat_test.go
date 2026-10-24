package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/catpark"
)

func serveCat(s *Server, host, remote, target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Host = host
	request.RemoteAddr = remote
	recorder := httptest.NewRecorder()
	s.handleCatBundle(recorder, request)
	return recorder
}

func TestCatBundleServesLoopbackWithFrameLimits(t *testing.T) {
	s := webUITestServer(t)
	page := serveCat(s, "127.0.0.1:9876", "127.0.0.1:50000", "/cat/")
	if page.Code != http.StatusOK || !strings.Contains(page.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("status=%d type=%q", page.Code, page.Header().Get("Content-Type"))
	}
	if page.Header().Get("X-Frame-Options") != "SAMEORIGIN" ||
		!strings.Contains(page.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") ||
		!strings.Contains(page.Header().Get("Content-Security-Policy"), "connect-src 'none'") {
		t.Fatalf("headers: %v", page.Header())
	}

	manifestResponse := serveCat(s, "127.0.0.1:9876", "127.0.0.1:50000", "/cat/manifest.json")
	var manifest catpark.Manifest
	if err := json.Unmarshal(manifestResponse.Body.Bytes(), &manifest); err != nil || manifest.Version == "" {
		t.Fatalf("manifest: %v %s", err, manifestResponse.Body.String())
	}
	found := false
	for _, file := range manifest.Files {
		found = found || file.Path == "index.html"
		image := serveCat(s, "127.0.0.1:9876", "127.0.0.1:50000", "/cat/"+file.Path)
		if image.Code != http.StatusOK || int64(image.Body.Len()) != file.Size {
			t.Fatalf("%s: status=%d size=%d want %d", file.Path, image.Code, image.Body.Len(), file.Size)
		}
	}
	if !found {
		t.Fatal("manifest lacks index.html")
	}
}

func TestCatBundleRefusesStrangersAndTraversal(t *testing.T) {
	s := webUITestServer(t)
	if r := serveCat(s, "192.168.1.5:9876", "192.168.1.9:50000", "/cat/"); r.Code != http.StatusNotFound {
		t.Fatalf("a LAN browser is not a web UI origin: %d", r.Code)
	}
	if r := serveCat(s, "127.0.0.1:9876", "127.0.0.1:50000", "/cat/../../etc/passwd"); r.Code != http.StatusNotFound {
		t.Fatalf("traversal: %d", r.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/cat/", nil)
	request.Host, request.RemoteAddr = "192.168.1.5:9876", "192.168.1.9:50000"
	request.Header.Set("Authorization", "Device v1:forged")
	recorder := httptest.NewRecorder()
	s.handleCatBundle(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("a forged signature must not pass: %d", recorder.Code)
	}
}

func TestObserveCatSurvivesAnUnopenedState(t *testing.T) {
	s := &Server{}
	s.observeCat(brain.Snapshot{}) // no state directory: the cat sleeps, nothing breaks
}
