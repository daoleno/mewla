// Package clientaddr carries the address of the client behind a relay.
//
// Requests that reach the daemon through Mewla Relay come from the relay's
// connection, so their RemoteAddr is the relay for every client. The relay
// reports each client's address when it opens a stream; the Link connector
// stores it here. Use it only to share limits fairly between clients, never
// to grant trust: loopback and other access decisions keep using RemoteAddr.
package clientaddr

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type contextKey struct{}

type relayKey struct{}

// MarkRelayed returns ctx marked as arriving through Mewla Relay, for a client
// whose address the relay did not report.
func MarkRelayed(ctx context.Context) context.Context {
	return context.WithValue(ctx, relayKey{}, true)
}

// Path names how a request reached this computer, from a bounded set:
// "relay" through Mewla Relay, "loopback", "tailscale", "lan" for a private
// address, and "direct" for any other. It is only for telling a viewer why
// its link is slow; it grants nothing.
func Path(r *http.Request) string {
	if _, ok := r.Context().Value(relayKey{}).(bool); ok {
		return "relay"
	}
	if ip, ok := r.Context().Value(contextKey{}).(net.IP); ok && ip != nil {
		return "relay"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return classify(net.ParseIP(host))
}

var tailscaleV4 = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func classify(ip net.IP) string {
	switch {
	case ip == nil:
		return "direct"
	case ip.IsLoopback():
		return "loopback"
	case tailscaleV4.Contains(ip) || (ip.To4() == nil && ip.To16()[0] == 0xfd && ip.To16()[1] == 0x7a && ip.To16()[2] == 0x11 && ip.To16()[3] == 0x5c):
		return "tailscale"
	case ip.IsPrivate() || ip.IsLinkLocalUnicast():
		return "lan"
	}
	return "direct"
}

// With returns ctx carrying the relay-reported client IP.
func With(ctx context.Context, ip net.IP) context.Context {
	return context.WithValue(ctx, contextKey{}, ip)
}

// IP returns the client's IP: the relay-reported one when the request came
// through Link, otherwise the host of RemoteAddr. It is nil when neither
// parses.
func IP(r *http.Request) net.IP {
	if ip, ok := r.Context().Value(contextKey{}).(net.IP); ok && ip != nil {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// Key groups addresses one client plausibly controls: an IPv4 address, or an
// IPv6 /64, which is what a single subscriber usually receives.
func Key(ip net.IP) string {
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// Parse validates a relay-reported address. Anything that is not a bare IP
// returns nil, and the caller falls back to RemoteAddr.
func Parse(value string) net.IP {
	if value == "" || len(value) > 64 {
		return nil
	}
	return net.ParseIP(value)
}

// forwardedHeaders are what a reverse proxy or tunnel adds when it relays a
// client: a request carrying one did not come from the proxy's own computer.
var forwardedHeaders = []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip", "Cf-Connecting-Ip", "True-Client-Ip"}

// Local reports whether the request comes from a browser or app on this very
// computer: it arrived over loopback, addressed to a loopback host, not through
// Mewla Relay and not through a proxy or tunnel on this computer that relays
// someone else. It exists so a viewer of this computer's own screen can be
// told it is looking at itself; like Path it grants nothing, and when in doubt
// it says false, which only costs a live view.
func Local(r *http.Request) bool {
	if Path(r) != "loopback" {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return false
	}
	for _, name := range forwardedHeaders {
		if r.Header.Get(name) != "" {
			return false
		}
	}
	for name := range r.Header {
		if strings.HasPrefix(strings.ToLower(name), "tailscale-") {
			return false
		}
	}
	return true
}
