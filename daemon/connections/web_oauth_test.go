package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/pluginreturn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A web UI sign-in registers and returns to the web origin's own callback,
// then lands back on its Plugins page with the account connected.
func TestWebConnectReturnsToPlugins(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "Web fixture", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "read_web", Description: "Read the web fixture", InputSchema: object(map[string]any{})}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
	})
	protocol := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	var base string
	var registered []string
	mux := http.NewServeMux()
	mux.HandleFunc("/resource", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(oauthMetadata{Issuer: base, AuthorizationEndpoint: base + "/authorize", TokenEndpoint: base + "/token", RegistrationEndpoint: base + "/register", CodeChallengeMethods: []string{"S256"}, AuthMethods: []string{"none"}, ScopesSupported: []string{"read"}})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RedirectURIs []string `json:"redirect_uris"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		registered = body.RedirectURIs
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "web-client", "token_endpoint_auth_method": "none"})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		callback, _ := url.Parse(q.Get("redirect_uri"))
		values := url.Values{"state": {q.Get("state")}}
		if q.Get("deny") == "" {
			values.Set("code", "web-code")
		} else {
			values.Set("error", "access_denied")
		}
		callback.RawQuery = values.Encode()
		http.Redirect(w, r, callback.String(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "web-access-secret", "refresh_token": "web-refresh-secret", "token_type": "Bearer", "expires_in": 3600, "scope": "read"})
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer web-access-secret" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+base+`/resource"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		protocol.ServeHTTP(w, r)
	})
	mux.HandleFunc("/plugins/oauth/callback", func(w http.ResponseWriter, r *http.Request) { m.BrowserOAuthCallback(w, r, true) })
	fixture := httptest.NewServer(mux)
	defer fixture.Close()
	base = fixture.URL
	m.SetWebOrigins(func(origin string) bool { return origin == base })
	// The browser follows the provider back to the daemon callback and stops
	// at the callback's redirect, where the web UI would load.
	browser := &http.Client{CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if req.URL.Path != "/plugins/oauth/callback" {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	visit := func(target string) *url.URL {
		t.Helper()
		response, err := browser.Get(target)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusSeeOther {
			t.Fatalf("callback answered %d, not a redirect to Plugins", response.StatusCode)
		}
		location, _ := url.Parse(response.Header.Get("Location"))
		return location
	}
	input := &Input{Integration: "mcp", Endpoint: base + "/mcp", TrustedNetworks: []string{"127.0.0.1/32"}, WebOrigin: base}

	for _, origin := range []string{"https://evil.example", "http://192.168.1.20:9876", base + "/plugins", base + "/"} {
		bad := *input
		bad.WebOrigin = origin
		if _, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &bad}); err == nil {
			t.Fatalf("connect_start accepted web origin %q", origin)
		}
	}
	if len(m.pending) != 0 || len(m.records) != 0 {
		t.Fatal("rejected origin left a pending flow")
	}

	start := mustHandle(t, m, Request{Action: "connect_start", Input: input}).Flow
	if !start.WebReturn || len(registered) != 1 || registered[0] != base+"/plugins/oauth/callback" {
		t.Fatalf("web flow registered %v, web_return %v", registered, start.WebReturn)
	}
	authorize, _ := url.Parse(start.AuthorizationURL)
	if authorize.Query().Get("redirect_uri") != base+"/plugins/oauth/callback" {
		t.Fatal("authorization does not return to the web origin")
	}
	location := visit(start.AuthorizationURL)
	status := mustHandle(t, m, Request{Action: "connect_status", FlowID: start.ID})
	if status.Flow.Status != "connected" || status.Account == nil || status.Account.Status != "connected" {
		t.Fatalf("web flow not connected: %+v", status.Flow)
	}
	if location.Path != "/plugins" || location.Query().Get("service") != "mcp" || location.Query().Get("connected") != status.Account.ID || location.Host != "" {
		t.Fatalf("callback returned to %v", location)
	}
	if finish := mustHandle(t, m, Request{Action: "connect_finish", FlowID: start.ID}); finish.Flow.Status != "connected" {
		t.Fatal("connect_finish did not report the finished web flow")
	}
	if strings.Contains(location.String(), "secret") || strings.Contains(location.String(), "web-code") {
		t.Fatal("return leaked a credential")
	}

	// A replay cannot be distinguished from another computer: preserve its address without exchanging it again.
	callback := base + "/plugins/oauth/callback?" + url.Values{"state": {authorize.Query().Get("state")}, "code": {"web-code"}}.Encode()
	replay, err := browser.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	replay.Body.Close()
	if replay.StatusCode != http.StatusBadRequest || replay.Header.Get("Location") != "" {
		t.Fatal("replay redirected or was accepted")
	}
	if mustHandle(t, m, Request{Action: "get", ID: status.Account.ID}).Account.Status != "connected" {
		t.Fatal("replay disturbed the connected account")
	}

	if a := status.Account.Access; a == nil || a.Tools != 1 || a.WriteConsent {
		t.Fatalf("web account access summary %+v", a)
	}

	// Declining consent lands on the list, naming the service, and leaves no account.
	declined := *input
	denied := mustHandle(t, m, Request{Action: "connect_start", Input: &declined}).Flow
	if back := visit(denied.AuthorizationURL + "&deny=1"); back.Path != "/plugins" || back.Query().Get("service") != "mcp" || back.Query().Get("plugin_error") != "denied" {
		t.Fatalf("denial returned to %v", back)
	}
	if after := mustHandle(t, m, Request{Action: "connect_status", FlowID: denied.ID}); after.Flow.Status != "failed" {
		t.Fatal("denied web flow not reported as failed")
	}
}

