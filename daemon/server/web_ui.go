package server

import (
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/enrollment"
	"github.com/daoleno/mewla/daemon/webui"
)

// parseWebOrigin canonicalizes a remote browser origin. Remote browsers cannot
// pin the daemon's Link certificate, so the origin must be https behind a
// proxy whose certificate the browser already trusts.
func parseWebOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse web origin: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", fmt.Errorf("web origin %q must be https://<host>", raw)
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("web origin %q must not include credentials, a path, a query or a fragment", raw)
	}
	host := strings.ToLower(parsed.Host)
	host = strings.TrimSuffix(host, ":443")
	return "https://" + host, nil
}

// SetAddressBook binds web admission to the daemon-owned, live-reloaded
// address book: its https entries are the remote web UI origins.
func (s *Server) SetAddressBook(book *addressbook.Store) { s.addresses = book }

func (s *Server) SetEnrollmentManager(manager *enrollment.Manager) { s.enrollments = manager }

// SetEnrollmentObserver is told about each new browser join request, so the
// daemon can show its number on this computer's terminal.
func (s *Server) SetEnrollmentObserver(observe func(enrollment.Request)) {
	s.enrollmentObserver = observe
}

// appRoute shares a path between a daemon API and a web app route, such as
// /resources and /browser. A browser loading the page (a GET that prefers
// HTML) on a web UI origin gets the app, so reload and deep links reopen it.
// The app's own requests never ask for HTML and keep the API.
func (s *Server) appRoute(webUI, api http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept")
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && prefersHTML(r.Header.Get("Accept")) && s.webUIAdmitted(r) {
			webUI.ServeHTTP(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
}

// prefersHTML reports whether an Accept header names text/html explicitly,
// at a quality no lower than JSON. Wildcards alone ("*/*") are API clients.
func prefersHTML(accept string) bool {
	html, json := -1.0, -1.0
	for _, part := range strings.Split(accept, ",") {
		mediaType, params, _ := strings.Cut(part, ";")
		quality := 1.0
		for _, param := range strings.Split(params, ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if ok && strings.EqualFold(strings.TrimSpace(name), "q") {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					quality = parsed
				}
			}
		}
		switch strings.ToLower(strings.TrimSpace(mediaType)) {
		case "text/html", "application/xhtml+xml":
			html = max(html, quality)
		case "application/json":
			json = max(json, quality)
		}
	}
	return html > 0 && html >= json
}

// SetWebUIDir names a directory whose web UI build replaces the embedded one
// while it holds one (its index.html.gz; files gzip-only, as dist/). The dev
// daemon serves the NSH client from <state-dir>/web-ui this way, kept current
// without a rebuild; with no build there, the embedded bundle is served as ever.
func (s *Server) SetWebUIDir(dir string) { s.webUIDir = dir }

// webUIOverride is the directory's NSH web package when it holds one now.
// Checked per request, so a build put there (or taken away) is served at once.
func (s *Server) webUIOverride() (fs.FS, bool) {
	if s.webUIDir == "" {
		return nil, false
	}
	info, err := os.Stat(filepath.Join(s.webUIDir, "index.html.gz"))
	present := err == nil && info.Mode().IsRegular()
	if s.webUIOverrideSeen.Swap(present) != present {
		if present {
			log.Printf("web UI: serving the build in %s", s.webUIDir)
		} else {
			log.Printf("web UI: serving the embedded build (none in %s)", s.webUIDir)
		}
	}
	if !present {
		return nil, false
	}
	return os.DirFS(s.webUIDir), true
}

// webUIHandler serves the browser app only where the operator expects it:
// loopback clients addressing a loopback host, or an explicit web origin.
// Every daemon API keeps its own device authorization either way.
func (s *Server) webUIHandler() http.Handler {
	files := s.webUIFiles
	if files == nil {
		files = webui.Embedded()
	}
	assets := webui.Handler(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Admission vouches for the Host (a loopback host or a known address), so it
		// may be written into a policy.
		origin, admitted := s.webUIOrigin(r)
		if !admitted {
			http.NotFound(w, r)
			return
		}
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Cross-Origin-Opener-Policy", "same-origin")
		if pkg, ok := s.webUIOverride(); ok {
			header.Set("Content-Security-Policy", nshPackagePolicy(origin, r.Host))
			if isTerminalPageFile(r.URL.Path) {
				// The terminal's page runs in NSH's web view, a frame sandboxed to an opaque
				// origin: its faces and its VT (fetched, as fonts and WebAssembly are) are read
				// cross-origin. They are public build files, the same for everyone.
				header.Set("Access-Control-Allow-Origin", "*")
			}
			webui.PackageHandler(pkg).ServeHTTP(w, r)
			return
		}
		header.Set("Content-Security-Policy", embeddedPolicy(r.Host))
		assets.ServeHTTP(w, r)
	})
}

