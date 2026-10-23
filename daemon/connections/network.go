package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var errAuth = errors.New("authorization expired or revoked; reconnect this account")

// errNetwork is a request that never got an HTTP answer. The cause is logged,
// not returned: transport errors carry URLs.
var errNetwork = errors.New("integration network request failed")

// StatusError is a service's non-2xx HTTP answer other than 401.
type StatusError struct{ Status int }

func (e StatusError) Error() string { return fmt.Sprintf("integration returned HTTP %d", e.Status) }

func trustedIP(ip net.IP, networks []string) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	for _, raw := range networks {
		p, err := netip.ParsePrefix(raw)
		if err == nil && p.Contains(a.Unmap()) {
			return true
		}
	}
	return false
}

func trustedEndpointURL(raw string, networks []string) (*url.URL, error) {
	if len(networks) > 8 {
		return nil, errors.New("choose at most eight internal network ranges")
	}
	for _, raw := range networks {
		p, err := netip.ParsePrefix(raw)
		valid := false
		if err == nil {
			for _, block := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "100.64.0.0/10", "fc00::/7", "::1/128"} {
				b := netip.MustParsePrefix(block)
				if p.Bits() >= b.Bits() && b.Contains(p.Addr()) {
					valid = true
				}
			}
		}
		if !valid {
			return nil, errors.New("trusted ranges must be explicit private, loopback or Tailscale CIDRs")
		}
	}
	u, err := url.Parse(raw)
	if err == nil && u.Scheme == "http" && trustedIP(net.ParseIP(u.Hostname()), networks) && u.User == nil && u.RawQuery == "" && u.Fragment == "" {
		// Plain HTTP is allowed only for an explicitly trusted literal internal IP.
		// Hostnames still require HTTPS and normal certificate validation.
		return u, nil
	}
	return endpointURL(raw)
}

func (m *Manager) clientFor(r *record) *http.Client {
	if !m.networkOwned {
		return m.http
	} // explicit test transport; production always uses checked dialing
	u, _ := url.Parse(r.Account.Endpoint)
	port := u.Port()
	if port == "" {
		port = "443"
		if u.Scheme == "http" {
			port = "80"
		}
	}
	return newHTTPClientFor(net.JoinHostPort(u.Hostname(), port), r.Account.TrustedNetworks)
}

func endpointURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("endpoint must be HTTPS without credentials, query or fragment")
	}
	return u, nil
}

func publicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	for _, block := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15"} {
		if netip.MustParsePrefix(block).Contains(address) {
			return false
		}
	}
	return true
}

// benchmarkNet is the RFC 2544 benchmarking range. It is never a real host, so
// proxies in fake-IP mode (Surge, Clash, sing-box and others, common for people
// behind a filtered network) answer every DNS lookup from it and then route by
// the name on the connection. For a hostname it is therefore safe to dial: the
// TLS handshake still checks the certificate against that name, which only the
// real service can present. A literal address in it is never a proxy's answer,
// so that stays refused. The other blocked ranges stay blocked on purpose:
// 100.64.0.0/10 (carrier NAT, Tailscale) and 192.0.0.0/24 (IETF protocol
// assignments) hold real internal hosts, and link-local, private, loopback and
// unspecified addresses are the host's own network.
var benchmarkNet = netip.MustParsePrefix("198.18.0.0/15")

func benchmarkIP(host string, ip net.IP) bool {
	if net.ParseIP(host) != nil {
		return false
	}
	address, ok := netip.AddrFromSlice(ip)
	return ok && benchmarkNet.Contains(address.Unmap())
}

// errPrivateEndpoint is a name that resolves to an address Mewla will not dial.
var errPrivateEndpoint = errors.New("private network endpoints are not supported")

// Resolve and dial the checked IP together, preventing DNS rebinding. Custom
// sources never get access to host-local services or ambient proxy credentials.
func newHTTPClient() *http.Client { return newHTTPClientFor("", nil) }

func newHTTPClientFor(origin string, networks []string) *http.Client {
	return &http.Client{Transport: checkedTransport(origin, networks), Timeout: 20 * time.Second, CheckRedirect: noRedirects}
}

func noRedirects(*http.Request, []*http.Request) error {
	return errors.New("redirects are not allowed")
}

// checkedTransport dials only addresses that pass the network check, trying
// each one in turn: a host behind a CDN often answers on one address while
// another times out.
func checkedTransport(origin string, networks []string) *http.Transport {
	return checkedTransportWith(origin, networks, net.DefaultResolver.LookupIPAddr, (&net.Dialer{}).DialContext)
}

