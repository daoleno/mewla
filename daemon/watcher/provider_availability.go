package watcher

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// Provider availability: whether an agent family's model provider is
// answering right now, read only from what the agents themselves record.
//
// A provider that refuses every request (a rate limit, a 5xx, a dropped
// network) makes every turn of that family fail at once: Brain's, every
// Worker's, a scheduled action's. None of those failures is an outcome. While
// the family is unavailable Mewla holds still: Brain receives no new deliveries,
// a Worker's cut-off turn waits instead of being reported lost, and a scheduled
// action that never started waits instead of failing. One request per probe
// interval goes out for the whole family, and the first Activity of that family
// that settles without an error ends the outage: everything held continues.
//
// The evidence is the provider's own structured error record on a settled
// Activity, classified by ClassifyProviderError: only a transient error (rate
// limit, overload, 5xx, timeout, dropped connection) counts. A persistent
// error (sign-in, billing, a bad request) needs a person and is not an outage;
// an unclassified one is left to each caller's own handling.

// ProviderOutage is one agent family whose provider is refusing requests.
type ProviderOutage struct {
	// Provider is the agent family (claude, codex, pi, opencode, …).
	Provider string `json:"provider"`
	// Since is when the first refused request of this outage settled.
	Since time.Time `json:"since"`
	// Cause is the newest refusal in the provider's own words, for the log
	// and Brain, e.g. "rate_limit, HTTP 429: …".
	Cause string `json:"cause"`
	// Kind is the refusal in two words for a person: rate limit, overloaded,
	// server error, timeout or connection lost.
	Kind string `json:"kind"`
}

// ProviderRefusalKind names a transient refusal for a person, from the same
// structured code and status ClassifyProviderError reads.
func ProviderRefusalKind(failure ProviderActivityError) string {
	code := strings.ToLower(strings.TrimSpace(failure.Code))
	switch {
	case failure.HTTPStatus == 429 || strings.Contains(code, "rate_limit"):
		return "rate limit"
	case failure.HTTPStatus == 529 || strings.Contains(code, "overloaded"):
		return "overloaded"
	case failure.HTTPStatus == 408 || code == "timeout":
		return "timeout"
	case failure.HTTPStatus >= 500 || code == "server_error" || code == "api_error" || code == "internal_server_error":
		return "server error"
	case strings.Contains(code, "stream") || strings.Contains(code, "connection"):
		return "connection lost"
	default:
		return "error"
	}
}

const (
	// providerOutageFresh: a refusal older than this does not start an
	// outage. A Session that has sat on an old error for days says nothing
	// about the provider now.
	providerOutageFresh = 30 * time.Minute
)

// providerProbeIntervals spaces the requests that test an unavailable
// provider: one per interval for the whole family, the last one repeating.
var providerProbeIntervals = []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 30 * time.Minute}

type providerAvailabilityState struct {
	// first is a refusal seen with no success since; a second distinct one
	// makes it an outage (since), so one dropped request is not.
	first   time.Time
	since   time.Time
	lastErr time.Time
	lastOK  time.Time
	cause   string
	kind    string
	probes  int
	next    time.Time
}

func (s *providerAvailabilityState) active() bool { return s != nil && !s.since.IsZero() }

type providerAvailability struct {
	mu       sync.Mutex
	state    map[string]*providerAvailabilityState
	listener func()
}

