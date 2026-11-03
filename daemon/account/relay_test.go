package account

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

// Two relays: Quebec-like "main" (the default) and a gated "hk".
func twoRelays(config *Config) {
	main := config.Relays[0]
	main.Name = "main"
	config.Relays = []Relay{main, {
		Name:              "hk",
		ControlAddress:    "hk.relay.example.com:8443",
		ControlServerName: "hk.relay.example.com",
		ControlCAPEM:      testCAPEM,
		ClientDomain:      "hk.link.example.com",
		IP:                "203.0.113.9",
		Feature:           FeatureRelayDNS,
	}}
}

func (h *harness) callWithFeatures(daemon *auth.Manager, method, path string, body any, features string) (int, []byte) {
	h.t.Helper()
	request := h.signedRequest(daemon, method, path, body, signOptions{})
	if features != "" {
		request.Header.Set(featuresHeader, features)
	}
	return h.send(request)
}

// An older daemon would register on the nearest relay and never say which, so
// its hostname would keep pointing at the default relay and it would be
// unreachable. It is therefore never offered the second relay.
func TestASecondRelayIsOfferedOnlyToDaemonsThatReportTheirRelay(t *testing.T) {
	h := newHarness(t, twoRelays)
	daemon := newDaemon(t)
	h.login(daemon, "studio", newRouteID(t))

	names := func(features string) []string {
		t.Helper()
		status, raw := h.callWithFeatures(daemon, http.MethodGet, "/v1/machine", nil, features)
		if status != http.StatusOK {
			t.Fatalf("GET /v1/machine = %d %s", status, raw)
		}
		var info MachineInfo
		mustDecode(t, raw, &info)
		var out []string
		for _, relay := range info.Relays {
			out = append(out, relay.Name+"@"+relay.ClientDomain)
		}
		return out
	}
	if got := names(""); strings.Join(got, ",") != "main@link.example.com" {
		t.Fatalf("a daemon without the feature sees %v", got)
	}
	if got := names("something-else"); len(got) != 1 {
		t.Fatalf("an unrelated feature unlocked a relay: %v", got)
	}
	if got := names("a, " + FeatureRelayDNS); strings.Join(got, ",") != "main@link.example.com,hk@hk.link.example.com" {
		t.Fatalf("a daemon with the feature sees %v", got)
	}
}

func TestRelayConfigurationIsValidated(t *testing.T) {
	base := func(mutate func(*Config)) error {
		config := Config{
			Store:        newStoreForTest(t),
			Identity:     &FakeProvider{},
			DNS:          &recordingDNS{},
			GrantKey:     newGrantKey(t),
			ClientDomain: "link.example.com",
			Relays:       []Relay{{Name: "main", ControlAddress: "relay.example.com:8443", ControlServerName: "relay.example.com"}},
		}
		mutate(&config)
		_, err := New(config)
		return err
	}
	if err := base(twoRelays); err != nil {
		t.Fatalf("a valid pair of relays was refused: %v", err)
	}
	for name, mutate := range map[string]func(*Config){
		"a later relay without a feature": func(c *Config) {
			twoRelays(c)
			c.Relays[1].Feature = ""
		},
		"the default relay behind a feature": func(c *Config) {
			twoRelays(c)
			c.Relays[0].Feature = FeatureRelayDNS
		},
		"a later relay without an address": func(c *Config) {
			twoRelays(c)
			c.Relays[1].IP = ""
		},
		"a later relay with an IPv6 address": func(c *Config) {
			twoRelays(c)
			c.Relays[1].IP = "2001:db8::1"
		},
		"a repeated name": func(c *Config) {
			twoRelays(c)
			c.Relays[1].Name = "MAIN"
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := base(mutate); err == nil {
				t.Fatal("the configuration was accepted")
			}
		})
	}
}

func newStoreForTest(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "account.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newGrantKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustDecode(t *testing.T, raw []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func TestAComputerReportingItsRelayGetsItsOwnAddressRecord(t *testing.T) {
	h := newHarness(t, twoRelays)
	daemon := newDaemon(t)
	info := h.login(daemon, "studio", newRouteID(t))
	host := info.Label + ".link.example.com"
	report := func(relay, features string) int {
		t.Helper()
		status, _ := h.callWithFeatures(daemon, http.MethodPut, "/v1/machine/relay", map[string]string{"relay": relay}, features)
		return status
	}

	if got := report("hk", ""); got != http.StatusBadRequest {
		t.Fatalf("a daemon without the feature reported hk: %d", got)
	}
	if got := report("nowhere", FeatureRelayDNS); got != http.StatusBadRequest {
		t.Fatalf("unknown relay: %d", got)
	}
	status, _ := h.send(h.signedRequest(daemon, http.MethodPut, "/v1/machine/relay", map[string]string{"relay": "hk"}, signOptions{signer: newDaemon(t)}))
	if status != http.StatusUnauthorized {
		t.Fatalf("a request signed by another key: %d", status)
	}
	if calls := h.addressCalls(); len(calls) != 0 {
		t.Fatalf("rejected reports wrote DNS: %v", calls)
	}

	if got := report("hk", FeatureRelayDNS); got != http.StatusNoContent {
		t.Fatalf("report hk = %d", got)
	}
	if got := h.addressCalls(); len(got) != 1 || got[0] != (dnsCall{"set-a", host, "203.0.113.9"}) {
		t.Fatalf("calls after hk: %v", got)
	}
	// Back on the default relay, the wildcard covers the name again.
	if got := report("main", FeatureRelayDNS); got != http.StatusNoContent {
		t.Fatalf("report main = %d", got)
	}
	if got := h.addressCalls(); len(got) != 2 || got[1] != (dnsCall{"clear-a", host, ""}) {
		t.Fatalf("calls after main: %v", got)
	}
	// The report is applied each time, so a daemon retrying after a DNS failure
	// is enough to repair the record.
	report("hk", FeatureRelayDNS)
	report("hk", FeatureRelayDNS)
	if got := h.addressCalls(); len(got) != 4 || got[3] != (dnsCall{"set-a", host, "203.0.113.9"}) {
		t.Fatalf("calls after repeats: %v", got)
	}
}

