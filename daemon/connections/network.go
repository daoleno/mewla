package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var errAuth = errors.New("authorization expired or revoked; reconnect this account")

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

// Resolve and dial the checked IP together, preventing DNS rebinding. Custom
// sources never get access to host-local services or ambient proxy credentials.
func newHTTPClient() *http.Client { return newHTTPClientFor("", nil) }

func newHTTPClientFor(origin string, networks []string) *http.Client {
	transport := &http.Transport{DisableKeepAlives: true, TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 32 << 10}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid endpoint")
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, errors.New("endpoint DNS failed")
		}
		for _, ip := range ips {
			if !publicIP(ip.IP) && !(address == origin && trustedIP(ip.IP, networks)) {
				return nil, errors.New("private network endpoints are not supported")
			}
		}
		if len(ips) == 0 {
			return nil, errors.New("endpoint DNS returned no addresses")
		}
		return (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are not allowed") }}
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
		return nil, errors.New("integration network request failed")
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
		return nil, errors.New("integration network request failed")
	}
	defer resp.Body.Close()
	if len(capture) > 0 {
		*capture[0] = resp.Header.Clone()
	}
	if resp.StatusCode == 401 {
		return nil, errAuth
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("integration returned HTTP %d", resp.StatusCode)
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
