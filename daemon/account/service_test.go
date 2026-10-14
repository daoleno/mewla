package account

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/linkproto"
)

const testCAPEM = "-----BEGIN CERTIFICATE-----\nrelay\n-----END CERTIFICATE-----\n"

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type dnsCall struct {
	op, fqdn, value string
}

type recordingDNS struct {
	mu    sync.Mutex
	calls []dnsCall
}

func (d *recordingDNS) SetTXT(_ context.Context, fqdn, value string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, dnsCall{"set", fqdn, value})
	return nil
}

func (d *recordingDNS) ClearTXT(_ context.Context, fqdn, value string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, dnsCall{"clear", fqdn, value})
	return nil
}

type harness struct {
	t       *testing.T
	clock   *testClock
	server  *httptest.Server
	service *Service
	store   *Store
	fake    *FakeProvider
	dns     *recordingDNS
	public  ed25519.PublicKey
}

func newHarness(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	clock := &testClock{now: time.UnixMilli(1_800_000_000_000)}
	store, err := OpenStore(filepath.Join(t.TempDir(), "account.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, clock: clock, store: store, dns: &recordingDNS{}, public: public}
	h.fake = &FakeProvider{Interval: 2 * time.Second, Now: clock.Now}
	config := Config{
		Store:        store,
		Identity:     h.fake,
		DNS:          h.dns,
		GrantKey:     private,
		ClientDomain: "link.example.com",
		Relays: []Relay{{
			Name:              "eu",
			ControlAddress:    "relay.example.com:8443",
			ControlServerName: "relay.example.com",
			ControlCAPEM:      testCAPEM,
		}},
		ACMEDirectoryURL: "https://acme.example.com/dir",
		DeviceHandler:    h.fake,
		Now:              clock.Now,
		Logf:             t.Logf,
	}
	if mutate != nil {
		mutate(&config)
	}
	h.service, err = New(config)
	if err != nil {
		t.Fatal(err)
	}
	h.server = httptest.NewServer(h.service.Handler())
	t.Cleanup(h.server.Close)
	h.fake.VerificationURI = h.server.URL + "/device"
	return h
}

func newDaemon(t *testing.T) *auth.Manager {
	t.Helper()
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func newRouteID(t *testing.T) string {
	t.Helper()
	route, err := randomHex(16)
	if err != nil {
		t.Fatal(err)
	}
	return route
}

type signOptions struct {
	timestamp time.Time
	signer    *auth.Manager
	nonce     string
}

func (h *harness) signedRequest(daemon *auth.Manager, method, path string, body any, options signOptions) *http.Request {
	h.t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
	}
	timestamp := options.timestamp
	if timestamp.IsZero() {
		timestamp = h.clock.Now()
	}
	nonce := options.nonce
	if nonce == "" {
		nonce, _ = randomHex(16)
	}
	signer := options.signer
	if signer == nil {
		signer = daemon
	}
	timestampMS := strconv.FormatInt(timestamp.UnixMilli(), 10)
	signature := signer.CreateLinkSignature(SignaturePayload(method, path, timestampMS, nonce, raw))
	request, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		h.t.Fatal(err)
	}
	request.Header.Set("Authorization", AuthorizationHeader(daemon.PublicKeyHex(), timestampMS, nonce, signature))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func (h *harness) send(request *http.Request) (int, []byte) {
	h.t.Helper()
	response, err := h.server.Client().Do(request)
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		h.t.Fatal(err)
	}
	return response.StatusCode, raw
}

func (h *harness) call(daemon *auth.Manager, method, path string, body any) (int, []byte) {
	h.t.Helper()
	return h.send(h.signedRequest(daemon, method, path, body, signOptions{}))
}

func (h *harness) startLogin(daemon *auth.Manager, name, route string) LoginStartResponse {
	h.t.Helper()
	status, raw := h.call(daemon, http.MethodPost, "/v1/login/start", LoginStartRequest{MachineName: name, RouteID: route})
	if status != http.StatusOK {
		h.t.Fatalf("login start = %d %s", status, raw)
	}
	var start LoginStartResponse
	if err := json.Unmarshal(raw, &start); err != nil {
		h.t.Fatal(err)
	}
	return start
}

func (h *harness) approve(userCode, username string) {
	h.t.Helper()
	response, err := h.server.Client().PostForm(h.server.URL+"/device", url.Values{
		"user_code": {userCode},
		"username":  {username},
	})
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(raw), "signed in") {
		h.t.Fatalf("POST /device = %d %s", response.StatusCode, raw)
	}
}

