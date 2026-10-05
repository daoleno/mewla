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