// embeddedPolicy is the embedded (Expo) web app's policy.
func embeddedPolicy(host string) string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'wasm-unsafe-eval'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob: https:",
		"font-src 'self' data:",
		"media-src 'self' data: blob: https:",
		"connect-src 'self' ws://" + host + " wss://" + host,
		"worker-src 'self' blob:",
		"frame-src 'none'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'none'",
	}, "; ")
}

// nshPackagePolicy is the NSH client's policy (the same as on nsh-client-m0).
func nshPackagePolicy(origin, host string) string {
	return strings.Join([]string{
		"default-src 'self'",
		// NSH's web runtime writes its import map as an inline script of blob:
		// URLs at run time, so neither a hash nor a nonce can name it. Never add a
		// hash, nonce or 'strict-dynamic' here: browsers then ignore 'unsafe-inline',
		// and NSH's web views (the terminal, Screen's viewer), srcdoc frames that
		// inherit this policy with an inline bridge, stop running.
		//
		// The page's own origin is named beside 'self' for those frames too: they are
		// sandboxed to an opaque origin, and WebKit (Safari) matches an inherited
		// 'self' against that origin, so without it the terminal's script, faces and
		// VT are refused there. Chromium and Firefox already allow exactly these loads.
		"script-src 'self' " + origin + " 'wasm-unsafe-eval' 'unsafe-inline' blob:",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob: https:",
		"font-src 'self' " + origin + " data:",
		"media-src 'self' data: blob: https:",
		"connect-src 'self' " + origin + " ws://" + host + " wss://" + host,
		"worker-src 'self' blob:",
		"frame-src 'none'",
		"object-src 'none'",
		// The page's <base href="/"> keeps relative files at the root on deep links.
		"base-uri 'self'",
		"form-action 'none'",
		"frame-ancestors 'none'",
	}, "; ")
}

// isTerminalPageFile reports whether path is one of the terminal page's own files under /terminal/
// (the NSH package's build script): its script, its VT and its faces. /terminal/<id> is an app route.
func isTerminalPageFile(path string) bool {
	name, ok := strings.CutPrefix(path, "/terminal/")
	if !ok || strings.Contains(name, "/") {
		return false
	}
	return name == "terminal.js" || name == "mewla-vt.wasm" || (strings.HasPrefix(name, "MapleMono-") && strings.HasSuffix(name, ".ttf"))
}

func (s *Server) webUIAdmitted(r *http.Request) bool {
	_, admitted := s.webUIOrigin(r)
	return admitted
}

// webUIOrigin is the origin the browser's page has (http on a loopback host, else
// the https address the Host names) and whether the web UI is served there.
func (s *Server) webUIOrigin(r *http.Request) (string, bool) {
	hostname := strings.ToLower(r.Host)
	if host, _, err := net.SplitHostPort(r.Host); err == nil {
		hostname = strings.ToLower(host)
	}
	hostname = strings.Trim(hostname, "[]")
	if hostname == "localhost" || net.ParseIP(hostname).IsLoopback() {
		return "http://" + strings.ToLower(r.Host), isLoopbackRemote(r.RemoteAddr)
	}
	return "https://" + strings.TrimSuffix(strings.ToLower(r.Host), ":443"), s.isKnownHTTPSHost(r.Host)
}

// servesWebOrigin reports whether a browser on origin is served the web UI:
// a loopback http address, or an https address book entry.
func (s *Server) servesWebOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return false
	}
	if parsed.Scheme == "http" {
		ip := net.ParseIP(parsed.Hostname())
		return ip != nil && ip.IsLoopback()
	}
	if canonical, err := parseWebOrigin(origin); err != nil || canonical != origin {
		return false
	}
	return s.addresses != nil && s.addresses.Contains(origin)
}

// isKnownHTTPSHost reports whether the request Host is an https address in
// the address book, such as a tunnel or proxy added with mewla address add.
func (s *Server) isKnownHTTPSHost(rawHost string) bool {
	requested := "https://" + strings.TrimSuffix(strings.ToLower(rawHost), ":443")
	return s.addresses != nil && s.addresses.Contains(requested)
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return net.ParseIP(strings.Trim(host, "[]")).IsLoopback()
}