// A web UI callback never consumes a native flow; it only says Start again.
func TestWebCallbackLeavesNativeFlow(t *testing.T) {
	m, _ := New(t.TempDir())
	m.pending["native"] = &oauthFlow{ID: "x", Mobile: true, Expires: time.Now().Add(time.Minute)}
	w := httptest.NewRecorder()
	m.BrowserOAuthCallback(w, httptest.NewRequest("GET", "https://daemon/plugins/oauth/callback?state=native&code=x", nil), true)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/plugins?plugin_error=expired" || m.pending["native"] == nil {
		t.Fatalf("web callback answered %d %q for a native flow", w.Code, w.Header().Get("Location"))
	}
}

// Slack's publisher app registers only Mewla's https return page, never a
// server's own address. A web UI sign-in therefore goes out with the relay as
// its return address and the web origin in the state. (Before this a web
// sign-in was refused: "Slack only connects from the phone".)
//
// On this computer's own loopback address the page sends the browser on to
// the server's callback, which finishes the sign-in with no further step.
func TestWebConnectOnLoopbackReturnsThroughRelayToTheCallback(t *testing.T) {
	previous := SlackPublicClientID
	SlackPublicClientID = "public-slack"
	defer func() { SlackPublicClientID = previous }()
	m, _ := New(t.TempDir())
	const origin = "http://127.0.0.1:9877"
	m.SetWebOrigins(func(o string) bool { return o == origin })
	web := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "slack", WebOrigin: origin}}).Flow
	authorize, _ := url.Parse(web.AuthorizationURL)
	state := authorize.Query().Get("state")
	if authorize.Host != "slack.com" || authorize.Query().Get("redirect_uri") != NativeReturnRelay || web.AddressRequired || !web.WebReturn || !strings.HasPrefix(state, "web.") || len(m.pending) != 1 {
		t.Fatalf("web Slack sign-in %s, flow %+v", web.AuthorizationURL, web)
	}
	// Slack returns to the relay, which names this server's callback.
	relay := httptest.NewRecorder()
	pluginreturn.Handler(relay, httptest.NewRequest("GET", NativeReturnRelay+"?"+url.Values{"code": {"returned"}, "state": {state}}.Encode(), nil))
	back, _ := url.Parse(relay.Header().Get("Location"))
	if relay.Code != http.StatusFound || back.Scheme+"://"+back.Host+back.Path != origin+"/plugins/oauth/callback" {
		t.Fatalf("relay sent the browser to %s", relay.Header().Get("Location"))
	}
	exchanged := slackRejects(m, t)
	done := httptest.NewRecorder()
	m.BrowserOAuthCallback(done, httptest.NewRequest("GET", origin+back.RequestURI(), nil), true)
	if exchanged().Get("redirect_uri") != NativeReturnRelay || exchanged().Get("code") != "returned" || exchanged().Get("code_verifier") == "" || exchanged().Has("client_secret") {
		t.Fatalf("exchange %v", exchanged())
	}
	if location, _ := url.Parse(done.Header().Get("Location")); done.Code != http.StatusSeeOther || location.Path != "/plugins" || location.Query().Get("plugin_error") != "token" || len(m.pending) != 0 {
		t.Fatalf("callback answered %d %s", done.Code, done.Header().Get("Location"))
	}
	// The phone's own sign-in is unchanged: same return page, no web state.
	native := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "slack"}}).Flow
	if native.WebReturn || native.AddressRequired || strings.HasPrefix(mustQuery(t, native.AuthorizationURL).Get("state"), "web.") {
		t.Fatalf("the app's sign-in changed: %+v", native)
	}
}

