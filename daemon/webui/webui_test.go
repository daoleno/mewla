package webui

import (
	"bytes"
	"compress/gzip"
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
		"index.html.gz":                     {Data: gz(t, "<html>app</html>")},
		"_expo/static/js/web/index-1.js.gz": {Data: gz(t, "console.log(1)")},
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
	response := get(Handler(bundle(t)), "/_expo/static/js/web/index-1.js", "br, gzip")
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

func TestPackageCacheControlCachesOnlyHashedNames(t *testing.T) {
	for name, want := range map[string]string{
		"index.html":                       "no-cache",
		"manifest.webmanifest":             "no-cache",
		"app.353e9fdadf.js":                immutableAge,
		"nsh.3188d92489.wasm":              immutableAge,
		"browser.bddb1349e4/bootstrap.js":  immutableAge,
		"assets/icons/icon-192.png":        "no-cache",
		"terminal/mewla-vt.wasm":           "no-cache",
		"_expo/static/js/web/entry-abc.js": "no-cache",
	} {
		if got := packageCacheControl(name); got != want {
			t.Fatalf("packageCacheControl(%q) = %q, want %q", name, got, want)
		}
	}
	// The embedded bundle's rules are unchanged.
	if cacheControl("assets/x.png") != immutableAge || cacheControl("index.html") != "no-cache" {
		t.Fatal("embedded cache rules changed")
	}
}

func TestPackageRevalidatesFixedNamesWithETag(t *testing.T) {
	files := fstest.MapFS{
		"index.html.gz":           {Data: gz(t, "<html>app</html>")},
		"terminal/terminal.js.gz": {Data: gz(t, "terminal()")},
		"app.353e9fdadf.js.gz":    {Data: gz(t, "app()")},
	}
	handler := PackageHandler(files)
	conditional := func(path, acceptEncoding, ifNoneMatch string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept-Encoding", acceptEncoding)
		request.Header.Set("If-None-Match", ifNoneMatch)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	first := get(handler, "/terminal/terminal.js", "gzip")
	tag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || tag == "" || first.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("first fetch: %d, ETag %q, Cache-Control %q", first.Code, tag, first.Header().Get("Cache-Control"))
	}
	// The same file asked again with its tag: 304 and no body.
	again := conditional("/terminal/terminal.js", "gzip", tag)
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 || again.Header().Get("ETag") != tag {
		t.Fatalf("revalidation: %d, %d bytes, ETag %q", again.Code, again.Body.Len(), again.Header().Get("ETag"))
	}
	if weak := conditional("/terminal/terminal.js", "gzip", `"other", W/`+tag); weak.Code != http.StatusNotModified {
		t.Fatalf("a weak tag in a list: %d", weak.Code)
	}
	// The decompressed form is another representation, with its own tag.
	plain := conditional("/terminal/terminal.js", "", tag)
	if plain.Code != http.StatusOK || plain.Body.String() != "terminal()" || plain.Header().Get("ETag") == tag {
		t.Fatalf("plain with the gzip tag: %d %q, ETag %q", plain.Code, plain.Body.String(), plain.Header().Get("ETag"))
	}
	// A changed file (a new build behind the same name) answers in full with a new tag.
	files["terminal/terminal.js.gz"] = &fstest.MapFile{Data: gz(t, "terminal(2)")}
	changed := conditional("/terminal/terminal.js", "gzip", tag)
	if changed.Code != http.StatusOK || changed.Header().Get("ETag") == tag {
		t.Fatalf("changed file: %d, ETag %q", changed.Code, changed.Header().Get("ETag"))
	}
	// index.html, and an app route that serves it, revalidate the same way.
	index := get(handler, "/brain", "gzip")
	if index.Header().Get("ETag") == "" || conditional("/", "gzip", index.Header().Get("ETag")).Code != http.StatusNotModified {
		t.Fatal("index.html is not revalidated by its tag")
	}
	// Hashed names are cached for good and need no tag.
	if hashed := get(handler, "/app.353e9fdadf.js", "gzip"); hashed.Header().Get("ETag") != "" || hashed.Header().Get("Cache-Control") != immutableAge {
		t.Fatalf("hashed file: ETag %q, Cache-Control %q", hashed.Header().Get("ETag"), hashed.Header().Get("Cache-Control"))
	}
}
