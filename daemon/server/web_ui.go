package server

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
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
		if !s.webUIAdmitted(r) {
			http.NotFound(w, r)
			return
		}
		host := r.Host
		header := w.Header()
		header.Set("Content-Security-Policy", strings.Join([]string{
			"default-src 'self'",
			"script-src 'self' 'wasm-unsafe-eval'",
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data: blob: https:",
			"font-src 'self' data:",
			"media-src 'self' data: blob:",
			"connect-src 'self' ws://" + host + " wss://" + host,
			"worker-src 'self' blob:",
			"frame-src 'none'",
			"object-src 'none'",
			"base-uri 'none'",
			"form-action 'none'",
			"frame-ancestors 'none'",
		}, "; "))
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Cross-Origin-Opener-Policy", "same-origin")
		assets.ServeHTTP(w, r)
	})
}

func (s *Server) webUIAdmitted(r *http.Request) bool {
	hostname := strings.ToLower(r.Host)
	if host, _, err := net.SplitHostPort(r.Host); err == nil {
		hostname = strings.ToLower(host)
	}
	hostname = strings.Trim(hostname, "[]")
	if hostname == "localhost" || net.ParseIP(hostname).IsLoopback() {
		return isLoopbackRemote(r.RemoteAddr)
	}
	return s.isKnownHTTPSHost(r.Host)
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
