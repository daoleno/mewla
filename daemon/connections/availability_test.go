package connections

import (
	"context"
	"strings"
	"testing"
)

func TestCatalogReportsPublisherReadiness(t *testing.T) {
	oldGitHub, oldSlack, oldGoogle := GitHubPublicClientID, SlackPublicClientID, GoogleExchangeOrigin
	GitHubPublicClientID, SlackPublicClientID, GoogleExchangeOrigin = "", "", ""
	defer func() {
		GitHubPublicClientID, SlackPublicClientID, GoogleExchangeOrigin = oldGitHub, oldSlack, oldGoogle
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
	// Google is listed for no one, even with an account or a configured client.
	expected := map[string]bool{"github": false, "slack": false, "notion": true, "linear": true, "onlora": true, "cloudflare": true, "mcp": true, "openapi": true}
	assertAvailability(expected)
	for _, kind := range []string{"github", "slack", "google"} {
		_, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: kind}})
		if err == nil || !strings.Contains(err.Error(), "sign-in is not set up yet") {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if len(m.pending) != 0 || len(m.connectFlows) != 0 || len(m.records) != 0 {
		t.Fatal("unavailable connection created state")
	}
	GitHubPublicClientID, SlackPublicClientID, GoogleExchangeOrigin = "public-github", "public-slack", "https://publisher.example"
	expected["github"], expected["slack"] = true, true
	m.records["existing-google"] = &record{Account: Account{ID: "existing-google", Integration: "google", Enabled: true, Status: "connected"}}
	assertAvailability(expected)
	GitHubPublicClientID, SlackPublicClientID, GoogleExchangeOrigin = "", "", ""
	for _, kind := range []string{"github", "slack", "google"} {
		mustHandle(t, m, Request{Action: "oauth_configure", Input: &Input{Integration: kind, OAuthClient: &OAuthClientConfig{ClientID: "operator", ClientSecret: "private", RedirectURL: "https://daemon.example/plugins/oauth/callback"}}})
	}
	assertAvailability(expected)
	if m.unavailableReason("google") != "" {
		t.Fatal("configured Google client unavailable")
	}
	// Incomplete saved configuration must not offer a connection that cannot exchange a code.
	if err := m.vault.Set("oauth-client:google", `{"client_id":"operator","redirect_url":"https://daemon.example/plugins/oauth/callback"}`); err != nil {
		t.Fatal(err)
	}
	if m.unavailableReason("google") == "" {
		t.Fatal("incomplete Google client offered")
	}
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
