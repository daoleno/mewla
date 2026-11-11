package webui

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func gz(t *testing.T, value string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write([]byte(value)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func bundle(t *testing.T) fstest.MapFS {
	return fstest.MapFS{
		"index.html.gz":           {Data: gz(t, "<html>app</html>")},
		"app.242ef32319.js.gz":    {Data: gz(t, "console.log(1)")},
		"manifest.webmanifest.gz": {Data: gz(t, "{}")},
	}
}

func get(handler http.Handler, path, acceptEncoding string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptEncoding != "" {
		request.Header.Set("Accept-Encoding", acceptEncoding)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestHandlerServesPrecompressedAssetsToGzipClients(t *testing.T) {
	response := get(Handler(bundle(t)), "/app.242ef32319.js", "br, gzip")
	if response.Code != http.StatusOK || response.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("status=%d encoding=%q", response.Code, response.Header().Get("Content-Encoding"))
	}
	if got := response.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != immutableAge {
		t.Fatalf("hashed asset cache = %q", got)
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(reader)
	if string(body) != "console.log(1)" {
		t.Fatalf("body = %q", body)
	}
}

func TestHandlerDecompressesForClientsWithoutGzip(t *testing.T) {
	response := get(Handler(bundle(t)), "/", "gzip;q=0")
	if response.Header().Get("Content-Encoding") != "" || response.Body.String() != "<html>app</html>" {
		t.Fatalf("encoding=%q body=%q", response.Header().Get("Content-Encoding"), response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("index cache = %q", got)
	}
}

func TestHandlerRoutesAppPathsToIndexAndMissingFilesTo404(t *testing.T) {
	handler := Handler(bundle(t))
	if response := get(handler, "/terminal/worker-1", ""); response.Body.String() != "<html>app</html>" {
		t.Fatalf("app route body = %q", response.Body.String())
	}
	if response := get(handler, "/missing.js", ""); response.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d", response.Code)
	}
	if response := get(handler, "/../../index.html", ""); response.Body.String() != "<html>app</html>" {
		t.Fatalf("traversal is not confined to the bundle: %q", response.Body.String())
	}
}

func TestHandlerExplainsBuildsWithoutTheWebUI(t *testing.T) {
	response := get(Handler(fstest.MapFS{}), "/", "")
	if response.Code != http.StatusNotFound || !bytes.Contains(response.Body.Bytes(), []byte("build-web-ui.sh")) {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestHandlerRejectsWrites(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	recorder := httptest.NewRecorder()
	Handler(bundle(t)).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestCacheControlKeepsHashedNamesForever(t *testing.T) {
	for name, want := range map[string]string{
		"nsh.5be0579ad8.wasm":                   immutableAge,
		"browser.991ab24c49/runtime.js":         immutableAge,
		"assets/icons/agents/pi.2c3120d5c4.svg": immutableAge,
		"index.html":                            "no-cache",
		"manifest.webmanifest":                  "no-cache",
		"assets/icons/readme.svg":               "no-cache",
	} {
		if got := cacheControl(name); got != want {
			t.Errorf("%s: cache = %q, want %q", name, got, want)
		}
	}
}

func TestInlineScriptHashesNameThePageScripts(t *testing.T) {
	page := "<head><script>const a = 1;</script><script src=\"x.js\"></script></head><body><script type=\"module\">\n  import \"./b.js\";\n</script></body>"
	got := InlineScriptHashes(fstest.MapFS{"index.html.gz": {Data: gz(t, page)}})
	// sha256 of the exact text between the tags, as browsers hash it.
	want := []string{"'sha256-" + sum("const a = 1;") + "'", "'sha256-" + sum("\n  import \"./b.js\";\n") + "'"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("hashes = %q, want %q", got, want)
	}
	if InlineScriptHashes(fstest.MapFS{}) != nil {
		t.Fatal("a build without the web UI has no hashes")
	}
}

func sum(text string) string {
	digest := sha256.Sum256([]byte(text))
	return base64.StdEncoding.EncodeToString(digest[:])
}