func (h *harness) poll(daemon *auth.Manager, loginID string) (int, LoginPollResponse, []byte) {
	h.t.Helper()
	status, raw := h.call(daemon, http.MethodPost, "/v1/login/poll", LoginPollRequest{LoginID: loginID})
	var response LoginPollResponse
	_ = json.Unmarshal(raw, &response)
	return status, response, raw
}

// login runs the whole device flow as "alice" and returns the machine.
func (h *harness) login(daemon *auth.Manager, name, route string) MachineInfo {
	h.t.Helper()
	return h.loginUser(daemon, name, route, "alice")
}

func (h *harness) loginUser(daemon *auth.Manager, name, route, username string) MachineInfo {
	h.t.Helper()
	start := h.startLogin(daemon, name, route)
	h.approve(start.UserCode, username)
	h.clock.Advance(3 * time.Second)
	status, response, raw := h.poll(daemon, start.LoginID)
	if status != http.StatusOK || response.Status != "complete" || response.Machine == nil {
		h.t.Fatalf("poll = %d %s", status, raw)
	}
	return *response.Machine
}

func errorOf(raw []byte) string {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	return body.Error
}

func TestSignatureMiddleware(t *testing.T) {
	h := newHarness(t, nil)
	daemon := newDaemon(t)
	other := newDaemon(t)

	request := h.signedRequest(daemon, http.MethodGet, "/v1/machine", nil, signOptions{})
	replay := request.Clone(context.Background())
	if status, raw := h.send(request); status != http.StatusNotFound {
		t.Fatalf("valid signature = %d %s, want 404 (no machine)", status, raw)
	}
	replay.Body = io.NopCloser(bytes.NewReader(nil))
	if status, _ := h.send(replay); status != http.StatusUnauthorized {
		t.Fatalf("replay = %d, want 401", status)
	}

	for name, options := range map[string]signOptions{
		"past skew":   {timestamp: h.clock.Now().Add(-3 * time.Minute)},
		"future skew": {timestamp: h.clock.Now().Add(3 * time.Minute)},
		"wrong key":   {signer: other},
		"bad nonce":   {nonce: "zz"},
	} {
		request := h.signedRequest(daemon, http.MethodGet, "/v1/machine", nil, options)
		if status, _ := h.send(request); status != http.StatusUnauthorized {
			t.Fatalf("%s = %d, want 401", name, status)
		}
	}
	if status, _ := h.send(h.signedRequest(daemon, http.MethodGet, "/v1/machine", nil,
		signOptions{timestamp: h.clock.Now().Add(-90 * time.Second)})); status != http.StatusNotFound {
		t.Fatalf("in-window skew = %d, want 404", status)
	}

	tampered := h.signedRequest(daemon, http.MethodPost, "/v1/login/start",
		LoginStartRequest{MachineName: "a", RouteID: newRouteID(t)}, signOptions{})
	tampered.Body = io.NopCloser(strings.NewReader(`{"machine_name":"b","route_id":"` + newRouteID(t) + `"}`))
	tampered.ContentLength = -1
	if status, _ := h.send(tampered); status != http.StatusUnauthorized {
		t.Fatalf("tampered body = %d, want 401", status)
	}

	wrongPath := h.signedRequest(daemon, http.MethodDelete, "/v1/machine", nil, signOptions{})
	wrongPath.Method = http.MethodGet
	if status, _ := h.send(wrongPath); status != http.StatusUnauthorized {
		t.Fatalf("method swap = %d, want 401", status)
	}

	unsigned, _ := http.NewRequest(http.MethodGet, h.server.URL+"/v1/machine", nil)
	if status, _ := h.send(unsigned); status != http.StatusUnauthorized {
		t.Fatalf("unsigned = %d, want 401", status)
	}

	large := h.signedRequest(daemon, http.MethodPost, "/v1/login/start",
		map[string]string{"pad": strings.Repeat("x", maxSignedBodyBytes)}, signOptions{})
	if status, _ := h.send(large); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body = %d, want 413", status)
	}
}

