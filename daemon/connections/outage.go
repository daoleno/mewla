package connections

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/diag"
)

// A remote MCP service that cannot be reached is one outage, not one error
// per attempt. The first failed attempt (after the quiet retry setup already
// makes) opens the outage and says so once. While it lasts, background
// readers such as Watch fail at once without touching the network, and one
// attempt is let through after each backoff step: it is the probe that
// notices the service is back. A person's own call always goes out, and its
// result counts the same way. Recovery is logged once.
const (
	outageBackoffMin = 5 * time.Second
	outageBackoffMax = 5 * time.Minute
	// An account is shown as unavailable once this many attempts in a row
	// have failed, so a single dropped connection does not flicker the UI.
	outageShownAfter = 3
)

// outageKey is what shares an outage: the service's host. Two GitHub
// accounts reach the same server, so they fail and recover together.
func outageKey(r *record) string {
	if i := strings.Index(r.Account.Endpoint, "://"); i >= 0 {
		rest := r.Account.Endpoint[i+3:]
		if j := strings.IndexAny(rest, "/?#"); j >= 0 {
			rest = rest[:j]
		}
		return strings.ToLower(rest)
	}
	return r.Account.Endpoint
}

type outage struct {
	since    time.Time
	next     time.Time
	failures int
	reason   string
}

// outages tracks every endpoint that is failing. It has its own lock, takes
// no timers and starts no goroutines: all it needs is the time of the next
// attempt, and the next caller reads it.
type outages struct {
	mu     sync.Mutex
	now    func() time.Time
	byHost map[string]*outage
	// changed runs, outside mu, when an outage opens, becomes visible or
	// ends; host names the service that came back, empty otherwise.
	changed func(recovered string)
}

func newOutages() *outages {
	return &outages{now: time.Now, byHost: map[string]*outage{}}
}

func outageBackoff(failures int) time.Duration {
	d := outageBackoffMin
	for i := 1; i < failures && d < outageBackoffMax; i++ {
		d *= 2
	}
	return min(d, outageBackoffMax)
}

// foregroundKey marks a context whose call a person or Brain asked for.
type foregroundKey struct{}

func foreground(ctx context.Context) context.Context {
	return context.WithValue(ctx, foregroundKey{}, true)
}

// errUnavailable is a refusal to try: the service is failing and its next
// attempt is not due. It wraps errNetwork, so callers treat it like the
// failure it stands for, and it names the real reason.
type errUnavailable struct {
	service, reason string
	wait            time.Duration
}

func (e errUnavailable) Error() string {
	// No changing numbers: Watch keeps one note per distinct message.
	return fmt.Sprintf("%s is unavailable: %s; Mewla backs off and tries again by itself (%s)", e.service, e.reason, errNetwork)
}
func (e errUnavailable) Unwrap() error { return errNetwork }

// gate refuses a background attempt while the endpoint backs off.
func (o *outages) gate(ctx context.Context, r *record) error {
	if ctx.Value(foregroundKey{}) != nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	f := o.byHost[outageKey(r)]
	if f == nil {
		return nil
	}
	if wait := f.next.Sub(o.now()); wait > 0 {
		return errUnavailable{accountService(r), f.reason, wait}
	}
	// Let one attempt through; the others wait for its answer.
	f.next = o.now().Add(outageBackoff(f.failures))
	return nil
}

// open reports whether the endpoint is already failing, so setup does not
// repeat an attempt that just failed.
func (o *outages) open(r *record) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.byHost[outageKey(r)] != nil
}

// failed records an attempt that did not reach the service.
func (o *outages) failed(r *record, reason string) {
	o.mu.Lock()
	key := outageKey(r)
	f := o.byHost[key]
	started := f == nil
	if started {
		f = &outage{since: o.now()}
		o.byHost[key] = f
	}
	f.failures++
	f.reason = reason
	f.next = o.now().Add(outageBackoff(f.failures))
	shown := f.failures == outageShownAfter
	o.mu.Unlock()
	if started {
		// The one report of this outage; Watch counts outages, not attempts.
		diag.Report("plugins.mcp.connect", diag.SeverityWarn, r.Account.Integration, fmt.Sprintf("%s unavailable: %s; trying again with backoff of %s up to %s", accountService(r), reason, outageBackoffMin, outageBackoffMax))
	}
	if (started || shown) && o.changed != nil {
		o.changed("")
	}
}

// reached records a service that answered.
func (o *outages) reached(r *record) {
	o.mu.Lock()
	key := outageKey(r)
	f := o.byHost[key]
	delete(o.byHost, key)
	o.mu.Unlock()
	if f == nil {
		return
	}
	log.Printf("plugins: %s reachable again after %s (%d failed attempts)", accountService(r), o.now().Sub(f.since).Round(time.Second), f.failures)
	if o.changed != nil {
		o.changed(key)
	}
}

// problem is why an account shows as unavailable, once the outage has lasted
// long enough to be worth showing.
func (o *outages) problem(r *record) (string, time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if f := o.byHost[outageKey(r)]; f != nil && f.failures >= outageShownAfter {
		return f.reason, f.since
	}
	return "", time.Time{}
}

// netCause names the failure of a request that got no HTTP answer, in words
// that carry no address, URL or vendor text.
func netCause(err error) string {
	var dns *net.DNSError
	var op net.Error
	switch {
	case errors.As(err, &dns):
		return "DNS lookup failed"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, syscall.ECONNRESET), strings.Contains(err.Error(), "connection reset"):
		return "connection closed by the other side"
	case errors.As(err, &op) && op.Timeout(), errors.Is(err, context.DeadlineExceeded), strings.Contains(err.Error(), "timeout"):
		return "connection timed out"
	}
	return "network error"
}

// networkError is errNetwork with its cause, which the SDK carries as text.
func networkError(err error) error {
	return fmt.Errorf("%w (%s)", errNetwork, netCause(err))
}

var netCauseText = regexp.MustCompile(`network request failed \(([a-z ]+)\)`)

// setupReason says why an MCP setup step failed, from fixed words only: the
// network cause, or the HTTP status the service answered with.
func setupReason(err error) string {
	text := err.Error()
	if m := netCauseText.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	if strings.Contains(text, errNetwork.Error()) {
		return "network error"
	}
	for code := 400; code < 600; code++ {
		if status := http.StatusText(code); status != "" && strings.Contains(text, status) {
			return fmt.Sprintf("the service answered %d %s", code, status)
		}
	}
	return genericSetupReason
}

// genericSetupReason is all that is known when the failure has no network
// cause and no HTTP status in its text.
const genericSetupReason = "the service did not complete the MCP handshake"
