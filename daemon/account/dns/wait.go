package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"
)

// TXTWaiter waits until every authoritative nameserver of Zone serves a TXT
// value. The ACME client asks the CA to validate as soon as the service
// answers, and a provider's API accepting a record does not mean its
// nameservers serve it yet.
type TXTWaiter struct {
	Zone string
	// Interval between rounds (default 1s); Settle is waited once every
	// nameserver answers (default 2s), for anycast sites the check cannot see.
	Interval time.Duration
	Settle   time.Duration
	// LookupNS returns nameserver host:port addresses (default: the zone's
	// NS records on port 53).
	LookupNS func(ctx context.Context, zone string) ([]string, error)
}

// Wait returns nil once the value is visible, or ctx's error.
func (w *TXTWaiter) Wait(ctx context.Context, fqdn, value string) error {
	lookupNS := w.LookupNS
	if lookupNS == nil {
		lookupNS = zoneNameservers
	}
	servers, err := lookupNS(ctx, w.Zone)
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		return errors.New("dns: zone has no nameservers")
	}
	interval := w.Interval
	if interval <= 0 {
		interval = time.Second
	}
	settle := w.Settle
	if settle <= 0 {
		settle = 2 * time.Second
	}
	name := strings.TrimSuffix(fqdn, ".") + "."
	pending := slices.Clone(servers)
	for {
		pending = slices.DeleteFunc(pending, func(server string) bool {
			return serves(ctx, server, name, value)
		})
		if len(pending) == 0 {
			break
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("dns: %s not served by %s: %w", fqdn, strings.Join(pending, ", "), ctx.Err())
		case <-timer.C:
		}
	}
	timer := time.NewTimer(settle)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func zoneNameservers(ctx context.Context, zone string) ([]string, error) {
	records, err := net.DefaultResolver.LookupNS(ctx, zone)
	if err != nil {
		return nil, fmt.Errorf("dns: nameservers of %s: %w", zone, err)
	}
	servers := make([]string, 0, len(records))
	for _, record := range records {
		servers = append(servers, net.JoinHostPort(strings.TrimSuffix(record.Host, "."), "53"))
	}
	return servers, nil
}

// serves asks one nameserver directly, bypassing caching resolvers.
func serves(ctx context.Context, server, name, value string) bool {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, server)
		},
	}
	lookup, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	values, err := resolver.LookupTXT(lookup, name)
	return err == nil && slices.Contains(values, value)
}
