package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// This fixed return target is shipped by both native clients. No caller can
// redirect a code/token to a supplied origin. PKCE and the paired channel bind
// native completion to this daemon's original flow.
const NativeCallback = "mewla://plugins"

type ConnectFlow struct {
	ID               string    `json:"id"`
	Integration      string    `json:"integration"`
	Status           string    `json:"status"`
	AuthorizationURL string    `json:"authorization_url,omitempty"`
	UserCode         string    `json:"user_code,omitempty"`
	Identity         string    `json:"identity,omitempty"`
	Message          string    `json:"message,omitempty"`
	Expires          time.Time `json:"expires"`
	AccountID        string    `json:"account_id,omitempty"`
}
type connectFlow struct {
	ConnectFlow
	state       string
	secret      string
	deviceCode  string
	clientID    string
	interval    time.Duration
	nextPoll    time.Time
	allowWrites bool
	google      *googlePending
}

func serviceName(kind string) string {
	for _, item := range Catalog() {
		if item.ID == kind {
			return item.Name
		}
	}
	return "Custom service"
}
func (m *Manager) isPending(id string) bool {
	for _, flow := range m.pending {
		if flow.ID == id {
			return true
		}
	}
	return false
}
func (m *Manager) expireFlows() {
	for state, flow := range m.pending {
		if time.Now().After(flow.Expires) {
			delete(m.records, flow.ID)
			delete(m.pending, state)
		}
	}
	for id, flow := range m.connectFlows {
		if time.Now().After(flow.Expires) {
			delete(m.connectFlows, id)
		}
	}
}
func flowResult(f *connectFlow) Response { copy := f.ConnectFlow; return Response{Flow: &copy} }
func readGitHubToken(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		return "", errors.New("No signed-in GitHub account is available on this server. Use GitHub authorization.")
	}
	token := strings.TrimSpace(string(out))
	if token == "" || len(token) > 16384 {
		return "", errors.New("GitHub account is unavailable")
	}
	return token, nil
}
func (m *Manager) connect(ctx context.Context, q Request) (Response, error) {
	if q.Action == "github_preview" {
		if len(m.connectFlows) >= 20 {
			return Response{}, errors.New("Too many connections in progress. Try again shortly.")
		}
		token, err := m.githubToken(ctx)
		if err != nil {
			return Response{}, err
		}
		r := &record{Account: Account{Integration: "github", Endpoint: "https://api.github.com"}}
		if err = m.discover(ctx, r, token); err != nil {
			return Response{}, err
		}
		f := &connectFlow{ConnectFlow: ConnectFlow{ID: uuid.NewString(), Integration: "github", Status: "confirm", Identity: r.Account.Identity, Expires: time.Now().Add(5 * time.Minute)}, secret: token}
		m.connectFlows[f.ID] = f
		return flowResult(f), nil
	}
	if q.Action == "connect_start" {
		if q.Input == nil {
			return Response{}, errors.New("Choose a service")
		}
		if len(m.connectFlows) >= 20 {
			return Response{}, errors.New("Too many connections in progress. Try again shortly.")
		}
		in := *q.Input
		in.Mobile = true
		if reason := m.unavailableReason(in.Integration); reason != "" {
			return Response{}, errors.New(reason)
		}
		if in.Integration == "google" && GoogleExchangeOrigin != "" {
			return m.startGoogleExchange(ctx, &in)
		}
		if in.Integration == "github" {
			return m.startGitHubDevice(ctx, &in)
		}
		response, err := m.startOAuth(ctx, &in)
		if err != nil {
			return Response{}, err
		}
		var state string
		for key, flow := range m.pending {
			if flow.ID == response.Account.ID {
				state = key
			}
		}
		f := &connectFlow{ConnectFlow: ConnectFlow{ID: uuid.NewString(), Integration: in.Integration, Status: "waiting", AuthorizationURL: response.AuthorizationURL, Expires: m.pending[state].Expires, AccountID: response.Account.ID}, state: state}
		m.connectFlows[f.ID] = f
		return flowResult(f), nil
	}
	f, ok := m.connectFlows[q.FlowID]
	if !ok {
		return Response{}, errors.New("This connection expired. Connect again.")
	}
	if q.Action == "connect_cancel" {
		if r := m.records[f.AccountID]; r != nil && r.Account.Status == "connected" && !m.isPending(r.Account.ID) {
			return m.connectedFlow(f)
		}
		if f.google != nil {
			_ = m.googleExchangeRequest(ctx, f.google.origin, "/google/cancel?id="+url.QueryEscape(f.google.session.ID), f.google.nonce, nil, nil)
		}
		if pending, ok := m.pending[f.state]; ok {
			delete(m.records, pending.ID)
			delete(m.pending, f.state)
		}
		// A completed consent is never disconnected by a late cancel.
		f.secret = ""
		f.deviceCode = ""
		f.Status = "cancelled"
		f.AuthorizationURL = ""
		return flowResult(f), nil
	}
	if q.Action == "github_import" {
		if f.Status == "connected" {
			return m.connectedFlow(f)
		}
		if f.Status != "confirm" || f.Integration != "github" {
			return Response{}, errors.New("Preview this GitHub identity before connecting")
		}
		r := &record{Account: Account{Integration: "github", Endpoint: "https://api.github.com"}}
		if err := m.discover(ctx, r, f.secret); err != nil {
			return Response{}, err
		}
		if r.Account.Identity != f.Identity {
			return Response{}, errors.New("GitHub identity changed. Review the account again.")
		}
		for _, existing := range m.records {
			if existing.Account.Integration == "github" && existing.Account.Identity == f.Identity && existing.Account.Status != "disconnected" {
				if err := m.vault.Set("integration:"+existing.Account.ID, f.secret); err != nil {
					return Response{}, errors.New("GitHub credential could not be saved")
				}
				existing.Account.Tools = r.Account.Tools
				existing.Account.Status = "connected"
				existing.Account.Enabled = true
				existing.Account.VerifiedAt = r.Account.VerifiedAt
				existing.OAuth = nil
				m.setGroup(existing, "read", true)
				if q.Input != nil && q.Input.AllowWrites {
					m.setGroup(existing, "write", true)
				}
				if err := m.save(); err != nil {
					return Response{}, err
				}
				f.AccountID = existing.Account.ID
				f.Status = "connected"
				f.secret = ""
				return m.connectedFlow(f)
			}
		}
		result, err := m.add(ctx, &Input{Integration: "github", Name: f.Identity, Credential: f.secret})
		if err != nil {
			return Response{}, err
		}
		if q.Input != nil && q.Input.AllowWrites {
			m.setGroup(m.records[result.Account.ID], "write", true)
			if err = m.save(); err != nil {
				return Response{}, err
			}
		}
		f.AccountID = result.Account.ID
		f.Status = "connected"
		f.secret = ""
		return m.connectedFlow(f)
	}
	if q.Action == "connect_finish" {
		if f.Status == "connected" {
			return m.connectedFlow(f)
		}
		if f.Status != "waiting" || f.state == "" {
			return Response{}, errors.New("This connection is no longer waiting for authorization")
		}
		callback, err := url.Parse(q.Callback)
		if err != nil || len(q.Callback) > 16384 || callback.Scheme != "mewla" || callback.Host != "plugins" || callback.Path != "" || callback.Fragment != "" || callback.User != nil || callback.Query().Get("state") != f.state || len(callback.Query()["state"]) != 1 || len(callback.Query()["code"]) > 1 || len(callback.Query()["error"]) > 1 || len(callback.Query()["iss"]) > 1 {
			return Response{}, errors.New("Authorization return does not match this connection")
		}
		req := httptest.NewRequest(http.MethodGet, "https://native.invalid/?"+callback.RawQuery, nil)
		w := httptest.NewRecorder()
		m.finishOAuth(w, req, ctx, true)
		if w.Code != http.StatusOK {
			f.Status = "failed"
			f.Message = "Authorization was declined or could not be verified. Connect again."
			if callback.Query().Get("error") == "access_denied" {
				f.Message = "Access was not granted. You can connect again whenever you’re ready."
			}
			return flowResult(f), nil
		}
	}
	if f.google != nil && f.Status == "waiting" {
		if err := m.pollGoogleExchange(ctx, f); err != nil {
			return Response{}, err
		}
	}
	if f.deviceCode != "" && f.Status == "waiting" {
		if err := m.pollGitHubDevice(ctx, f); err != nil {
			return Response{}, err
		}
	}
	return m.connectedFlow(f)
}
func (m *Manager) connectedFlow(f *connectFlow) (Response, error) {
	if f.state != "" && f.Status == "waiting" {
		if _, ok := m.pending[f.state]; !ok {
			if r := m.records[f.AccountID]; r == nil {
				f.Status = "failed"
				f.Message = "Authorization was declined or expired. Connect again."
			}
		}
	}
	result := flowResult(f)
	if r, ok := m.records[f.AccountID]; ok && r.Account.Status == "connected" && !m.isPending(r.Account.ID) {
		f.Status = "connected"
		f.AuthorizationURL = ""
		result = flowResult(f)
		a := m.projection(r)
		result.Account = &a
	}
	return result, nil
}
func (m *Manager) nameAndDeduplicate(r *record) {
	if r.Account.Identity != "" && !strings.Contains(r.Account.Identity, "identity not provided") {
		r.Account.Name = r.Account.Identity
	}
	if r.Account.Identity == "" || strings.Contains(r.Account.Identity, "identity not provided") {
		return
	}
	native := false
	for _, f := range m.connectFlows {
		if f.AccountID == r.Account.ID {
			native = true
		}
	}
	if !native {
		return
	}
	for id, existing := range m.records {
		if existing == r || existing.Account.Status == "disconnected" || existing.Account.Integration != r.Account.Integration || existing.Account.Identity != r.Account.Identity {
			continue
		}
		// Identity has been reverified by the provider. Keep stable account IDs,
		// replacing credentials only after successful consent and discovery.
		raw, ok, err := m.vault.Get("integration:" + r.Account.ID)
		if err != nil || !ok {
			return
		}
		if m.vault.Set("integration:"+id, raw) != nil {
			return
		}
		oldID := r.Account.ID
		_ = m.vault.Delete("integration:" + oldID)
		r.Account.ID = id
		m.records[id] = r
		delete(m.records, oldID)
		for _, f := range m.connectFlows {
			if f.AccountID == oldID {
				f.AccountID = id
			}
		}
		return
	}
}
func (m *Manager) startGitHubDevice(ctx context.Context, in *Input) (Response, error) {
	client, ok, err := m.clientConfig("github")
	if err != nil || !ok || client.ClientID == "" {
		return Response{}, errors.New("Mewla’s GitHub browser authorization is not ready yet. You can use the account already signed in on this server.")
	}
	config := oauth2.Config{ClientID: client.ClientID, Scopes: []string{"read:user", "repo"}, Endpoint: oauth2.Endpoint{DeviceAuthURL: "https://github.com/login/device/code"}}
	r := &record{Account: Account{Endpoint: "https://api.github.com"}}
	auth, err := config.DeviceAuth(context.WithValue(ctx, oauth2.HTTPClient, m.oauthHTTP(r)))
	if err != nil {
		return Response{}, errors.New("GitHub authorization could not start. Try again.")
	}
	if auth.VerificationURI != "https://github.com/login/device" {
		return Response{}, errors.New("GitHub returned an unexpected authorization destination")
	}
	interval := time.Duration(auth.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	f := &connectFlow{ConnectFlow: ConnectFlow{ID: uuid.NewString(), Integration: "github", Status: "waiting", AuthorizationURL: auth.VerificationURI, UserCode: auth.UserCode, Expires: auth.Expiry}, deviceCode: auth.DeviceCode, clientID: client.ClientID, interval: interval, nextPoll: time.Now().Add(interval), allowWrites: in.AllowWrites}
	m.connectFlows[f.ID] = f
	return flowResult(f), nil
}
func (m *Manager) pollGitHubDevice(ctx context.Context, f *connectFlow) error {
	if time.Now().Before(f.nextPoll) {
		return nil
	}
	f.nextPoll = time.Now().Add(f.interval)
	r := &record{Account: Account{Endpoint: "https://api.github.com"}}
	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
	}
	if err := m.oauthForm(ctx, r, "https://github.com/login/oauth/access_token", url.Values{"client_id": {f.clientID}, "device_code": {f.deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, &data); err != nil {
		return errors.New("Could not check GitHub authorization. Try again.")
	}
	switch data.Error {
	case "authorization_pending":
		return nil
	case "slow_down":
		f.interval += 5 * time.Second
		f.nextPoll = time.Now().Add(f.interval)
		return nil
	case "":
	default:
		f.Status = "failed"
		f.Message = "GitHub authorization was declined or expired. Connect again."
		f.deviceCode = ""
		return nil
	}
	if data.AccessToken == "" || len(data.AccessToken) > 16384 || len(data.RefreshToken) > 16384 || strings.ContainsAny(data.AccessToken+data.RefreshToken, "\r\n") || data.ExpiresIn < 0 || data.ExpiresIn > 365*24*60*60 || data.ExpiresIn > 0 && data.RefreshToken == "" {
		return errors.New("GitHub returned an invalid credential")
	}
	if len(m.records) >= 50 {
		return errors.New("maximum of 50 accounts reached")
	}
	// Device-issued tokens can expire. Keep the complete lifecycle in the
	// vault, with public-client authentication for secret-free refresh.
	token := &oauth2.Token{AccessToken: data.AccessToken, RefreshToken: data.RefreshToken, TokenType: "Bearer"}
	if data.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(data.ExpiresIn) * time.Second)
	}
	account := &record{Account: Account{ID: uuid.NewString(), Integration: "github", Name: "GitHub", Endpoint: "https://api.github.com", AuthMethod: "oauth", Enabled: true, Scopes: strings.Fields(strings.ReplaceAll(data.Scope, ",", " ")), History: []Event{}}, OAuth: &oauthAccount{ClientID: f.clientID, Issuer: "https://github.com", TokenURL: "https://github.com/login/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams}, Grants: map[string]string{}}
	// Verify identity before creating an account or persisting any credential.
	if err := m.discover(ctx, account, token.AccessToken); err != nil {
		return err
	}
	public, _ := json.Marshal(account.Account)
	for _, credential := range []string{token.AccessToken, token.RefreshToken} {
		if credential != "" && bytes.Contains(public, []byte(credential)) {
			return errors.New("service echoed credential in account metadata")
		}
	}
	m.setGroup(account, "read", true)
	if f.allowWrites {
		m.setGroup(account, "write", true)
	}
	var previous *record
	if account.Account.Identity != "" && !strings.Contains(account.Account.Identity, "identity not provided") {
		account.Account.Name = account.Account.Identity
		for _, existing := range m.records {
			if existing.Account.Integration == "github" && existing.Account.Status != "disconnected" && existing.Account.Identity == account.Account.Identity {
				account.Account.ID = existing.Account.ID
				previous = existing
				break
			}
		}
	}
	ref := "integration:" + account.Account.ID
	oldRaw, oldOK, err := m.vault.Get(ref)
	if err != nil {
		return errors.New("credential store unavailable")
	}
	raw, _ := json.Marshal(oauthSecret{Token: token})
	if err := m.vault.Set(ref, string(raw)); err != nil {
		return errors.New("credential could not be stored")
	}
	m.records[account.Account.ID] = account
	if err := m.save(); err != nil {
		delete(m.records, account.Account.ID)
		if previous != nil {
			m.records[account.Account.ID] = previous
		}
		if oldOK {
			_ = m.vault.Set(ref, oldRaw)
		} else {
			_ = m.vault.Delete(ref)
		}
		return err
	}
	f.AccountID = account.Account.ID
	f.Status = "connected"
	f.deviceCode = ""
	return nil
}
func (m *Manager) oauthForm(ctx context.Context, r *record, endpoint string, values url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := m.oauthHTTP(r).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("authorization service rejected the request")
	}
	return json.NewDecoder(http.MaxBytesReader(nil, resp.Body, MaxResultBytes)).Decode(out)
}
func (m *Manager) exchangeSlack(ctx context.Context, r *record, secret, code, verifier string) (*oauth2.Token, error) {
	values := url.Values{"client_id": {r.OAuth.ClientID}, "code": {code}, "redirect_uri": {r.OAuth.RedirectURL}, "code_verifier": {verifier}}
	if secret != "" {
		values.Set("client_secret", secret)
	}
	return m.slackToken(ctx, r, values)
}
func (m *Manager) slackToken(ctx context.Context, r *record, values url.Values) (*oauth2.Token, error) {
	type payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	var data struct {
		payload
		OK         bool    `json:"ok"`
		AuthedUser payload `json:"authed_user"`
	}
	if err := m.oauthForm(ctx, r, r.OAuth.TokenURL, values, &data); err != nil || !data.OK {
		return nil, errors.New("Slack authorization could not be verified")
	}
	value := data.payload
	if values.Get("grant_type") != "refresh_token" {
		value = data.AuthedUser
	}
	if value.AccessToken == "" || len(value.AccessToken) > 16384 || len(value.RefreshToken) > 16384 {
		return nil, errors.New("Slack returned an invalid credential")
	}
	token := &oauth2.Token{AccessToken: value.AccessToken, RefreshToken: value.RefreshToken, TokenType: "Bearer"}
	if value.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(value.ExpiresIn) * time.Second)
	}
	return token.WithExtra(map[string]any{"scope": strings.ReplaceAll(value.Scope, ",", " ")}), nil
}
