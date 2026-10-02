package googleauth

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"golang.org/x/crypto/nacl/box"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T) *Exchange {
	t.Helper()
	e, err := New(Config{Origin: "https://zen-owned.example", ClientID: "owned-google-web", ClientSecret: "publisher-secret", ReceiptKey: bytes.Repeat([]byte{1}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func post(e *Exchange, path string, data any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(data)
	req := httptest.NewRequest("POST", path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}
func TestBoundExchangeRefreshAndBrowserSecrecy(t *testing.T) {
	e := fixture(t)
	public, private, _ := box.GenerateKey(rand.Reader)
	other, otherPrivate, _ := box.GenerateKey(rand.Reader)
	nonce := oauth2.GenerateVerifier()
	w := post(e, "/google/start", Start{PublicKey: base64.RawURLEncoding.EncodeToString(public[:]), Nonce: nonce})
	var session Session
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &session) != nil {
		t.Fatal("start failed", w.Body.String())
	}
	flow := e.flows[session.ID]
	e.client = &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		_ = req.ParseForm()
		if req.URL.String() != "https://oauth2.googleapis.com/token" || req.PostForm.Get("client_secret") != "publisher-secret" || req.PostForm.Get("client_id") != "owned-google-web" {
			t.Fatal("client binding lost")
		}
		if req.PostForm.Get("grant_type") == "authorization_code" && (req.PostForm.Get("code_verifier") != flow.verifier || req.PostForm.Get("redirect_uri") != e.config.Origin+CallbackPath) {
			t.Fatal("PKCE or registered callback lost")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"vendor-access","refresh_token":"vendor-refresh","expires_in":3600,"token_type":"Bearer","scope":"openid email"}`))}, nil
	})}
	begin := httptest.NewRecorder()
	e.ServeHTTP(begin, httptest.NewRequest("GET", session.AuthorizationURL, nil))
	location, _ := url.Parse(begin.Header().Get("Location"))
	if location.Host != "accounts.google.com" || location.Query().Get("code_challenge") != oauth2.S256ChallengeFromVerifier(flow.verifier) {
		t.Fatal("official browser entry missing PKCE")
	}
	callback := e.config.Origin + CallbackPath + "?state=" + session.ID + "&code=vendor-code"
	bad := httptest.NewRecorder()
	e.ServeHTTP(bad, httptest.NewRequest("GET", callback, nil))
	if bad.Code != 400 {
		t.Fatal("callback without browser binding accepted")
	}
	req := httptest.NewRequest("GET", callback, nil)
	req.AddCookie(begin.Result().Cookies()[0])
	done := httptest.NewRecorder()
	e.ServeHTTP(done, req)
	if done.Code != http.StatusFound || done.Header().Get("Location") != "zen://plugins" {
		t.Fatal(done.Body.String())
	}
	for _, secret := range []string{"vendor-code", "vendor-access", "vendor-refresh", "publisher-secret", nonce, flow.proof} {
		if strings.Contains(done.Body.String(), secret) {
			t.Fatal("browser leaked sensitive value")
		}
	}
	replay := httptest.NewRecorder()
	e.ServeHTTP(replay, req)
	if replay.Code != 400 {
		t.Fatal("callback replay accepted")
	}
	statusURL := "/google/status?id=" + session.ID
	status := httptest.NewRecorder()
	e.ServeHTTP(status, httptest.NewRequest("GET", statusURL, nil))
	if status.Code != 403 {
		t.Fatal("browser flow ID exposed credential")
	}
	req = httptest.NewRequest("GET", statusURL, nil)
	req.Header.Set("Authorization", "Bearer "+nonce)
	status = httptest.NewRecorder()
	e.ServeHTTP(status, req)
	if json.Unmarshal(status.Body.Bytes(), &session) != nil || session.Status != "connected" {
		t.Fatal("completion missing")
	}
	if _, err := Open(session.Ciphertext, other, otherPrivate); err == nil {
		t.Fatal("another daemon decrypted result")
	}
	credential, err := Open(session.Ciphertext, public, private)
	if err != nil || credential.Token.RefreshToken != "vendor-refresh" || credential.Nonce != nonce {
		t.Fatal("original daemon completion failed", err)
	}
	for _, input := range []Refresh{{RefreshToken: "other-account", Receipt: credential.Receipt, Proof: credential.Proof}, {RefreshToken: "vendor-refresh", Receipt: credential.Receipt, Proof: "wrong"}, {RefreshToken: "vendor-refresh", Receipt: credential.Receipt + "x", Proof: credential.Proof}} {
		if post(e, "/google/refresh", input).Code != 403 {
			t.Fatal("unbound refresh accepted")
		}
	}
	refreshed := post(e, "/google/refresh", Refresh{RefreshToken: "vendor-refresh", Receipt: credential.Receipt, Proof: credential.Proof})
	var result Session
	if refreshed.Code != 200 || json.Unmarshal(refreshed.Body.Bytes(), &result) != nil {
		t.Fatal("refresh failed")
	}
	if _, err := Open(result.Ciphertext, public, private); err != nil {
		t.Fatal(err)
	}
	// Host restart retains only its publisher receipt key, not any account token.
	restarted := fixture(t)
	restarted.client = e.client
	if post(restarted, "/google/refresh", Refresh{RefreshToken: "vendor-refresh", Receipt: credential.Receipt, Proof: credential.Proof}).Code != 200 {
		t.Fatal("stateless bound refresh failed after restart")
	}
	req = httptest.NewRequest("POST", "/google/cancel?id="+session.ID, nil)
	req.Header.Set("Authorization", "Bearer "+nonce)
	cancel := httptest.NewRecorder()
	e.ServeHTTP(cancel, req)
	if len(e.flows) != 0 {
		t.Fatal("cancel retained transient credential")
	}
}
func TestDenialExpiryAndFixedDestination(t *testing.T) {
	e := fixture(t)
	public, _, _ := box.GenerateKey(rand.Reader)
	for _, expired := range []bool{false, true} {
		result := post(e, "/google/start", Start{PublicKey: base64.RawURLEncoding.EncodeToString(public[:]), Nonce: oauth2.GenerateVerifier()})
		var session Session
		_ = json.Unmarshal(result.Body.Bytes(), &session)
		if expired {
			e.flows[session.ID].Expires = time.Now().Add(-time.Second)
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest("GET", session.AuthorizationURL, nil))
			if w.Code != 410 {
				t.Fatal("expired flow accepted")
			}
			continue
		}
		begin := httptest.NewRecorder()
		e.ServeHTTP(begin, httptest.NewRequest("GET", session.AuthorizationURL, nil))
		req := httptest.NewRequest("GET", CallbackPath+"?state="+session.ID+"&error=access_denied", nil)
		req.AddCookie(begin.Result().Cookies()[0])
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		if e.flows[session.ID].Status != "failed" || e.flows[session.ID].Ciphertext != "" {
			t.Fatal("denial produced token")
		}
	}
	if post(e, "/google/start", map[string]any{"public_key": base64.RawURLEncoding.EncodeToString(public[:]), "nonce": oauth2.GenerateVerifier(), "redirect_uri": "https://attacker.test"}).Code != 400 {
		t.Fatal("caller redirect accepted")
	}
}
