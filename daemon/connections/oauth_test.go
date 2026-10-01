package connections

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// This is a real loopback HTTP/MCP/OAuth chain using New and its production
// checked dialer. No transport replacement bypasses the internal-address policy.
func TestTrustedLocalMCPAuthorizationRefreshRestartRevoke(t *testing.T) {
	root := t.TempDir()
	m, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	var refreshes, revokes, registrations, calls, rejectedResources atomic.Int32
	var challenge string
	server := mcp.NewServer(&mcp.Implementation{Name: "Owned local adapter", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "read_local", Description: "Read the owned local fixture", InputSchema: object(map[string]any{})}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "local adapter reached"}}}, nil
	})
	protocol := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("/resource", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(oauthMetadata{Issuer: base, AuthorizationEndpoint: base + "/authorize", TokenEndpoint: base + "/token", RegistrationEndpoint: base + "/register", RevocationEndpoint: base + "/revoke", CodeChallengeMethods: []string{"S256"}, AuthMethods: []string{"none"}, ScopesSupported: []string{"read", "write"}})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		registrations.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "local-client", "token_endpoint_auth_method": "none"})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		challenge = q.Get("code_challenge")
		if q.Get("code_challenge_method") != "S256" || q.Get("resource") != base+"/mcp" || q.Get("scope") != "read" {
			t.Error("missing PKCE, resource binding or least-privilege scope")
		}
		callback, _ := url.Parse(q.Get("redirect_uri"))
		callback.RawQuery = url.Values{"code": {"fixture-code"}, "state": {q.Get("state")}, "iss": {base}}.Encode()
		http.Redirect(w, r, callback.String(), 302)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if resources := r.PostForm["resource"]; len(resources) != 1 || resources[0] != base+"/mcp" {
			rejectedResources.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"invalid_target"}`)
			return
		}
		access := "local-access-secret"
		if r.Form.Get("grant_type") == "authorization_code" {
			if oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != challenge || r.Form.Get("resource") != base+"/mcp" {
				t.Error("exchange lost PKCE or resource")
			}
		} else if r.Form.Get("grant_type") == "refresh_token" {
			if r.Form.Get("refresh_token") != "local-refresh-secret" {
				t.Error("wrong account refresh")
			}
			refreshes.Add(1)
			access = "local-refreshed-access-secret"
		} else {
			t.Error("unexpected grant")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "refresh_token": "local-refresh-secret", "token_type": "Bearer", "expires_in": 3600, "scope": "read"})
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("token") != "local-refresh-secret" {
			t.Error("wrong revoke token")
		}
		revokes.Add(1)
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		access := "local-access-secret"
		if refreshes.Load() > 0 {
			access = "local-refreshed-access-secret"
		}
		if r.Header.Get("Authorization") != "Bearer "+access {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+base+`/resource"`)
			w.WriteHeader(401)
			return
		}
		protocol.ServeHTTP(w, r)
	})
	mux.HandleFunc("/plugins/oauth/callback", func(w http.ResponseWriter, r *http.Request) { m.OAuthCallback(w, r) })
	fixture := httptest.NewServer(mux)
	defer fixture.Close()
	base = fixture.URL
	for _, resource := range []string{"", base + "/another-account"} {
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"local-refresh-secret"}}
		if resource != "" {
			form.Set("resource", resource)
		}
		response, err := http.PostForm(base+"/token", form)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatal("strict provider accepted missing or wrong resource")
		}
	}
	in := &Input{Integration: "mcp", Name: "Local OAuth", Endpoint: base + "/mcp"}
	mustHandle(t, m, Request{Action: "oauth_configure", Input: &Input{Integration: "mcp", OAuthClient: &OAuthClientConfig{RedirectURL: base + "/plugins/oauth/callback"}}})
	if _, err := m.Handle(context.Background(), Request{Action: "oauth_start", Input: in}); err == nil {
		t.Fatal("untrusted loopback OAuth accepted")
	}
	in.TrustedNetworks = []string{"127.0.0.1/32"}
	begin := mustHandle(t, m, Request{Action: "oauth_start", Input: in})
	if begin.Account.Status != "authorization_required" {
		t.Fatal("pending OAuth falsely connected")
	}
	bad, err := http.Get(base + "/plugins/oauth/callback?state=wrong&code=wrong")
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != 400 {
		t.Fatal("bad state accepted")
	}
	browser, err := http.Get(begin.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Body.Close()
	page, _ := io.ReadAll(browser.Body)
	if browser.StatusCode != 200 || strings.Contains(string(page), "secret") {
		t.Fatal("authorization failed or browser leaked token")
	}
	replay, err := http.Get(browser.Request.URL.String())
	if err != nil {
		t.Fatal(err)
	}
	replay.Body.Close()
	if replay.StatusCode != 400 {
		t.Fatal("OAuth callback replay accepted")
	}
	id := begin.Account.ID
	account := mustHandle(t, m, Request{Action: "get", ID: id}).Account
	if account.Status != "connected" || len(account.Tools) != 1 || account.Tools[0].Allowed {
		t.Fatal("MCP OAuth discovery/policy failed")
	}
	mustHandle(t, m, Request{Action: "policy", ID: id, Tool: "read_local", Allowed: true})
	q := Request{Action: "invoke", ID: id, Tool: "read_local"}
	if !strings.Contains(string(mustHandle(t, m, q).Result), "local adapter reached") {
		t.Fatal("real local adapter was not reached")
	}
	raw, _, _ := m.vault.Get("integration:" + id)
	var secret oauthSecret
	_ = json.Unmarshal([]byte(raw), &secret)
	secret.Token.Expiry = time.Now().Add(-time.Minute)
	encoded, _ := json.Marshal(secret)
	_ = m.vault.Set("integration:"+id, string(encoded))
	m, err = New(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mustHandle(t, m, q).Result), "local adapter reached") || calls.Load() != 2 {
		t.Fatal("post-restart invocation did not reach the adapter with the refreshed token")
	}
	if refreshes.Load() != 1 || registrations.Load() != 1 || rejectedResources.Load() != 2 {
		t.Fatal("restart did not preserve the registered client, refresh token and exact resource")
	}
	mustHandle(t, m, Request{Action: "disable", ID: id})
	if _, err := m.Handle(context.Background(), q); err == nil {
		t.Fatal("disabled OAuth account executed")
	}
	mustHandle(t, m, Request{Action: "disconnect", ID: id})
	if revokes.Load() != 1 {
		t.Fatal("remote token not revoked")
	}
	if _, ok, _ := m.vault.Get("integration:" + id); ok {
		t.Fatal("OAuth credential retained")
	}
	metadata, _ := os.ReadFile(m.path)
	if strings.Contains(string(metadata), "local-access-secret") || strings.Contains(string(metadata), "local-refresh-secret") {
		t.Fatal("OAuth secret leaked to metadata")
	}
}