func checkedTransportWith(origin string, networks []string, lookup func(context.Context, string) ([]net.IPAddr, error), dial func(context.Context, string, string) (net.Conn, error)) *http.Transport {
	transport := &http.Transport{DisableKeepAlives: true, TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 32 << 10}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid endpoint")
		}
		ips, err := lookup(ctx, host)
		if err != nil {
			return nil, errors.New("endpoint DNS failed")
		}
		addresses := make([]net.IP, 0, len(ips))
		for _, ip := range ips {
			if !publicIP(ip.IP) && !benchmarkIP(host, ip.IP) && !(address == origin && trustedIP(ip.IP, networks)) {
				log.Printf("plugins: %s resolves to %s, a private address; not connecting", host, ip.IP)
				return nil, errPrivateEndpoint
			}
			addresses = append(addresses, ip.IP)
		}
		return dialEach(ctx, network, port, addresses, dial)
	}
	return transport
}

// dialEach dials checked addresses in order until one answers. Each attempt
// gets a share of the time, so one silent address cannot use it all.
func dialEach(ctx context.Context, network, port string, addresses []net.IP, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	if len(addresses) == 0 {
		return nil, errors.New("endpoint DNS returned no addresses")
	}
	each := 8 * time.Second
	if len(addresses) > 1 {
		each = 5 * time.Second
	}
	var last error
	for _, ip := range addresses {
		attempt, cancel := context.WithTimeout(ctx, each)
		conn, err := dial(attempt, network, net.JoinHostPort(ip.String(), port))
		cancel()
		if err == nil {
			return conn, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, last
}

type limitedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errors.New("integration response exceeds size limit")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}

type bearerTransport struct {
	base   http.RoundTripper
	secret string
	origin string
}

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme+"://"+r.URL.Host != t.origin {
		return nil, errors.New("credential origin mismatch")
	}
	c := r.Clone(r.Context())
	c.Header = r.Header.Clone()
	if t.secret != "" {
		c.Header.Set("Authorization", "Bearer "+t.secret)
	}
	resp, err := t.base.RoundTrip(c)
	if err != nil {
		if r.Context().Err() == nil {
			log.Printf("plugins: request to %s failed: %.300s", t.origin, err)
		}
		return nil, errNetwork
	}
	if resp.StatusCode == 401 {
		resp.Body.Close()
		return nil, errAuth
	}
	resp.Body = &limitedBody{resp.Body, MaxResultBytes + 1}
	return resp, nil
}

func safeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("integration request timed out; writes may have completed; inspect before retrying")
	}
	// Only our fixed messages cross the public boundary. Vendor error bodies,
	// transport URLs and schema validation values can contain credentials.
	return err
}

func (m *Manager) request(ctx context.Context, r *record, endpoint, secret, method, path string, body io.Reader, capture ...*http.Header) ([]byte, error) {
	u, err := trustedEndpointURL(endpoint, r.Account.TrustedNetworks)
	if err != nil {
		return nil, err
	}
	target, err := url.Parse(strings.TrimRight(endpoint, "/") + path)
	if err != nil || target.Host != u.Host || target.Scheme != u.Scheme {
		return nil, errors.New("invalid operation URL")
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, errors.New("invalid request")
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mewla-Integrations")
	if u.Hostname() == "api.notion.com" {
		req.Header.Set("Notion-Version", "2022-06-28")
	}
	if u.Hostname() == "api.github.com" {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	resp, err := m.clientFor(r).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, safeError(ctx.Err())
		}
		return nil, errNetwork
	}
	defer resp.Body.Close()
	if len(capture) > 0 {
		*capture[0] = resp.Header.Clone()
	}
	if resp.StatusCode == 401 {
		return nil, errAuth
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, StatusError{Status: resp.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxResultBytes+1))
	if err != nil {
		return nil, errors.New("integration response could not be read")
	}
	if len(raw) > MaxResultBytes {
		return nil, errors.New("integration response exceeds 1 MiB; request a smaller page")
	}
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	if u.Hostname() == "slack.com" {
		var result struct {
			OK bool `json:"ok"`
		}
		if json.Unmarshal(raw, &result) != nil || !result.OK {
			return nil, errors.New("Slack rejected the call; check account authorization and required scopes")
		}
	}
	return raw, nil
}
