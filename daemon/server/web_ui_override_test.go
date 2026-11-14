package server

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGzip(t *testing.T, path, body string) {
	t.Helper()
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, _ = writer.Write([]byte(body))
	_ = writer.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func getWebUI(s *Server, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Host = "127.0.0.1:9876"
	request.RemoteAddr = "127.0.0.1:50000"
	recorder := httptest.NewRecorder()
	s.webUIHandler().ServeHTTP(recorder, request)
	return recorder
}

// With no build in the web UI directory (or none named), the embedded bundle is
// served, under the NSH client's policy.
func TestWebUIWithoutABuildInItsDirectoryServesTheEmbeddedBundle(t *testing.T) {
	want := webUIPolicy("http://127.0.0.1:9876", "127.0.0.1:9876")
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "web-ui"), t.TempDir()} {
		s := webUITestServer(t)
		s.SetWebUIDir(dir)
		response := getWebUI(s, "/")
		if response.Code != http.StatusOK || response.Body.String() != "<html>mewla</html>" {
			t.Fatalf("dir %q: status=%d body=%q", dir, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Content-Security-Policy"); got != want {
			t.Fatalf("dir %q: policy = %q", dir, got)
		}
		if response.Header().Get("Cross-Origin-Opener-Policy") != "same-origin" || response.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("dir %q: headers = %v", dir, response.Header())
		}
	}
}

// A build in the web UI directory replaces the embedded one, from the next
// request on, with the NSH client's policy; taken away, the embedded one is back.
func TestWebUIServesTheBuildInItsDirectoryWhileItHoldsOne(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "web-ui")
	s := webUITestServer(t)
	s.SetWebUIDir(dir)

	writeGzip(t, filepath.Join(dir, "index.html.gz"), `<base href="/"><html>nsh</html>`)
	writeGzip(t, filepath.Join(dir, "app.353e9fdadf.js.gz"), "app")
	writeGzip(t, filepath.Join(dir, "assets", "icons", "icon.png.gz"), "png")
	writeGzip(t, filepath.Join(dir, "terminal", "mewla-vt.wasm.gz"), "vt")

	page := getWebUI(s, "/sessions/w1")
	if page.Code != http.StatusOK || page.Body.String() != `<base href="/"><html>nsh</html>` {
		t.Fatalf("page: status=%d body=%q", page.Code, page.Body.String())
	}
	policy := page.Header().Get("Content-Security-Policy")
	for _, part := range []string{
		"script-src 'self' http://127.0.0.1:9876 'wasm-unsafe-eval' 'unsafe-inline' blob:",
		"font-src 'self' http://127.0.0.1:9876 data:",
		"connect-src 'self' http://127.0.0.1:9876 ws://127.0.0.1:9876 wss://127.0.0.1:9876",
		"base-uri 'self'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(policy, part) {
			t.Fatalf("policy %q lacks %q", policy, part)
		}
	}
	if page.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index cache = %q", page.Header().Get("Cache-Control"))
	}
	if got := getWebUI(s, "/app.353e9fdadf.js").Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("hashed file cache = %q", got)
	}
	// The package's assets/ keeps plain names: never cached for good.
	if got := getWebUI(s, "/assets/icons/icon.png").Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("plain asset cache = %q", got)
	}
	vt := getWebUI(s, "/terminal/mewla-vt.wasm")
	if vt.Code != http.StatusOK || vt.Header().Get("Access-Control-Allow-Origin") != "*" || vt.Header().Get("Content-Type") != "application/wasm" {
		t.Fatalf("terminal VT: status=%d headers=%v", vt.Code, vt.Header())
	}
	if route := getWebUI(s, "/terminal/w1"); route.Header().Get("Access-Control-Allow-Origin") != "" || route.Body.String() != `<base href="/"><html>nsh</html>` {
		t.Fatalf("terminal route: headers=%v body=%q", route.Header(), route.Body.String())
	}

	// Taken away (the dev daemon's override renamed): the embedded bundle again.
	if err := os.Rename(dir, filepath.Join(root, "web-ui.off")); err != nil {
		t.Fatal(err)
	}
	back := getWebUI(s, "/")
	if back.Body.String() != "<html>mewla</html>" {
		t.Fatalf("after removal: body=%q", back.Body.String())
	}
}

// The directory may be a symlink to the current build, swapped for the next one.
func TestWebUIFollowsItsDirectorySymlinkToTheCurrentBuild(t *testing.T) {
	root := t.TempDir()
	writeGzip(t, filepath.Join(root, "a", "index.html.gz"), "first")
	writeGzip(t, filepath.Join(root, "b", "index.html.gz"), "second")
	link := filepath.Join(root, "web-ui")
	if err := os.Symlink(filepath.Join(root, "a"), link); err != nil {
		t.Fatal(err)
	}
	s := webUITestServer(t)
	s.SetWebUIDir(link)
	if got := getWebUI(s, "/").Body.String(); got != "first" {
		t.Fatalf("first = %q", got)
	}
	next := filepath.Join(root, "web-ui.next")
	if err := os.Symlink(filepath.Join(root, "b"), next); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, link); err != nil {
		t.Fatal(err)
	}
	if got := getWebUI(s, "/").Body.String(); got != "second" {
		t.Fatalf("second = %q", got)
	}
}
