// Package webui serves the browser build of the Mewla app from the daemon
// binary. scripts/build-web-ui.sh exports the Expo web bundle into dist/ and
// gzips every file; only the .gz files are embedded.
package webui

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
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

// In an NSH web package (`nsh package --target web`) each hashed file, or the
// folder it is in, is named `<name>.<10 hex>`; its `assets/` keeps plain names.
var hashedSegment = regexp.MustCompile(`^[^.]+(\.[^.]+)*\.[0-9a-f]{10}(\.[A-Za-z0-9]+)?$`)

var contentTypes = map[string]string{
	".css":         "text/css; charset=utf-8",
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".json":        "application/json",
	".otf":         "font/otf",
	".png":         "image/png",
	".svg":         "image/svg+xml",
	".ttf":         "font/ttf",
	".wasm":        "application/wasm",
	".webmanifest": "application/manifest+json",
	".webp":        "image/webp",
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
	return handler(files, cacheControl)
}

// PackageHandler serves a gzip-precompressed NSH web package the same way;
// only its hashed names are cached for good.
func PackageHandler(files fs.FS) http.Handler {
	return handler(files, packageCacheControl)
}

func handler(files fs.FS, cacheControl func(string) string) http.Handler {
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
				http.Error(w, "This mewla build does not include the web UI. Run scripts/build-web-ui.sh, then rebuild mewla.", http.StatusNotFound)
				return
			}
			http.NotFound(w, r)
			return
		}

		header := w.Header()
		header.Set("Content-Type", contentType(name))
		policy := cacheControl(name)
		header.Set("Cache-Control", policy)
		header.Add("Vary", "Accept-Encoding")
		gzipped := acceptsGzip(r)
		// A name that can change (index.html, a package's fixed names such as terminal/) is revalidated on every
		// use: its ETag lets the browser and a cache in front keep it and get 304 Not Modified while it is the same.
		if policy != immutableAge {
			tag := entityTag(body, gzipped)
			header.Set("ETag", tag)
			if matchesNoneOf(r.Header.Get("If-None-Match"), tag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		if r.Method == http.MethodHead {
			return
		}
		if gzipped {
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

func packageCacheControl(name string) string {
	for _, segment := range strings.Split(name, "/") {
		if hashedSegment.MatchString(segment) {
			return immutableAge
		}
	}
	return "no-cache"
}

// entityTag names a file's content as served: the gzip bytes and their decompressed form are two representations.
func entityTag(body []byte, gzipped bool) string {
	sum := sha256.Sum256(body)
	tag := hex.EncodeToString(sum[:12])
	if gzipped {
		tag += "-gzip"
	}
	return `"` + tag + `"`
}

// matchesNoneOf reports whether If-None-Match names tag (weak comparison, as RFC 9110 asks for it), or is "*".
func matchesNoneOf(ifNoneMatch, tag string) bool {
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate == tag {
			return true
		}
	}
	return false
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
