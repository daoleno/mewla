// Package webui serves the browser build of the Mewla app from the daemon
// binary. scripts/build-web-ui.sh exports the Expo web bundle into dist/ and
// gzips every file; only the .gz files are embedded.
package webui

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

const (
	indexFile    = "index.html"
	gzipSuffix   = ".gz"
	immutableAge = "public, max-age=31536000, immutable"
)

// Hashed build outputs never change content under the same name.
var immutablePrefixes = []string{"_expo/static/", "assets/"}

var contentTypes = map[string]string{
	".css":  "text/css; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".json": "application/json",
	".otf":  "font/otf",
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".ttf":  "font/ttf",
	".wasm": "application/wasm",
}

// Embedded returns the bundle compiled into this binary.
func Embedded() fs.FS {
	files, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return files
}

// Handler serves a gzip-precompressed bundle. Paths without a file extension
// are app routes and receive index.html.
func Handler(files fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = indexFile
		}
		body, err := fs.ReadFile(files, name+gzipSuffix)
		if err != nil && path.Ext(name) == "" {
			name = indexFile
			body, err = fs.ReadFile(files, name+gzipSuffix)
		}
		if err != nil {
			if name == indexFile {
				http.Error(w, "This zen build does not include the web UI. Run scripts/build-web-ui.sh, then rebuild zen.", http.StatusNotFound)
				return
			}
			http.NotFound(w, r)
			return
		}

		header := w.Header()
		header.Set("Content-Type", contentType(name))
		header.Set("Cache-Control", cacheControl(name))
		header.Add("Vary", "Accept-Encoding")
		if r.Method == http.MethodHead {
			return
		}
		if acceptsGzip(r) {
			header.Set("Content-Encoding", "gzip")
			_, _ = w.Write(body)
			return
		}
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			http.Error(w, "corrupt web UI asset", http.StatusInternalServerError)
			return
		}
		_, _ = io.Copy(w, reader)
	})
}

func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if value, ok := contentTypes[ext]; ok {
		return value
	}
	if value := mime.TypeByExtension(ext); value != "" {
		return value
	}
	return "application/octet-stream"
}

func cacheControl(name string) string {
	for _, prefix := range immutablePrefixes {
		if strings.HasPrefix(name, prefix) {
			return immutableAge
		}
	}
	return "no-cache"
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			continue
		}
		return strings.ReplaceAll(strings.TrimSpace(params), " ", "") != "q=0"
	}
	return false
}
