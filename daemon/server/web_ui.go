package server

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/daoleno/zen/daemon/webui"
)

// ParseWebOrigin validates a remote browser origin for -web-origin. Remote
// browsers cannot pin the daemon's Link certificate, so the origin must be
// https behind a proxy whose certificate the browser already trusts.
func ParseWebOrigin(raw string) (string, error) {
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

// SetWebOrigins admits browsers on these https origins in addition to
// loopback. Origins must come from ParseWebOrigin.
func (s *Server) SetWebOrigins(origins []string) {
	s.webOrigins = append([]string(nil), origins...)
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
	requested := "https://" + strings.TrimSuffix(strings.ToLower(r.Host), ":443")
	for _, origin := range s.webOrigins {
		if origin == requested {
			return true
		}
	}
	return false
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return net.ParseIP(strings.Trim(host, "[]")).IsLoopback()
}
