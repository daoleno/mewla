package connections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/daoleno/zen/daemon/googleauth"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGooglePublisherHandoffKeepsSecretOffDaemon(t *testing.T) {
	old := GoogleExchangeOrigin
	GoogleExchangeOrigin = "https://zen-owned.example"
	defer func() { GoogleExchangeOrigin = old }()
	m, _ := New(t.TempDir())
	m.networkOwned = false
	var start googleauth.Start
	var key [32]byte
	identityCalls := 0
	m.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var result any
		switch req.URL.Host + req.URL.Path {
		case "zen-owned.example/google/start":
			_ = json.NewDecoder(req.Body).Decode(&start)
			raw, _ := base64.RawURLEncoding.DecodeString(start.PublicKey)
			copy(key[:], raw)
			result = googleauth.Session{ID: "publisher-flow", AuthorizationURL: GoogleExchangeOrigin + "/google/authorize?id=publisher-flow", Expires: time.Now().Add(10 * time.Minute), Status: "waiting"}
		case "zen-owned.example/google/status":
			if req.Header.Get("Authorization") != "Bearer "+start.Nonce {
				t.Fatal("daemon retrieval proof missing")
			}
			sealed, _ := googleauth.Seal(googleauth.Credential{Token: &oauth2.Token{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}, Receipt: "signed-publisher-binding", Proof: "daemon-proof", Nonce: start.Nonce, Scopes: []string{"openid", "email", googleScopeBase + "drive.readonly"}}, start.PublicKey)
			result = googleauth.Session{ID: "publisher-flow", Status: "connected", Ciphertext: sealed}
		case "zen-owned.example/google/cancel":
			result = map[string]bool{"cancelled": true}
		case "zen-owned.example/google/refresh":
			var refresh googleauth.Refresh
			_ = json.NewDecoder(req.Body).Decode(&refresh)
			if refresh.RefreshToken != "refresh" || refresh.Receipt != "signed-publisher-binding" || refresh.Proof != "daemon-proof" {
				t.Fatal("refresh ownership lost")
			}
			sealed, _ := googleauth.Seal(googleauth.Credential{Token: &oauth2.Token{AccessToken: "new-access", RefreshToken: "refresh", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}, Receipt: "new-signed-binding", Proof: "daemon-proof", Nonce: start.Nonce, Scopes: []string{"openid", "email", googleScopeBase + "drive.readonly"}}, start.PublicKey)
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
	begun := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "google"}})
	connected := mustHandle(t, m, Request{Action: "connect_status", FlowID: begun.Flow.ID})
	if connected.Account == nil || connected.Account.Identity != "person@example.test" || identityCalls != 1 {
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
