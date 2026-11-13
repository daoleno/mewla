package connections

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// An OAuth account whose stored token has expired, refreshed through a
// token endpoint that answers as `answer` says.
func expiredOAuthAccount(t *testing.T, integration string, answer func(*http.Request) (*http.Response, error)) (*Manager, *record) {
	t.Helper()
	m := testManager(t, answer)
	r := &record{Account: Account{ID: "acct", Integration: integration, Name: "daoleno", Enabled: true, Status: "connected", AuthMethod: "oauth"}, OAuth: &oauthAccount{ClientID: "client", TokenURL: "https://auth.example/token", AuthURL: "https://auth.example/authorize"}, Grants: map[string]string{}}
	m.records[r.Account.ID] = r
	raw, _ := json.Marshal(oauthSecret{Token: &oauth2.Token{AccessToken: "old-access", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Minute)}})
	if err := m.vault.Set("integration:acct", string(raw)); err != nil {
		t.Fatal(err)
	}
	return m, r
}

func answer(status int, body string) func(*http.Request) (*http.Response, error) {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
}

// Owner 11-14: GitHub (daoleno) read "Sign-in expired" while its token
// worked: a refresh that never got an answer marked the account expired.
// Only the service refusing the refresh does now.
func TestRefreshWithoutAnAnswerKeepsTheSignIn(t *testing.T) {
	for name, c := range map[string]struct {
		integration string
		answer      func(*http.Request) (*http.Response, error)
		expired     bool
	}{
		"connection dropped": {"github", func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection closed by the other side")
		}, false},
		"service unavailable":         {"github", answer(503, `{"error":"temporarily_unavailable"}`), false},
		"refused: invalid_grant":      {"github", answer(400, `{"error":"invalid_grant"}`), true},
		"slack connection dropped":    {"slack", func(*http.Request) (*http.Response, error) { return nil, errors.New("connection reset") }, false},
		"slack 502":                   {"slack", answer(502, `upstream`), false},
		"slack invalid_refresh_token": {"slack", answer(200, `{"ok":false,"error":"invalid_refresh_token"}`), true},
	} {
		m, r := expiredOAuthAccount(t, c.integration, c.answer)
		_, err := m.accountToken(context.Background(), r)
		if err == nil {
			t.Fatalf("%s: refresh succeeded", name)
		}
		if got := r.Account.Status == "authorization_required"; got != c.expired {
			t.Errorf("%s: marked expired %v, want %v (status %q, err %v)", name, got, c.expired, r.Account.Status, err)
		}
		if !c.expired && !errors.Is(err, errNetwork) {
			t.Errorf("%s: a refresh with no answer returned %v, not a network failure", name, err)
		}
	}
}

// A routine read that the service answers with this account's sign-in
// proves the sign-in works: an expiry it was marked with is over.
func TestRoutineReadThatSucceedsClearsAnExpiry(t *testing.T) {
	m := testManager(t, nil)
	r := &record{Account: Account{ID: "acct", Integration: "github", Enabled: true, Status: "authorization_required"}, Grants: map[string]string{}}
	m.records[r.Account.ID] = r
	if _, err := m.finishMCP("acct", "get_me", nil, mcpOutcome{Result: json.RawMessage(`{"login":"daoleno"}`)}, false); err != nil {
		t.Fatal(err)
	}
	if r.Account.Status != "connected" {
		t.Fatalf("status after a successful read: %q", r.Account.Status)
	}
	// A failed read leaves it as it was.
	r.Account.Status = "authorization_required"
	_, _ = m.finishMCP("acct", "get_me", nil, mcpOutcome{Err: errNetwork}, false)
	if r.Account.Status != "authorization_required" {
		t.Fatalf("status after a failed read: %q", r.Account.Status)
	}
}