// From any other web origin the relay never forwards the code: it shows the
// address on its own page and the person pastes it into their own Mewla.
func TestWebConnectElsewhereFinishesByPastingTheRelayAddress(t *testing.T) {
	previous := SlackPublicClientID
	SlackPublicClientID = "public-slack"
	defer func() { SlackPublicClientID = previous }()
	m, _ := New(t.TempDir())
	m.SetWebOrigins(func(o string) bool { return o == "https://mewla.example" })
	web := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "slack", WebOrigin: "https://mewla.example"}}).Flow
	authorize, _ := url.Parse(web.AuthorizationURL)
	state := authorize.Query().Get("state")
	if authorize.Query().Get("redirect_uri") != NativeReturnRelay || !web.AddressRequired || web.WebReturn || !strings.HasPrefix(state, "web.") {
		t.Fatalf("web Slack sign-in %s, flow %+v", web.AuthorizationURL, web)
	}
	relay := httptest.NewRecorder()
	pluginreturn.Handler(relay, httptest.NewRequest("GET", NativeReturnRelay+"?"+url.Values{"code": {"pasted"}, "state": {state}}.Encode(), nil))
	if relay.Code != http.StatusOK || relay.Header().Get("Location") != "" || !strings.Contains(relay.Body.String(), "Only paste this into your own Mewla.") {
		t.Fatalf("relay answered %d %q", relay.Code, relay.Header().Get("Location"))
	}
	// Pasting an address that isn't this sign-in's return is refused.
	for _, pasted := range []string{
		"https://evil.example/plugins/return?code=c&state=" + state,
		NativeReturnRelay + "?code=c&state=other",
		NativeCallback + "?code=c&state=" + state,
	} {
		if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: web.ID, Callback: pasted}); err == nil {
			t.Fatalf("accepted pasted address %q", pasted)
		}
	}
	exchanged := slackRejects(m, t)
	finish := mustHandle(t, m, Request{Action: "connect_finish", FlowID: web.ID, Callback: NativeReturnRelay + "?" + url.Values{"code": {"pasted"}, "state": {state}}.Encode()}).Flow
	if exchanged().Get("redirect_uri") != NativeReturnRelay || exchanged().Get("code") != "pasted" || exchanged().Get("code_verifier") == "" || exchanged().Has("client_secret") || finish.Status != "failed" {
		t.Fatalf("exchange %v, flow %+v", exchanged(), finish)
	}
}

