// Package clientaddr carries the address of the client behind a relay.
//
// Requests that reach the daemon through Mewla Link come from the relay's
// connection, so their RemoteAddr is the relay for every client. The relay
// reports each client's address when it opens a stream; the Link connector
// stores it here. Use it only to share limits fairly between clients, never
// to grant trust: loopback and other access decisions keep using RemoteAddr.
package clientaddr

import (
	"context"
	"net"
	"net/http"
)

type contextKey struct{}

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
