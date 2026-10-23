package connections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/daoleno/mewla/daemon/googleauth"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGooglePublisherHandoffKeepsSecretOffDaemon(t *testing.T) {
	old := GoogleExchangeOrigin
	GoogleExchangeOrigin = "https://mewla-owned.example"
	defer func() { GoogleExchangeOrigin = old }()
	m, _ := New(t.TempDir())
	m.networkOwned = false
	var start googleauth.Start
	var key [32]byte
	identityCalls := 0
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var result any
		switch req.URL.Host + req.URL.Path {
		case "mewla-owned.example/google/start":
			_ = json.NewDecoder(req.Body).Decode(&start)
			raw, _ := base64.RawURLEncoding.DecodeString(start.PublicKey)
			copy(key[:], raw)
			result = googleauth.Session{ID: "publisher-flow", AuthorizationURL: GoogleExchangeOrigin + "/google/authorize?id=publisher-flow", Expires: time.Now().Add(10 * time.Minute), Status: "waiting"}
		case "mewla-owned.example/google/status":
			if req.Header.Get("Authorization") != "Bearer "+start.Nonce {
				t.Fatal("daemon retrieval proof missing")
			}
			sealed, _ := googleauth.Seal(googleauth.Credential{Token: &oauth2.Token{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}, Receipt: "signed-publisher-binding", Proof: "daemon-proof", Nonce: start.Nonce, Scopes: []string{"openid", "email", googleScopeBase + "gmail.readonly"}}, start.PublicKey)
			result = googleauth.Session{ID: "publisher-flow", Status: "connected", Ciphertext: sealed}
		case "mewla-owned.example/google/cancel":
			result = map[string]bool{"cancelled": true}
		case "mewla-owned.example/google/refresh":
			var refresh googleauth.Refresh
			_ = json.NewDecoder(req.Body).Decode(&refresh)
			if refresh.RefreshToken != "refresh" || refresh.Receipt != "signed-publisher-binding" || refresh.Proof != "daemon-proof" {
				t.Fatal("refresh ownership lost")
			}
			sealed, _ := googleauth.Seal(googleauth.Credential{Token: &oauth2.Token{AccessToken: "new-access", RefreshToken: "refresh", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}, Receipt: "new-signed-binding", Proof: "daemon-proof", Nonce: start.Nonce, Scopes: []string{"openid", "email", googleScopeBase + "gmail.readonly"}}, start.PublicKey)
			result = googleauth.Session{Status: "connected", Ciphertext: sealed}
		case "openidconnect.googleapis.com/v1/userinfo":
			identityCalls++
			result = map[string]string{"sub": "123", "email": "person@example.test"}
		case "oauth2.googleapis.com/revoke":
			result = map[string]any{}
		default:
			t.Fatalf("unexpected request %s", req.URL)
		}
		raw, _ := json.Marshal(result)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}
	begun := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "gmail"}})
	if start.Service != googleauth.Gmail || start.Writes {
		t.Fatalf("the exchange was asked for %q (writes %t)", start.Service, start.Writes)
	}
	connected := mustHandle(t, m, Request{Action: "connect_status", FlowID: begun.Flow.ID})
	if connected.Account == nil || connected.Account.Integration != "gmail" || connected.Account.Identity != "person@example.test" || identityCalls != 1 {
		t.Fatal("handoff did not verify actual identity")
	}
	raw, _, _ := m.vault.Get("integration:" + connected.Account.ID)
	var secret oauthSecret
	_ = json.Unmarshal([]byte(raw), &secret)
	if secret.ClientSecret != "" || secret.Exchange == nil {
		t.Fatal("publisher secret copied into daemon")
	}
	secret.Token.Expiry = time.Now().Add(-time.Minute)
	rawBytes, _ := json.Marshal(secret)
	_ = m.vault.Set("integration:"+connected.Account.ID, string(rawBytes))
	token, err := m.accountToken(context.Background(), m.records[connected.Account.ID])
	if err != nil || token != "new-access" {
		t.Fatal("bound refresh failed", err)
	}
	mustHandle(t, m, Request{Action: "disconnect", ID: connected.Account.ID})
	if _, err = m.Handle(context.Background(), Request{Action: "invoke", ID: connected.Account.ID, Tool: "get_me"}); err == nil {
		t.Fatal("disconnected Google executed")
	}
}

// A web UI on any address signs in to Google: the exchange is polled, so no
// browser return has to reach this daemon.
func TestGoogleSignInIgnoresTheWebOrigin(t *testing.T) {
	old := GoogleExchangeOrigin
	GoogleExchangeOrigin = "https://mewla-owned.example"
	defer func() { GoogleExchangeOrigin = old }()
	m, _ := New(t.TempDir())
	m.networkOwned = false
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		raw, _ := json.Marshal(googleauth.Session{ID: "flow", AuthorizationURL: GoogleExchangeOrigin + "/google/authorize?id=flow", Expires: time.Now().Add(10 * time.Minute), Status: "waiting"})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}
	response := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "google-calendar", WebOrigin: "http://192.168.1.20:9876"}})
	if response.Flow == nil || response.Flow.Status != "waiting" || response.Flow.WebReturn {
		t.Fatalf("flow: %+v", response.Flow)
	}
}

// The browser can close after approval but before the daemon has collected the
// result. A cancel then must not throw the finished sign-in away.
func TestLateCancelKeepsACompletedGoogleSignIn(t *testing.T) {
	old := GoogleExchangeOrigin
	GoogleExchangeOrigin = "https://mewla-owned.example"
	defer func() { GoogleExchangeOrigin = old }()
	m, _ := New(t.TempDir())
	m.networkOwned = false
	var start googleauth.Start
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var result any
		switch req.URL.Host + req.URL.Path {
		case "mewla-owned.example/google/start":
			_ = json.NewDecoder(req.Body).Decode(&start)
			result = googleauth.Session{ID: "flow", AuthorizationURL: GoogleExchangeOrigin + "/google/authorize?id=flow", Expires: time.Now().Add(10 * time.Minute), Status: "waiting"}
		case "mewla-owned.example/google/status":
			sealed, _ := googleauth.Seal(googleauth.Credential{Token: &oauth2.Token{AccessToken: "ya29.late-cancel-access", RefreshToken: "1//late-cancel-refresh", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}, Receipt: "signed-binding", Proof: "daemon-proof", Nonce: start.Nonce, Scopes: googleauth.Scopes(googleauth.Calendar, false)}, start.PublicKey)
			result = googleauth.Session{ID: "flow", Status: "connected", Ciphertext: sealed}
		case "mewla-owned.example/google/cancel":
			result = map[string]bool{"cancelled": true}
		case "openidconnect.googleapis.com/v1/userinfo":
			result = map[string]string{"sub": "123", "email": "person@example.test"}
		default:
			t.Fatalf("unexpected request %s", req.URL)
		}
		raw, _ := json.Marshal(result)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}
	begun := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "google-calendar"}})
	late := mustHandle(t, m, Request{Action: "connect_cancel", FlowID: begun.Flow.ID})
	if late.Flow == nil || late.Flow.Status != "connected" || late.Account == nil || late.Account.Integration != "google-calendar" {
		t.Fatalf("late cancel lost the sign-in: %+v", late)
	}
	if len(mustHandle(t, m, Request{Action: "list"}).Accounts) != 1 {
		t.Fatal("account missing after late cancel")
	}
}