func TestFakeLoginFlow(t *testing.T) {
	h := newHarness(t, nil)
	daemon := newDaemon(t)
	route := newRouteID(t)

	start := h.startLogin(daemon, "Dao's Studio!", route)
	if start.VerificationURI != h.server.URL+"/device" ||
		!strings.HasPrefix(start.VerificationURIComplete, h.server.URL+"/device?user_code=") ||
		start.IntervalSeconds != 2 || start.UserCode == "" || !linkproto.IsOpaqueID(start.LoginID, 16) {
		t.Fatalf("start = %+v", start)
	}

	page, err := h.server.Client().Get(start.VerificationURIComplete)
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(pageBody), "Sign in to Mewla remote access (development)") ||
		!strings.Contains(string(pageBody), start.UserCode) {
		t.Fatalf("GET /device = %s", pageBody)
	}

	if status, response, _ := h.poll(daemon, start.LoginID); status != http.StatusAccepted || response.Status != "pending" {
		t.Fatalf("early poll = %d %+v", status, response)
	}
	other := newDaemon(t)
	if status, _, _ := h.poll(other, start.LoginID); status != http.StatusNotFound {
		t.Fatalf("poll by another key = %d, want 404", status)
	}

	h.approve(strings.ToLower(start.UserCode), "Dao")
	if status, _, _ := h.poll(daemon, start.LoginID); status != http.StatusAccepted {
		t.Fatalf("poll before interval = %d, want 202", status)
	}
	h.clock.Advance(2 * time.Second)
	status, response, raw := h.poll(daemon, start.LoginID)
	if status != http.StatusOK || response.Status != "complete" || response.Machine == nil {
		t.Fatalf("poll = %d %s", status, raw)
	}
	machine := *response.Machine
	if !linkproto.IsHostLabel(machine.Label) || !strings.HasPrefix(machine.Label, "dao-s-studio-") ||
		len(machine.Label) != len("dao-s-studio-")+6 {
		t.Fatalf("label = %q", machine.Label)
	}
	if machine.Hostname != machine.Label+".link.example.com" || machine.RouteID != route ||
		machine.Provider != "dev" || machine.AccountLogin != "dao" {
		t.Fatalf("machine = %+v", machine)
	}
	if len(machine.Relays) != 1 || machine.Relays[0].ControlCAPEM != testCAPEM ||
		machine.Relays[0].ClientDomain != "link.example.com" || machine.Relays[0].ClientPort != 443 ||
		machine.Relays[0].ControlAddress != "relay.example.com:8443" ||
		machine.ACME.DirectoryURL != "https://acme.example.com/dir" {
		t.Fatalf("relays/acme = %+v %+v", machine.Relays, machine.ACME)
	}
	grant, err := linkproto.VerifyRouteGrant([]ed25519.PublicKey{h.public}, machine.Grant, h.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if grant.RouteID != route || grant.Label != machine.Label || grant.DaemonPublicKey != daemon.PublicKeyHex() ||
		grant.AccountID != machine.AccountID || grant.MachineID != machine.MachineID ||
		grant.ExpiresAtMS != machine.GrantExpiresAtMS ||
		grant.ExpiresAtMS-grant.IssuedAtMS != time.Hour.Milliseconds() {
		t.Fatalf("grant = %+v", grant)
	}
	if _, err := linkproto.VerifyRouteGrant(nil, machine.Grant, h.clock.Now()); err == nil {
		t.Fatal("grant verified without the service key")
	}

	// A repeated poll of a complete login is idempotent.
	if status, again, _ := h.poll(daemon, start.LoginID); status != http.StatusOK ||
		again.Machine == nil || again.Machine.Label != machine.Label {
		t.Fatalf("repeat poll = %d %+v", status, again)
	}

	h.clock.Advance(10 * time.Minute)
	status, raw = h.call(daemon, http.MethodGet, "/v1/machine", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /v1/machine = %d %s", status, raw)
	}
	var fetched MachineInfo
	if err := json.Unmarshal(raw, &fetched); err != nil {
		t.Fatal(err)
	}
	if fetched.MachineID != machine.MachineID || fetched.GrantExpiresAtMS <= machine.GrantExpiresAtMS {
		t.Fatalf("fetched = %+v", fetched)
	}
	if _, err := linkproto.VerifyRouteGrant([]ed25519.PublicKey{h.public}, fetched.Grant, h.clock.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestReloginKeepsLabel(t *testing.T) {
	h := newHarness(t, nil)
	daemon := newDaemon(t)
	route := newRouteID(t)
	first := h.login(daemon, "studio", route)
	second := h.login(daemon, "renamed", route)
	if second.Label != first.Label || second.MachineID != first.MachineID || second.RouteID != route {
		t.Fatalf("relogin = %+v, first = %+v", second, first)
	}
	// A new route for the same key moves the machine and keeps the label.
	newRoute := newRouteID(t)
	third := h.login(daemon, "studio", newRoute)
	if third.Label != first.Label || third.RouteID != newRoute {
		t.Fatalf("new route relogin = %+v", third)
	}
}

func TestRouteBoundToAnotherKey(t *testing.T) {
	h := newHarness(t, nil)
	owner := newDaemon(t)
	route := newRouteID(t)
	h.login(owner, "studio", route)

	intruder := newDaemon(t)
	status, raw := h.call(intruder, http.MethodPost, "/v1/login/start", LoginStartRequest{MachineName: "x", RouteID: route})
	if status != http.StatusConflict {
		t.Fatalf("start with bound route = %d %s, want 409", status, raw)
	}

	// The route is claimed between start and completion.
	late := newDaemon(t)
	contested := newRouteID(t)
	start := h.startLogin(late, "late", contested)
	h.login(newDaemon(t), "first", contested)
	h.approve(start.UserCode, "late")
	h.clock.Advance(3 * time.Second)
	if status, _, raw := h.poll(late, start.LoginID); status != http.StatusConflict {
		t.Fatalf("racing completion = %d %s, want 409", status, raw)
	}
}

func TestMachineQuota(t *testing.T) {
	h := newHarness(t, func(config *Config) { config.MaxMachinesPerAccount = 2 })
	first := newDaemon(t)
	firstRoute := newRouteID(t)
	firstMachine := h.login(first, "one", firstRoute)
	h.login(newDaemon(t), "two", newRouteID(t))

	third := newDaemon(t)
	start := h.startLogin(third, "three", newRouteID(t))
	h.approve(start.UserCode, "alice")
	h.clock.Advance(3 * time.Second)
	status, _, raw := h.poll(third, start.LoginID)
	if status != http.StatusForbidden || errorOf(raw) != "quota" {
		t.Fatalf("over quota = %d %s, want 403 quota", status, raw)
	}
	if status, _, raw := h.poll(third, start.LoginID); status != http.StatusForbidden || errorOf(raw) != "quota" {
		t.Fatalf("repeat over quota = %d %s", status, raw)
	}

	// Re-login of an existing machine at the limit still works.
	again := h.login(first, "one", firstRoute)
	if again.Label != firstMachine.Label {
		t.Fatalf("relogin at quota label = %q, want %q", again.Label, firstMachine.Label)
	}
	// Another account is unaffected.
	h.loginUser(third, "three", newRouteID(t), "bob")
}

func (h *harness) loginAs(daemon *auth.Manager, username string) MachineInfo {
	return h.loginUser(daemon, "box", newRouteID(h.t), username)
}

func TestACMEChallengeScope(t *testing.T) {
	h := newHarness(t, func(config *Config) { config.ACMEOpsPerHour = 3 })
	a := newDaemon(t)
	b := newDaemon(t)
	machineA := h.loginAs(a, "alice")
	machineB := h.loginAs(b, "alice")
	value := strings.Repeat("A", 42) + "_"

	status, raw := h.call(b, http.MethodPut, "/v1/machine/acme-challenge",
		ACMEChallengeRequest{FQDN: "_acme-challenge." + machineA.Hostname, Value: value})
	if status != http.StatusForbidden {
		t.Fatalf("B sets A's TXT = %d %s, want 403", status, raw)
	}
	for _, fqdn := range []string{machineB.Hostname, "_acme-challenge.x." + machineB.Hostname, "_acme-challenge.link.example.com"} {
		if status, _ := h.call(b, http.MethodPut, "/v1/machine/acme-challenge",
			ACMEChallengeRequest{FQDN: fqdn, Value: value}); status != http.StatusForbidden {
			t.Fatalf("fqdn %q = %d, want 403", fqdn, status)
		}
	}
	for _, bad := range []string{"", strings.Repeat("A", 42), strings.Repeat("A", 42) + "=", strings.Repeat("A", 44)} {
		if status, _ := h.call(b, http.MethodPut, "/v1/machine/acme-challenge",
			ACMEChallengeRequest{FQDN: "_acme-challenge." + machineB.Hostname, Value: bad}); status != http.StatusBadRequest {
			t.Fatalf("value %q = %d, want 400", bad, status)
		}
	}

	own := "_ACME-Challenge." + strings.ToUpper(machineB.Hostname) + "."
	if status, raw := h.call(b, http.MethodPut, "/v1/machine/acme-challenge",
		ACMEChallengeRequest{FQDN: own, Value: value}); status != http.StatusNoContent {
		t.Fatalf("own TXT set = %d %s", status, raw)
	}
	if status, raw := h.call(b, http.MethodDelete, "/v1/machine/acme-challenge",
		ACMEChallengeRequest{FQDN: own, Value: value}); status != http.StatusNoContent {
		t.Fatalf("own TXT clear = %d %s", status, raw)
	}
	want := []dnsCall{
		{"set", "_acme-challenge." + machineB.Hostname, value},
		{"clear", "_acme-challenge." + machineB.Hostname, value},
	}
	h.dns.mu.Lock()
	got := append([]dnsCall(nil), h.dns.calls...)
	h.dns.mu.Unlock()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("dns calls = %+v, want %+v", got, want)
	}

	for range 2 {
		if status, _ := h.call(b, http.MethodPut, "/v1/machine/acme-challenge",
			ACMEChallengeRequest{FQDN: own, Value: value}); status != http.StatusNoContent {
			t.Fatalf("set within limit = %d", status)
		}
	}
	if status, _ := h.call(b, http.MethodPut, "/v1/machine/acme-challenge",
		ACMEChallengeRequest{FQDN: own, Value: value}); status != http.StatusTooManyRequests {
		t.Fatalf("set over limit = %d, want 429", status)
	}
	h.clock.Advance(time.Hour + time.Second)
	if status, _ := h.call(b, http.MethodPut, "/v1/machine/acme-challenge",
		ACMEChallengeRequest{FQDN: own, Value: value}); status != http.StatusNoContent {
		t.Fatalf("set after window = %d", status)
	}
}

func TestDisabledAccount(t *testing.T) {
	h := newHarness(t, nil)
	daemon := newDaemon(t)
	machine := h.loginAs(daemon, "mallory")
	if err := h.store.SetAccountDisabled(context.Background(), machine.AccountID, true); err != nil {
		t.Fatal(err)
	}
	status, raw := h.call(daemon, http.MethodGet, "/v1/machine", nil)
	if status != http.StatusForbidden || strings.Contains(string(raw), "grant") {
		t.Fatalf("disabled GET /v1/machine = %d %s, want 403 without grant", status, raw)
	}
	if status, _ := h.call(daemon, http.MethodPut, "/v1/machine/acme-challenge", ACMEChallengeRequest{
		FQDN: "_acme-challenge." + machine.Hostname, Value: strings.Repeat("a", 43),
	}); status != http.StatusForbidden {
		t.Fatalf("disabled ACME = %d, want 403", status)
	}

	// A new login by the disabled account is denied.
	other := newDaemon(t)
	start := h.startLogin(other, "box", newRouteID(t))
	h.approve(start.UserCode, "mallory")
	h.clock.Advance(3 * time.Second)
	if status, _, raw := h.poll(other, start.LoginID); status != http.StatusForbidden || errorOf(raw) != "denied" {
		t.Fatalf("disabled login = %d %s, want 403 denied", status, raw)
	}

	if err := h.store.SetAccountDisabled(context.Background(), machine.AccountID, false); err != nil {
		t.Fatal(err)
	}
	if status, _ := h.call(daemon, http.MethodGet, "/v1/machine", nil); status != http.StatusOK {
		t.Fatalf("re-enabled GET /v1/machine = %d", status)
	}
}

func TestLogoutDeletesMachine(t *testing.T) {
	h := newHarness(t, nil)
	daemon := newDaemon(t)
	route := newRouteID(t)
	machine := h.login(daemon, "studio", route)
	if status, raw := h.call(daemon, http.MethodDelete, "/v1/machine", nil); status != http.StatusNoContent {
		t.Fatalf("logout = %d %s", status, raw)
	}
	if status, _ := h.call(daemon, http.MethodGet, "/v1/machine", nil); status != http.StatusNotFound {
		t.Fatalf("GET after logout = %d, want 404", status)
	}
	if status, _ := h.call(daemon, http.MethodDelete, "/v1/machine", nil); status != http.StatusNotFound {
		t.Fatalf("second logout = %d, want 404", status)
	}
	// The route is free for another key and the old key gets a new label.
	h.login(newDaemon(t), "other", route)
	again := h.login(daemon, "studio", newRouteID(t))
	if again.MachineID == machine.MachineID {
		t.Fatal("machine id reused after logout")
	}
}

func TestLoginExpiresAndValidates(t *testing.T) {
	h := newHarness(t, nil)
	daemon := newDaemon(t)
	start := h.startLogin(daemon, "", newRouteID(t))
	h.clock.Advance(fakeLoginTTL + time.Second)
	if status, _, raw := h.poll(daemon, start.LoginID); status != http.StatusGone || errorOf(raw) != "expired" {
		t.Fatalf("expired poll = %d %s, want 410", status, raw)
	}
	if status, _ := h.call(daemon, http.MethodPost, "/v1/login/start",
		LoginStartRequest{MachineName: "x", RouteID: "not-a-route"}); status != http.StatusBadRequest {
		t.Fatalf("bad route = %d, want 400", status)
	}
	if status, _, _ := h.poll(daemon, newRouteID(t)); status != http.StatusNotFound {
		t.Fatalf("unknown login = %d, want 404", status)
	}
	for range maxPendingLoginsByKey {
		h.startLogin(daemon, "x", newRouteID(t))
	}
	if status, _ := h.call(daemon, http.MethodPost, "/v1/login/start",
		LoginStartRequest{MachineName: "x", RouteID: newRouteID(t)}); status != http.StatusTooManyRequests {
		t.Fatalf("too many pending = %d, want 429", status)
	}
}

// scriptedProvider returns queued poll results and counts polls.
type scriptedProvider struct {
	mu      sync.Mutex
	results []PollResult
	polls   int
}

func (p *scriptedProvider) Name() string { return "script" }

func (p *scriptedProvider) StartDevice(context.Context) (DeviceStart, error) {
	return DeviceStart{
		Handle:          "secret-handle",
		UserCode:        "CODE-1234",
		VerificationURI: "https://idp.example.com/device",
		Interval:        5 * time.Second,
		ExpiresAt:       time.UnixMilli(1_800_000_000_000).Add(15 * time.Minute),
	}, nil
}

func (p *scriptedProvider) Poll(_ context.Context, handle string) (PollResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.polls++
	if handle != "secret-handle" || len(p.results) == 0 {
		return PollResult{Status: PollPending}, nil
	}
	result := p.results[0]
	p.results = p.results[1:]
	return result, nil
}

func TestPollThrottlesIdentityProvider(t *testing.T) {
	provider := &scriptedProvider{results: []PollResult{
		{Status: PollPending},
		{Status: PollPending, SlowDown: true},
		{Status: PollComplete, Subject: Subject{ID: "42", Login: "octo"}},
	}}
	h := newHarness(t, func(config *Config) {
		config.Identity = provider
		config.DeviceHandler = nil
	})
	daemon := newDaemon(t)
	start := h.startLogin(daemon, "box", newRouteID(t))
	if start.IntervalSeconds != 5 || start.VerificationURIComplete != "" {
		t.Fatalf("start = %+v", start)
	}
	pollUntil := func(advance time.Duration, wantPolls int, wantStatus int) LoginPollResponse {
		t.Helper()
		h.clock.Advance(advance)
		var response LoginPollResponse
		var status int
		for range 5 {
			status, response, _ = h.poll(daemon, start.LoginID)
		}
		provider.mu.Lock()
		polls := provider.polls
		provider.mu.Unlock()
		if polls != wantPolls || status != wantStatus {
			t.Fatalf("after %v: polls=%d status=%d, want %d %d", advance, polls, status, wantPolls, wantStatus)
		}
		return response
	}
	pollUntil(4*time.Second, 0, http.StatusAccepted)
	pollUntil(time.Second, 1, http.StatusAccepted)   // pending
	pollUntil(5*time.Second, 2, http.StatusAccepted) // slow_down -> 10s
	pollUntil(9*time.Second, 2, http.StatusAccepted)
	response := pollUntil(time.Second, 3, http.StatusOK)
	if response.Machine == nil || response.Machine.Provider != "script" || response.Machine.AccountLogin != "octo" {
		t.Fatalf("response = %+v", response)
	}
	account, err := getAccount(context.Background(), h.store.db, `subject = ?`, "script:42")
	if err != nil || account.ID != response.Machine.AccountID {
		t.Fatalf("account = %+v %v", account, err)
	}
}

func TestNewLabel(t *testing.T) {
	for name, prefix := range map[string]string{
		"Studio":                          "studio",
		"  --Mac mini (Home)--":           "mac-mini-home",
		"日本語":                             "mewla",
		"a-very-long-machine-name-indeed": "a-very-long-machine",
		"aaaaaaaaaaaaaaaaaaaa-b":          "aaaaaaaaaaaaaaaaaaaa",
	} {
		label, err := newLabel(name)
		if err != nil {
			t.Fatal(err)
		}
		if !linkproto.IsHostLabel(label) || !strings.HasPrefix(label, prefix+"-") || len(label) != len(prefix)+7 {
			t.Fatalf("newLabel(%q) = %q, want prefix %q", name, label, prefix)
		}
		if _, err := hex.DecodeString(label[len(prefix)+1:]); err != nil {
			t.Fatalf("label suffix %q", label)
		}
	}
}

func TestUnnamedMachinesGetRandomWordLabels(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		label, err := newLabel("  ")
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(label, "-")
		if !linkproto.IsHostLabel(label) || len(parts) != 3 || len(parts[2]) != 4 {
			t.Fatalf("random label %q", label)
		}
		seen[label] = true
	}
	if len(seen) < 15 {
		t.Fatalf("random labels repeat too often: %d distinct of 20", len(seen))
	}
	h := newHarness(t, nil)
	info := h.login(newDaemon(t), "", newRouteID(t))
	if strings.HasPrefix(info.Label, "mewla-") || len(strings.Split(info.Label, "-")) != 3 {
		t.Fatalf("unnamed login label = %q", info.Label)
	}
}

