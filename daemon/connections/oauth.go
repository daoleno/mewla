package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// OAuthClientConfig is entered once by the server owner. Secrets live only in
// the credential vault. RedirectURL is a registered daemon HTTPS callback,
// opened by the user's system browser, never a provider embedded WebView.
type OAuthClientConfig struct {
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	RedirectURL  string `json:"redirect_url"`
	ResourceURL  string `json:"resource_url,omitempty"`
}
type oauthAccount struct {
	ClientID       string           `json:"client_id"`
	RedirectURL    string           `json:"redirect_url"`
	Issuer         string           `json:"issuer"`
	AuthURL        string           `json:"auth_url"`
	TokenURL       string           `json:"token_url"`
	RevokeURL      string           `json:"revoke_url,omitempty"`
	Resource       string           `json:"resource,omitempty"`
	GoogleExchange string           `json:"google_exchange,omitempty"`
	AuthStyle      oauth2.AuthStyle `json:"auth_style"`
}
type oauthSecret struct {
	Exchange     *googleExchangeSecret `json:"exchange,omitempty"`
	Token        *oauth2.Token         `json:"token"`
	ClientSecret string                `json:"client_secret,omitempty"`
}
type oauthFlow struct {
	ID           string
	Verifier     string
	ClientSecret string
	Expires      time.Time
	Mobile       bool
	Web          bool // returns to the web UI's Plugins page
	AllowWrites  bool
}
type oauthMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	RevocationEndpoint    string   `json:"revocation_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
	AuthMethods           []string `json:"token_endpoint_auth_methods_supported"`
}

func validCallback(raw string) bool {
	u, err := url.Parse(raw)
	return len(raw) <= 4096 && err == nil && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "/plugins/oauth/callback" && (u.Scheme == "https" && u.Hostname() != "" || u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))
}
func (m *Manager) configureOAuth(in *Input) (Response, error) {
	if in == nil || in.OAuthClient == nil || !(validCallback(in.OAuthClient.RedirectURL) || in.Integration == "github" && in.OAuthClient.RedirectURL == "" || in.Integration == "slack" && in.OAuthClient.RedirectURL == NativeCallback && in.OAuthClient.ClientSecret == "") || len(in.OAuthClient.ClientID) > 2048 || len(in.OAuthClient.ClientSecret) > 16384 {
		return Response{}, errors.New("provide an OAuth client and registered daemon /plugins/oauth/callback URL")
	}
	switch in.Integration {
	case "github", "slack", "google", "notion", "linear", "mcp":
	default:
		return Response{}, errors.New("OAuth is not supported for this plugin")
	}
	if (in.Integration == "google" || in.Integration == "slack" || in.Integration == "github") && in.OAuthClient.ClientID == "" {
		return Response{}, errors.New("this service requires a publisher-owned client ID")
	}
	if in.Integration == "mcp" && in.OAuthClient.ClientID != "" {
		if _, err := trustedEndpointURL(in.OAuthClient.ResourceURL, in.TrustedNetworks); err != nil {
			return Response{}, errors.New("bind the registered MCP client to its exact resource_url")
		}
	}
	raw, _ := json.Marshal(in.OAuthClient)
	if err := m.vault.Set("oauth-client:"+in.Integration, string(raw)); err != nil {
		return Response{}, errors.New("OAuth configuration could not be saved")
	}
	return Response{OAuthConfigured: m.oauthConfigured()}, nil
}
func (m *Manager) oauthConfigured() []string {
	configured := []string{}
	for _, kind := range []string{"github", "slack", "google", "notion", "linear", "mcp"} {
		if _, ok, err := m.vault.Get("oauth-client:" + kind); err == nil && ok {
			configured = append(configured, kind)
		}
	}
	return configured
}
func (m *Manager) startOAuth(ctx context.Context, in *Input) (Response, error) {
	if in == nil || len(in.Name) > 80 || len(m.records) >= 50 {
		return Response{}, errors.New("maximum accounts reached or invalid account name")
	}
	if strings.TrimSpace(in.Name) == "" {
		copy := *in
		in = &copy
		in.Name = serviceName(in.Integration)
	}
	for state, flow := range m.pending {
		if time.Now().After(flow.Expires) {
			delete(m.records, flow.ID)
			delete(m.pending, state)
		}
	}
	if len(m.pending) >= 20 {
		return Response{}, errors.New("too many pending authorizations; try again later")
	}
	client, ok, err := m.clientConfig(in.Integration)
	if err != nil {
		return Response{}, errors.New("authorization configuration unavailable")
	}
	// connect_start returns to the native app or to the web UI it came from.
	callback := in.callback
	if callback == "" && in.Mobile {
		callback = NativeCallback
	}
	if !ok && callback != "" && (in.Integration == "notion" || in.Integration == "linear" || in.Integration == "mcp") {
		client.RedirectURL = callback
	} else if !ok {
		return Response{}, errors.New("Sign-in is not set up for this service yet.")
	}
	if callback != "" && (in.Integration == "notion" || in.Integration == "linear") {
		client = OAuthClientConfig{RedirectURL: callback}
	}

	endpoint := in.Endpoint
	switch in.Integration {
	case "slack":
		endpoint = "https://slack.com/api"
	case "google":
		endpoint = "https://www.googleapis.com"
	case "notion":
		endpoint = "https://mcp.notion.com/mcp"
	case "linear":
		endpoint = "https://mcp.linear.app/mcp"
	case "mcp":
	default:
		return Response{}, errors.New("OAuth is not supported for this plugin")
	}
	if len(in.TrustedNetworks) > 0 && in.Integration != "mcp" {
		return Response{}, errors.New("internal network trust is only available for custom MCP")
	}
	if _, err := trustedEndpointURL(endpoint, in.TrustedNetworks); err != nil {
		return Response{}, err
	}
	if in.Integration == "mcp" && client.ClientID != "" && client.ResourceURL != endpoint {
		return Response{}, errors.New("this OAuth client belongs to another MCP endpoint; configure the matching client first")
	}
	r := &record{Account: Account{ID: uuid.NewString(), Integration: in.Integration, Name: strings.TrimSpace(in.Name), Endpoint: endpoint, TrustedNetworks: append([]string(nil), in.TrustedNetworks...), Enabled: true, Status: "authorization_required", AuthMethod: "mcp_oauth", History: []Event{}}, Grants: map[string]string{}}
	var meta oauthMetadata
	if in.Integration == "slack" {
		r.Account.AuthMethod = "oauth"
		meta = oauthMetadata{Issuer: "https://slack.com", AuthorizationEndpoint: "https://slack.com/oauth/v2/authorize", TokenEndpoint: "https://slack.com/api/oauth.v2.access", AuthMethods: []string{"client_secret_post"}}
		r.Account.Scopes = []string{"channels:read", "channels:history", "search:read"}
		if in.AllowWrites {
			r.Account.Scopes = append(r.Account.Scopes, "chat:write")
		}
	} else if in.Integration == "google" {
		r.Account.AuthMethod = "oauth"
		meta = oauthMetadata{Issuer: "https://accounts.google.com", AuthorizationEndpoint: "https://accounts.google.com/o/oauth2/v2/auth", TokenEndpoint: "https://oauth2.googleapis.com/token", RevocationEndpoint: "https://oauth2.googleapis.com/revoke", AuthMethods: []string{"client_secret_post"}}
		r.Account.Scopes = googleScopes(in.AllowWrites)
	} else {
		meta, err = m.discoverOAuth(ctx, r)
		if err != nil {
			return Response{}, err
		}
		// Ask the provider for its normal read scope. Writes require a separate
		// connect action requesting them, then the usual per-tool grant.
		for _, scope := range meta.ScopesSupported {
			if scope == "read" || scope == "openid" || scope == "email" || in.AllowWrites && scope == "write" {
				r.Account.Scopes = append(r.Account.Scopes, scope)
			}
		}
	}
	style := oauth2.AuthStyleInParams
	if client.ClientSecret != "" && contains(meta.AuthMethods, "client_secret_basic") {
		style = oauth2.AuthStyleInHeader
	}
	if client.ClientID == "" {
		if meta.RegistrationEndpoint == "" {
			if in.Integration == "mcp" {
				return Response{}, errors.New("this custom service requires a client registered by its operator")
			}
			return Response{}, errors.New("Mewla's authorization for this service is not ready. No account was connected.")
		}
		registration := map[string]any{"client_name": "Mewla Plugins", "redirect_uris": []string{client.RedirectURL}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"}
		body, _ := json.Marshal(registration)
		var registered struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			AuthMethod   string `json:"token_endpoint_auth_method"`
		}
		if err := m.oauthJSON(ctx, r, "POST", meta.RegistrationEndpoint, body, &registered); err != nil {
			return Response{}, errors.New("OAuth dynamic registration failed; this provider may require a registered client")
		}
		if registered.ClientID == "" || len(registered.ClientID) > 2048 || len(registered.ClientSecret) > 16384 {
			return Response{}, errors.New("provider returned an invalid OAuth client")
		}
		client.ClientID, client.ClientSecret = registered.ClientID, registered.ClientSecret
		style = oauth2.AuthStyleInParams
		if registered.AuthMethod == "client_secret_basic" || registered.AuthMethod == "" && registered.ClientSecret != "" {
			style = oauth2.AuthStyleInHeader
		}
	}
	r.OAuth = &oauthAccount{ClientID: client.ClientID, RedirectURL: client.RedirectURL, Issuer: meta.Issuer, AuthURL: meta.AuthorizationEndpoint, TokenURL: meta.TokenEndpoint, RevokeURL: meta.RevocationEndpoint, AuthStyle: style}
	if r.Account.AuthMethod == "mcp_oauth" {
		r.OAuth.Resource = endpoint
	}
	state, verifier := oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
	options := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier)}
	if r.OAuth.Resource != "" {
		options = append(options, oauth2.SetAuthURLParam("resource", r.OAuth.Resource))
	}
	if in.Integration == "google" {
		options = append(options, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	}
	config := oauthConfig(r, client.ClientSecret)
	if in.Integration == "slack" {
		config.Scopes = nil
		options = append(options, oauth2.SetAuthURLParam("user_scope", strings.Join(r.Account.Scopes, ",")))
	}
	authURL := config.AuthCodeURL(state, options...)
	m.records[r.Account.ID] = r
	m.pending[state] = &oauthFlow{ID: r.Account.ID, Verifier: verifier, ClientSecret: client.ClientSecret, Expires: time.Now().Add(10 * time.Minute), Mobile: callback != "" && client.RedirectURL == NativeCallback, Web: callback != "" && callback != NativeCallback && client.RedirectURL == callback, AllowWrites: callback != "" && in.AllowWrites}
	account := m.projection(r)
	return Response{Account: &account, AuthorizationURL: authURL}, nil
}
func oauthConfig(r *record, secret string) oauth2.Config {
	return oauth2.Config{ClientID: r.OAuth.ClientID, ClientSecret: secret, RedirectURL: r.OAuth.RedirectURL, Scopes: r.Account.Scopes, Endpoint: oauth2.Endpoint{AuthURL: r.OAuth.AuthURL, TokenURL: r.OAuth.TokenURL, AuthStyle: r.OAuth.AuthStyle}}
}
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// OAuthCallback is intentionally public: an unguessable, expiring single-use
// state and PKCE bind it to setup authorized via the paired/control boundary.
// Neither tokens nor authorization codes appear in the browser response.
func (m *Manager) OAuthCallback(w http.ResponseWriter, req *http.Request) {
	m.BrowserOAuthCallback(w, req, false)
}

// BrowserOAuthCallback is OAuthCallback for a request on a host that serves
// the web UI (webUI), where a stale or replayed return lands on Plugins.
func (m *Manager) BrowserOAuthCallback(w http.ResponseWriter, req *http.Request, webUI bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	if req.Method != "GET" || len(req.URL.RawQuery) > 16384 {
		http.Error(w, "Invalid authorization response", 400)
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 20*time.Second)
	defer cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finishOAuth(w, req, ctx, false, webUI)
}

// pluginsReturn sends a web sign-in back to the Plugins list, where it
// started, naming the service it was for. The target is a path, so the
// browser stays on the origin the provider returned it to.
func pluginsReturn(w http.ResponseWriter, req *http.Request, integration string, query url.Values) {
	if integration != "" {
		query.Set("service", integration)
	}
	http.Redirect(w, req, "/plugins?"+query.Encode(), http.StatusSeeOther)
}

func (m *Manager) finishOAuth(w http.ResponseWriter, req *http.Request, ctx context.Context, mobile, webUI bool) {
	state := req.URL.Query().Get("state")
	flow, ok := m.pending[state]
	if !ok || flow.Mobile != mobile || time.Now().After(flow.Expires) {
		if webUI && !mobile {
			pluginsReturn(w, req, "", url.Values{"plugin_error": {"expired"}})
			return
		}
		http.Error(w, "Authorization expired. Return to Plugins and connect again.", 400)
		return
	}
	delete(m.pending, state)
	r, ok := m.records[flow.ID]
	if !ok || !r.Account.Enabled || r.Account.Status == "disconnected" {
		if flow.Web {
			pluginsReturn(w, req, "", url.Values{"plugin_error": {"cancelled"}})
			return
		}
		http.Error(w, "Account authorization cancelled", 400)
		return
	}
	pendingID := r.Account.ID
	fail := func() {
		// Never remove an established account if a verified reconnect reused its ID.
		if r.Account.ID == pendingID {
			delete(m.records, pendingID)
			_ = m.vault.Delete("integration:" + pendingID)
		}
		_ = m.save()
		if flow.Web {
			outcome := "failed"
			if req.URL.Query().Get("error") == "access_denied" {
				outcome = "denied"
			}
			pluginsReturn(w, req, r.Account.Integration, url.Values{"plugin_error": {outcome}})
			return
		}
		http.Error(w, "Authorization failed. Return to Plugins and connect again.", 400)
	}
	if req.URL.Query().Get("error") != "" || req.URL.Query().Get("code") == "" {
		fail()
		return
	}
	if issuer := req.URL.Query().Get("iss"); issuer != "" && issuer != r.OAuth.Issuer {
		fail()
		return
	}
	config := oauthConfig(r, flow.ClientSecret)
	options := []oauth2.AuthCodeOption{oauth2.VerifierOption(flow.Verifier)}
	if r.OAuth.Resource != "" {
		options = append(options, oauth2.SetAuthURLParam("resource", r.OAuth.Resource))
	}
	var token *oauth2.Token
	var err error
	if r.Account.Integration == "slack" {
		token, err = m.exchangeSlack(ctx, r, flow.ClientSecret, req.URL.Query().Get("code"), flow.Verifier)
	} else {
		token, err = config.Exchange(context.WithValue(ctx, oauth2.HTTPClient, m.oauthHTTP(r)), req.URL.Query().Get("code"), options...)
	}
	if err != nil || token == nil || token.AccessToken == "" || len(token.AccessToken) > 16384 || len(token.RefreshToken) > 16384 {
		fail()
		return
	}
	if scope, ok := token.Extra("scope").(string); ok {
		r.Account.Scopes = strings.Fields(scope)
	}
	secret := oauthSecret{Token: token, ClientSecret: flow.ClientSecret}
	raw, _ := json.Marshal(secret)
	if err = m.vault.Set("integration:"+r.Account.ID, string(raw)); err != nil {
		fail()
		return
	}
	if err = m.discover(ctx, r, token.AccessToken); err != nil {
		fail()
		return
	}
	m.setGroup(r, "read", true)
	if flow.AllowWrites {
		m.setGroup(r, "write", true)
	}
	m.nameAndDeduplicate(r)
	m.event(r, "authorize", nil)
	if err = m.save(); err != nil {
		fail()
		return
	}
	if flow.Web {
		pluginsReturn(w, req, r.Account.Integration, url.Values{"connected": {r.Account.ID}})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><meta name="viewport" content="width=device-width"><title>Mewla Plugins</title><h1>Account connected</h1><p>You can return to Mewla.</p><a href="mewla://plugins">Open Plugins</a>`)
}
func (m *Manager) accountToken(ctx context.Context, r *record) (string, error) {
	raw, ok, err := m.vault.Get("integration:" + r.Account.ID)
	if err != nil {
		return "", errors.New("credential store unavailable")
	}
	if r.OAuth == nil {
		return raw, nil
	}
	if !ok {
		return "", errAuth
	}
	var secret oauthSecret
	if json.Unmarshal([]byte(raw), &secret) != nil || secret.Token == nil {
		return "", errAuth
	}
	if secret.Token.Valid() {
		return secret.Token.AccessToken, nil
	}
	if secret.Exchange != nil {
		return m.refreshGoogleExchange(ctx, r, &secret)
	}
	config := oauthConfig(r, secret.ClientSecret)
	client := m.oauthHTTP(r)
	if r.OAuth.Resource != "" {
		// oauth2 v0.34 does not expose refresh AuthCodeOptions. Keep its token
		// lifecycle and client authentication, adding RFC8707's resource only to
		// this account's exact token endpoint and refresh grant.
		client.Transport = oauthRefreshResourceTransport{client.Transport, r.OAuth.TokenURL, r.OAuth.Resource}
	}
	var token *oauth2.Token
	if r.Account.Integration == "slack" {
		token, err = m.slackToken(ctx, r, url.Values{"client_id": {r.OAuth.ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {secret.Token.RefreshToken}})
	} else {
		token, err = config.TokenSource(context.WithValue(ctx, oauth2.HTTPClient, client), secret.Token).Token()
	}
	if err != nil {
		r.Account.Status = "authorization_required"
		_ = m.save()
		return "", errAuth
	}
	if token.AccessToken == "" || len(token.AccessToken) > 16384 || len(token.RefreshToken) > 16384 {
		return "", errAuth
	}
	secret.Token = token
	encoded, _ := json.Marshal(secret)
	if err = m.vault.Set("integration:"+r.Account.ID, string(encoded)); err != nil {
		return "", errors.New("refreshed credential could not be saved")
	}
	return token.AccessToken, nil
}

type oauthRefreshResourceTransport struct {
	base               http.RoundTripper
	tokenURL, resource string
}

func (t oauthRefreshResourceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodPost || r.URL.String() != t.tokenURL || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Body == nil {
		return nil, errors.New("invalid OAuth refresh request")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (128<<10)+1))
	r.Body.Close()
	if err != nil || len(raw) > 128<<10 {
		return nil, errors.New("invalid OAuth refresh body")
	}
	form, err := url.ParseQuery(string(raw))
	if err != nil || form.Get("grant_type") != "refresh_token" {
		return nil, errors.New("invalid OAuth refresh grant")
	}
	if resources, present := form["resource"]; present && (len(resources) != 1 || resources[0] != t.resource) {
		return nil, errors.New("OAuth refresh resource mismatch")
	}
	form.Set("resource", t.resource)
	body := form.Encode()
	request := r.Clone(r.Context())
	request.Body = io.NopCloser(strings.NewReader(body))
	request.ContentLength = int64(len(body))
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body)), nil }
	return t.base.RoundTrip(request)
}

