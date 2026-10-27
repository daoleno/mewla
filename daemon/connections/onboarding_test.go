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
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestNativeReturnBoundToFlowAndServer(t *testing.T) {
	m, _ := New(t.TempDir())
	other, _ := New(t.TempDir())
	r := &record{Account: Account{ID: "pending", Integration: "github", Name: "GitHub", Enabled: true, Status: "authorization_required"}, Grants: map[string]string{}}
	m.records[r.Account.ID] = r
	m.pending["state-a"] = &oauthFlow{ID: r.Account.ID, Mobile: true, Expires: time.Now().Add(time.Minute)}
	f := &connectFlow{ConnectFlow: ConnectFlow{ID: "flow-a", Status: "waiting", AccountID: r.Account.ID, Expires: time.Now().Add(time.Minute)}, state: "state-a"}
	m.connectFlows[f.ID] = f
	if len(mustHandle(t, m, Request{Action: "list"}).Accounts) != 0 {
		t.Fatal("pending account leaked into list")
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(m.path)
	if strings.Contains(string(data), "pending") {
		t.Fatal("unfinished account persisted")
	}
	for _, callback := range []string{"https://evil.example/?state=state-a&code=x", "mewla://plugins?state=wrong&code=x", "mewla://plugins/extra?state=state-a&code=x"} {
		if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: f.ID, Callback: callback}); err == nil {
			t.Fatal("unbound callback accepted")
		}
	}
	if _, err := other.Handle(context.Background(), Request{Action: "connect_finish", FlowID: f.ID, Callback: "mewla://plugins?state=state-a&code=x"}); err == nil {
		t.Fatal("another server accepted callback")
	}
	w := httptest.NewRecorder()
	m.OAuthCallback(w, httptest.NewRequest("GET", "https://daemon/plugins/oauth/callback?state=state-a&code=x", nil))
	if w.Code != 400 || m.pending["state-a"] == nil {
		t.Fatal("public callback consumed native flow")
	}
	denied := mustHandle(t, m, Request{Action: "connect_finish", FlowID: f.ID, Callback: "mewla://plugins?state=state-a&error=access_denied"})
	if denied.Flow.Status != "failed" || len(m.records) != 0 || len(m.pending) != 0 {
		t.Fatal("denial left a pending account")
	}
	if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: f.ID, Callback: "mewla://plugins?state=state-a&code=x"}); err == nil {
		t.Fatal("denial replay accepted")
	}
}
func TestCancelExpiryAndNoOrphanAccounts(t *testing.T) {
	m, _ := New(t.TempDir())
	for _, expired := range []bool{false, true} {
		expiry := time.Now().Add(time.Minute)
		if expired {
			expiry = time.Now().Add(-time.Second)
		}
		m.records["pending"] = &record{Account: Account{ID: "pending"}}
		m.pending["state"] = &oauthFlow{ID: "pending", Expires: expiry}
		m.connectFlows["flow"] = &connectFlow{ConnectFlow: ConnectFlow{ID: "flow", Status: "waiting", Expires: expiry}, state: "state"}
		_, err := m.Handle(context.Background(), Request{Action: "connect_cancel", FlowID: "flow"})
		if !expired && err != nil {
			t.Fatal(err)
		}
		if len(m.records) != 0 || len(m.pending) != 0 {
			t.Fatal("cancel/expiry retained unfinished accounts")
		}
	}
}
func TestReviewedGroupsRejectCustomAnnotationsAndChangedSchemas(t *testing.T) {
	m, _ := New(t.TempDir())
	tool := Tool{Name: "notion-fetch", InputSchema: json.RawMessage(`{"type":"object"}`)}
	r := &record{Account: Account{Integration: "notion", AuthMethod: "mcp_oauth", Endpoint: "https://mcp.notion.com/mcp", Tools: []Tool{tool, {Name: "arbitrary_delete", Read: true}}}, Grants: map[string]string{}}
	if !m.setGroup(r, "read", true) || r.Grants[tool.Name] == "" || r.Grants["arbitrary_delete"] != "" {
		t.Fatal("reviewed groups are incorrect")
	}
	r.Account.Tools[0].InputSchema = json.RawMessage(`{"type":"object","properties":{"danger":{"type":"string"}}}`)
	if m.projection(r).Tools[0].Allowed {
		t.Fatal("schema change retained permission")
	}
	r.Account.Integration = "mcp"
	r.Grants = map[string]string{}
	if m.setGroup(r, "read", true) || len(r.Grants) != 0 {
		t.Fatal("custom MCP inherited built-in permission")
	}
}
func TestGitHubPreviewRequiresConsentAndDeduplicates(t *testing.T) {
	m, _ := New(t.TempDir())
	m.networkOwned = false
	m.githubToken = func(context.Context) (string, error) { return "owned-test-token", nil }
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.github.com" || req.URL.Path != "/user" || req.Header.Get("Authorization") != "Bearer owned-test-token" {
			t.Fatalf("unexpected request %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"login":"test-user","id":42}`))}, nil
	})}
	preview := mustHandle(t, m, Request{Action: "github_preview"})
	if preview.Flow.Identity != "test-user" || len(m.records) != 0 {
		t.Fatal("preview imported an account")
	}
	q := Request{Action: "github_import", FlowID: preview.Flow.ID}
	first := mustHandle(t, m, q)
	again := mustHandle(t, m, q)
	if first.Account.ID != again.Account.ID || len(m.records) != 1 {
		t.Fatal("duplicate tap created accounts")
	}
	mustHandle(t, m, Request{Action: "disable", ID: first.Account.ID})
	preview = mustHandle(t, m, Request{Action: "github_preview"})
	second := mustHandle(t, m, Request{Action: "github_import", FlowID: preview.Flow.ID})
	if second.Account.ID != first.Account.ID || !second.Account.Enabled || len(m.records) != 1 {
		t.Fatal("duplicate identity imported")
	}
	mustHandle(t, m, Request{Action: "disconnect", ID: first.Account.ID})
	if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: first.Account.ID, Tool: "get_me"}); err == nil {
		t.Fatal("disconnected import executed")
	}
}
func TestSlackOfficialUserTokenExchange(t *testing.T) {
	m, _ := New(t.TempDir())
	m.networkOwned = false
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		_ = req.ParseForm()
		if req.PostForm.Get("client_id") != "mewla-owned" || req.PostForm.Get("redirect_uri") != "https://owned.example/plugins/oauth/callback" || req.PostForm.Get("client_secret") != "publisher-secret" {
			t.Fatal("lost registered client or redirect binding")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"access_token":"bot-token-not-selected","authed_user":{"access_token":"user-token","scope":"channels:read,search:read"}}`))}, nil
	})}
	r := &record{Account: Account{Endpoint: "https://slack.com/api"}, OAuth: &oauthAccount{ClientID: "mewla-owned", TokenURL: "https://slack.com/api/oauth.v2.access", RedirectURL: "https://owned.example/plugins/oauth/callback"}}
	token, err := slackCodec.exchange(m, context.Background(), r, "publisher-secret", "code", "verifier")
	if err != nil || token.AccessToken != "user-token" || token.Extra("scope") != "channels:read search:read" {
		t.Fatal("Slack user authorization parsed incorrectly", err)
	}
}
func TestGitHubDevicePollingRespectsSlowDown(t *testing.T) {
	m, _ := New(t.TempDir())
	m.networkOwned = false
	calls := 0
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":"slow_down"}`))}, nil
	})}
	f := &connectFlow{ConnectFlow: ConnectFlow{Integration: "github"}, interval: 5 * time.Second, nextPoll: time.Now().Add(time.Minute)}
	if err := m.pollDevice(context.Background(), f); err != nil || calls != 0 {
		t.Fatal("polled before provider interval")
	}
	f.nextPoll = time.Now().Add(-time.Second)
	_ = m.pollDevice(context.Background(), f)
	if calls != 1 || f.interval != 10*time.Second {
		t.Fatal("slow_down ignored")
	}
}
func TestNativePKCEExchangeAndIdempotentFinish(t *testing.T) {
	m, _ := New(t.TempDir())
	m.networkOwned = false
	verifier := oauth2.GenerateVerifier()
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"login":"native-user","id":7}`
		if req.URL.Path == "/token" {
			_ = req.ParseForm()
			if req.PostForm.Get("code_verifier") != verifier || req.PostForm.Get("redirect_uri") != NativeCallback {
				t.Fatal("PKCE or return binding lost")
			}
			body = `{"access_token":"native-token","token_type":"Bearer"}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	r := &record{Account: Account{ID: "new", Integration: "github", Name: "GitHub", Enabled: true, Endpoint: "https://api.github.com", Status: "authorization_required"}, OAuth: &oauthAccount{ClientID: "owned", RedirectURL: NativeCallback, TokenURL: "https://auth.example/token", AuthStyle: oauth2.AuthStyleInParams}, Grants: map[string]string{}}
	m.records["new"] = r
	m.pending["state"] = &oauthFlow{ID: "new", Verifier: verifier, Mobile: true, Expires: time.Now().Add(time.Minute)}
	m.connectFlows["flow"] = &connectFlow{ConnectFlow: ConnectFlow{ID: "flow", Status: "waiting", AccountID: "new", Expires: time.Now().Add(time.Minute)}, state: "state"}
	q := Request{Action: "connect_finish", FlowID: "flow", Callback: NativeCallback + "?" + url.Values{"code": {"code"}, "state": {"state"}}.Encode()}
	for i := 0; i < 2; i++ {
		result := mustHandle(t, m, q)
		if result.Account == nil || result.Account.Name != "native-user" || !result.Account.Tools[0].Allowed {
			t.Fatal("native completion was not usable/idempotent")
		}
	}
}

