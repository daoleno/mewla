package connections

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/googleauth"
	"github.com/google/uuid"
	"golang.org/x/crypto/nacl/box"
	"golang.org/x/oauth2"
)

type googleExchangeSecret struct {
	Public  [32]byte `json:"public"`
	Private [32]byte `json:"private"`
	Receipt string   `json:"receipt"`
	Proof   string   `json:"proof"`
	Nonce   string   `json:"nonce"`
}
type googlePending struct {
	session         googleauth.Session
	origin          string
	public, private *[32]byte
	nonce           string
}

func validExchangeOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}
func (m *Manager) googleExchangeRequest(ctx context.Context, origin, path, proof string, input, out any) error {
	if !validExchangeOrigin(origin) {
		return errors.New("Mewla's Google connection is not configured correctly")
	}
	var body io.Reader
	method := "GET"
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
		method = "POST"
	}
	if strings.HasPrefix(path, "/google/cancel?") {
		method = "POST"
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if proof != "" {
		req.Header.Set("Authorization", "Bearer "+proof)
	}
	response, err := m.oauthHTTP(&record{Account: Account{Endpoint: origin}}).Do(req)
	if err != nil {
		return errors.New("Could not reach Mewla's Google authorization. Try again.")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return errAuth
	}
	if response.StatusCode == 410 {
		return errors.New("This Google connection expired. Connect again.")
	}
	if response.StatusCode != 200 {
		return errors.New("Google authorization is temporarily unavailable. Try again.")
	}
	if out == nil {
		return nil
	}
	if json.NewDecoder(io.LimitReader(response.Body, MaxResultBytes)).Decode(out) != nil {
		return errors.New("Invalid Google authorization response")
	}
	return nil
}
func (m *Manager) startGoogleExchange(ctx context.Context, in *Input) (Response, error) {
	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return Response{}, err
	}
	pending := &googlePending{origin: GoogleExchangeOrigin, public: public, private: private, nonce: oauth2.GenerateVerifier()}
	if err = m.googleExchangeRequest(ctx, pending.origin, "/google/start", "", googleauth.Start{PublicKey: base64.RawURLEncoding.EncodeToString(public[:]), Nonce: pending.nonce, Writes: in.AllowWrites}, &pending.session); err != nil {
		return Response{}, err
	}
	auth, err := url.Parse(pending.session.AuthorizationURL)
	if err != nil || auth.Scheme+"://"+auth.Host != pending.origin || auth.Path != "/google/authorize" || pending.session.ID == "" || time.Until(pending.session.Expires) > 11*time.Minute || time.Now().After(pending.session.Expires) {
		return Response{}, errors.New("Google authorization returned an invalid flow")
	}
	f := &connectFlow{ConnectFlow: ConnectFlow{ID: uuid.NewString(), Integration: "google", Status: "waiting", AuthorizationURL: pending.session.AuthorizationURL, Expires: pending.session.Expires}, google: pending, allowWrites: in.AllowWrites}
	m.connectFlows[f.ID] = f
	return flowResult(f), nil
}
func (m *Manager) pollGoogleExchange(ctx context.Context, f *connectFlow) error {
	var session googleauth.Session
	p := f.google
	if err := m.googleExchangeRequest(ctx, p.origin, "/google/status?id="+url.QueryEscape(p.session.ID), p.nonce, nil, &session); err != nil {
		return err
	}
	if session.ID != p.session.ID {
		return errors.New("Google authorization belongs to another flow")
	}
	if session.Status == "failed" {
		f.Status = "failed"
		f.Message = "Google authorization was declined or could not be verified. Connect again."
		return nil
	}
	if session.Status != "connected" {
		return nil
	}
	credential, err := googleauth.Open(session.Ciphertext, p.public, p.private)
	if err != nil {
		return err
	}
	if credential.Nonce != p.nonce || credential.Token.RefreshToken == "" || credential.Receipt == "" || credential.Proof == "" {
		return errors.New("Google authorization binding is invalid")
	}
	r := &record{Account: Account{ID: uuid.NewString(), Integration: "google", Name: "Google Workspace", Endpoint: "https://www.googleapis.com", Enabled: true, AuthMethod: "oauth", Scopes: credential.Scopes, History: []Event{}}, OAuth: &oauthAccount{GoogleExchange: p.origin, RevokeURL: "https://oauth2.googleapis.com/revoke"}, Grants: map[string]string{}}
	if err = m.discover(ctx, r, credential.Token.AccessToken); err != nil {
		return err
	}
	secret := oauthSecret{Token: credential.Token, Exchange: &googleExchangeSecret{Public: *p.public, Private: *p.private, Nonce: p.nonce, Receipt: credential.Receipt, Proof: credential.Proof}}
	raw, _ := json.Marshal(secret)
	if err = m.vault.Set("integration:"+r.Account.ID, string(raw)); err != nil {
		return errors.New("Google credential could not be saved")
	}
	m.setGroup(r, "read", true)
	if f.allowWrites {
		m.setGroup(r, "write", true)
	}
	m.records[r.Account.ID] = r
	f.AccountID = r.Account.ID
	m.nameAndDeduplicate(r)
	if err = m.save(); err != nil {
		return err
	}
	f.Status = "connected"
	_ = m.googleExchangeRequest(ctx, p.origin, "/google/cancel?id="+url.QueryEscape(p.session.ID), p.nonce, nil, nil)
	f.google = nil
	return nil
}
func (m *Manager) refreshGoogleExchange(ctx context.Context, r *record, secret *oauthSecret) (string, error) {
	binding := secret.Exchange
	var result googleauth.Session
	if err := m.googleExchangeRequest(ctx, r.OAuth.GoogleExchange, "/google/refresh", "", googleauth.Refresh{RefreshToken: secret.Token.RefreshToken, Receipt: binding.Receipt, Proof: binding.Proof}, &result); err != nil {
		if errors.Is(err, errAuth) {
			r.Account.Status = "authorization_required"
			_ = m.save()
		}
		return "", err
	}
	credential, err := googleauth.Open(result.Ciphertext, &binding.Public, &binding.Private)
	if err != nil {
		return "", err
	}
	if credential.Nonce != binding.Nonce || credential.Proof != binding.Proof || credential.Receipt == "" {
		return "", errors.New("Google refresh binding is invalid")
	}
	r.Account.Scopes = credential.Scopes
	if err = m.save(); err != nil {
		return "", err
	}
	secret.Token = credential.Token
	binding.Receipt = credential.Receipt
	raw, _ := json.Marshal(secret)
	if err = m.vault.Set("integration:"+r.Account.ID, string(raw)); err != nil {
		return "", errors.New("Refreshed Google credential could not be saved")
	}
	return secret.Token.AccessToken, nil
}
