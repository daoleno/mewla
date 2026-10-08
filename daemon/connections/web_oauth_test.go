package connections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

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

	// A refresh replays the consumed state: friendly redirect, account kept.
	callback := base + "/plugins/oauth/callback?" + url.Values{"state": {authorize.Query().Get("state")}, "code": {"web-code"}}.Encode()
	if replay := visit(callback); replay.Path != "/plugins" || replay.Query().Get("plugin_error") != "expired" {
		t.Fatalf("replay returned to %v", replay)
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