func (m *Manager) revokeOAuth(ctx context.Context, r *record) error {
	raw, ok, err := m.vault.Get("integration:" + r.Account.ID)
	if err != nil {
		return errors.New("account disabled; credential store unavailable")
	}
	if !ok {
		return nil
	}
	var secret oauthSecret
	if json.Unmarshal([]byte(raw), &secret) != nil || secret.Token == nil {
		return errors.New("account disabled; stored OAuth credential is invalid")
	}
	if r.Account.Integration == "slack" {
		if _, err := m.request(ctx, r, "https://slack.com/api", secret.Token.AccessToken, "POST", "/auth.revoke", nil); err != nil {
			return errors.New("account disabled; Slack revocation failed, retry disconnect")
		}
		return nil
	}
	if r.OAuth.RevokeURL == "" {
		return nil
	} // Vendor without RFC7009: local revoke; UI/docs identify vendor step.
	token := secret.Token.RefreshToken
	if token == "" {
		token = secret.Token.AccessToken
	}
	form := url.Values{"token": {token}, "client_id": {r.OAuth.ClientID}}
	if secret.ClientSecret != "" {
		form.Set("client_secret", secret.ClientSecret)
	}
	if r.OAuth.AuthStyle == oauth2.AuthStyleInHeader {
		form.Del("client_id")
		form.Del("client_secret")
	}
	request, _ := http.NewRequestWithContext(ctx, "POST", r.OAuth.RevokeURL, strings.NewReader(form.Encode()))
	if r.OAuth.AuthStyle == oauth2.AuthStyleInHeader {
		request.SetBasicAuth(url.QueryEscape(r.OAuth.ClientID), url.QueryEscape(secret.ClientSecret))
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := m.oauthHTTP(r).Do(request)
	if err != nil {
		return errors.New("account disabled; remote revocation failed, retry disconnect")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("account disabled; remote revocation failed, retry disconnect")
	}
	return nil
}

// OAuth calls never use the resource bearer token. Metadata/DCR/token bodies
// are bounded by the transport before decoding by maintained oauth2 code.
func (m *Manager) oauthHTTP(r *record) *http.Client {
	c := *m.clientFor(r)
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c.Transport = oauthBoundedTransport{base: base}
	return &c
}

type oauthBoundedTransport struct{ base http.RoundTripper }

func (t oauthBoundedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" && r.URL.Scheme != "http" {
		return nil, errors.New("invalid OAuth URL")
	}
	response, err := t.base.RoundTrip(r)
	if err == nil {
		response.Body = &limitedBody{response.Body, MaxResultBytes + 1}
	}
	return response, err
}
func (m *Manager) oauthJSON(ctx context.Context, r *record, method, target string, body []byte, out any) error {
	if _, err := trustedEndpointURL(target, r.Account.TrustedNetworks); err != nil {
		return errors.New("invalid OAuth endpoint")
	}
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid OAuth request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := m.oauthHTTP(r).Do(request)
	if err != nil {
		return errors.New("OAuth endpoint request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("OAuth endpoint rejected request")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, MaxResultBytes+1))
	if err != nil || len(raw) > MaxResultBytes || json.Unmarshal(raw, out) != nil {
		return errors.New("invalid OAuth metadata response")
	}
	return nil
}
func (m *Manager) discoverOAuth(ctx context.Context, r *record) (oauthMetadata, error) {
	endpoint, _ := url.Parse(r.Account.Endpoint)
	resourceURL := endpoint.Scheme + "://" + endpoint.Host + "/.well-known/oauth-protected-resource" + strings.TrimRight(endpoint.Path, "/")
	var resource struct {
		Resource string   `json:"resource"`
		Servers  []string `json:"authorization_servers"`
	}
	err := m.oauthJSON(ctx, r, "GET", resourceURL, nil, &resource)
	if err != nil && endpoint.Path != "" {
		err = m.oauthJSON(ctx, r, "GET", endpoint.Scheme+"://"+endpoint.Host+"/.well-known/oauth-protected-resource", nil, &resource)
	}
	if err != nil {
		// RFC9728 resource_metadata from the unauthenticated MCP challenge takes
		// precedence when a server does not publish conventional well-known URLs.
		request, _ := http.NewRequestWithContext(ctx, "POST", r.Account.Endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"Mewla","version":"1"}}}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		response, challengeErr := m.oauthHTTP(r).Do(request)
		if challengeErr == nil {
			challenge := response.Header.Get("WWW-Authenticate")
			response.Body.Close()
			if response.StatusCode == 401 {
				match := regexp.MustCompile(`(?:^|[ ,])resource_metadata="([^"\r\n]+)"`).FindStringSubmatch(challenge)
				if len(match) == 2 {
					err = m.oauthJSON(ctx, r, "GET", match[1], nil, &resource)
				}
			}
		}
	}
	if err != nil || resource.Resource != r.Account.Endpoint || len(resource.Servers) == 0 || len(resource.Servers) > 5 {
		return oauthMetadata{}, errors.New("MCP OAuth resource discovery failed; check the endpoint or provider registration requirements")
	}
	issuer := resource.Servers[0]
	u, err := trustedEndpointURL(issuer, r.Account.TrustedNetworks)
	if err != nil {
		return oauthMetadata{}, errors.New("invalid OAuth issuer")
	}
	target := u.Scheme + "://" + u.Host + "/.well-known/oauth-authorization-server" + strings.TrimRight(u.Path, "/")
	var meta oauthMetadata
	if err = m.oauthJSON(ctx, r, "GET", target, nil, &meta); err != nil {
		return meta, err
	}
	if meta.Issuer != issuer || !contains(meta.CodeChallengeMethods, "S256") {
		return meta, errors.New("OAuth issuer must match discovery and support PKCE S256")
	}
	for _, target := range []string{meta.AuthorizationEndpoint, meta.TokenEndpoint, meta.RegistrationEndpoint, meta.RevocationEndpoint} {
		if target != "" {
			if _, err := trustedEndpointURL(target, r.Account.TrustedNetworks); err != nil {
				return meta, errors.New("invalid OAuth metadata endpoint")
			}
		}
	}
	if meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return meta, errors.New("OAuth endpoints missing")
	}
	return meta, nil
}

func (m *Manager) sensitiveValues(r *record, access string) []string {
	values := []string{access}
	if r.OAuth != nil {
		if raw, ok, err := m.vault.Get("integration:" + r.Account.ID); err == nil && ok {
			var secret oauthSecret
			if json.Unmarshal([]byte(raw), &secret) == nil {
				values = append(values, secret.ClientSecret)
				if secret.Token != nil {
					values = append(values, secret.Token.RefreshToken, secret.Token.AccessToken)
				}
			}
		}
	}
	return values
}