func TestSlackPublicPKCEAndRotatingRefresh(t *testing.T) {
	m, _ := New(t.TempDir())
	m.networkOwned = false
	step := 0
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		_ = req.ParseForm()
		if req.PostForm.Get("client_secret") != "" {
			t.Fatal("public Slack flow sent a secret")
		}
		body := `{"ok":true,"authed_user":{"access_token":"access-one","refresh_token":"refresh-one","expires_in":3600,"scope":"channels:read"}}`
		if step == 0 {
			if req.PostForm.Get("code_verifier") != "original-verifier" {
				t.Fatal("missing PKCE")
			}
		} else {
			if req.PostForm.Get("grant_type") != "refresh_token" || req.PostForm.Get("refresh_token") != "refresh-one" {
				t.Fatal("refresh not bound to saved token")
			}
			body = `{"ok":true,"access_token":"access-two","refresh_token":"refresh-two","expires_in":3600,"scope":"channels:read"}`
		}
		step++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	r := &record{Account: Account{ID: "slack", Integration: "slack", Endpoint: "https://slack.com/api"}, OAuth: &oauthAccount{ClientID: "owned-public", TokenURL: "https://slack.com/api/oauth.v2.access", RedirectURL: NativeCallback}}
	token, err := slackCodec.exchange(m, context.Background(), r, "", "code", "original-verifier")
	if err != nil {
		t.Fatal(err)
	}
	token.Expiry = time.Now().Add(-time.Minute)
	raw, _ := json.Marshal(oauthSecret{Token: token})
	_ = m.vault.Set("integration:slack", string(raw))
	access, err := m.accountToken(context.Background(), r)
	if err != nil || access != "access-two" {
		t.Fatal("refresh failed", err)
	}
	rawSecret, _, _ := m.vault.Get("integration:slack")
	var saved oauthSecret
	_ = json.Unmarshal([]byte(rawSecret), &saved)
	if saved.Token.RefreshToken != "refresh-two" {
		t.Fatal("rotated refresh token was not saved")
	}
}

