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
	ClientID    string           `json:"client_id"`
	RedirectURL string           `json:"redirect_url"`
	Issuer      string           `json:"issuer"`
	AuthURL     string           `json:"auth_url"`
	TokenURL    string           `json:"token_url"`
	RevokeURL   string           `json:"revoke_url,omitempty"`
	Resource    string           `json:"resource,omitempty"`
	AuthStyle   oauth2.AuthStyle `json:"auth_style"`
}
type oauthSecret struct {
	Token        *oauth2.Token `json:"token"`
	ClientSecret string        `json:"client_secret,omitempty"`
}
type oauthFlow struct {
	ID           string
	Verifier     string
	ClientSecret string
	Expires      time.Time
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
	if in == nil || in.OAuthClient == nil || !validCallback(in.OAuthClient.RedirectURL) || len(in.OAuthClient.ClientID) > 2048 || len(in.OAuthClient.ClientSecret) > 16384 {
		return Response{}, errors.New("provide an OAuth client and registered daemon /plugins/oauth/callback URL")
	}
	switch in.Integration {
	case "google", "notion", "linear", "mcp":
	default:
		return Response{}, errors.New("OAuth is not supported for this plugin")
	}
	if in.Integration == "google" && in.OAuthClient.ClientID == "" {
		return Response{}, errors.New("Google requires your registered web application OAuth client ID")
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
	for _, kind := range []string{"google", "notion", "linear", "mcp"} {
		if _, ok, err := m.vault.Get("oauth-client:" + kind); err == nil && ok {
			configured = append(configured, kind)
		}
	}
	return configured
}
func (m *Manager) startOAuth(ctx context.Context, in *Input) (Response, error) {
	if in == nil || strings.TrimSpace(in.Name) == "" || len(in.Name) > 80 || len(m.records) >= 50 {
		return Response{}, errors.New("provide a unique account name (up to 80 characters)")
	}
	for _, r := range m.records {
		if r.Account.Integration == in.Integration && r.Account.Name == strings.TrimSpace(in.Name) && r.Account.Status != "disconnected" {
			return Response{}, errors.New("choose a unique account name for this plugin")
		}
	}
	for state, flow := range m.pending {
		if time.Now().After(flow.Expires) {
			delete(m.pending, state)
		}
	}
	if len(m.pending) >= 20 {
		return Response{}, errors.New("too many pending authorizations; try again later")
	}
	raw, ok, err := m.vault.Get("oauth-client:" + in.Integration)
	var client OAuthClientConfig
	if err != nil || !ok || json.Unmarshal([]byte(raw), &client) != nil {
		return Response{}, errors.New("configure this server's OAuth callback/client first in Plugins")
	}
	endpoint := in.Endpoint
	switch in.Integration {
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
	if in.Integration == "google" {
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
			return Response{}, errors.New("provider requires an OAuth client registration; configure its client ID on this server")
		}
		registration := map[string]any{"client_name": "Zen Plugins", "redirect_uris": []string{client.RedirectURL}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"}
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
	if in.Integration != "google" {
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
	authURL := config.AuthCodeURL(state, options...)
	m.records[r.Account.ID] = r
	if err := m.save(); err != nil {
		delete(m.records, r.Account.ID)
		return Response{}, err
	}
	m.pending[state] = &oauthFlow{ID: r.Account.ID, Verifier: verifier, ClientSecret: client.ClientSecret, Expires: time.Now().Add(10 * time.Minute)}
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
	state := req.URL.Query().Get("state")
	flow, ok := m.pending[state]
	if !ok || time.Now().After(flow.Expires) {
		http.Error(w, "Authorization expired. Return to Plugins and connect again.", 400)
		return
	}
	delete(m.pending, state)
	r, ok := m.records[flow.ID]
	if !ok || !r.Account.Enabled || r.Account.Status == "disconnected" {
		http.Error(w, "Account authorization cancelled", 400)
		return
	}
	fail := func() {
		r.Account.Status = "authorization_required"
		m.event(r, "authorize", errors.New("authorization failed; connect again"))
		_ = m.save()
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
	token, err := config.Exchange(context.WithValue(ctx, oauth2.HTTPClient, m.oauthHTTP(r)), req.URL.Query().Get("code"), options...)
	if err != nil || token.AccessToken == "" || len(token.AccessToken) > 16384 || len(token.RefreshToken) > 16384 {
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
	for _, tool := range r.Account.Tools {
		if tool.Read {
			r.Grants[tool.Name] = fingerprint(tool)
		}
	}
	m.event(r, "authorize", nil)
	if err = m.save(); err != nil {
		fail()
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><meta name="viewport" content="width=device-width"><title>Zen Plugins</title><h1>Account connected</h1><p>You can return to Zen.</p><a href="zen://plugins">Open Plugins</a>`)
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
	config := oauthConfig(r, secret.ClientSecret)
	token, err := config.TokenSource(context.WithValue(ctx, oauth2.HTTPClient, m.oauthHTTP(r)), secret.Token).Token()
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
		request, _ := http.NewRequestWithContext(ctx, "POST", r.Account.Endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"Zen","version":"1"}}}`))
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