// slackRejects answers Slack's token endpoint with a rejection and returns
// what the daemon sent it.
func slackRejects(m *Manager, t *testing.T) func() url.Values {
	exchanged := url.Values{}
	m.networkOwned = false
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method+" "+req.URL.String() != "POST https://slack.com/api/oauth.v2.access" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		}
		_ = req.ParseForm()
		exchanged = req.PostForm
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error":"invalid_code"}`))}, nil
	})}
	return func() url.Values { return exchanged }
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

// A registered app that returns only to the Mewla app scheme has no page to
// show the address on, so a web sign-in says so before the browser leaves.
func TestWebConnectRefusesAppSchemeOnlyReturn(t *testing.T) {
	previous := SlackPublicClientID
	SlackPublicClientID = ""
	defer func() { SlackPublicClientID = previous }()
	m, _ := New(t.TempDir())
	raw, _ := json.Marshal(OAuthClientConfig{ClientID: "owned", RedirectURL: NativeCallback})
	if err := m.vault.Set("oauth-client:slack", string(raw)); err != nil {
		t.Fatal(err)
	}
	m.SetWebOrigins(func(origin string) bool { return origin == "https://mewla.example" })
	_, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: "slack", WebOrigin: "https://mewla.example"}})
	if err == nil || !strings.Contains(err.Error(), "from the Mewla app") || len(m.pending) != 0 {
		t.Fatalf("web connect with an app-scheme-only client: %v", err)
	}
}

func TestAppConnectReturnsThroughRelay(t *testing.T) {
	previous := SlackPublicClientID
	SlackPublicClientID = "public-slack"
	defer func() { SlackPublicClientID = previous }()
	m, _ := New(t.TempDir())
	m.SetWebOrigins(func(origin string) bool { return origin == "https://mewla.example" })
	native := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "slack"}}).Flow
	authorize, _ := url.Parse(native.AuthorizationURL)
	q := authorize.Query()
	state := q.Get("state")
	if authorize.Host != "slack.com" || q.Get("redirect_uri") != NativeReturnRelay || q.Get("client_id") != "public-slack" || q.Get("code_challenge_method") != "S256" || !m.pending[state].Mobile || m.pending[state].Web {
		t.Fatalf("the app's Slack sign-in %s", native.AuthorizationURL)
	}
	// The relay forwards Slack's code and state to mewla://plugins; the
	// exchange repeats the https redirect with PKCE and no secret.
	exchanged := url.Values{}
	m.networkOwned = false
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method+" "+req.URL.String() != "POST https://slack.com/api/oauth.v2.access" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		}
		_ = req.ParseForm()
		exchanged = req.PostForm
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error":"invalid_code"}`))}, nil
	})}
	finish := mustHandle(t, m, Request{Action: "connect_finish", FlowID: native.ID, Callback: NativeCallback + "?" + url.Values{"code": {"relayed"}, "state": {state}}.Encode()}).Flow
	if exchanged.Get("redirect_uri") != NativeReturnRelay || exchanged.Get("code") != "relayed" || exchanged.Get("code_verifier") == "" || exchanged.Has("client_secret") || finish.Status != "failed" {
		t.Fatalf("exchange %v, flow %+v", exchanged, finish)
	}
}