func TestRenameMachine(t *testing.T) {
	h := newHarness(t, nil)
	daemon := newDaemon(t)
	route := newRouteID(t)
	before := h.login(daemon, "", route)
	status, raw := h.call(daemon, http.MethodPut, "/v1/machine/name", map[string]string{"name": "Studio"})
	if status != http.StatusOK {
		t.Fatalf("rename status=%d body=%s", status, raw)
	}
	var after MachineInfo
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(after.Label, "studio-") || after.RouteID != route || after.MachineID != before.MachineID {
		t.Fatalf("renamed machine = %#v", after)
	}
	grant, err := linkproto.VerifyRouteGrant([]ed25519.PublicKey{h.public}, after.Grant, h.clock.Now())
	if err != nil || grant.Label != after.Label || after.Hostname != after.Label+".link.example.com" {
		t.Fatalf("grant=%#v err=%v hostname=%s", grant, err, after.Hostname)
	}
	stranger := newDaemon(t)
	if status, _ := h.call(stranger, http.MethodPut, "/v1/machine/name", map[string]string{"name": "x"}); status != http.StatusNotFound {
		t.Fatalf("unknown machine rename status=%d", status)
	}
	// Each label costs a certificate from the domain's weekly budget.
	for range renamesPerDay - 1 {
		if status, raw := h.call(daemon, http.MethodPut, "/v1/machine/name", map[string]string{"name": "studio"}); status != http.StatusOK {
			t.Fatalf("rename within the daily limit status=%d body=%s", status, raw)
		}
	}
	if status, _ := h.call(daemon, http.MethodPut, "/v1/machine/name", map[string]string{"name": "studio"}); status != http.StatusTooManyRequests {
		t.Fatalf("rename past the daily limit status=%d", status)
	}
	h.clock.Advance(25 * time.Hour)
	if status, raw := h.call(daemon, http.MethodPut, "/v1/machine/name", map[string]string{"name": "studio"}); status != http.StatusOK {
		t.Fatalf("rename the next day status=%d body=%s", status, raw)
	}
}