func TestPermissionGroupsSurviveRestart(t *testing.T) {
	m, _ := New(t.TempDir())
	r := &record{Account: Account{ID: "github", Integration: "github", Tools: lookup("github").reviewed()}, Grants: map[string]string{}}
	m.records[r.Account.ID] = r
	_ = m.save()
	reloaded, err := New(strings.TrimSuffix(m.path, "/integrations.json"))
	if err != nil {
		t.Fatal(err)
	}
	r = reloaded.records["github"]
	reloaded.setGroup(r, "read", true)
	for _, tool := range reloaded.projection(r).Tools {
		if tool.Name == "get_me" && (!tool.Allowed || tool.Group != "read") {
			t.Fatal("read permission lost across persistence")
		}
		if tool.Name == "create_issue" && (tool.Allowed || tool.Group != "write") {
			t.Fatal("read group granted writes after restart")
		}
	}
}

func TestGitHubDeviceExpiryRefreshAndDisconnect(t *testing.T) {
	stateDir := t.TempDir()
	m, _ := New(stateDir)
	m.networkOwned = false
	refreshCalls, reads := 0, 0
	rejectRefresh := false
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"login":"device-user","id":42}`
		switch req.URL.String() {
		case "https://github.com/login/oauth/access_token":
			_ = req.ParseForm()
			if req.PostForm.Get("client_id") != "mewla-public" || req.PostForm.Has("client_secret") || req.Header.Get("Authorization") != "" {
				t.Fatal("device flow must use only the registered public client")
			}
			if req.PostForm.Get("grant_type") == "refresh_token" {
				refreshCalls++
				if req.PostForm.Get("refresh_token") != "device-refresh" && req.PostForm.Get("refresh_token") != "rotated-refresh" {
					t.Fatal("refresh token was not retained")
				}
				body = `{"access_token":"refreshed-access","refresh_token":"rotated-refresh","expires_in":28800,"token_type":"bearer"}`
				if rejectRefresh {
					body = `{"error":"invalid_grant"}`
				}
			} else {
				if req.PostForm.Get("device_code") != "device-code" {
					t.Fatal("lost device binding")
				}
				body = `{"access_token":"device-access","refresh_token":"device-refresh","expires_in":28800,"scope":"repo,read:user"}`
			}
		case "https://api.github.com/user":
			reads++
			if req.Header.Get("Authorization") != "Bearer device-access" && req.Header.Get("Authorization") != "Bearer refreshed-access" {
				t.Fatal("wrong resource credential")
			}
		default:
			t.Fatalf("unexpected request %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	f := &connectFlow{ConnectFlow: ConnectFlow{ID: "device-flow", Integration: "github", Status: "waiting", Expires: time.Now().Add(time.Minute)}, deviceCode: "device-code", clientID: "mewla-public", interval: 5 * time.Second}
	m.connectFlows[f.ID] = f
	if err := m.pollDevice(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	r := m.records[f.AccountID]
	if f.Status != "connected" || r.Account.Name != "device-user" || r.OAuth == nil || r.Account.AuthMethod != "oauth" || reads != 1 {
		t.Fatal("device account not verified and connected exactly once")
	}
	readSecret := func() oauthSecret {
		t.Helper()
		raw, ok, err := m.vault.Get("integration:" + r.Account.ID)
		var secret oauthSecret
		if err != nil || !ok || json.Unmarshal([]byte(raw), &secret) != nil || secret.Token == nil {
			t.Fatal("missing token lifecycle")
		}
		return secret
	}
	secret := readSecret()
	if secret.Token.RefreshToken != "device-refresh" || secret.Token.Expiry.Before(time.Now().Add(7*time.Hour)) || secret.ClientSecret != "" {
		t.Fatal("expiry/refresh persistence failed")
	}
	expire := func() {
		t.Helper()
		secret := readSecret()
		secret.Token.Expiry = time.Now().Add(-time.Minute)
		raw, _ := json.Marshal(secret)
		if err := m.vault.Set("integration:"+r.Account.ID, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	// Restart keeps the public client binding and token lifecycle.
	reopened, err := New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	reopened.networkOwned, reopened.http = false, m.http
	m = reopened
	r = m.records[r.Account.ID]
	if r.OAuth == nil || r.OAuth.ClientID != "mewla-public" {
		t.Fatal("restart lost client binding")
	}
	expire()
	mustHandle(t, m, Request{Action: "invoke", ID: r.Account.ID, Tool: "get_me", Arguments: json.RawMessage(`{}`)})
	if refreshCalls != 1 || readSecret().Token.RefreshToken != "rotated-refresh" {
		t.Fatal("token rotation was not persisted")
	}
	expire()
	rejectRefresh = true
	if _, err := m.accountToken(context.Background(), r); err == nil || r.Account.Status != "authorization_required" {
		t.Fatal("expired grant must require reconnect")
	}
	// Re-consent replaces only this verified identity and retains the account ID.
	oldID := r.Account.ID
	rejectRefresh = false
	f.deviceCode, f.nextPoll, f.Status = "device-code", time.Time{}, "waiting"
	if err := m.pollDevice(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	r = m.records[f.AccountID]
	if r.Account.ID != oldID || len(m.records) != 1 || r.Account.Status != "connected" {
		t.Fatal("reconnect duplicated or lost the verified identity")
	}
	mustHandle(t, m, Request{Action: "disconnect", ID: r.Account.ID})
	before := reads
	if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: r.Account.ID, Tool: "get_me"}); err == nil || reads != before {
		t.Fatal("disconnected device account reached the provider")
	}
}

// Onlora connects with one tap: discovery and dynamic registration against its
// published metadata, asking for its own scopes rather than read and write.
func TestOnloraConnectRegistersAndAsksForItsScopes(t *testing.T) {
	m, _ := New(t.TempDir())
	m.networkOwned = false
	m.SetWebOrigins(func(origin string) bool { return origin == "https://mewla.example" })
	var registered struct {
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
		Application  string   `json:"application_type"`
	}
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.Method + " " + req.URL.String() {
		case "GET https://api.onlora.ai/.well-known/oauth-protected-resource/mcp":
			body = `{"resource":"https://api.onlora.ai/mcp","authorization_servers":["https://onlora.ai/api/auth"],"scopes_supported":["projects","generate"]}`
		case "GET https://onlora.ai/.well-known/oauth-authorization-server/api/auth":
			body = `{"issuer":"https://onlora.ai/api/auth","authorization_endpoint":"https://onlora.ai/api/auth/oauth2/authorize","token_endpoint":"https://onlora.ai/api/auth/oauth2/token","registration_endpoint":"https://onlora.ai/api/auth/oauth2/register","revocation_endpoint":"https://onlora.ai/api/auth/oauth2/revoke","scopes_supported":["openid","profile","email","offline_access","projects","generate"],"code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["none","client_secret_basic"]}`
		case "POST https://onlora.ai/api/auth/oauth2/register":
			registered.Application = ""
			_ = json.NewDecoder(req.Body).Decode(&registered)
			body = `{"client_id":"onlora-client","token_endpoint_auth_method":"none"}`
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	start := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "onlora", WebOrigin: "https://mewla.example"}}).Flow
	callback := "https://mewla.example/plugins/oauth/callback"
	if len(registered.RedirectURIs) != 1 || registered.RedirectURIs[0] != callback || registered.AuthMethod != "none" || registered.Application != "" {
		t.Fatalf("registered %+v", registered)
	}
	authorize, _ := url.Parse(start.AuthorizationURL)
	q := authorize.Query()
	if authorize.Host != "onlora.ai" || authorize.Path != "/api/auth/oauth2/authorize" || q.Get("client_id") != "onlora-client" || q.Get("redirect_uri") != callback {
		t.Fatalf("authorization URL %s", start.AuthorizationURL)
	}
	if q.Get("scope") != "projects generate offline_access" || q.Get("resource") != "https://api.onlora.ai/mcp" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("authorization asks for %v", q)
	}
	r := m.records[start.AccountID]
	if r.Account.Name != "Onlora" || r.Account.Endpoint != "https://api.onlora.ai/mcp" || r.Account.AuthMethod != "mcp_oauth" || writeConsent(r) {
		t.Fatalf("pending account %+v", r.Account)
	}
	// The phone app uses the client Onlora registered for it, with no registration.
	registered.RedirectURIs = nil
	app := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "onlora"}}).Flow
	appURL, _ := url.Parse(app.AuthorizationURL)
	aq := appURL.Query()
	if registered.RedirectURIs != nil || aq.Get("client_id") != "mewla" || aq.Get("redirect_uri") != NativeCallback || aq.Get("code_challenge_method") != "S256" || aq.Get("scope") != "projects generate offline_access" || aq.Get("resource") != "https://api.onlora.ai/mcp" {
		t.Fatalf("app authorization %s, registered %+v", app.AuthorizationURL, registered)
	}
	if flow := m.pending[m.connectFlows[app.ID].state]; flow == nil || !flow.Mobile || flow.ClientSecret != "" || m.records[app.AccountID].OAuth.ClientID != "mewla" {
		t.Fatal("app flow is not a public mobile flow on the mewla client")
	}
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "onlora", Name: "Pasted", Credential: "token"}}); err == nil {
		t.Fatal("Onlora accepted a pasted credential")
	}
}