// A failed web sign-in returns naming the step that failed, so the next
// failure says what broke instead of a generic "couldn't be verified".
func TestWebConnectNamesFailedStep(t *testing.T) {
	cases := []struct {
		name, want, query, iss string
		tokenStatus            int
		schemaBytes            int
	}{
		{name: "provider error", want: "provider", query: "&vendor_error=server_error"},
		{name: "issuer mismatch", want: "issuer", iss: "https://other.example"},
		{name: "token rejected", want: "token", tokenStatus: http.StatusBadRequest},
		{name: "oversized tool schema", want: "tools", schemaBytes: MaxToolSchemaBytes + 1},
		// Notion's notion-query-data-sources schema is 76 KiB; it connects.
		{name: "notion-sized tool schema", want: "", schemaBytes: 76 << 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			server := mcp.NewServer(&mcp.Implementation{Name: "Failing fixture", Version: "1"}, nil)
			schema := map[string]any{}
			if c.schemaBytes > 0 {
				schema["note"] = map[string]any{"type": "string", "description": strings.Repeat("x", c.schemaBytes)}
			}
			server.AddTool(&mcp.Tool{Name: "read_fixture", InputSchema: object(schema)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			})
			protocol := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
			var base string
			mux := http.NewServeMux()
			mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base}})
			})
			mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(oauthMetadata{Issuer: base, AuthorizationEndpoint: base + "/authorize", TokenEndpoint: base + "/token", RegistrationEndpoint: base + "/register", CodeChallengeMethods: []string{"S256"}})
			})
			mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "web-client", "token_endpoint_auth_method": "none"})
			})
			mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				callback, _ := url.Parse(q.Get("redirect_uri"))
				values := url.Values{"state": {q.Get("state")}, "code": {"web-code"}}
				if e := q.Get("vendor_error"); e != "" {
					values = url.Values{"state": {q.Get("state")}, "error": {e}}
				}
				if c.iss != "" {
					values.Set("iss", c.iss)
				}
				callback.RawQuery = values.Encode()
				http.Redirect(w, r, callback.String(), http.StatusFound)
			})
			mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if c.tokenStatus != 0 {
					w.WriteHeader(c.tokenStatus)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "web-code-secret-detail"})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "web-access-secret", "token_type": "Bearer", "expires_in": 3600})
			})
			mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer web-access-secret" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				protocol.ServeHTTP(w, r)
			})
			mux.HandleFunc("/plugins/oauth/callback", func(w http.ResponseWriter, r *http.Request) { m.BrowserOAuthCallback(w, r, true) })
			fixture := httptest.NewServer(mux)
			defer fixture.Close()
			base = fixture.URL
			m.SetWebOrigins(func(origin string) bool { return origin == base })
			var logged bytes.Buffer
			log.SetOutput(&logged)
			defer log.SetOutput(os.Stderr)

			start := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "mcp", Endpoint: base + "/mcp", TrustedNetworks: []string{"127.0.0.1/32"}, WebOrigin: base}}).Flow
			browser := &http.Client{CheckRedirect: func(req *http.Request, _ []*http.Request) error {
				if req.URL.Path != "/plugins/oauth/callback" {
					return http.ErrUseLastResponse
				}
				return nil
			}}
			response, err := browser.Get(start.AuthorizationURL + c.query)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			back, _ := url.Parse(response.Header.Get("Location"))
			if back.Path != "/plugins" || back.Query().Get("plugin_error") != c.want || back.Query().Get("service") != "mcp" {
				t.Fatalf("returned to %v, want plugin_error=%s", back, c.want)
			}
			if c.want == "" {
				account := m.records[back.Query().Get("connected")]
				if account == nil || account.Account.Status != "connected" || len(account.Account.Tools) != 1 || len(account.Account.Tools[0].InputSchema) < 76<<10 {
					t.Fatal("a large tool schema did not connect intact")
				}
				return
			}
			if len(m.records) != 0 {
				t.Fatal("failed sign-in left an account")
			}
			line := logged.String()
			if !strings.Contains(line, "sign-in failed at "+c.want) || strings.Contains(line, "secret") {
				t.Fatalf("log line %q", line)
			}
			if c.want == "token" && !strings.Contains(line, "HTTP 400 invalid_grant") {
				t.Fatalf("token failure log %q does not name the vendor code", line)
			}
			if c.want == "tools" && !strings.Contains(line, `"read_fixture" schema is 256 KB, over the 256 KB limit`) {
				t.Fatalf("tools failure log %q does not name the tool", line)
			}
		})
	}
}
