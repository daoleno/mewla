package connections

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
)

func TestCatalogReportsPublisherReadiness(t *testing.T) {
	oldGitHub, oldSlack := GitHubPublicClientID, SlackPublicClientID
	GitHubPublicClientID, SlackPublicClientID = "", ""
	defer func() {
		GitHubPublicClientID, SlackPublicClientID = oldGitHub, oldSlack
	}()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	assertAvailability := func(expected map[string]bool) {
		t.Helper()
		result := mustHandle(t, m, Request{Action: "list"})
		if len(result.Catalog) != len(expected) {
			t.Fatalf("catalog: %+v", result.Catalog)
		}
		for _, item := range result.Catalog {
			available, listed := expected[item.ID]
			if !listed || item.Available != available || (item.UnavailableReason == "") != item.Available {
				t.Fatalf("incorrect readiness: %+v", item)
			}
		}
	}
	expected := map[string]bool{"github": false, "slack": false, "notion": true, "linear": true, "onlora": true, "cloudflare": true, "aws": true, "hyperliquid": true, "mcp": true, "openapi": true}
	assertAvailability(expected)
	for _, kind := range []string{"github", "slack"} {
		_, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: kind}})
		if err == nil || !strings.Contains(err.Error(), "sign-in is not set up yet") {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if len(m.pending) != 0 || len(m.connectFlows) != 0 || len(m.records) != 0 {
		t.Fatal("unavailable connection created state")
	}
	GitHubPublicClientID, SlackPublicClientID = "public-github", "public-slack"
	expected["github"], expected["slack"] = true, true
	m.records["existing-github"] = &record{Account: Account{ID: "existing-github", Integration: "github", Enabled: true, Status: "connected"}}
	assertAvailability(expected)
	GitHubPublicClientID, SlackPublicClientID = "", ""
	for _, kind := range []string{"github", "slack"} {
		mustHandle(t, m, Request{Action: "oauth_configure", Input: &Input{Integration: kind, OAuthClient: &OAuthClientConfig{ClientID: "operator", ClientSecret: "private", RedirectURL: "https://daemon.example/plugins/oauth/callback"}}})
	}
	assertAvailability(expected)
	// Existing linked accounts remain accessible when new sign-in is unavailable.
	if len(mustHandle(t, m, Request{Action: "list"}).Accounts) != 1 {
		t.Fatal("existing account hidden")
	}
}

// fal and Image generation were removed: their accounts and stored keys are
// deleted when the daemon starts, and other accounts are untouched.
func TestRetiredIntegrationsAreRemovedOnStart(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	for id, kind := range map[string]string{"fal-1": "fal", "image-1": "image", "github-1": "github"} {
		m.records[id] = &record{Account: Account{ID: id, Integration: kind, Enabled: true, Status: "connected"}, Grants: map[string]string{}}
		if err := m.vault.Set("integration:"+id, "key-"+id); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.records) != 1 || restarted.records["github-1"] == nil {
		t.Fatalf("records after start: %v", restarted.records)
	}
	for id, kept := range map[string]bool{"fal-1": false, "image-1": false, "github-1": true} {
		if _, ok, _ := restarted.vault.Get("integration:" + id); ok != kept {
			t.Fatalf("%s credential kept=%t", id, ok)
		}
	}
	again, err := New(dir)
	if err != nil || len(again.records) != 1 {
		t.Fatalf("removal did not persist: %v", err)
	}
}

// A daemon that still holds the old single Google Workspace account deletes
// it, with its saved key, when it starts.
func TestOldGoogleWorkspaceAccountIsRemovedOnStart(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.records["old"] = &record{Account: Account{ID: "old", Integration: "google", Enabled: true, Status: "connected"}, Grants: map[string]string{}}
	if err := m.vault.Set("integration:old", "token"); err != nil {
		t.Fatal(err)
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.records) != 0 {
		t.Fatalf("records after start: %v", restarted.records)
	}
	if _, ok, _ := restarted.vault.Get("integration:old"); ok {
		t.Fatal("old Google credential kept")
	}
}

// Google Calendar and Gmail were removed. On start their grants are handed
// back to Google and the accounts and stored tokens deleted, even when Google
// can't be reached; other accounts are untouched.
func TestGoogleAccountsAreRevokedAndRemovedOnStart(t *testing.T) {
	for _, reachable := range []bool{true, false} {
		dir := t.TempDir()
		m, err := New(dir)
		if err != nil {
			t.Fatal(err)
		}
		// The stored shape before removal: Google's revoke URL on the account
		// and the exchange binding beside the token.
		for id, kind := range map[string]string{"cal": "google-calendar", "mail": "gmail", "gh": "github"} {
			r := &record{Account: Account{ID: id, Integration: kind, Enabled: true, Status: "connected", AuthMethod: "oauth"}, Grants: map[string]string{}}
			if kind != "github" {
				r.OAuth = &oauthAccount{RevokeURL: "https://oauth2.googleapis.com/revoke"}
			}
			m.records[id] = r
			secret := `{"token":{"access_token":"access-` + id + `","refresh_token":"refresh-` + id + `"},"exchange":{"nonce":"n","receipt":"r","proof":"p"}}`
			if err := m.vault.Set("integration:"+id, secret); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.save(); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(m.path)
		data = []byte(strings.Replace(string(data), `"revoke_url":"https://oauth2.googleapis.com/revoke"`, `"revoke_url":"https://oauth2.googleapis.com/revoke","google_exchange":"https://connect.mewla.ai"`, 1))
		if err := os.WriteFile(m.path, data, 0o600); err != nil || !strings.Contains(string(data), "google_exchange") {
			t.Fatalf("seed: %v", err)
		}
		var revoked []string
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if !reachable {
				return nil, errors.New("offline")
			}
			body, _ := io.ReadAll(req.Body)
			form, _ := url.ParseQuery(string(body))
			if req.Method != "POST" || req.URL.String() != "https://oauth2.googleapis.com/revoke" {
				t.Fatalf("unexpected %s %s", req.Method, req.URL)
			}
			revoked = append(revoked, form.Get("token"))
			return reply(200, `{}`), nil
		})}
		restarted, err := newManager(m.path, m.vault, client)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(revoked)
		if reachable && strings.Join(revoked, ",") != "refresh-cal,refresh-mail" {
			t.Fatalf("revoked %v", revoked)
		}
		if len(restarted.records) != 1 || restarted.records["gh"] == nil {
			t.Fatalf("reachable=%t records after start: %v", reachable, restarted.records)
		}
		for id, kept := range map[string]bool{"cal": false, "mail": false, "gh": true} {
			if _, ok, _ := restarted.vault.Get("integration:" + id); ok != kept {
				t.Fatalf("reachable=%t %s credential kept=%t", reachable, id, ok)
			}
		}
		for _, item := range mustHandle(t, restarted, Request{Action: "list"}).Catalog {
			if item.ID == "google-calendar" || item.ID == "gmail" {
				t.Fatalf("%s still offered", item.ID)
			}
		}
	}
}