func TestLabelsAreNeverHandedOutAgain(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	renamed := newDaemon(t)
	old := h.login(renamed, "", newRouteID(t)).Label
	attempts := []string{old, "fresh-000001"}
	machine, err := h.store.renameMachine(ctx, renamed.PublicKeyHex(), "fresh", func(string) (string, error) {
		next := attempts[0]
		attempts = attempts[1:]
		return next, nil
	})
	if err != nil || machine.Label != "fresh-000001" {
		t.Fatalf("rename got %q err=%v; it must skip its own previous label", machine.Label, err)
	}
	if _, err := h.store.renameMachine(ctx, renamed.PublicKeyHex(), "again", func(string) (string, error) { return old, nil }); err == nil {
		t.Fatalf("label %s was handed out again after a rename", old)
	}

	signedOut := newDaemon(t)
	gone := h.login(signedOut, "", newRouteID(t)).Label
	if status, raw := h.call(signedOut, http.MethodDelete, "/v1/machine", nil); status != http.StatusOK && status != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", status, raw)
	}
	tx, err := h.store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, label := range []string{old, gone} {
		if taken, err := labelTaken(ctx, tx, label); err != nil || !taken {
			t.Fatalf("retired label %s taken=%v err=%v", label, taken, err)
		}
	}
}
