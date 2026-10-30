package server

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// privateListenInterval is how often the set of private addresses is looked
// at again, so Wi-Fi or Tailscale coming and going is followed without a restart.
const privateListenInterval = 10 * time.Second

// privateAllowedPaths are the routes the private listener serves. Every one is
// either signed by a paired device or proves the daemon to the caller. Pairing,
// join requests, short links, OAuth return and the web UI stay on loopback (and
// on whatever addresses the person opened with --lan).
var privateAllowedPaths = map[string]bool{
	"/ws":                      true,
	"/health":                  true,
	"/auth-check":              true,
	"/screen":                  true,
	"/screen/viewer":           true,
	"/browser":                 true,
	"/browser/viewer":          true,
	"/resources":               true,
	"/devices":                 true,
	"/computer":                true,
	"/computers":               true,
	"/computers/invite":        true,
	"/agent-use":               true,
	"/phone":                   true,
	"/phone/device":            true,
	"/friends":                 true,
	"/upload":                  true,
	"/session-file-capability": true,
	"/session-file":            true,
}

// privateListeners is the set of TLS-only listeners on this computer's private
// addresses, which a daemon that listens on loopback opens so a paired phone
// on the same network can reach it without the relay.
type privateListeners struct {
	port    int
	detect  func() []net.IP
	allow   func(net.IP) bool
	mu      sync.Mutex
	open    map[string]*privateListener
	refused map[string]bool
}

type privateListener struct {
	endpoint DirectEndpoint
	listener net.Listener
	server   *http.Server
}

// EnablePrivateListeners makes the daemon listen on `port` on each private
// address `detect` reports, over TLS only. It is for a daemon whose main
// listener is on loopback; the main listener is unchanged.
func (s *Server) EnablePrivateListeners(port int, detect func() []net.IP) {
	s.private = &privateListeners{
		port:    port,
		detect:  detect,
		allow:   privateAddress,
		open:    map[string]*privateListener{},
		refused: map[string]bool{},
	}
}

// PrivateEndpoints are the private addresses the daemon is listening on now.
func (s *Server) PrivateEndpoints() []DirectEndpoint {
	if s.private == nil {
		return nil
	}
	s.private.mu.Lock()
	defer s.private.mu.Unlock()
	endpoints := make([]DirectEndpoint, 0, len(s.private.open))
	for _, listener := range s.private.open {
		endpoints = append(endpoints, listener.endpoint)
	}
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].Host < endpoints[j].Host })
	return endpoints
}

// privateAddress accepts an IPv4 address on a private network: RFC 1918 or
// Tailscale's 100.64.0.0/10. Loopback, link-local and public addresses never
// qualify.
func privateAddress(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4.IsPrivate() || ip4[0] == 100 && ip4[1]&0xc0 == 0x40
}

func privateKind(ip net.IP) string {
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 {
		return "tailscale"
	}
	return "lan"
}

// runPrivateListeners keeps the listeners in step with the private addresses
// until ctx ends.
func (s *Server) runPrivateListeners(ctx context.Context) {
	p := s.private
	if p == nil || s.tlsConfig == nil {
		return
	}
	defer s.closePrivateListeners()
	s.syncPrivateListeners()
	ticker := time.NewTicker(privateListenInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncPrivateListeners()
		}
	}
}

func (s *Server) syncPrivateListeners() {
	p := s.private
	want := map[string]net.IP{}
	for _, ip := range p.detect() {
		if p.allow(ip) {
			want[ip.String()] = ip
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for host, listener := range p.open {
		if _, keep := want[host]; !keep {
			closePrivate(listener)
			delete(p.open, host)
			log.Printf("private listener closed: %s (address gone)", host)
		}
	}
	for host := range p.refused {
		if _, keep := want[host]; !keep {
			delete(p.refused, host)
		}
	}
	for host, ip := range want {
		if p.open[host] != nil || p.refused[host] {
			continue
		}
		raw, err := net.Listen("tcp4", net.JoinHostPort(host, strconv.Itoa(p.port)))
		if err != nil {
			// Another listener (for example --lan on the same port) already
			// serves this address. Say so once, not every interval.
			p.refused[host] = true
			log.Printf("private listener not opened on %s:%d: %v", host, p.port, err)
			continue
		}
		listener := tls.NewListener(raw, s.tlsConfig.Clone())
		server := &http.Server{
			Handler:           s.privateHandler(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		p.open[host] = &privateListener{
			endpoint: DirectEndpoint{Host: host, Port: p.port, Kind: privateKind(ip)},
			listener: listener,
			server:   server,
		}
		go func() { _ = server.Serve(listener) }()
		log.Printf("private listener opened: %s:%d (TLS only)", host, p.port)
	}
}

func (s *Server) closePrivateListeners() {
	p := s.private
	p.mu.Lock()
	defer p.mu.Unlock()
	for host, listener := range p.open {
		closePrivate(listener)
		delete(p.open, host)
	}
}

func closePrivate(listener *privateListener) {
	_ = listener.server.Close()
	_ = listener.listener.Close()
}

// privateHandler is the daemon's handler restricted to privateAllowedPaths.
func (s *Server) privateHandler() http.Handler {
	inner := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !privateAllowedPaths[r.URL.Path] {
			http.NotFound(w, r)
			return
		}
		inner.ServeHTTP(w, r)
	})
}