// observe takes one provider Activity observation of a Session of family
// provider and reports whether the family's availability changed.
func (a *providerAvailability) observe(provider string, observation ProviderActivityObservation, now time.Time) bool {
	provider = strings.TrimSpace(provider)
	settled := observation.SettledAt
	if provider == "" || observation.ProbeState != ProbeStateOK || settled.IsZero() || !providerActivityTerminal(observation.Status) {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == nil {
		a.state = map[string]*providerAvailabilityState{}
	}
	st := a.state[provider]
	if st == nil {
		st = &providerAvailabilityState{}
		a.state[provider] = st
	}
	if failure := observation.Error; failure != nil {
		if ClassifyProviderError(*failure) != ProviderErrorTransient || !settled.After(st.lastOK) || !settled.After(st.lastErr) {
			return false
		}
		st.lastErr, st.cause, st.kind = settled, ProviderErrorSummary(*failure), ProviderRefusalKind(*failure)
		if st.active() {
			return false
		}
		if now.Sub(settled) > providerOutageFresh {
			st.first = time.Time{}
			return false
		}
		if st.first.IsZero() {
			st.first = settled
			return false
		}
		st.since, st.probes = st.first, 0
		st.next = settled.Add(providerProbeIntervals[0])
		return true
	}
	if strings.TrimSpace(observation.Status) != "completed" || !settled.After(st.lastOK) {
		return false
	}
	st.lastOK = settled
	if !settled.After(st.lastErr) {
		return false
	}
	wasActive := st.active()
	st.first, st.since, st.probes, st.next = time.Time{}, time.Time{}, 0, time.Time{}
	return wasActive
}

func (a *providerAvailability) outage(provider string) (ProviderOutage, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.state[strings.TrimSpace(provider)]
	if !st.active() {
		return ProviderOutage{}, false
	}
	return ProviderOutage{Provider: strings.TrimSpace(provider), Since: st.since, Cause: st.cause, Kind: st.kind}, true
}

func (a *providerAvailability) outages() []ProviderOutage {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []ProviderOutage{}
	for provider, st := range a.state {
		if st.active() {
			out = append(out, ProviderOutage{Provider: provider, Since: st.since, Cause: st.cause, Kind: st.kind})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

// probe grants one request to family provider: always while it is available,
// and once per probe interval while it is not. The second result is when the
// next probe is due (zero while available).
func (a *providerAvailability) probe(provider string, now time.Time) (bool, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.state[strings.TrimSpace(provider)]
	if !st.active() {
		return true, time.Time{}
	}
	if now.Before(st.next) {
		return false, st.next
	}
	st.probes++
	st.next = now.Add(providerProbeIntervals[min(st.probes, len(providerProbeIntervals)-1)])
	return true, st.next
}

func (w *Watcher) availability() *providerAvailability {
	w.availabilityOnce.Do(func() {
		if w.providerAvailability == nil {
			w.providerAvailability = &providerAvailability{}
		}
	})
	return w.providerAvailability
}

// noteProviderObservation feeds one Session's provider observation to the
// family's availability and tells the listener when it changed.
func (w *Watcher) noteProviderObservation(command string, observation ProviderActivityObservation) {
	availability := w.availability()
	family := workerProviderFamily(command)
	if !availability.observe(family, observation, time.Now().UTC()) {
		return
	}
	if outage, unavailable := availability.outage(family); unavailable {
		log.Printf("provider outage: %s is refusing requests since %s; work is held: %s",
			family, outage.Since.Format(time.RFC3339), outage.Cause)
	} else {
		log.Printf("provider outage: %s answers again; held work continues", family)
	}
	availability.mu.Lock()
	listener := availability.listener
	availability.mu.Unlock()
	if listener != nil {
		go listener()
	}
}

// ObserveProviderOutcome accepts an agent-pushed settled Activity. Fast turns
// can finish between watcher polls, so their provider result must be recorded
// when the agent reports it.
func (w *Watcher) ObserveProviderOutcome(command string, observation ProviderActivityObservation) {
	w.noteProviderObservation(command, observation)
}

// providerFamilyDown says whether command's agent family is in an outage.
func (w *Watcher) providerFamilyDown(command string) bool {
	family := workerProviderFamily(command)
	if family == "" {
		return false
	}
	_, down := w.availability().outage(family)
	return down
}

// SetProviderAvailabilityListener installs what runs when an agent family's
// provider becomes unavailable or answers again (Brain's snapshot and lane).
func (w *Watcher) SetProviderAvailabilityListener(listener func()) {
	availability := w.availability()
	availability.mu.Lock()
	availability.listener = listener
	availability.mu.Unlock()
}

// ProviderFamily is the agent family a command runs ("" when unknown).
func ProviderFamily(command string) string { return workerProviderFamily(command) }

// ProviderOutage reports whether family provider is refusing requests now.
func (w *Watcher) ProviderOutage(provider string) (ProviderOutage, bool) {
	return w.availability().outage(provider)
}

// ProviderOutages lists every family whose provider is refusing requests now,
// oldest first.
func (w *Watcher) ProviderOutages() []ProviderOutage { return w.availability().outages() }

// ProviderProbe grants one request to family provider: always while it is
// available, once per probe interval for the whole family while it is not.
// next is when the following probe is due while it is unavailable.
func (w *Watcher) ProviderProbe(provider string, now time.Time) (granted bool, next time.Time) {
	return w.availability().probe(provider, now)
}
