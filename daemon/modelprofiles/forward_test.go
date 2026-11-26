package modelprofiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubRetrySleep records the pauses sendUpstream takes instead of sleeping.
func stubRetrySleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var mu sync.Mutex
	pauses := []time.Duration{}
	old := retrySleep
	retrySleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		pauses = append(pauses, d)
		mu.Unlock()
		return ctx.Err()
	}
	t.Cleanup(func() { retrySleep = old })
	return &pauses
}

// scripted answers each request with the next of its replies (the last one
// repeats) and keeps the bodies it got.
type scripted struct {
	mu      sync.Mutex
	replies []func(http.ResponseWriter)
	bodies  []string
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.bodies = append(s.bodies, string(body))
	reply := s.replies[min(len(s.bodies)-1, len(s.replies)-1)]
	s.mu.Unlock()
	reply(w)
}

func (s *scripted) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func status(code int, header map[string]string, body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

func sendOnce(t *testing.T, url string, retry bool) (*http.Response, string) {
	t.Helper()
	body := []byte(`{"model":"m"}`)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := sendUpstream(http.DefaultClient, req, body, retry)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	return resp, string(got)
}

func TestSendUpstreamRetriesBusyThenAnswers(t *testing.T) {
	pauses := stubRetrySleep(t)
	up := &scripted{replies: []func(http.ResponseWriter){
		status(503, nil, "busy"), status(529, nil, "overloaded"), status(200, nil, "ok"),
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	resp, body := sendOnce(t, srv.URL, true)
	if resp.StatusCode != 200 || body != "ok" || up.calls() != 3 {
		t.Fatalf("status=%d body=%q calls=%d, want the third answer", resp.StatusCode, body, up.calls())
	}
	for _, b := range up.bodies {
		if b != `{"model":"m"}` {
			t.Fatalf("a retry sent %q, want the same body every time", b)
		}
	}
	if got := *pauses; len(got) != 2 || got[0] != time.Second || got[1] != 2*time.Second {
		t.Fatalf("pauses=%v, want [1s 2s]", got)
	}
}

func TestSendUpstreamRateLimitHonoursShortRetryAfter(t *testing.T) {
	pauses := stubRetrySleep(t)
	up := &scripted{replies: []func(http.ResponseWriter){
		status(429, map[string]string{"Retry-After": "3"}, `{"error":{"type":"rate_limit_error"}}`), status(200, nil, "ok"),
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	resp, _ := sendOnce(t, srv.URL, true)
	if resp.StatusCode != 200 || up.calls() != 2 || len(*pauses) != 1 || (*pauses)[0] != 3*time.Second {
		t.Fatalf("status=%d calls=%d pauses=%v, want one 3 s wait then the answer", resp.StatusCode, up.calls(), *pauses)
	}
}

func TestSendUpstreamRateLimitGivesUpAfterThreeRetries(t *testing.T) {
	pauses := stubRetrySleep(t)
	up := &scripted{replies: []func(http.ResponseWriter){status(429, nil, `{"error":{"message":"slow down"}}`)}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	resp, body := sendOnce(t, srv.URL, true)
	if resp.StatusCode != 429 || up.calls() != 4 || !strings.Contains(body, "slow down") {
		t.Fatalf("status=%d calls=%d body=%q, want the last 429 with its body after 4 calls", resp.StatusCode, up.calls(), body)
	}
	if got := *pauses; len(got) != 3 || got[0] != time.Second || got[1] != 2*time.Second || got[2] != 4*time.Second {
		t.Fatalf("pauses=%v, want [1s 2s 4s]", got)
	}
}

func TestSendUpstreamDoesNotRetryWhatWillNotPass(t *testing.T) {
	cases := map[string]func(http.ResponseWriter){
		"long Retry-After": status(429, map[string]string{"Retry-After": "120"}, "wait"),
		"used-up quota":    status(429, nil, `{"error":{"type":"insufficient_quota","message":"You exceeded your current quota"}}`),
		"bad request":      status(400, nil, "bad"),
		"unauthorized":     status(401, nil, "no"),
		"not found":        status(404, nil, "none"),
		"busy, long wait":  status(503, map[string]string{"Retry-After": "600"}, "later"),
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			pauses := stubRetrySleep(t)
			up := &scripted{replies: []func(http.ResponseWriter){reply}}
			srv := httptest.NewServer(up)
			defer srv.Close()
			resp, body := sendOnce(t, srv.URL, true)
			if up.calls() != 1 || len(*pauses) != 0 || body == "" {
				t.Fatalf("status=%d calls=%d pauses=%v body=%q, want one call and its body", resp.StatusCode, up.calls(), *pauses, body)
			}
		})
	}
}

func TestSendUpstreamOnceWhenRetryIsOff(t *testing.T) {
	stubRetrySleep(t)
	up := &scripted{replies: []func(http.ResponseWriter){status(503, nil, "busy")}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	resp, _ := sendOnce(t, srv.URL, false)
	if resp.StatusCode != 503 || up.calls() != 1 {
		t.Fatalf("status=%d calls=%d, want one call (billed tool endpoint)", resp.StatusCode, up.calls())
	}
}

func TestSendUpstreamRetriesADroppedConnection(t *testing.T) {
	stubRetrySleep(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	resp, body := sendOnce(t, srv.URL, true)
	if resp.StatusCode != 200 || body != "ok" || calls.Load() != 2 {
		t.Fatalf("status=%d body=%q calls=%d, want the answer after one retry", resp.StatusCode, body, calls.Load())
	}
}

func TestSendUpstreamStopsWhenTheClientLeaves(t *testing.T) {
	old := retrySleep
	t.Cleanup(func() { retrySleep = old })
	ctx, cancel := context.WithCancel(context.Background())
	retrySleep = func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }
	up := &scripted{replies: []func(http.ResponseWriter){status(503, nil, "busy")}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	body := []byte(`{}`)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader(body))
	if _, err := sendUpstream(http.DefaultClient, req, body, true); !errors.Is(err, context.Canceled) || up.calls() != 1 {
		t.Fatalf("err=%v calls=%d, want the wait to end with the client", err, up.calls())
	}
}

func TestUpstreamRetryAfterForms(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, c := range []struct {
		h    map[string]string
		want time.Duration
		ok   bool
	}{
		{map[string]string{"Retry-After": "2"}, 2 * time.Second, true},
		{map[string]string{"Retry-After": now.Add(5 * time.Second).Format(http.TimeFormat)}, 5 * time.Second, true},
		{map[string]string{"Retry-After-Ms": "1500"}, 1500 * time.Millisecond, true},
		{map[string]string{"X-Ratelimit-Reset-Requests": "6m0s"}, 6 * time.Minute, true},
		{map[string]string{"X-Ratelimit-Reset-Tokens": "1.5"}, 1500 * time.Millisecond, true},
		{map[string]string{}, 0, false},
	} {
		h := http.Header{}
		for k, v := range c.h {
			h.Set(k, v)
		}
		got, ok := upstreamRetryAfter(h, now)
		if got != c.want || ok != c.ok {
			t.Errorf("%v: got %v %v, want %v %v", c.h, got, ok, c.want, c.ok)
		}
	}
}

// routeFixture is a Claude Session route to an upstream the test scripts.
func routeFixture(t *testing.T, up http.Handler) (owner *Owner, creds *FileCredentialStore, routeRoot, connID string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = io.WriteString(w, `{"data":[{"id":"claude-test"}]}`)
			return
		}
		up.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	owner, creds = newDurableOwner(t, t.TempDir())
	t.Cleanup(func() { _ = owner.Close() })
	created, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: "Relay One", Client: ClientClaude, PresetID: ProviderPresetCustom,
		BaseURL: srv.URL + "/v1", ModelID: "claude-test", Advanced: true,
	}, "fixture-key-not-real", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connID = created.Connections[0].ID
	if _, err := owner.DiscoverProviderModelsDetailed(connID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderConnection(ClientClaude, connID, created.Revision); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunchModel(ExecutorClaude, "", "claude-test", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "forward-fixture"); err != nil {
		t.Fatal(err)
	}
	return owner, creds, plan.Env[EnvAnthropicBaseURL], connID
}

func postMessage(t *testing.T, routeRoot string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, routeRoot+"/v1/messages", strings.NewReader(`{"model":"claude-test","max_tokens":2,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// Sessions Mewla starts used to send each request once: a relay's passing
// 503 was a failed turn, recovered only by turn recovery 30 s later.
func TestSessionRouteRetriesABusyUpstream(t *testing.T) {
	stubRetrySleep(t)
	up := &scripted{replies: []func(http.ResponseWriter){
		status(503, nil, "busy"),
		status(200, map[string]string{"Content-Type": "application/json"}, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`),
	}}
	_, _, root, _ := routeFixture(t, up)
	code, body := postMessage(t, root)
	if code != 200 || !strings.Contains(body, `"ok"`) || up.calls() != 2 {
		t.Fatalf("status=%d calls=%d body=%s, want the retried answer", code, up.calls(), body)
	}
}

// 11-27: a connection with no key answered 502 "request failed"; Claude Code
// retried it for 182 s and then printed "502 request failed".
func TestSessionRouteSaysWhichProviderHasNoKey(t *testing.T) {
	up := &scripted{replies: []func(http.ResponseWriter){status(200, nil, "{}")}}
	owner, creds, root, connID := routeFixture(t, up)
	profile, err := owner.store.Get(connID)
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.Delete(activeCredentialRef(profile)); err != nil {
		t.Fatal(err)
	}
	code, body := postMessage(t, root)
	var payload struct {
		Error struct{ Type, Message string } `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &payload)
	if code != http.StatusForbidden || payload.Error.Type != "credential_not_ready" ||
		!strings.Contains(payload.Error.Message, `"Relay One"`) || !strings.Contains(payload.Error.Message, "Model Providers") {
		t.Fatalf("status=%d body=%s, want 403 naming the Provider and where to add the key", code, body)
	}
	if up.calls() != 0 {
		t.Fatalf("upstream called %d times without a key", up.calls())
	}
}

func TestRouteErrorSaysWhyTheUpstreamCannotBeReached(t *testing.T) {
	stubRetrySleep(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore(), WithGatewayRequestResolver(func(string, string) (GatewayUpstream, error) {
		return GatewayUpstream{ProfileID: "p", Name: "Home GPU", BaseURL: deadURL, Protocol: ProtocolOpenAIResponses, AuthMode: AuthModeNone}, nil
	}))
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:3425/v1/responses", strings.NewReader(`{"model":"m"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	res := httptest.NewRecorder()
	g.ServeHTTP(res, req)
	body := res.Body.String()
	if res.Code != http.StatusBadGateway || !strings.Contains(body, `Mewla could not reach Provider \"Home GPU\"`) || !strings.Contains(body, "refused") || strings.Contains(body, deadURL) {
		t.Fatalf("status=%d body=%s, want 502 naming the Provider and the reason, never its URL", res.Code, body)
	}
}

func TestGatewayRunsTheProvidersRequestAdapter(t *testing.T) {
	var got atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.Store(string(b))
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	old := upstreamAdapters["fixture-vendor"]
	upstreamAdapters["fixture-vendor"] = upstreamAdapter{Responses: func(body []byte) ([]byte, error) {
		return []byte(`{"model":"m","adapted":true}`), nil
	}}
	t.Cleanup(func() {
		if old.Responses == nil {
			delete(upstreamAdapters, "fixture-vendor")
		} else {
			upstreamAdapters["fixture-vendor"] = old
		}
	})
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore(), WithGatewayRequestResolver(func(string, string) (GatewayUpstream, error) {
		return GatewayUpstream{ProfileID: "p", ProviderID: "fixture-vendor", BaseURL: srv.URL, Protocol: ProtocolOpenAIResponses, AuthMode: AuthModeNone}, nil
	}))
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:3425/v1/responses", strings.NewReader(`{"model":"m","store":true}`))
	req.RemoteAddr = "127.0.0.1:1234"
	res := httptest.NewRecorder()
	g.ServeHTTP(res, req)
	if res.Code != 200 || got.Load() != `{"model":"m","adapted":true}` {
		t.Fatalf("status=%d upstream got %v, want the adapted body (a plain-terminal Codex fits its Provider as a Session does)", res.Code, got.Load())
	}
}

func TestTailscaleAddressesAreDialableButMetadataIsNot(t *testing.T) {
	for addr, want := range map[string]bool{
		"100.101.102.103": true,  // a Tailscale node
		"100.64.0.1":      true,  // carrier-grade NAT
		"100.100.100.200": false, // Alibaba Cloud metadata
		"169.254.169.254": false, // AWS/GCP/Azure metadata
		"192.168.1.20":    true,
	} {
		if got := isDialableUpstreamIP(netip.MustParseAddr(addr), false); got != want {
			t.Errorf("%s dialable = %v, want %v", addr, got, want)
		}
	}
}

// 11-27 side by side: Codex asked the machine gateway for a model that only
// an older connection's cached list held, and the request went there while
// the newly added, chosen connection (never listed yet) was skipped.
func TestGatewayPrefersTheChosenCodexProviderBeforeItsListIsKnown(t *testing.T) {
	models := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"shared-model"}]}`)
	}))
	defer models.Close()
	owner, _ := newDurableOwner(t, t.TempDir())
	defer owner.Close()
	add := func(name, url string) string {
		t.Helper()
		cat, err := owner.UpsertProviderConnection(ProviderConnectionInput{
			Name: name, Client: ClientCodex, PresetID: ProviderPresetCustom, BaseURL: url, Advanced: true,
		}, "fixture-key-not-real", owner.Catalog().Revision, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cat.Connections {
			if c.Name == name {
				return c.ID
			}
		}
		t.Fatalf("connection %s not created", name)
		return ""
	}
	older := add("Older", models.URL+"/v1")
	if _, err := owner.DiscoverProviderModelsDetailed(older, true); err != nil {
		t.Fatal(err)
	}
	chosen := add("Chosen", "http://127.0.0.1:9/v1")
	if _, err := owner.SetProviderConnection(ClientCodex, chosen, owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	up, err := owner.resolveGatewayRequest(GatewayProtocolResponses, "shared-model")
	if err != nil || up.ProfileID != chosen {
		t.Fatalf("resolved %q (err %v), want the chosen connection %q", up.ProfileID, err, chosen)
	}
}