func TestRelayReportsAreRateLimited(t *testing.T) {
	h := newHarness(t, twoRelays)
	daemon := newDaemon(t)
	h.login(daemon, "studio", newRouteID(t))
	limited := false
	for i := 0; i < relayReportsPerHour+5; i++ {
		status, _ := h.callWithFeatures(daemon, http.MethodPut, "/v1/machine/relay", map[string]string{"relay": "hk"}, FeatureRelayDNS)
		if status == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("a computer can rewrite DNS without limit")
	}
	if n := len(h.addressCalls()); n > relayReportsPerHour {
		t.Fatalf("%d DNS writes", n)
	}
	h.clock.Advance(time.Hour + time.Minute)
	if status, _ := h.callWithFeatures(daemon, http.MethodPut, "/v1/machine/relay", map[string]string{"relay": "hk"}, FeatureRelayDNS); status != http.StatusNoContent {
		t.Fatalf("after the window: %d", status)
	}
}

func TestTheRelayRecordFollowsARenameAndEndsWithTheMachine(t *testing.T) {
	h := newHarness(t, twoRelays)
	daemon := newDaemon(t)
	info := h.login(daemon, "studio", newRouteID(t))
	h.callWithFeatures(daemon, http.MethodPut, "/v1/machine/relay", map[string]string{"relay": "hk"}, FeatureRelayDNS)
	h.dns.reset()

	status, raw := h.callWithFeatures(daemon, http.MethodPut, "/v1/machine/name", map[string]string{"name": "desk"}, FeatureRelayDNS)
	if status != http.StatusOK {
		t.Fatalf("rename = %d %s", status, raw)
	}
	var renamed MachineInfo
	mustDecode(t, raw, &renamed)
	got := h.addressCalls()
	if len(got) != 2 || got[0] != (dnsCall{"clear-a", info.Label + ".link.example.com", ""}) ||
		got[1] != (dnsCall{"set-a", renamed.Label + ".link.example.com", "203.0.113.9"}) {
		t.Fatalf("calls after rename: %v", got)
	}

	h.dns.reset()
	if status, _ := h.call(daemon, http.MethodDelete, "/v1/machine", nil); status != http.StatusNoContent {
		t.Fatalf("logout = %d", status)
	}
	if got := h.addressCalls(); len(got) != 1 || got[0] != (dnsCall{"clear-a", renamed.Label + ".link.example.com", ""}) {
		t.Fatalf("calls after logout: %v", got)
	}
}

func TestDeletingAnAccountClearsEveryMachinesRecord(t *testing.T) {
	h := newHarness(t, twoRelays)
	first, second := newDaemon(t), newDaemon(t)
	one := h.login(first, "one", newRouteID(t))
	two := h.login(second, "two", newRouteID(t))
	h.dns.reset()
	if status, _ := h.call(first, http.MethodDelete, "/v1/account", nil); status != http.StatusNoContent {
		t.Fatalf("delete account = %d", status)
	}
	hosts := map[string]bool{}
	for _, call := range h.addressCalls() {
		if call.op != "clear-a" {
			t.Fatalf("unexpected call %v", call)
		}
		hosts[call.fqdn] = true
	}
	if !hosts[one.Label+".link.example.com"] || !hosts[two.Label+".link.example.com"] {
		t.Fatalf("cleared %v", hosts)
	}
}

// Rolling back the second relay removes every override in one step.
func TestClearRelayRecordsRemovesEveryOverride(t *testing.T) {
	h := newHarness(t, twoRelays)
	a, b, c := newDaemon(t), newDaemon(t), newDaemon(t)
	ia := h.login(a, "a", newRouteID(t))
	ib := h.loginAs(b, "bob")
	h.login(c, "c", newRouteID(t)) // never reported
	for _, d := range []*auth.Manager{a, b} {
		h.callWithFeatures(d, http.MethodPut, "/v1/machine/relay", map[string]string{"relay": "hk"}, FeatureRelayDNS)
	}
	h.dns.reset()
	cleared, err := h.service.ClearRelayRecords(t.Context())
	if err != nil || cleared != 2 {
		t.Fatalf("cleared %d, err %v", cleared, err)
	}
	hosts := map[string]bool{}
	for _, call := range h.addressCalls() {
		hosts[call.fqdn] = call.op == "clear-a"
	}
	if !hosts[ia.Label+".link.example.com"] || !hosts[ib.Label+".link.example.com"] || len(hosts) != 2 {
		t.Fatalf("cleared %v", hosts)
	}
}

// addressCalls are the A-record calls recorded so far.
func (h *harness) addressCalls() []dnsCall {
	h.dns.mu.Lock()
	defer h.dns.mu.Unlock()
	var out []dnsCall
	for _, call := range h.dns.calls {
		if call.op == "set-a" || call.op == "clear-a" {
			out = append(out, call)
		}
	}
	return out
}

func (d *recordingDNS) reset() {
	d.mu.Lock()
	d.calls = nil
	d.mu.Unlock()
}