func TestTrustedEndpointScopeAndRealOpenAPIDispatch(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/items/42" || r.Header.Get("Authorization") != "Bearer local-key" {
			t.Error("wrong local route/account")
		}
		io.WriteString(w, `{"local":true}`)
	}))
	defer target.Close()
	in := &Input{Integration: "openapi", Name: "local", Endpoint: target.URL, Credential: "local-key", Spec: json.RawMessage(fixtureSpec)}
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: in}); err == nil {
		t.Fatal("plaintext untrusted endpoint accepted")
	}
	in.TrustedNetworks = []string{"127.0.0.1/32"}
	a := mustHandle(t, m, Request{Action: "add", Input: in}).Account
	mustHandle(t, m, Request{Action: "policy", ID: a.ID, Tool: "read_item", Allowed: true})
	mustHandle(t, m, Request{Action: "invoke", ID: a.ID, Tool: "read_item", Arguments: json.RawMessage(`{"path":{"id":"42"}}`)})
	if calls.Load() != 1 {
		t.Fatal("production dialer failed real local dispatch")
	}
	for _, cidr := range []string{"0.0.0.0/0", "169.254.0.0/16", "100.0.0.0/8"} {
		if _, err := trustedEndpointURL(target.URL, []string{cidr}); err == nil {
			t.Fatal("overbroad trust accepted")
		}
	}
	// A grant belongs to the chosen origin only. A second endpoint never inherits it.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("cross-origin credential request reached target")
	}))
	defer other.Close()
	request, _ := http.NewRequest("GET", other.URL, nil)
	if _, err := m.clientFor(m.records[a.ID]).Do(request); err == nil {
		t.Fatal("internal trust escaped selected origin")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 302) }))
	defer redirect.Close()
	r := &record{Account: Account{Endpoint: redirect.URL, TrustedNetworks: []string{"127.0.0.1/32"}}}
	request, _ = http.NewRequest("GET", redirect.URL, nil)
	if _, err := m.clientFor(r).Do(request); err == nil {
		t.Fatal("redirect followed")
	}
}

