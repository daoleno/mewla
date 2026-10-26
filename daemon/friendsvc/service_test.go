package friendsvc

import (
	"bytes"
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
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// keyPair is a raw Ed25519 key that can sign either dialect: humans sign the
// payload as is; agents sign it behind the daemon's link-control prefix, the
// way auth.Manager.CreateLinkSignature does.
type keyPair struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func newKeyPair(t *testing.T) keyPair {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return keyPair{pub, priv}
}

func (k keyPair) hex() string { return hex.EncodeToString(k.pub) }

type dialect int

const (
	asHuman dialect = iota
	asAgent
)

type harness struct {
	t      *testing.T
	svc    *Service
	server *httptest.Server
	clock  *clock
	oauth  *FakeOAuth
	client *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "friends.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	clk := &clock{t: time.Unix(1_800_000_000, 0)}
	oauth := &FakeOAuth{DefaultLogin: "Tester", DefaultAgeDays: 400, Now: clk.Now}
	h := &harness{t: t, clock: clk, oauth: oauth}
	svc, err := New(Config{Store: store, OAuth: oauth, PublicURL: "http://placeholder", Now: clk.Now, Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	h.svc = svc
	h.server = httptest.NewServer(svc.Handler())
	t.Cleanup(h.server.Close)
	svc.config.PublicURL = h.server.URL
	h.client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return h
}

type signOptions struct {
	at    time.Time
	nonce string
	as    dialect
}

func (h *harness) signed(method, path string, body any, key keyPair, options signOptions) *http.Request {
	h.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	at := options.at
	if at.IsZero() {
		at = h.clock.Now()
	}
	nonce := options.nonce
	if nonce == "" {
		nonce, _ = randomHex(16)
	}
	scheme := HumanScheme
	if options.as == asAgent {
		scheme = AgentScheme
	}
	timestamp := strconv.FormatInt(at.UnixMilli(), 10)
	payload := scheme.Payload(method, path, timestamp, nonce, raw)
	if options.as == asAgent {
		payload = append([]byte("mewla-link-control-v1\x00"), payload...)
	}
	signature := hex.EncodeToString(ed25519.Sign(key.priv, payload))
	request, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		h.t.Fatal(err)
	}
	request.Header.Set("Authorization", scheme.Header(key.hex(), timestamp, nonce, signature))
	return request
}

func (h *harness) do(request *http.Request) (int, []byte) {
	h.t.Helper()
	response, err := h.client.Do(request)
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	return response.StatusCode, raw
}

func (h *harness) call(method, path string, body any, key keyPair, as dialect) (int, map[string]any) {
	h.t.Helper()
	status, raw := h.do(h.signed(method, path, body, key, signOptions{as: as}))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return status, out
}

const returnURL = "mewla://friends/enroll"

// startEnroll runs start and callback and returns the app return URL's query.
func (h *harness) startEnroll(key keyPair, hints url.Values) url.Values {
	h.t.Helper()
	query := url.Values{"key": {key.hex()}, "return": {returnURL}}
	for name, values := range hints {
		query[name] = values
	}
	response, err := h.client.Get(h.server.URL + apiPrefix + "/enroll/start?" + query.Encode())
	if err != nil {
		h.t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound {
		h.t.Fatalf("start = %d", response.StatusCode)
	}
	callback, err := h.client.Get(response.Header.Get("Location"))
	if err != nil {
		h.t.Fatal(err)
	}
	callback.Body.Close()
	if callback.StatusCode != http.StatusFound {
		h.t.Fatalf("callback = %d", callback.StatusCode)
	}
	location, err := url.Parse(callback.Header.Get("Location"))
	if err != nil || !strings.HasPrefix(location.String(), returnURL+"?") {
		h.t.Fatalf("callback redirect = %q", callback.Header.Get("Location"))
	}
	return location.Query()
}

func (h *harness) enroll(key keyPair, hints url.Values) (int, map[string]any) {
	h.t.Helper()
	back := h.startEnroll(key, hints)
	if back.Get("ticket") == "" {
		h.t.Fatalf("no ticket: %v", back)
	}
	return h.call(http.MethodPost, apiPrefix+"/enroll/finish",
		map[string]string{"ticket": back.Get("ticket"), "challenge": back.Get("challenge"), "device_label": "Pixel"}, key, asHuman)
}

func (h *harness) linkAgent(person keyPair, agent keyPair) (int, map[string]any) {
	h.t.Helper()
	status, out := h.call(http.MethodPost, apiPrefix+"/me/link-code", nil, person, asHuman)
	if status != 200 {
		h.t.Fatalf("link-code = %d %v", status, out)
	}
	return h.call(http.MethodPost, apiPrefix+"/agent/link", map[string]string{"code": out["code"].(string), "name": "Mochi"}, agent, asAgent)
}

// world has one person with a registered human key and a linked agent key.
func newWorld(t *testing.T) (*harness, keyPair, keyPair) {
	h := newHarness(t)
	human, agent := newKeyPair(t), newKeyPair(t)
	if status, out := h.enroll(human, nil); status != 200 {
		t.Fatalf("enroll = %d %v", status, out)
	}
	if status, out := h.linkAgent(human, agent); status != 200 {
		t.Fatalf("link = %d %v", status, out)
	}
	return h, human, agent
}

func TestEveryRouteHasAClassAndTheTableIsWhatIsMounted(t *testing.T) {
	h := newHarness(t)
	for _, route := range h.svc.Routes() {
		if route.Class == classUntagged || route.Handler == nil {
			t.Errorf("route %s is untagged", route.Pattern())
		}
		if !strings.HasPrefix(route.Path, PathPrefix+"/") {
			t.Errorf("route %s is outside %s", route.Pattern(), PathPrefix)
		}
	}
	// Whatever the mux serves is in the table: an unlisted path is a 404.
	status, _ := h.do(h.signed(http.MethodGet, apiPrefix+"/me/unlisted", nil, newKeyPair(t), signOptions{}))
	if status != http.StatusNotFound {
		t.Fatalf("unlisted route = %d", status)
	}
}

func TestUntaggedRouteIsRefused(t *testing.T) {
	noop := func(http.ResponseWriter, *http.Request, Principal) {}
	good := Route{http.MethodGet, "/friends/v1/a", ClassHuman, noop}
	for name, routes := range map[string][]Route{
		"untagged":   {good, {http.MethodGet, "/friends/v1/b", classUntagged, noop}},
		"unknown":    {{http.MethodGet, "/friends/v1/b", Class(99), noop}},
		"duplicate":  {good, good},
		"no handler": {{http.MethodGet, "/friends/v1/b", ClassHuman, nil}},
	} {
		if err := validateRoutes(routes); err == nil {
			t.Errorf("%s: validateRoutes accepted it", name)
		}
	}
	if err := validateRoutes([]Route{good}); err != nil {
		t.Fatal(err)
	}
}

// bodyFor is a body each signed route accepts as well-formed, so a request
// that is let in reaches its handler instead of failing on its input.
func bodyFor(route Route) any {
	switch route.Path {
	case apiPrefix + "/enroll/finish":
		return map[string]string{"ticket": strings.Repeat("0", 32), "challenge": strings.Repeat("0", 32)}
	case apiPrefix + "/me/handle":
		return map[string]string{"handle": "brand-new"}
	case apiPrefix + "/agent/link":
		return map[string]string{"code": strings.Repeat("0", 32)}
	}
	return nil
}

func TestRouteMatrix(t *testing.T) {
	denied := func(status int) bool { return status == 401 || status == 403 }
	for _, route := range newHarness(t).svc.Routes() {
		if !route.Class.signed() {
			continue
		}
		route := route
		t.Run(route.Class.String()+" "+route.Pattern(), func(t *testing.T) {
			h, human, agent := newWorld(t)
			own, other, otherDialect, ownDialect := human, agent, asAgent, asHuman
			if !route.Class.human() {
				own, other, otherDialect, ownDialect = agent, human, asHuman, asAgent
			}
			send := func(key keyPair, options signOptions) int {
				status, _ := h.do(h.signed(route.Method, route.Path, bodyFor(route), key, options))
				return status
			}
			// The right credential is let in (the handler may still say 400/409).
			if status := send(own, signOptions{as: ownDialect}); denied(status) || status == 429 {
				t.Errorf("own credential = %d", status)
			}
			// The other class's credential, correctly signed, is refused.
			if status := send(other, signOptions{as: otherDialect}); status != 403 {
				t.Errorf("other class credential = %d, want 403", status)
			}
			// No signature.
			request, _ := http.NewRequest(route.Method, h.server.URL+route.Path, bytes.NewReader(nil))
			if status, _ := h.do(request); status != 401 {
				t.Errorf("unsigned = %d, want 401", status)
			}
			// Replay: the same nonce twice.
			options := signOptions{as: ownDialect, nonce: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
			send(own, options)
			if status := send(own, options); status != 401 {
				t.Errorf("replay = %d, want 401", status)
			}
			// Expired and from the future.
			for _, offset := range []time.Duration{-10 * time.Minute, 10 * time.Minute} {
				if status := send(own, signOptions{as: ownDialect, at: h.clock.Now().Add(offset)}); status != 401 {
					t.Errorf("clock offset %s = %d, want 401", offset, status)
				}
			}
			// A key nobody registered. Enrollment and linking take new keys;
			// every other route knows none.
			stranger := newKeyPair(t)
			status := send(stranger, signOptions{as: ownDialect})
			switch route.Class {
			case ClassEnroll, ClassAgentLink:
				if denied(status) {
					t.Errorf("new key = %d, want let in", status)
				}
			default:
				if status != 401 {
					t.Errorf("unregistered key = %d, want 401", status)
				}
			}
		})
	}
}

func TestPublicRoutesNeedNoSignature(t *testing.T) {
	h := newHarness(t)
	for _, route := range h.svc.Routes() {
		if route.Class != ClassPublic {
			continue
		}
		request, _ := http.NewRequest(route.Method, h.server.URL+route.Path, nil)
		status, _ := h.do(request)
		if status == 401 || status == 403 {
			t.Errorf("%s = %d without a signature", route.Pattern(), status)
		}
	}
}

func TestKeyIsOneClassOnly(t *testing.T) {
	h, human, agent := newWorld(t)
	// A human key cannot become the agent.
	status, out := h.call(http.MethodPost, apiPrefix+"/me/link-code", nil, human, asHuman)
	if status != 200 {
		t.Fatal(status)
	}
	if status, _ := h.call(http.MethodPost, apiPrefix+"/agent/link", map[string]string{"code": out["code"].(string)}, human, asAgent); status != 403 {
		t.Errorf("human key linked as agent = %d, want 403", status)
	}
	// An agent key cannot enroll as a device.
	back := h.startEnroll(agent, url.Values{"fake_login": {"someone-else"}})
	if status, _ := h.call(http.MethodPost, apiPrefix+"/enroll/finish",
		map[string]string{"ticket": back.Get("ticket"), "challenge": back.Get("challenge")}, agent, asHuman); status != 403 {
		t.Errorf("agent key enrolled as human = %d, want 403", status)
	}
	// An agent key signing a human route is refused even when valid.
	if status, _ := h.call(http.MethodGet, apiPrefix+"/me", nil, agent, asHuman); status != 401 && status != 403 {
		t.Errorf("agent key on /me = %d", status)
	}
}

func TestAgentCannotDoWhatPeopleDo(t *testing.T) {
	h, _, agent := newWorld(t)
	for _, route := range h.svc.Routes() {
		if route.Class != ClassHuman {
			continue
		}
		status, _ := h.do(h.signed(route.Method, route.Path, bodyFor(route), agent, signOptions{as: asAgent}))
		if status != 403 {
			t.Errorf("agent on %s = %d, want 403", route.Pattern(), status)
		}
	}
}

func TestEnrollmentAndHandle(t *testing.T) {
	h := newHarness(t)
	human := newKeyPair(t)
	status, out := h.enroll(human, nil)
	if status != 200 {
		t.Fatalf("enroll = %d %v", status, out)
	}
	person := out["person"].(map[string]any)
	if person["handle"] != "tester" || person["github_login"] != "Tester" {
		t.Fatalf("person = %v", person)
	}
	// Same device again is idempotent through a fresh sign-in.
	if status, _ := h.enroll(human, nil); status != 200 {
		t.Errorf("re-enroll same device = %d", status)
	}
	// A second device on the same GitHub account is refused for now.
	if status, out := h.enroll(newKeyPair(t), nil); status != 409 || out["error"] != "already_enrolled" {
		t.Errorf("second device = %d %v", status, out)
	}
	// A different person whose login collides gets a suffixed handle.
	other := newKeyPair(t)
	status, out = h.enroll(other, url.Values{"fake_login": {"TESTER"}})
	if status != 200 {
		t.Fatalf("colliding login enroll = %d %v", status, out)
	}
	if handle, _ := out["person"].(map[string]any)["handle"].(string); handle == "tester" || !strings.HasPrefix(handle, "tester-") {
		t.Errorf("collision handle = %q", handle)
	}
	// The handle can change once.
	if status, _ := h.call(http.MethodPut, apiPrefix+"/me/handle", map[string]string{"handle": "Bad Handle"}, human, asHuman); status != 400 {
		t.Errorf("bad handle = %d", status)
	}
	if status, _ := h.call(http.MethodPut, apiPrefix+"/me/handle", map[string]string{"handle": "mochi-owner"}, human, asHuman); status != 200 {
		t.Errorf("change handle = %d", status)
	}
	if status, out := h.call(http.MethodPut, apiPrefix+"/me/handle", map[string]string{"handle": "again-owner"}, human, asHuman); status != 409 || out["error"] != "handle_already_changed" {
		t.Errorf("second change = %d %v", status, out)
	}
	if status, out := h.call(http.MethodGet, apiPrefix+"/me", nil, human, asHuman); status != 200 || out["person"].(map[string]any)["handle"] != "mochi-owner" || out["handle_changed"] != true {
		t.Errorf("me = %d %v", status, out)
	}
}

func TestHandleTakenOnChange(t *testing.T) {
	h := newHarness(t)
	a, b := newKeyPair(t), newKeyPair(t)
	h.enroll(a, url.Values{"fake_login": {"alice"}})
	h.enroll(b, url.Values{"fake_login": {"bobby"}})
	if status, out := h.call(http.MethodPut, apiPrefix+"/me/handle", map[string]string{"handle": "alice"}, b, asHuman); status != 409 || out["error"] != "handle_taken" {
		t.Errorf("taken handle = %d %v", status, out)
	}
}

func TestAccountAgeRule(t *testing.T) {
	h := newHarness(t)
	for days, want := range map[string]string{"29": "account_too_new", "31": ""} {
		back := h.startEnroll(newKeyPair(t), url.Values{"fake_login": {"age" + days}, "fake_age_days": {days}})
		if back.Get("error") != want {
			t.Errorf("age %s days: error=%q, want %q", days, back.Get("error"), want)
		}
		if want == "" && back.Get("ticket") == "" {
			t.Errorf("age %s days: no ticket", days)
		}
	}
}

func TestEnrollRejectsStolenOrReusedTickets(t *testing.T) {
	h := newHarness(t)
	owner, thief := newKeyPair(t), newKeyPair(t)
	back := h.startEnroll(owner, nil)
	body := map[string]string{"ticket": back.Get("ticket"), "challenge": back.Get("challenge")}
	if status, _ := h.call(http.MethodPost, apiPrefix+"/enroll/finish", body, thief, asHuman); status != 400 {
		t.Errorf("ticket used by another key = %d", status)
	}
	if status, _ := h.call(http.MethodPost, apiPrefix+"/enroll/finish", map[string]string{"ticket": body["ticket"], "challenge": strings.Repeat("1", 32)}, owner, asHuman); status != 400 {
		t.Errorf("wrong challenge = %d", status)
	}
	if status, _ := h.call(http.MethodPost, apiPrefix+"/enroll/finish", body, owner, asHuman); status != 200 {
		t.Errorf("owner finish = %d", status)
	}
	if status, _ := h.call(http.MethodPost, apiPrefix+"/enroll/finish", body, owner, asHuman); status != 400 {
		t.Errorf("ticket reuse = %d", status)
	}
	// An expired session cannot finish.
	late := newKeyPair(t)
	back = h.startEnroll(late, url.Values{"fake_login": {"slow"}})
	h.clock.Advance(enrollTTL + time.Minute)
	if status, _ := h.call(http.MethodPost, apiPrefix+"/enroll/finish", map[string]string{"ticket": back.Get("ticket"), "challenge": back.Get("challenge")}, late, asHuman); status != 400 {
		t.Errorf("expired session = %d", status)
	}
}

func TestEnrollStartValidatesInput(t *testing.T) {
	h := newHarness(t)
	key := newKeyPair(t)
	for name, query := range map[string]url.Values{
		"bad key":      {"key": {"zz"}, "return": {returnURL}},
		"other scheme": {"key": {key.hex()}, "return": {"https://evil.example/enroll"}},
		"other path":   {"key": {key.hex()}, "return": {"mewla://friends/steal"}},
		"with query":   {"key": {key.hex()}, "return": {returnURL + "?x=1"}},
		"missing":      {},
	} {
		response, err := h.client.Get(h.server.URL + apiPrefix + "/enroll/start?" + query.Encode())
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 400 {
			t.Errorf("%s = %d", name, response.StatusCode)
		}
	}
	// A callback with an unknown state, and a replayed one, are refused.
	response, _ := h.client.Get(h.server.URL + apiPrefix + "/enroll/callback?state=nope&code=fake:x:400")
	response.Body.Close()
	if response.StatusCode != 400 {
		t.Errorf("unknown state = %d", response.StatusCode)
	}
}

func TestLinkCodeSingleUseExpiryAndRebind(t *testing.T) {
	h, human, agent := newWorld(t)
	// Status works for the linked agent and shows the defaults.
	status, out := h.call(http.MethodGet, apiPrefix+"/agent/status", nil, agent, asAgent)
	if status != 200 || out["handle"] != "tester" || out["auto_post"] != false || out["daily_cap"] != float64(3) {
		t.Fatalf("status = %d %v", status, out)
	}
	// A code works once.
	_, code := h.call(http.MethodPost, apiPrefix+"/me/link-code", nil, human, asHuman)
	next := newKeyPair(t)
	body := map[string]string{"code": code["code"].(string)}
	if status, out := h.call(http.MethodPost, apiPrefix+"/agent/link", body, next, asAgent); status != 200 || out["replaced"] != true {
		t.Fatalf("rebind = %d %v", status, out)
	}
	if status, _ := h.call(http.MethodPost, apiPrefix+"/agent/link", body, newKeyPair(t), asAgent); status != 400 {
		t.Errorf("code reuse = %d", status)
	}
	// Linking a new computer replaced the old one.
	if status, _ := h.call(http.MethodGet, apiPrefix+"/agent/status", nil, agent, asAgent); status != 401 {
		t.Errorf("old agent after rebind = %d, want 401", status)
	}
	if status, _ := h.call(http.MethodGet, apiPrefix+"/agent/status", nil, next, asAgent); status != 200 {
		t.Errorf("new agent = %d", status)
	}
	// A code expires.
	_, code = h.call(http.MethodPost, apiPrefix+"/me/link-code", nil, human, asHuman)
	h.clock.Advance(linkCodeTTL + time.Second)
	if status, _ := h.call(http.MethodPost, apiPrefix+"/agent/link", map[string]string{"code": code["code"].(string)}, newKeyPair(t), asAgent); status != 400 {
		t.Errorf("expired code = %d", status)
	}
	// Guessing is limited per key.
	guesser := newKeyPair(t)
	var last int
	for i := 0; i <= linkAttemptsPerKeyHour; i++ {
		last, _ = h.call(http.MethodPost, apiPrefix+"/agent/link", map[string]string{"code": strings.Repeat("a", 32)}, guesser, asAgent)
	}
	if last != 429 {
		t.Errorf("link attempts after the limit = %d, want 429", last)
	}
}

func TestLinkCodesAreLimited(t *testing.T) {
	h, human, _ := newWorld(t)
	var last int
	for i := 0; i < linkCodesPerPersonHour+1; i++ {
		last, _ = h.call(http.MethodPost, apiPrefix+"/me/link-code", nil, human, asHuman)
	}
	if last != 429 {
		t.Errorf("link codes over the limit = %d, want 429", last)
	}
}

func TestOnlyHashesOfCodesAreStored(t *testing.T) {
	h, human, _ := newWorld(t)
	_, out := h.call(http.MethodPost, apiPrefix+"/me/link-code", nil, human, asHuman)
	code := out["code"].(string)
	var count int
	if err := h.svc.config.Store.db.QueryRow(`SELECT COUNT(*) FROM link_codes WHERE hash = ?`, code).Scan(&count); err != nil || count != 0 {
		t.Fatalf("raw code stored: %d %v", count, err)
	}
}

func TestDaemonManagerSignaturesVerify(t *testing.T) {
	// The real daemon identity signs the agent dialect with CreateLinkSignature.
	h, human, _ := newWorld(t)
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, out := h.call(http.MethodPost, apiPrefix+"/me/link-code", nil, human, asHuman)
	raw, _ := json.Marshal(map[string]string{"code": out["code"].(string)})
	timestamp := strconv.FormatInt(h.clock.Now().UnixMilli(), 10)
	nonce, _ := randomHex(16)
	path := apiPrefix + "/agent/link"
	signature := manager.CreateLinkSignature(AgentScheme.Payload(http.MethodPost, path, timestamp, nonce, raw))
	request, _ := http.NewRequest(http.MethodPost, h.server.URL+path, bytes.NewReader(raw))
	request.Header.Set("Authorization", AgentScheme.Header(manager.PublicKeyHex(), timestamp, nonce, signature))
	if status, body := h.do(request); status != 200 {
		t.Fatalf("daemon link = %d %s", status, body)
	}
}
