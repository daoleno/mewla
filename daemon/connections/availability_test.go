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
	// Google, fal and Image generation are listed only on a daemon that has
	// an account for one of them.
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
	assertAvailability(expected)
	m.records["existing-google"] = &record{Account: Account{ID: "existing-google", Integration: "google", Enabled: true, Status: "connected"}}
	m.records["existing-fal"] = &record{Account: Account{ID: "existing-fal", Integration: "fal", Enabled: true, Status: "connected"}}
	m.records["gone-image"] = &record{Account: Account{ID: "gone-image", Integration: "image", Status: "disconnected"}}
	expected["google"], expected["fal"] = true, true
	assertAvailability(expected)
	GitHubPublicClientID, SlackPublicClientID, GoogleExchangeOrigin = "", "", ""
	for _, kind := range []string{"github", "slack", "google"} {
		mustHandle(t, m, Request{Action: "oauth_configure", Input: &Input{Integration: kind, OAuthClient: &OAuthClientConfig{ClientID: "operator", ClientSecret: "private", RedirectURL: "https://daemon.example/plugins/oauth/callback"}}})
	}
	assertAvailability(expected)
	// Incomplete saved configuration must not offer a connection that cannot exchange a code.
	if err := m.vault.Set("oauth-client:google", `{"client_id":"operator","redirect_url":"https://daemon.example/plugins/oauth/callback"}`); err != nil {
		t.Fatal(err)
	}
	expected["google"] = false
	assertAvailability(expected)
	// Existing linked accounts remain accessible when new sign-in is unavailable.
	if len(mustHandle(t, m, Request{Action: "list"}).Accounts) != 3 {
		t.Fatal("existing account hidden")
	}
}