func TestGoogleOAuthScopeDispatchAndRefreshFailure(t *testing.T) {
	var scopes string
	var refreshDenied bool
	var revokeDenied bool
	var calls []string
	m := testManager(t, func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Host+r.URL.Path)
		switch r.URL.Host {
		case "oauth2.googleapis.com":
			if r.URL.Path == "/revoke" {
				if revokeDenied {
					return reply(500, `{}`), nil
				}
				return reply(200, `{}`), nil
			}
			_ = r.ParseForm()
			if r.Form.Get("client_secret") != "google-client-secret" {
				t.Error("client secret not host-owned")
			}
			if r.Form.Get("grant_type") == "refresh_token" && refreshDenied {
				return reply(400, `{"error":"invalid_grant","error_description":"google-refresh-secret"}`), nil
			}
			response := reply(200, `{"access_token":"google-access-secret","refresh_token":"google-refresh-secret","token_type":"Bearer","expires_in":3600,"scope":"`+scopes+`"}`)
			response.Header.Set("Content-Type", "application/json")
			return response, nil
		case "openidconnect.googleapis.com":
			return reply(200, `{"sub":"123","email":"fixture@example.com"}`), nil
		case "www.googleapis.com", "gmail.googleapis.com":
			if r.Header.Get("Authorization") != "Bearer google-access-secret" {
				t.Error("wrong Google bearer")
			}
			return reply(200, `{"items":[]}`), nil
		}
		t.Error("unexpected endpoint")
		return reply(404, `{}`), nil
	})
	mustHandle(t, m, Request{Action: "oauth_configure", Input: &Input{Integration: "google", OAuthClient: &OAuthClientConfig{ClientID: "test-client", ClientSecret: "google-client-secret", RedirectURL: "https://zen.example.com/plugins/oauth/callback"}}})
	begin := mustHandle(t, m, Request{Action: "oauth_start", Input: &Input{Integration: "google", Name: "Google"}})
	authURL, _ := url.Parse(begin.AuthorizationURL)
	scopes = authURL.Query().Get("scope")
	if authURL.Host != "accounts.google.com" || authURL.Query().Get("code_challenge") == "" || authURL.Query().Get("access_type") != "offline" {
		t.Fatal("Google browser flow not configured")
	}
	request := httptest.NewRequest("GET", "https://zen.example.com/plugins/oauth/callback?"+url.Values{"code": {"fixture-code"}, "state": {authURL.Query().Get("state")}}.Encode(), nil)
	response := httptest.NewRecorder()
	m.OAuthCallback(response, request)
	if response.Code != 200 {
		t.Fatal("Google callback failed", response.Body.String())
	}
	account := mustHandle(t, m, Request{Action: "get", ID: begin.Account.ID}).Account
	if account.Identity != "fixture@example.com" {
		t.Fatal("Google identity not verified")
	}
	for _, tool := range account.Tools {
		if !tool.Allowed {
			t.Fatal("read-only Google scope advertised a write tool")
		}
	}
	for _, name := range []string{"drive_list_files", "gmail_list_messages", "calendar_list_calendars"} {
		mustHandle(t, m, Request{Action: "invoke", ID: account.ID, Tool: name})
	}
	if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: account.ID, Tool: "gmail_send_message", Arguments: json.RawMessage(`{"body":{"raw":"YWJj"}}`)}); err == nil {
		t.Fatal("missing Gmail send scope accepted")
	}
	raw, _, _ := m.vault.Get("integration:" + account.ID)
	var secret oauthSecret
	_ = json.Unmarshal([]byte(raw), &secret)
	secret.Token.Expiry = time.Now().Add(-time.Hour)
	encoded, _ := json.Marshal(secret)
	_ = m.vault.Set("integration:"+account.ID, string(encoded))
	refreshDenied = true
	if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: account.ID, Tool: "get_me"}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("refresh failure lost safe authorization boundary")
	}
	if mustHandle(t, m, Request{Action: "get", ID: account.ID}).Account.Status != "authorization_required" {
		t.Fatal("refresh failure not visible")
	}
	// Requesting write scopes does not grant write tools.
	begin = mustHandle(t, m, Request{Action: "oauth_start", Input: &Input{Integration: "google", Name: "Google writes", AllowWrites: true}})
	authURL, _ = url.Parse(begin.AuthorizationURL)
	scopes = authURL.Query().Get("scope")
	request = httptest.NewRequest("GET", "https://zen.example.com/plugins/oauth/callback?"+url.Values{"code": {"fixture-code"}, "state": {authURL.Query().Get("state")}}.Encode(), nil)
	response = httptest.NewRecorder()
	m.OAuthCallback(response, request)
	if response.Code != 200 {
		t.Fatal("write-scoped callback failed")
	}
	account = mustHandle(t, m, Request{Action: "get", ID: begin.Account.ID}).Account
	found := false
	for _, tool := range account.Tools {
		if tool.Name == "gmail_send_message" {
			found = true
			if tool.Allowed {
				t.Fatal("OAuth scope implicitly granted write")
			}
		}
	}
	if !found {
		t.Fatal("Google send tool missing")
	}
	before := len(calls)
	if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: account.ID, Tool: "gmail_send_message", Arguments: json.RawMessage(`{"body":{"raw":"YWJj"}}`)}); err == nil || len(calls) != before {
		t.Fatal("ungranted write dispatched")
	}
	revokeDenied = true
	if _, err := m.Handle(context.Background(), Request{Action: "disconnect", ID: account.ID}); err == nil {
		t.Fatal("remote revoke failure hidden")
	}
	pending := mustHandle(t, m, Request{Action: "get", ID: account.ID}).Account
	if pending.Enabled || pending.Status != "disconnected" || !pending.CredentialRemovalPending {
		t.Fatal("revoke failure did not preserve disabled retry state")
	}
	revokeDenied = false
	mustHandle(t, m, Request{Action: "disconnect", ID: account.ID})
	if mustHandle(t, m, Request{Action: "get", ID: account.ID}).Account.CredentialRemovalPending {
		t.Fatal("successful revoke still requests retry")
	}
	if calls[len(calls)-1] != "POST oauth2.googleapis.com/revoke" {
		t.Fatal("Google revocation not attempted")
	}
}

func TestRegisteredMCPClientBoundToResource(t *testing.T) {
	calls := 0
	m := testManager(t, func(r *http.Request) (*http.Response, error) { calls++; return reply(500, `{}`), nil })
	client := &OAuthClientConfig{ClientID: "registered", ClientSecret: "fixture-client-secret", RedirectURL: "https://zen.example/plugins/oauth/callback"}
	if _, err := m.Handle(context.Background(), Request{Action: "oauth_configure", Input: &Input{Integration: "mcp", OAuthClient: client}}); err == nil {
		t.Fatal("unbound private MCP client accepted")
	}
	client.ResourceURL = "https://trusted.example/mcp"
	mustHandle(t, m, Request{Action: "oauth_configure", Input: &Input{Integration: "mcp", OAuthClient: client}})
	if _, err := m.Handle(context.Background(), Request{Action: "oauth_start", Input: &Input{Integration: "mcp", Name: "other", Endpoint: "https://other.example/mcp"}}); err == nil {
		t.Fatal("registered client crossed resources")
	}
	if calls != 0 {
		t.Fatal("wrong resource reached network with registered credentials")
	}
}