func TestOnloraToolGroupsOnlyOnItsEndpoint(t *testing.T) {
	r := &record{Account: Account{Integration: "onlora", AuthMethod: "mcp_oauth", Endpoint: "https://api.onlora.ai/mcp"}}
	for name, group := range map[string]string{"search_library": "read", "list_projects": "read", "get_asset": "read", "read_file": "read", "generate_image": "write", "edit_image": "write", "add_asset": "write", "update_asset": "write", "write_file": "write", "generate_voice": "write", "list_models": "read", "delete_file": "write", "publish": "", "propose_drafts": "", "new_tool": ""} {
		if got := toolGroup(r, Tool{Name: name}); got != group {
			t.Fatalf("%s is %q, not %q", name, got, group)
		}
	}
	r.Account.Endpoint = "https://onlora.example/mcp"
	if toolGroup(r, Tool{Name: "search_library"}) != "" {
		t.Fatal("another endpoint inherited Onlora's reviewed tools")
	}
}

// Cloudflare connects with one tap: discovery and dynamic registration against
// its published metadata, asking only for the scopes every grant needs so its
// consent page picks the rest. Cloudflare refuses the app's mewla:// return,
// so the app's sign-in returns through Mewla's https return page.
func TestCloudflareConnectRegistersEachReturn(t *testing.T) {
	m, _ := New(t.TempDir())
	m.networkOwned = false
	m.SetWebOrigins(func(origin string) bool { return origin == "https://mewla.example" })
	var registered struct {
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	registrations := 0
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.Method + " " + req.URL.String() {
		case "GET https://mcp.cloudflare.com/.well-known/oauth-protected-resource/mcp":
			body = `{"resource":"https://mcp.cloudflare.com/mcp","authorization_servers":["https://mcp.cloudflare.com"],"scopes_supported":["user:read","account:read"]}`
		case "GET https://mcp.cloudflare.com/.well-known/oauth-authorization-server":
			body = `{"issuer":"https://mcp.cloudflare.com","authorization_endpoint":"https://mcp.cloudflare.com/authorize","token_endpoint":"https://mcp.cloudflare.com/token","registration_endpoint":"https://mcp.cloudflare.com/register","revocation_endpoint":"https://mcp.cloudflare.com/token","scopes_supported":["offline_access","user:read","account:read","workers.write"],"code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["client_secret_basic","client_secret_post","none"]}`
		case "POST https://mcp.cloudflare.com/register":
			registrations++
			_ = json.NewDecoder(req.Body).Decode(&registered)
			body = `{"client_id":"cloudflare-client","token_endpoint_auth_method":"none"}`
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	start := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "cloudflare", WebOrigin: "https://mewla.example"}}).Flow
	callback := "https://mewla.example/plugins/oauth/callback"
	if len(registered.RedirectURIs) != 1 || registered.RedirectURIs[0] != callback || registered.AuthMethod != "none" {
		t.Fatalf("registered %+v", registered)
	}
	authorize, _ := url.Parse(start.AuthorizationURL)
	q := authorize.Query()
	if authorize.Host != "mcp.cloudflare.com" || authorize.Path != "/authorize" || q.Get("client_id") != "cloudflare-client" || q.Get("redirect_uri") != callback {
		t.Fatalf("authorization URL %s", start.AuthorizationURL)
	}
	if q.Get("scope") != "user:read account:read offline_access" || q.Get("resource") != "https://mcp.cloudflare.com/mcp" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("authorization asks for %v", q)
	}
	r := m.records[start.AccountID]
	if r.Account.Name != "Cloudflare" || r.Account.Endpoint != "https://mcp.cloudflare.com/mcp" || r.Account.AuthMethod != "mcp_oauth" || writeConsent(r) {
		t.Fatalf("pending account %+v", r.Account)
	}
	// The app's sign-in registers Mewla's https return page and finishes
	// through connect_finish with the mewla://plugins URL it forwards.
	app := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "cloudflare"}}).Flow
	if registrations != 2 || len(registered.RedirectURIs) != 1 || registered.RedirectURIs[0] != NativeReturnRelay || app.WebReturn {
		t.Fatalf("app registered %+v after %d registrations", registered, registrations)
	}
	authorize, _ = url.Parse(app.AuthorizationURL)
	state := authorize.Query().Get("state")
	if authorize.Query().Get("redirect_uri") != NativeReturnRelay || !m.pending[state].Mobile || m.pending[state].Web {
		t.Fatalf("app authorization %s", app.AuthorizationURL)
	}
	exchanged := url.Values{}
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method+" "+req.URL.String() != "POST https://mcp.cloudflare.com/token" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		}
		_ = req.ParseForm()
		exchanged = req.PostForm
		return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`))}, nil
	})}
	finish := mustHandle(t, m, Request{Action: "connect_finish", FlowID: app.ID, Callback: NativeCallback + "?" + url.Values{"code": {"relayed"}, "state": {state}}.Encode()}).Flow
	if exchanged.Get("redirect_uri") != NativeReturnRelay || exchanged.Get("code") != "relayed" || exchanged.Get("code_verifier") == "" || finish.Status != "failed" {
		t.Fatalf("exchange %v, flow %+v", exchanged, finish)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "cloudflare", Name: "Pasted", Credential: "token"}}); err == nil {
		t.Fatal("Cloudflare accepted a pasted credential")
	}
}

func TestCloudflareToolGroupsOnlyOnItsEndpoint(t *testing.T) {
	r := &record{Account: Account{Integration: "cloudflare", AuthMethod: "mcp_oauth", Endpoint: "https://mcp.cloudflare.com/mcp"}}
	for name, group := range map[string]string{"search": "read", "docs": "read", "whoami": "read", "execute": "write", "new_tool": ""} {
		if got := toolGroup(r, Tool{Name: name}); got != group {
			t.Fatalf("%s is %q, not %q", name, got, group)
		}
	}
	r.Account.Endpoint = "https://cloudflare.example/mcp"
	if toolGroup(r, Tool{Name: "search"}) != "" {
		t.Fatal("another endpoint inherited Cloudflare's reviewed tools")
	}
}

func TestNativeRedirectForAppSchemesAndLoopback(t *testing.T) {
	for raw, native := range map[string]bool{
		"mewla://plugins": true,
		"http://127.0.0.1:9876/plugins/oauth/callback":    true,
		"http://[::1]:9876/plugins/oauth/callback":        true,
		"http://localhost:9876/plugins/oauth/callback":    true,
		"https://mewla.invalid/plugins/oauth/callback":    false,
		"https://127.0.0.1/plugins/oauth/callback":        false,
		"http://192.168.1.20:9876/plugins/oauth/callback": false,
		"": false,
	} {
		if nativeRedirect(raw) != native {
			t.Fatalf("%q native = %v", raw, !native)
		}
	}
}

// Registration carries Mewla's name and branding for consent pages, and a
// server that refuses the branding still registers Mewla without it.
func TestDynamicRegistrationBrandingFallsBack(t *testing.T) {
	for _, refuses := range []bool{false, true} {
		m, _ := New(t.TempDir())
		m.networkOwned = false
		m.SetWebOrigins(func(origin string) bool { return origin == "https://mewla.example" })
		var bodies []map[string]any
		m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			status, body := 200, ""
			switch req.Method + " " + req.URL.String() {
			case "GET https://mcp.cloudflare.com/.well-known/oauth-protected-resource/mcp":
				body = `{"resource":"https://mcp.cloudflare.com/mcp","authorization_servers":["https://mcp.cloudflare.com"]}`
			case "GET https://mcp.cloudflare.com/.well-known/oauth-authorization-server":
				body = `{"issuer":"https://mcp.cloudflare.com","authorization_endpoint":"https://mcp.cloudflare.com/authorize","token_endpoint":"https://mcp.cloudflare.com/token","registration_endpoint":"https://mcp.cloudflare.com/register","code_challenge_methods_supported":["S256"]}`
			case "POST https://mcp.cloudflare.com/register":
				var sent map[string]any
				_ = json.NewDecoder(req.Body).Decode(&sent)
				bodies = append(bodies, sent)
				body = `{"client_id":"cloudflare-client","token_endpoint_auth_method":"none"}`
				if _, branded := sent["logo_uri"]; refuses && branded {
					status, body = 400, `{"error":"invalid_client_metadata"}`
				}
			default:
				t.Fatalf("unexpected request %s %s", req.Method, req.URL)
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "cloudflare", WebOrigin: "https://mewla.example"}})
		want := 1
		if refuses {
			want = 2
		}
		if len(bodies) != want || bodies[0]["client_name"] != "Mewla" || bodies[0]["client_uri"] != "https://mewla.ai" || bodies[0]["logo_uri"] != "https://mewla.ai/mark.png" || bodies[0]["policy_uri"] != "https://mewla.ai/privacy/" {
			t.Fatalf("refuses=%t registrations %v", refuses, bodies)
		}
		if last := bodies[len(bodies)-1]; refuses && (last["client_name"] != "Mewla" || last["logo_uri"] != nil || last["client_uri"] != nil || last["policy_uri"] != nil) {
			t.Fatalf("fallback registration %v", last)
		}
	}
}
