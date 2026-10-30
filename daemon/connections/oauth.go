package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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
	Mobile       bool
	Web          bool // returns to the web UI's Plugins page
	AllowWrites  bool
	// Loopback: the provider returns to this daemon's own port; the person may
	// also paste the address the browser ended on. One state finishes once,
	// from either.
	Loopback bool
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

// nativeRedirect reports a return address that OpenID Connect registration
// calls native: an app scheme or an http loopback address. Servers that follow
// it, such as Onlora's, refuse those addresses for the default web type.
func nativeRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return false
	}
	if u.Scheme == "https" {
		return false
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		return u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
	}
	return true
}

// registrationBranding is the RFC 7591 metadata providers show on their
// consent pages for clients Mewla registers.
var registrationBranding = map[string]string{
	"client_uri": "https://mewla.ai",
	"logo_uri":   "https://mewla.ai/mark.png",
	"policy_uri": "https://mewla.ai/privacy/",
}

var errOAuthRejected = errors.New("OAuth endpoint rejected request")

func validCallback(raw string) bool {
	u, err := url.Parse(raw)
	return len(raw) <= 4096 && err == nil && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "/plugins/oauth/callback" && (u.Scheme == "https" && u.Hostname() != "" || u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))
}

// appReturn reports a redirect that ends in the Mewla app, directly or
// through Mewla's https return page.
func appReturn(redirect string) bool {
	return redirect == NativeCallback || redirect == NativeReturnRelay
}
func (m *Manager) configureOAuth(in *Input) (Response, error) {
	var e *entry
	if in != nil {
		e = lookup(in.Integration)
	}
	var oauth *oauthSpec
	if e != nil {
		oauth = e.OAuth
	}
	if in == nil || in.OAuthClient == nil || !(validCallback(in.OAuthClient.RedirectURL) || oauth.acceptsRedirect(in.OAuthClient)) || len(in.OAuthClient.ClientID) > 2048 || len(in.OAuthClient.ClientSecret) > 16384 {
		return Response{}, errors.New("provide an OAuth client and registered daemon /plugins/oauth/callback URL")
	}
	if oauth == nil || !oauth.Configurable {
		return Response{}, errors.New("OAuth is not supported for this plugin")
	}
	if oauth.PublisherClientID && in.OAuthClient.ClientID == "" {
		return Response{}, errors.New("this service requires a publisher-owned client ID")
	}
	if oauth.Source == clientOperator && in.OAuthClient.ClientID != "" {
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

// acceptsRedirect reports whether the service takes this client with a return
// address other than a registered daemon callback.
func (o *oauthSpec) acceptsRedirect(c *OAuthClientConfig) bool {
	return o != nil && o.ExtraRedirect != nil && c.RedirectURL == o.ExtraRedirect.URL && (!o.ExtraRedirect.PublicOnly || c.ClientSecret == "")
}
func (m *Manager) oauthConfigured() []string {
	configured := []string{}
	for _, e := range integrations {
		if e.OAuth == nil || !e.OAuth.Configurable {
			continue
		}
		if _, ok, err := m.vault.Get("oauth-client:" + e.ID); err == nil && ok {
			configured = append(configured, e.ID)
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
	e := lookup(in.Integration)
	var oauth *oauthSpec
	if e != nil {
		oauth = e.OAuth
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
	// A loopback service returns to this daemon's own port, whatever device the
	// person signs in on.
	loopback := oauth != nil && oauth.Loopback
	if loopback {
		redirect, err := m.loopbackRedirect()
		if err != nil {
			return Response{}, err
		}
		client, ok, callback = OAuthClientConfig{RedirectURL: redirect}, true, ""
	}
	registers := oauth != nil && (oauth.Source == clientRegistered || oauth.Source == clientOperator)
	if !ok && callback != "" && registers {
		client.RedirectURL = callback
	} else if !ok {
		return Response{}, errors.New("Sign-in is not set up for this service yet.")
	}
	if callback != "" && oauth != nil && oauth.Source == clientRegistered {
		client = OAuthClientConfig{RedirectURL: callback}
	}
	if oauth != nil && oauth.NativeClientID != "" && callback == NativeCallback {
		client.ClientID = oauth.NativeClientID
	}
	// A service that registers only https and loopback returns, never mewla://,
	// gets the app's sign-in through Mewla's https page, which forwards the
	// code and state to mewla://plugins.
	if oauth != nil && oauth.NativeViaRelay && client.RedirectURL == NativeCallback {
		client.RedirectURL = NativeReturnRelay
	}
	// A registered app that returns only to the Mewla app (Slack's publisher
	// app) can't bring a web UI sign-in back; say so before the browser leaves.
	if callback != "" && callback != NativeCallback && appReturn(client.RedirectURL) {
		return Response{}, errors.New(serviceName(in.Integration) + " sign-in returns to the Mewla app. Connect " + serviceName(in.Integration) + " from the Mewla app on your phone.")
	}

	endpoint := in.Endpoint
	switch {
	case oauth == nil:
		return Response{}, errors.New("OAuth is not supported for this plugin")
	case e.MCP != nil:
		endpoint = e.MCP.Endpoint
	case oauth.Static != nil:
		endpoint = e.Endpoint
	case !e.Custom:
		return Response{}, errors.New("OAuth is not supported for this plugin")
	}
	if len(in.TrustedNetworks) > 0 && !e.Custom {
		return Response{}, errors.New("internal network trust is only available for custom MCP")
	}
	if _, err := trustedEndpointURL(endpoint, in.TrustedNetworks); err != nil {
		return Response{}, err
	}
	if e.Custom && client.ClientID != "" && client.ResourceURL != endpoint {
		return Response{}, errors.New("this OAuth client belongs to another MCP endpoint; configure the matching client first")
	}
	r := &record{Account: Account{ID: uuid.NewString(), Integration: in.Integration, Name: strings.TrimSpace(in.Name), Endpoint: endpoint, TrustedNetworks: append([]string(nil), in.TrustedNetworks...), Enabled: true, Status: "authorization_required", AuthMethod: "mcp_oauth", History: []Event{}}, Grants: map[string]string{}}
	var meta oauthMetadata
	if oauth.Static != nil {
		r.Account.AuthMethod = "oauth"
		meta = oauthMetadata{Issuer: oauth.Static.Issuer, AuthorizationEndpoint: oauth.Static.AuthURL, TokenEndpoint: oauth.Static.TokenURL, RegistrationEndpoint: oauth.Static.Registration, AuthMethods: oauth.Static.AuthMethods}
		r.Account.Scopes = oauth.Scopes(in.AllowWrites)
	} else {
		meta, err = m.discoverOAuth(ctx, r)
		if err != nil {
			return Response{}, err
		}
		if oauth.Scopes != nil {
			r.Account.Scopes = oauth.Scopes(in.AllowWrites)
		} else {
			// Ask the provider for its normal read scope. Writes require a separate
			// connect action requesting them, then the usual per-tool grant.
			for _, scope := range meta.ScopesSupported {
				if scope == "read" || scope == "openid" || scope == "email" || in.AllowWrites && scope == "write" {
					r.Account.Scopes = append(r.Account.Scopes, scope)
				}
			}
		}
	}
	style := oauth2.AuthStyleInParams
	if client.ClientSecret != "" && contains(meta.AuthMethods, "client_secret_basic") {
		style = oauth2.AuthStyleInHeader
	}
	if loopback {
		client.ClientID = m.savedRegistration(in.Integration, client.RedirectURL)
	}
	if client.ClientID == "" {
		if meta.RegistrationEndpoint == "" {
			if e.Custom {
				return Response{}, errors.New("this custom service requires a client registered by its operator")
			}
			return Response{}, errors.New("Mewla's authorization for this service is not ready. No account was connected.")
		}
		registration := map[string]any{"client_name": "Mewla", "redirect_uris": []string{client.RedirectURL}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"}
		if nativeRedirect(client.RedirectURL) {
			registration["application_type"] = "native"
		}
		var registered struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			AuthMethod   string `json:"token_endpoint_auth_method"`
		}
		// Consent pages show the branding. A server that refuses one of these
		// fields gets the registration without them, once.
		branded := map[string]any{}
		for key, value := range registration {
			branded[key] = value
		}
		for key, value := range registrationBranding {
			branded[key] = value
		}
		body, _ := json.Marshal(branded)
		err := m.oauthJSON(ctx, r, "POST", meta.RegistrationEndpoint, body, &registered)
		if errors.Is(err, errOAuthRejected) {
			body, _ = json.Marshal(registration)
			err = m.oauthJSON(ctx, r, "POST", meta.RegistrationEndpoint, body, &registered)
		}
		if err != nil {
			return Response{}, errors.New("OAuth dynamic registration failed; this provider may require a registered client")
		}
		if registered.ClientID == "" || len(registered.ClientID) > 2048 || len(registered.ClientSecret) > 16384 {
			return Response{}, errors.New("provider returned an invalid OAuth client")
		}
		client.ClientID, client.ClientSecret = registered.ClientID, registered.ClientSecret
		if loopback && registered.ClientSecret == "" {
			m.saveRegistration(in.Integration, client.RedirectURL, client.ClientID)
		}
		style = oauth2.AuthStyleInParams
		if registered.AuthMethod == "client_secret_basic" || registered.AuthMethod == "" && registered.ClientSecret != "" {
			style = oauth2.AuthStyleInHeader
		}
	}
	r.OAuth = &oauthAccount{ClientID: client.ClientID, RedirectURL: client.RedirectURL, Issuer: meta.Issuer, AuthURL: meta.AuthorizationEndpoint, TokenURL: meta.TokenEndpoint, RevokeURL: meta.RevocationEndpoint, AuthStyle: style}
	if r.Account.AuthMethod == "mcp_oauth" {
		r.OAuth.Resource = endpoint
	} else if oauth.Static != nil {
		r.OAuth.Resource = oauth.Static.Resource
	}
	state, verifier := oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
	options := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier)}
	if r.OAuth.Resource != "" {
		options = append(options, oauth2.SetAuthURLParam("resource", r.OAuth.Resource))
	}
	config := oauthConfig(r, client.ClientSecret)
	if oauth.Static != nil && oauth.Static.ScopeParam != "" {
		config.Scopes = nil
		options = append(options, oauth2.SetAuthURLParam(oauth.Static.ScopeParam, strings.Join(r.Account.Scopes, ",")))
	}
	authURL := config.AuthCodeURL(state, options...)
	m.records[r.Account.ID] = r
	lifetime := 10 * time.Minute
	if loopback {
		// The person may be away from the screen the sign-in started on.
		lifetime = oauthLoopbackLifetime
	}
	m.pending[state] = &oauthFlow{ID: r.Account.ID, Verifier: verifier, ClientSecret: client.ClientSecret, Loopback: loopback, Expires: time.Now().Add(lifetime), Mobile: callback == NativeCallback && appReturn(client.RedirectURL), Web: callback != "" && callback != NativeCallback && client.RedirectURL == callback, AllowWrites: callback != "" && in.AllowWrites}
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

// signInFailures name the step a sign-in return failed at. Web returns carry
// the key as plugin_error; native connect_finish shows the message.
var signInFailures = map[string]string{
	"denied":   "Access was not granted. You can connect again whenever you’re ready.",
	"provider": "The service didn’t complete sign-in. Connect again.",
	"issuer":   "Sign-in came back from an unexpected service. Connect again.",
	"token":    "The service didn’t accept the sign-in. Connect again.",
	"tools":    "Signed in, but the service’s tools couldn’t be loaded. Connect again.",
	"storage":  "Signed in, but this server couldn’t save the account. Connect again.",
}

// signInDetail is the log detail for a failed step. Vendor bodies can hold
// credentials, so a token endpoint error contributes only its status and code.
func signInDetail(err error) string {
	var retrieve *oauth2.RetrieveError
	var transport *url.Error
	switch {
	case err == nil:
		return "invalid response"
	case errors.As(err, &retrieve):
		status := 0
		if retrieve.Response != nil {
			status = retrieve.Response.StatusCode
		}
		return fmt.Sprintf("HTTP %d %s", status, oauthCode(retrieve.ErrorCode))
	case errors.As(err, &transport):
		return transport.Err.Error()
	}
	return err.Error()
}

// oauthCode keeps a vendor-supplied OAuth error code loggable.
func oauthCode(code string) string {
	if len(code) > 64 || strings.Trim(code, "abcdefghijklmnopqrstuvwxyz_") != "" {
		return "unrecognized error"
	}
	return code
}

// finishOAuth completes a returned authorization. It returns "" on success
// or the signInFailures key for the step that failed.
func (m *Manager) finishOAuth(w http.ResponseWriter, req *http.Request, ctx context.Context, mobile, webUI bool) string {
	state := req.URL.Query().Get("state")
	flow, ok := m.pending[state]
	if !ok || flow.Mobile != mobile && !flow.Loopback || time.Now().After(flow.Expires) {
		if webUI && !mobile {
			pluginsReturn(w, req, "", url.Values{"plugin_error": {"expired"}})
			return "expired"
		}
		http.Error(w, "Authorization expired. Return to Plugins and connect again.", 400)
		return "expired"
	}
	delete(m.pending, state)
	r, ok := m.records[flow.ID]
	if !ok || !r.Account.Enabled || r.Account.Status == "disconnected" {
		if flow.Web {
			pluginsReturn(w, req, "", url.Values{"plugin_error": {"cancelled"}})
			return "cancelled"
		}
		http.Error(w, "Account authorization cancelled", 400)
		return "cancelled"
	}
	pendingID := r.Account.ID
	fail := func(cause, detail string) string {
		// Never remove an established account if a verified reconnect reused its ID.
		if r.Account.ID == pendingID {
			delete(m.records, pendingID)
			_ = m.vault.Delete("integration:" + pendingID)
		}
		_ = m.save()
		log.Printf("plugins: %s sign-in failed at %s: %s", r.Account.Integration, cause, detail)
		if flow.Web {
			pluginsReturn(w, req, r.Account.Integration, url.Values{"plugin_error": {cause}})
			return cause
		}
		http.Error(w, signInFailures[cause], 400)
		return cause
	}
	if code := req.URL.Query().Get("error"); code == "access_denied" {
		return fail("denied", code)
	} else if code != "" || req.URL.Query().Get("code") == "" {
		return fail("provider", "returned "+oauthCode(code))
	}
	if issuer := req.URL.Query().Get("iss"); issuer != "" && issuer != r.OAuth.Issuer {
		return fail("issuer", "iss does not match the discovered issuer")
	}
	config := oauthConfig(r, flow.ClientSecret)
	options := []oauth2.AuthCodeOption{oauth2.VerifierOption(flow.Verifier)}
	if r.OAuth.Resource != "" {
		options = append(options, oauth2.SetAuthURLParam("resource", r.OAuth.Resource))
	}
	var token *oauth2.Token
	var err error
	if codec := codecFor(r); codec != nil {
		token, err = codec.exchange(m, ctx, r, flow.ClientSecret, req.URL.Query().Get("code"), flow.Verifier)
	} else {
		token, err = config.Exchange(context.WithValue(ctx, oauth2.HTTPClient, m.oauthHTTP(r)), req.URL.Query().Get("code"), options...)
	}
	if err != nil || token == nil || token.AccessToken == "" || len(token.AccessToken) > 16384 || len(token.RefreshToken) > 16384 {
		var retrieve *oauth2.RetrieveError
		if flow.Loopback && errors.As(err, &retrieve) && retrieve.ErrorCode == "invalid_client" {
			// The provider no longer knows the client Mewla registered.
			m.forgetRegistration(r.Account.Integration)
		}
		return fail("token", signInDetail(err))
	}
	if scope, ok := token.Extra("scope").(string); ok {
		r.Account.Scopes = strings.Fields(scope)
	}
	secret := oauthSecret{Token: token, ClientSecret: flow.ClientSecret}
	raw, _ := json.Marshal(secret)
	if err = m.vault.Set("integration:"+r.Account.ID, string(raw)); err != nil {
		return fail("storage", "credential could not be stored")
	}
	if err = m.discover(ctx, r, token.AccessToken); err != nil {
		return fail("tools", signInDetail(err))
	}
	m.setGroup(r, "read", true)
	if flow.AllowWrites {
		m.setGroup(r, "write", true)
	}
	m.nameAndDeduplicate(r)
	m.event(r, "authorize", nil)
	if err = m.save(); err != nil {
		return fail("storage", err.Error())
	}
	if flow.Web {
		pluginsReturn(w, req, r.Account.Integration, url.Values{"connected": {r.Account.ID}})
		return ""
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><meta name="viewport" content="width=device-width"><title>Mewla</title><h1>Account connected</h1><p>You can return to Mewla.</p><a href="mewla://plugins">Open Plugins</a>`)
	return ""
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
	client := m.oauthHTTP(r)
	if r.OAuth.Resource != "" {
		// oauth2 v0.34 does not expose refresh AuthCodeOptions. Keep its token
		// lifecycle and client authentication, adding RFC8707's resource only to
		// this account's exact token endpoint and refresh grant.
		client.Transport = oauthRefreshResourceTransport{client.Transport, r.OAuth.TokenURL, r.OAuth.Resource}
	}
	var token *oauth2.Token
	if codec := codecFor(r); codec != nil {
		token, err = codec.refresh(m, ctx, r, secret.Token.RefreshToken)
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
	if codec := codecFor(r); codec != nil {
		return codec.revoke(m, ctx, r, secret.Token.AccessToken)
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
		return errOAuthRejected
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

// ---- loopback sign-in ---------------------------------------------------

// oauthLoopbackLifetime is how long a loopback sign-in waits for its return.
const oauthLoopbackLifetime = time.Hour

// oauthRegistrationLife is how long Mewla keeps a client it registered. Providers
// expire these (AWS after 90 days); a stale one fails in the browser, where this
// daemon can't see it, so it is replaced well before.
const oauthRegistrationLife = 80 * 24 * time.Hour

// SetLoopbackPort tells the manager the port this daemon serves on. A service
// that takes only loopback returns sends the browser to 127.0.0.1 on it.
func (m *Manager) SetLoopbackPort(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loopbackPort = port
}

// loopbackRedirect is the address a loopback provider returns the browser to.
func (m *Manager) loopbackRedirect() (string, error) {
	if m.loopbackPort <= 0 {
		return "", errors.New("Sign-in is not ready on this server yet. Try again in a moment.")
	}
	return "http://127.0.0.1:" + strconv.Itoa(m.loopbackPort) + "/plugins/oauth/callback", nil
}

type oauthRegistration struct {
	RedirectURL  string    `json:"redirect_url"`
	ClientID     string    `json:"client_id"`
	RegisteredAt time.Time `json:"registered_at"`
}

// savedRegistration is the client Mewla registered for this return address,
// while it is still fresh. The address carries the port, so two daemons on one
// computer keep separate clients.
func (m *Manager) savedRegistration(integration, redirect string) string {
	raw, ok, err := m.vault.Get("oauth-registration:" + integration)
	if err != nil || !ok {
		return ""
	}
	var saved oauthRegistration
	if json.Unmarshal([]byte(raw), &saved) != nil || saved.RedirectURL != redirect || saved.ClientID == "" || time.Since(saved.RegisteredAt) > oauthRegistrationLife || saved.RegisteredAt.After(time.Now().Add(time.Minute)) {
		return ""
	}
	return saved.ClientID
}

func (m *Manager) saveRegistration(integration, redirect, clientID string) {
	raw, _ := json.Marshal(oauthRegistration{RedirectURL: redirect, ClientID: clientID, RegisteredAt: time.Now().UTC()})
	_ = m.vault.Set("oauth-registration:"+integration, string(raw))
}

func (m *Manager) forgetRegistration(integration string) {
	_ = m.vault.Delete("oauth-registration:" + integration)
}

// loopbackReturn reads the address a person pasted back: the one the browser
// ended on after sign-in. It must be this account's registered return address
// exactly, so no other address can carry a code into the sign-in.
func loopbackReturn(raw, registered string) (url.Values, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 16384 || strings.ContainsAny(raw, " \t\r\n") {
		return nil, false
	}
	if strings.HasPrefix(raw, "127.0.0.1") {
		raw = "http://" + raw
	}
	got, err := url.Parse(raw)
	want, werr := url.Parse(registered)
	if err != nil || werr != nil || got.Scheme != want.Scheme || got.Host != want.Host || got.Path != want.Path || got.User != nil || got.Fragment != "" {
		return nil, false
	}
	q := got.Query()
	if len(q["state"]) != 1 || len(q["code"]) > 1 || len(q["error"]) > 1 || len(q["iss"]) > 1 {
		return nil, false
	}
	return q, true
}
